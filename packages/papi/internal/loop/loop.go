package loop

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"papi/internal/config"
	"papi/internal/evals"
	researchgit "papi/internal/git"
	"papi/internal/runner"
	"papi/internal/scorer"
	"papi/internal/types"

	"gopkg.in/yaml.v3"
)

//go:embed program.md
var defaultProgramMd string

func pct(s float64) string {
	return fmt.Sprintf("%.1f", s*100)
}

func loadScenarios(scenariosDir string, tags []string) ([]types.Scenario, *types.Hooks, string, error) {
	hooksBaseDir := filepath.Dir(scenariosDir)

	var hooks *types.Hooks
	if raw, err := os.ReadFile(filepath.Join(hooksBaseDir, "config.yaml")); err == nil {
		var cfg struct {
			Hooks *types.Hooks `yaml:"hooks"`
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return nil, nil, "", err
		}
		hooks = cfg.Hooks
	}

	entries, err := os.ReadDir(scenariosDir)
	if err != nil {
		return nil, nil, "", err
	}
	var scenarios []types.Scenario
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(scenariosDir, entry.Name()))
		if err != nil {
			return nil, nil, "", err
		}
		var s types.Scenario
		if err := yaml.Unmarshal(raw, &s); err != nil {
			return nil, nil, "", err
		}
		scenarios = append(scenarios, s)
	}

	if len(tags) == 0 {
		return scenarios, hooks, hooksBaseDir, nil
	}
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[t] = true
	}
	var filtered []types.Scenario
	for _, s := range scenarios {
		for _, t := range s.Tags {
			if tagSet[t] {
				filtered = append(filtered, s)
				break
			}
		}
	}
	return filtered, hooks, hooksBaseDir, nil
}

func iterationDirPath(repoRoot, skillName, runTimestamp string, iteration int) string {
	return filepath.Join(repoRoot, ".papi", "skills", skillName, "runs", runTimestamp,
		fmt.Sprintf("iteration-%03d", iteration))
}

type evalSummary struct {
	Score       float64            `json:"score"`
	LLMScore    float64            `json:"llmScore"`
	NonLLMScore float64            `json:"nonLlmScore"`
	Evals       []scaledEvalResult `json:"evals"`
}

func categorizeEvals(results []types.EvalResult) (nonLLM, llm []types.EvalResult) {
	for _, e := range results {
		if e.IsLLMJudge {
			llm = append(llm, e)
		} else {
			nonLLM = append(nonLLM, e)
		}
	}
	return
}

func groupScore(evals []types.EvalResult) float64 {
	if len(evals) == 0 {
		return 0
	}
	var sum float64
	for _, e := range evals {
		sum += e.Score
	}
	return sum / float64(len(evals))
}

type scaledEvalResult struct {
	EvalID     string  `json:"evalId"`
	Name       string  `json:"name"`
	Score      float64 `json:"score"`
	Reasoning  string  `json:"reasoning"`
	Required   bool    `json:"required,omitempty"`
	IsLLMJudge bool    `json:"isLLMJudge,omitempty"`
}

func runAllScenarios(
	scenarios []types.Scenario,
	cfg *types.ResearchConfig,
	evalList []types.Eval,
	iterationDir string,
	hooks *types.Hooks,
	hooksBaseDir string,
) ([]types.ScenarioRunResult, float64, error) {
	desc, content, _, err := config.ReadSkillMd(cfg.SkillDir)
	if err != nil {
		return nil, 0, fmt.Errorf("read SKILL.md: %w", err)
	}

	var totalCost float64
	results := make([]types.ScenarioRunResult, 0, len(scenarios))

	for _, scenario := range scenarios {
		scenarioDir := filepath.Join(iterationDir, scenario.ID)
		fmt.Printf("  ▸ %s  ", scenario.ID)

		ctx, cost, durationMs, err := runner.RunScenario(
			scenario,
			cfg.SkillName, desc, content, cfg.SkillDir, scenarioDir,
			cfg.ScenarioModel, cfg.QualityModel,
			hooks, hooksBaseDir,
		)
		if err != nil {
			return nil, totalCost, fmt.Errorf("scenario %s: %w", scenario.ID, err)
		}
		totalCost += cost

		// Assessment Phase: run all evals against the invocation + quality transcripts.
		evalResults, scenarioScore, err := scorer.ScoreScenario(ctx, evalList, cfg.LLMJudgeWeight, cfg.NonLLMJudgeWeight, hooks, hooksBaseDir)
		if err != nil {
			return nil, totalCost, fmt.Errorf("score scenario %s: %w", scenario.ID, err)
		}

		if hooks != nil && len(hooks.PostScenario) > 0 {
			postEnv := []string{
				"SCENARIO_ID=" + scenario.ID,
				"WORK_DIR=" + scenarioDir,
				fmt.Sprintf("SCENARIO_SCORE=%g", scenarioScore),
			}
			if _, err := runner.RunHooks(hooks.PostScenario, hooksBaseDir, postEnv); err != nil {
				return nil, totalCost, fmt.Errorf("post-scenario hook: %w", err)
			}
		}

		result := types.ScenarioRunResult{
			Scenario:         scenario,
			InvocationOutput: ctx.InvocationOutput,
			QualityOutput:    ctx.QualityOutput,
			Invoked:          ctx.Invoked,
			EvalResults:      evalResults,
			ScenarioScore:    scenarioScore,
			DurationMs:       durationMs,
		}
		results = append(results, result)

		_ = os.MkdirAll(scenarioDir, 0755)
		_ = os.WriteFile(filepath.Join(scenarioDir, "prompt.md"), []byte(scenario.Prompt), 0644)
		_ = os.WriteFile(filepath.Join(scenarioDir, "invocation.md"), []byte(ctx.InvocationTranscript), 0644)
		if ctx.QualityTranscript != "" {
			_ = os.WriteFile(filepath.Join(scenarioDir, "response.md"), []byte(ctx.QualityTranscript), 0644)
		}

		nonLLMEvals, llmEvals := categorizeEvals(evalResults)
		nonLLMScore := groupScore(nonLLMEvals)
		llmScore := groupScore(llmEvals)

		scaled := make([]scaledEvalResult, len(evalResults))
		for i, r := range evalResults {
			scaled[i] = scaledEvalResult{r.EvalID, r.Name, parseFloat(pct(r.Score)), r.Reasoning, r.Required, r.IsLLMJudge}
		}
		summary := evalSummary{
			Score:       parseFloat(pct(scenarioScore)),
			LLMScore:    parseFloat(pct(llmScore)),
			NonLLMScore: parseFloat(pct(nonLLMScore)),
			Evals:       scaled,
		}
		if b, err := json.MarshalIndent(summary, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(scenarioDir, "evals.json"), b, 0644)
		}

		printScenarioResult(result, cfg.LLMJudgeWeight, cfg.NonLLMJudgeWeight)
	}

	return results, totalCost, nil
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func printEvalGroup(evals []types.EvalResult) {
	for i, e := range evals {
		branch := "├─"
		if i == len(evals)-1 {
			branch = "└─"
		}
		name := e.Name
		if e.Required {
			name += " [required]"
		}
		if e.IsLLMJudge {
			name += " [llm]"
		}
		fmt.Printf("    %s %-36s %6.1f   %q\n", branch, name, e.Score*100, e.Reasoning)
	}
}

func printScenarioResult(result types.ScenarioRunResult, llmWeight, nonLLMWeight float64) {
	invokedLabel := "INVOKED"
	if !result.Invoked {
		shouldInvoke := result.Scenario.ShouldInvoke == nil || *result.Scenario.ShouldInvoke
		if shouldInvoke {
			invokedLabel = "NOT INVOKED"
		} else {
			invokedLabel = "NOT INVOKED ✓"
		}
	}
	fmt.Printf("[%s]  score: %s\n", invokedLabel, pct(result.ScenarioScore))

	nonLLMEvals, llmEvals := categorizeEvals(result.EvalResults)
	nonLLMScore := groupScore(nonLLMEvals)
	llmScore := groupScore(llmEvals)

	if len(nonLLMEvals) > 0 {
		fmt.Printf("    ── non-llm  %.0f%% ──────────────────────────  %5.1f\n", nonLLMWeight*100, nonLLMScore*100)
		printEvalGroup(nonLLMEvals)
		fmt.Println()
	}
	if len(llmEvals) > 0 {
		fmt.Printf("    ── llm  %.0f%% ─────────────────────────────  %5.1f\n", llmWeight*100, llmScore*100)
		printEvalGroup(llmEvals)
		fmt.Println()
	}
}

func printScenarioBreakdown(results []types.ScenarioRunResult) {
	for i, r := range results {
		branch := "├─"
		if i == len(results)-1 {
			branch = "└─"
		}
		fmt.Printf("    %s %-32s %6.1f\n", branch, r.Scenario.ID, r.ScenarioScore*100)
	}
}

func buildResearchPrompt(currentSkillMd string, prevResults []types.ScenarioRunResult, prevScore float64, iteration int) string {
	var sb strings.Builder
	for _, r := range prevResults {
		var evalLines []string
		for _, e := range r.EvalResults {
			evalLines = append(evalLines, fmt.Sprintf("    %s: %s — %s", e.EvalID, pct(e.Score), e.Reasoning))
		}
		sb.WriteString(fmt.Sprintf("  Scenario %q:\n    invoked=%v | score=%s\n%s\n\n",
			r.Scenario.ID, r.Invoked, pct(r.ScenarioScore), strings.Join(evalLines, "\n")))
	}

	return fmt.Sprintf("## Iteration %d — Research Agent Input\n\n"+
		"### Current aggregate score: %s\n\n"+
		"### Current SKILL.md:\n```markdown\n%s\n```\n\n"+
		"### Previous scenario results:\n%s\n"+
		"### Your task:\n"+
		"Propose an improved version of SKILL.md that will score higher.\n\n"+
		"Output ONLY valid JSON (no markdown fences, no preamble) in this exact format:\n"+
		`{"description": "<one sentence: what you are changing and why>", "skillMd": "<complete SKILL.md content starting with --->"}`,
		iteration, pct(prevScore), currentSkillMd, sb.String())
}

var frontmatterPrefixRe = regexp.MustCompile(`(?s)^[\s\S]*?(---\n)`)

func stripPreamble(s string) string {
	return frontmatterPrefixRe.ReplaceAllString(s, "$1")
}

func saveIterationResults(iterationDir string, results []types.ScenarioRunResult, score float64) error {
	type summary struct {
		Score     float64                   `json:"score"`
		Scenarios []types.ScenarioRunResult `json:"scenarios"`
	}
	b, err := json.MarshalIndent(summary{Score: parseFloat(pct(score)), Scenarios: results}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(iterationDir, "results.json"), b, 0644)
}

func callResearchAgent(agentPrompt, systemPrompt, model string) (description, skillMd string, cost float64, err error) {
	cmd := exec.Command("claude",
		"-p", agentPrompt,
		"--model", model,
		"--system-prompt", systemPrompt,
		"--output-format", "json",
		"--no-session-persistence",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return "", "", 0, fmt.Errorf("research agent exec: %w\n%s", err, stderr.String())
	}
	var out types.ClaudeJsonOutput
	if err = json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return "", "", 0, fmt.Errorf("parse research agent output: %w", err)
	}
	var parsed struct {
		Description string `json:"description"`
		SkillMd     string `json:"skillMd"`
	}
	if jsonErr := json.Unmarshal([]byte(out.Result), &parsed); jsonErr == nil && parsed.SkillMd != "" {
		return parsed.Description, parsed.SkillMd, out.TotalCostUSD, nil
	}
	return "", stripPreamble(out.Result), out.TotalCostUSD, nil
}

func purgeOldRuns(runsDir string, maxRuns int) error {
	if maxRuns <= 0 {
		return nil
	}
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var dirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e)
		}
	}
	if len(dirs) <= maxRuns {
		return nil
	}
	for _, d := range dirs[:len(dirs)-maxRuns] {
		path := filepath.Join(runsDir, d.Name())
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		fmt.Printf("Purged old run: %s\n", d.Name())
	}
	return nil
}

func acquireLock(repoRoot, skillName string) (func(), error) {
	lockPath := filepath.Join(repoRoot, ".papi", "skills", skillName, "lock")

	if data, err := os.ReadFile(lockPath); err == nil {
		var lf struct {
			PID       int    `json:"pid"`
			StartedAt string `json:"startedAt"`
		}
		if json.Unmarshal(data, &lf) == nil {
			proc, procErr := os.FindProcess(lf.PID)
			alive := procErr == nil && proc.Signal(syscall.Signal(0)) == nil
			if alive {
				return nil, fmt.Errorf("skill %q locked by PID %d (started %s); another experiment is already running", skillName, lf.PID, lf.StartedAt)
			}
			fmt.Printf("Removing stale lock from PID %d\n", lf.PID)
			_ = os.Remove(lockPath)
		}
	}

	type lockFile struct {
		PID       int    `json:"pid"`
		StartedAt string `json:"startedAt"`
	}
	lf := lockFile{PID: os.Getpid(), StartedAt: time.Now().UTC().Format(time.RFC3339)}
	data, _ := json.MarshalIndent(lf, "", "  ")
	_ = os.MkdirAll(filepath.Dir(lockPath), 0755)
	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	return func() { _ = os.Remove(lockPath) }, nil
}

// Run executes the full research loop.
func Run(cfg *types.ResearchConfig, repoRoot string) error {
	bar := strings.Repeat("━", 55)
	fmt.Printf("\n%s\n", bar)
	fmt.Printf("Skill: %s    Max: %d iterations    Budget: $%.2f\n", cfg.SkillName, cfg.MaxIterations, cfg.MaxBudgetUSD)
	fmt.Printf("%s\n", bar)

	release, err := acquireLock(repoRoot, cfg.SkillName)
	if err != nil {
		return err
	}
	defer release()

	scenarios, hooks, hooksBaseDir, err := loadScenarios(cfg.ScenariosDir, cfg.Tags)
	if err != nil {
		return fmt.Errorf("load scenarios: %w", err)
	}
	ids := make([]string, len(scenarios))
	for i, s := range scenarios {
		ids[i] = s.ID
	}
	fmt.Printf("Scenarios (%d): %s\n", len(scenarios), strings.Join(ids, ", "))

	evalList := evals.NewRegistry(cfg.CustomEvalsDir)
	git := researchgit.New(repoRoot)
	runTimestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	var totalCost float64

	if hooks != nil && len(hooks.PreRun) > 0 {
		preRunEnv := []string{
			"SKILL_NAME=" + cfg.SkillName,
			"RUN_TIMESTAMP=" + runTimestamp,
		}
		if _, err := runner.RunHooks(hooks.PreRun, hooksBaseDir, preRunEnv); err != nil {
			return fmt.Errorf("pre-run hook: %w", err)
		}
	}

	// Baseline
	fmt.Println("\n══ BASELINE ══")
	baselineDir := iterationDirPath(repoRoot, cfg.SkillName, runTimestamp, 0)
	_ = os.MkdirAll(baselineDir, 0755)
	baselineResults, baselineCost, err := runAllScenarios(scenarios, cfg, evalList, baselineDir, hooks, hooksBaseDir)
	if err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	totalCost += baselineCost
	bestScore := scorer.AggregateScore(baselineResults)
	fmt.Printf("  Aggregate: %s  |  Cost: $%.4f\n", pct(bestScore), baselineCost)
	printScenarioBreakdown(baselineResults)

	if err := saveIterationResults(baselineDir, baselineResults, bestScore); err != nil {
		return err
	}

	var bestSha string
	if !cfg.DryRun {
		skillMdPath := filepath.Join(cfg.SkillDir, "SKILL.md")
		bestSha, err = git.CommitSkill(skillMdPath,
			fmt.Sprintf("research(%s): baseline score=%s", cfg.SkillName, pct(bestScore)))
		if err != nil {
			return fmt.Errorf("baseline commit: %w", err)
		}
	}

	programMd := defaultProgramMd
	if global, err := os.ReadFile(filepath.Join(repoRoot, ".papi", "program.md")); err == nil {
		programMd = string(global)
	}
	if skillSpecific, err := os.ReadFile(filepath.Join(repoRoot, ".papi", "skills", cfg.SkillName, "program.md")); err == nil {
		programMd = string(skillSpecific)
	}

	prevResults := baselineResults

	for iter := 1; iter <= cfg.MaxIterations; iter++ {
		if totalCost >= cfg.MaxBudgetUSD {
			fmt.Printf("\nBudget exhausted ($%.2f). Stopping.\n", totalCost)
			break
		}

		fmt.Printf("\n══ ITERATION %d/%d  (best: %s) ══\n", iter, cfg.MaxIterations, pct(bestScore))

		if hooks != nil && len(hooks.PreIteration) > 0 {
			preIterEnv := []string{
				"SKILL_NAME=" + cfg.SkillName,
				fmt.Sprintf("ITERATION=%d", iter),
				fmt.Sprintf("BEST_SCORE=%g", bestScore),
			}
			if _, err := runner.RunHooks(hooks.PreIteration, hooksBaseDir, preIterEnv); err != nil {
				return fmt.Errorf("pre-iteration hook: %w", err)
			}
		}

		currentSkillMdBytes, err := os.ReadFile(filepath.Join(cfg.SkillDir, "SKILL.md"))
		if err != nil {
			return err
		}
		agentPrompt := buildResearchPrompt(string(currentSkillMdBytes), prevResults, bestScore, iter)

		fmt.Print("  Calling research agent... ")
		description, proposedSkillMd, agentCost, err := callResearchAgent(agentPrompt, programMd, cfg.ResearchModel)
		if err != nil {
			return fmt.Errorf("research agent iter %d: %w", iter, err)
		}
		totalCost += agentCost
		fmt.Printf("$%.4f\n", agentCost)
		if description != "" {
			fmt.Printf("  Experiment: %q\n", description)
		}

		if !cfg.DryRun {
			if err := os.WriteFile(filepath.Join(cfg.SkillDir, "SKILL.md"), []byte(proposedSkillMd), 0644); err != nil {
				return err
			}
		}

		iterDir := iterationDirPath(repoRoot, cfg.SkillName, runTimestamp, iter)
		_ = os.MkdirAll(iterDir, 0755)
		iterResults, iterCost, err := runAllScenarios(scenarios, cfg, evalList, iterDir, hooks, hooksBaseDir)
		if err != nil {
			return fmt.Errorf("iter %d scenarios: %w", iter, err)
		}
		totalCost += iterCost
		iterScore := scorer.AggregateScore(iterResults)
		delta := iterScore - bestScore
		deltaDisplay := pct(delta)
		if delta > 0 {
			deltaDisplay = "+" + deltaDisplay
		}
		fmt.Printf("  Aggregate: %s  (Δ %s)  |  Cost: $%.4f\n", pct(iterScore), deltaDisplay, iterCost)
		printScenarioBreakdown(iterResults)

		if err := saveIterationResults(iterDir, iterResults, iterScore); err != nil {
			return err
		}

		if !cfg.DryRun {
			skillMdPath := filepath.Join(cfg.SkillDir, "SKILL.md")
			if iterScore > bestScore {
				bestSha, err = git.CommitSkill(skillMdPath,
					fmt.Sprintf("research(%s): iter %03d score=%s [+%s]",
						cfg.SkillName, iter, pct(iterScore), pct(delta)))
				if err != nil {
					return err
				}
				bestScore = iterScore
				fmt.Printf("  → IMPROVED +%s\n", pct(delta))
			} else {
				if bestSha != "" {
					if err := git.RevertSkillFile(skillMdPath, bestSha); err != nil {
						return err
					}
				}
				fmt.Printf("  → REVERTED to best (%s)\n", pct(bestScore))
			}
		}

		prevResults = iterResults

		if hooks != nil && len(hooks.PostIteration) > 0 {
			postIterEnv := []string{
				"SKILL_NAME=" + cfg.SkillName,
				fmt.Sprintf("ITERATION=%d", iter),
				fmt.Sprintf("ITER_SCORE=%g", iterScore),
				fmt.Sprintf("BEST_SCORE=%g", bestScore),
				fmt.Sprintf("IMPROVED=%v", delta > 0),
			}
			if _, err := runner.RunHooks(hooks.PostIteration, hooksBaseDir, postIterEnv); err != nil {
				return fmt.Errorf("post-iteration hook: %w", err)
			}
		}
	}

	fmt.Printf("\n%s\n", bar)
	fmt.Printf("Done. Best score: %s | Total cost: $%.4f\n", pct(bestScore), totalCost)
	if !cfg.DryRun && bestSha != "" {
		tag := fmt.Sprintf("research/%s/%s-best-%s", cfg.SkillName, runTimestamp, pct(bestScore))
		if err := git.CreateTag(tag); err != nil {
			return err
		}
		fmt.Printf("Tagged: %s\n", tag)
	}
	fmt.Printf("%s\n", bar)

	runsDir := filepath.Join(repoRoot, ".papi", "skills", cfg.SkillName, "runs")
	if err := purgeOldRuns(runsDir, cfg.MaxRuns); err != nil {
		return fmt.Errorf("purge old runs: %w", err)
	}

	if hooks != nil && len(hooks.PostRun) > 0 {
		postRunEnv := []string{
			"SKILL_NAME=" + cfg.SkillName,
			"RUN_TIMESTAMP=" + runTimestamp,
			fmt.Sprintf("BEST_SCORE=%g", bestScore),
			fmt.Sprintf("TOTAL_COST_USD=%g", totalCost),
		}
		if _, err := runner.RunHooks(hooks.PostRun, hooksBaseDir, postRunEnv); err != nil {
			return fmt.Errorf("post-run hook: %w", err)
		}
	}

	return nil
}
