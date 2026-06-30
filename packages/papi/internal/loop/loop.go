package loop

import (
	"bytes"
	"context"
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
	"papi/internal/progress"
	"papi/internal/runner"
	"papi/internal/scorer"
	"papi/internal/store"
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

func runAllScenarios(
	ctx context.Context,
	iter int,
	scenarios []types.Scenario,
	cfg *types.ResearchConfig,
	evalList []types.Eval,
	iterationDir string,
	hooks *types.Hooks,
	hooksBaseDir string,
	rep progress.Reporter,
	stream bool,
) ([]types.ScenarioRunResult, float64, error) {
	desc, content, _, err := config.ReadSkillMd(cfg.SkillDir)
	if err != nil {
		return nil, 0, fmt.Errorf("read SKILL.md: %w", err)
	}

	var totalCost float64
	results := make([]types.ScenarioRunResult, 0, len(scenarios))

	for _, scenario := range scenarios {
		if ctx.Err() != nil {
			return results, totalCost, ctx.Err()
		}
		scenarioDir := filepath.Join(iterationDir, scenario.ID)
		rep.Emit(progress.ScenarioStarted{Iter: iter, ID: scenario.ID})

		// Scope logs emitted while this scenario runs (and scores) to it, so the
		// TUI can filter the log panel to the selected scenario node.
		scenRep := progress.WithScope(rep, iter, scenario.ID, "")

		var sink runner.StreamSink
		if stream {
			sid := scenario.ID
			sink = func(phase progress.Phase, text string) {
				rep.Emit(progress.StreamChunk{Iter: iter, ID: sid, Phase: phase, Text: text})
			}
		}

		evalCtx, cost, durationMs, err := runner.RunScenario(
			ctx,
			scenario,
			cfg.SkillName, desc, content, cfg.SkillDir, scenarioDir,
			cfg.ScenarioModel, cfg.QualityModel,
			hooks, hooksBaseDir,
			sink,
			scenRep,
		)
		if err != nil {
			return results, totalCost, fmt.Errorf("scenario %s: %w", scenario.ID, err)
		}
		totalCost += cost

		// Assessment Phase: run all evals against the invocation + quality transcripts.
		rep.Emit(progress.PhaseChanged{Iter: iter, ID: scenario.ID, Phase: progress.PhaseScoring})
		evalResults, scenarioScore, err := scorer.ScoreScenario(iter, evalCtx, evalList, cfg.LLMJudgeWeight, cfg.NonLLMJudgeWeight, hooks, hooksBaseDir, scenRep)
		if err != nil {
			return results, totalCost, fmt.Errorf("score scenario %s: %w", scenario.ID, err)
		}
		for _, er := range evalResults {
			rep.Emit(progress.EvalDone{Iter: iter, ScenarioID: scenario.ID, Eval: er})
		}

		if hooks != nil && len(hooks.PostScenario) > 0 {
			postEnv := []string{
				"SCENARIO_ID=" + scenario.ID,
				"WORK_DIR=" + scenarioDir,
				fmt.Sprintf("SCENARIO_SCORE=%g", scenarioScore),
			}
			if _, err := runner.RunHooks(hooks.PostScenario, hooksBaseDir, postEnv, rep); err != nil {
				return results, totalCost, fmt.Errorf("post-scenario hook: %w", err)
			}
		}

		result := types.ScenarioRunResult{
			Scenario:             scenario,
			InvocationOutput:     evalCtx.InvocationOutput,
			QualityOutput:        evalCtx.QualityOutput,
			Invoked:              evalCtx.Invoked,
			EvalResults:          evalResults,
			ScenarioScore:        scenarioScore,
			DurationMs:           durationMs,
			InvocationTranscript: evalCtx.InvocationTranscript,
			QualityTranscript:    evalCtx.QualityTranscript,
		}
		results = append(results, result)

		rep.Emit(progress.ScenarioDone{Iter: iter, Result: result})
	}

	return results, totalCost, nil
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

// resumable reports whether a run can still be continued under cfg: it has not
// reached its natural end yet (more iterations and budget remain).
func resumable(st types.RunState, cfg *types.ResearchConfig) bool {
	return st.LastCompletedIteration < cfg.MaxIterations && st.TotalCost < cfg.MaxBudgetUSD
}

// findResumableRun returns the newest run for the skill that can be resumed under
// cfg. When cfg.ResumeTimestamp is set, only that run is considered.
func findResumableRun(st *store.Store, cfg *types.ResearchConfig) (types.RunState, error) {
	rs, err := st.RunStates(cfg.SkillName)
	if err != nil {
		return types.RunState{}, err
	}
	// RunStates returns oldest-first; scan newest-first for the first eligible run.
	for i := len(rs) - 1; i >= 0; i-- {
		if cfg.ResumeTimestamp != "" && rs[i].Timestamp != cfg.ResumeTimestamp {
			continue
		}
		if resumable(rs[i], cfg) {
			return rs[i], nil
		}
	}
	if cfg.ResumeTimestamp != "" {
		return types.RunState{}, fmt.Errorf("run %s for skill %q is not resumable (no state, already complete, or out of budget/iterations)", cfg.ResumeTimestamp, cfg.SkillName)
	}
	return types.RunState{}, fmt.Errorf("no resumable run found for skill %q", cfg.SkillName)
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

// purgeOldRuns drops all but the newest maxRuns runs from the store and removes
// their on-disk work-dir trees (the generated files that stay on the filesystem).
func purgeOldRuns(st *store.Store, repoRoot, skill string, maxRuns int, rep progress.Reporter) error {
	purged, err := st.PurgeOldRuns(skill, maxRuns)
	if err != nil {
		return err
	}
	for _, ts := range purged {
		dir := filepath.Join(repoRoot, ".papi", "skills", skill, "runs", ts)
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		rep.Emit(progress.LogLine{Text: "Purged old run: " + ts})
	}
	return nil
}

func acquireLock(repoRoot, skillName string, rep progress.Reporter) (func(), error) {
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
			rep.Emit(progress.LogLine{Text: fmt.Sprintf("Removing stale lock from PID %d", lf.PID)})
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

// snapshotSkillMd returns the SKILL.md currently on disk so the exact version that
// ran (accepted or rejected) can be persisted with the iteration for diffing.
func snapshotSkillMd(skillDir string) string {
	b, _ := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	return string(b)
}

// Run executes the full research loop, emitting progress events to rep. When
// stream is true, Claude output is streamed live via stream-json. Cancelling ctx
// stops the loop gracefully at the next scenario/iteration boundary.
func Run(ctx context.Context, cfg *types.ResearchConfig, repoRoot string, st *store.Store, rep progress.Reporter, stream bool) error {
	runStart := time.Now()

	// Resolve the run to write into: a fresh timestamp, or the latest resumable run.
	var runTimestamp string
	var resumeState types.RunState
	resuming := cfg.Resume
	if resuming {
		rs, err := findResumableRun(st, cfg)
		if err != nil {
			return err
		}
		resumeState = rs
		runTimestamp = rs.Timestamp
	} else {
		runTimestamp = fmt.Sprintf("%d", runStart.UnixMilli())
	}
	skillMdPath := filepath.Join(cfg.SkillDir, "SKILL.md")

	// Persist LogLine events to the store so past runs can replay their output.
	// The tee wraps the raw reporter; WithScope layers on top so each line it sees
	// already carries its final iteration/scenario/eval scope.
	logTee := progress.NewLogTee(rep, st, cfg.SkillName, runTimestamp)
	defer logTee.Close()
	// Scope all run-level logs to iter -1 so they read as run-level (not iteration
	// 0). Per-iteration and per-scenario scopes are derived from this as we descend.
	rep = progress.WithScope(logTee, -1, "", "")

	release, err := acquireLock(repoRoot, cfg.SkillName, rep)
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

	evalList := evals.NewRegistry(cfg.CustomEvalsDir)
	git := researchgit.New(repoRoot)
	var totalCost float64

	resumeFrom := 0
	if resuming {
		resumeFrom = resumeState.LastCompletedIteration + 1
	}
	rep.Emit(progress.RunStarted{
		Skill:         cfg.SkillName,
		Timestamp:     runTimestamp,
		MaxIterations: cfg.MaxIterations,
		Budget:        cfg.MaxBudgetUSD,
		ScenarioIDs:   ids,
		ResumeFrom:    resumeFrom,
	})

	evalIDs := make([]string, len(evalList))
	for i, e := range evalList {
		evalIDs[i] = e.ID()
	}
	rep.Emit(progress.LogLine{Text: "Loaded evals: " + strings.Join(evalIDs, ", ")})

	if hooks != nil && len(hooks.PreRun) > 0 {
		preRunEnv := []string{
			"SKILL_NAME=" + cfg.SkillName,
			"RUN_TIMESTAMP=" + runTimestamp,
		}
		if _, err := runner.RunHooks(hooks.PreRun, hooksBaseDir, preRunEnv, rep); err != nil {
			return fmt.Errorf("pre-run hook: %w", err)
		}
	}

	// finalize tags the best SHA, purges old runs, runs the post-run hook and
	// emits RunDone. Shared by the normal and the cancelled (graceful stop) paths.
	// completed marks the run's checkpoint Done: true on a natural end (so resume
	// skips it), false on a graceful stop (so it stays resumable).
	finalize := func(bestScore float64, bestSha string, completed bool) error {
		if rs, ok, err := st.GetRunState(cfg.SkillName, runTimestamp); err == nil && ok {
			rs.Done = completed
			rs.BestScore = bestScore * 100
			rs.BestSha = bestSha
			rs.TotalCost = totalCost
			_ = st.UpsertRun(rs)
		}

		tag := ""
		if !cfg.DryRun && bestSha != "" {
			tag = fmt.Sprintf("research/%s/%s-best-%s", cfg.SkillName, runTimestamp, pct(bestScore))
			if err := git.CreateTag(tag); err != nil {
				return err
			}
		}

		if err := purgeOldRuns(st, repoRoot, cfg.SkillName, cfg.MaxRuns, rep); err != nil {
			return fmt.Errorf("purge old runs: %w", err)
		}

		if hooks != nil && len(hooks.PostRun) > 0 {
			postRunEnv := []string{
				"SKILL_NAME=" + cfg.SkillName,
				"RUN_TIMESTAMP=" + runTimestamp,
				fmt.Sprintf("BEST_SCORE=%g", bestScore),
				fmt.Sprintf("TOTAL_COST_USD=%g", totalCost),
			}
			if _, err := runner.RunHooks(hooks.PostRun, hooksBaseDir, postRunEnv, rep); err != nil {
				return fmt.Errorf("post-run hook: %w", err)
			}
		}

		rep.Emit(progress.RunDone{Best: bestScore, Cost: totalCost, DurationMs: time.Since(runStart).Milliseconds(), Tag: tag})
		return nil
	}

	var bestScore float64
	var bestSha string
	var prevResults []types.ScenarioRunResult

	if resuming {
		bestScore = resumeState.BestScore / 100.0
		bestSha = resumeState.BestSha
		totalCost = resumeState.TotalCost
		// Restore the working SKILL.md to the best committed version so the research
		// agent continues from the best skill, not a half-finished iteration.
		if !cfg.DryRun && bestSha != "" {
			if err := git.RevertSkillFile(skillMdPath, bestSha); err != nil {
				return err
			}
		}
		// Rebuild prevResults from the last completed iteration for the research prompt.
		if pr, err := st.ScenarioResults(cfg.SkillName, runTimestamp, resumeState.LastCompletedIteration); err == nil {
			prevResults = pr
		}
		rep.Emit(progress.LogLine{Text: fmt.Sprintf("Resuming run %s from iteration %d (best %s, spent $%.2f)",
			runTimestamp, resumeFrom, pct(bestScore), totalCost)})
	} else {
		// Baseline (iteration 0)
		iterStart := time.Now()
		rep.Emit(progress.IterationStarted{Iter: 0, Best: 0})
		baselineDir := iterationDirPath(repoRoot, cfg.SkillName, runTimestamp, 0)
		_ = os.MkdirAll(baselineDir, 0755)
		baselineSkillMd := snapshotSkillMd(cfg.SkillDir)
		baselineResults, baselineCost, err := runAllScenarios(ctx, 0, scenarios, cfg, evalList, baselineDir, hooks, hooksBaseDir, progress.WithScope(rep, 0, "", ""), stream)
		totalCost += baselineCost
		if err != nil {
			if ctx.Err() != nil {
				score := scorer.AggregateScore(baselineResults)
				_ = st.SaveIteration(cfg.SkillName, runTimestamp, 0, score, time.Since(iterStart).Milliseconds(), "", baselineSkillMd, baselineResults)
				rep.Emit(progress.LogLine{Text: "Stopped."})
				return finalize(score, "", false)
			}
			return fmt.Errorf("baseline: %w", err)
		}
		bestScore = scorer.AggregateScore(baselineResults)
		baselineMs := time.Since(iterStart).Milliseconds()
		rep.Emit(progress.IterationDone{Iter: 0, Score: bestScore, Cost: baselineCost, DurationMs: baselineMs, Results: baselineResults, SkillMd: baselineSkillMd})

		if err := st.SaveIteration(cfg.SkillName, runTimestamp, 0, bestScore, baselineMs, "", baselineSkillMd, baselineResults); err != nil {
			return err
		}

		if !cfg.DryRun {
			bestSha, err = git.CommitSkill(skillMdPath,
				fmt.Sprintf("research(%s): baseline score=%s", cfg.SkillName, pct(bestScore)))
			if err != nil {
				return fmt.Errorf("baseline commit: %w", err)
			}
		}

		if err := st.UpsertRun(types.RunState{
			Skill: cfg.SkillName, Timestamp: runTimestamp,
			BestScore: bestScore * 100, BestSha: bestSha,
			LastCompletedIteration: 0, TotalCost: totalCost,
			MaxIterations: cfg.MaxIterations, Budget: cfg.MaxBudgetUSD,
		}); err != nil {
			return err
		}

		prevResults = baselineResults
	}

	programMd := defaultProgramMd
	if global, err := os.ReadFile(filepath.Join(repoRoot, ".papi", "program.md")); err == nil {
		programMd = string(global)
	}
	if skillSpecific, err := os.ReadFile(filepath.Join(repoRoot, ".papi", "skills", cfg.SkillName, "program.md")); err == nil {
		programMd = string(skillSpecific)
	}

	startIter := 1
	if resuming {
		startIter = resumeFrom
	}
	for iter := startIter; iter <= cfg.MaxIterations; iter++ {
		if ctx.Err() != nil {
			rep.Emit(progress.LogLine{Text: "Stopped."})
			break
		}
		if totalCost >= cfg.MaxBudgetUSD {
			rep.Emit(progress.LogLine{Text: fmt.Sprintf("Budget exhausted ($%.2f). Stopping.", totalCost)})
			break
		}

		iterStart := time.Now()
		rep.Emit(progress.IterationStarted{Iter: iter, Best: bestScore})
		iterRep := progress.WithScope(rep, iter, "", "")

		if hooks != nil && len(hooks.PreIteration) > 0 {
			preIterEnv := []string{
				"SKILL_NAME=" + cfg.SkillName,
				fmt.Sprintf("ITERATION=%d", iter),
				fmt.Sprintf("BEST_SCORE=%g", bestScore),
			}
			if _, err := runner.RunHooks(hooks.PreIteration, hooksBaseDir, preIterEnv, rep); err != nil {
				return fmt.Errorf("pre-iteration hook: %w", err)
			}
		}

		currentSkillMdBytes, err := os.ReadFile(filepath.Join(cfg.SkillDir, "SKILL.md"))
		if err != nil {
			return err
		}
		agentPrompt := buildResearchPrompt(string(currentSkillMdBytes), prevResults, bestScore, iter)

		description, proposedSkillMd, agentCost, err := callResearchAgent(agentPrompt, programMd, cfg.ResearchModel)
		if err != nil {
			return fmt.Errorf("research agent iter %d: %w", iter, err)
		}
		totalCost += agentCost
		rep.Emit(progress.ResearchAgentDone{Iter: iter, Description: description, Cost: agentCost, SkillMd: proposedSkillMd})

		if !cfg.DryRun {
			if err := os.WriteFile(filepath.Join(cfg.SkillDir, "SKILL.md"), []byte(proposedSkillMd), 0644); err != nil {
				return err
			}
		}

		iterDir := iterationDirPath(repoRoot, cfg.SkillName, runTimestamp, iter)
		_ = os.MkdirAll(iterDir, 0755)
		// The snapshot for this iteration is the proposal itself — what the agent
		// changed and (in non-dry-run) what ran. Using the proposal directly keeps
		// live, persisted, and dry-run views consistent.
		iterSkillMd := proposedSkillMd
		iterResults, iterCost, err := runAllScenarios(ctx, iter, scenarios, cfg, evalList, iterDir, hooks, hooksBaseDir, iterRep, stream)
		totalCost += iterCost
		if err != nil {
			if ctx.Err() != nil {
				_ = st.SaveIteration(cfg.SkillName, runTimestamp, iter, scorer.AggregateScore(iterResults), time.Since(iterStart).Milliseconds(), description, iterSkillMd, iterResults)
				// Restore best SKILL.md so a half-finished iteration is not left on disk.
				if !cfg.DryRun && bestSha != "" {
					_ = git.RevertSkillFile(filepath.Join(cfg.SkillDir, "SKILL.md"), bestSha)
				}
				rep.Emit(progress.LogLine{Text: "Stopped."})
				return finalize(bestScore, bestSha, false)
			}
			return fmt.Errorf("iter %d scenarios: %w", iter, err)
		}
		iterScore := scorer.AggregateScore(iterResults)
		delta := iterScore - bestScore
		improved := iterScore > bestScore
		iterMs := time.Since(iterStart).Milliseconds()

		if err := st.SaveIteration(cfg.SkillName, runTimestamp, iter, iterScore, iterMs, description, iterSkillMd, iterResults); err != nil {
			return err
		}

		rep.Emit(progress.IterationDone{Iter: iter, Score: iterScore, Delta: delta, Improved: improved, Cost: iterCost, DurationMs: iterMs, Results: iterResults, SkillMd: iterSkillMd})

		if !cfg.DryRun {
			if improved {
				bestSha, err = git.CommitSkill(skillMdPath,
					fmt.Sprintf("research(%s): iter %03d score=%s [+%s]",
						cfg.SkillName, iter, pct(iterScore), pct(delta)))
				if err != nil {
					return err
				}
				bestScore = iterScore
				iterRep.Emit(progress.LogLine{Text: fmt.Sprintf("  → IMPROVED +%s", pct(delta))})
			} else {
				if bestSha != "" {
					if err := git.RevertSkillFile(skillMdPath, bestSha); err != nil {
						return err
					}
				}
				iterRep.Emit(progress.LogLine{Text: fmt.Sprintf("  → REVERTED to best (%s)", pct(bestScore))})
			}
		}

		// Checkpoint the run so it can be resumed from the next iteration.
		if err := st.UpsertRun(types.RunState{
			Skill: cfg.SkillName, Timestamp: runTimestamp,
			BestScore: bestScore * 100, BestSha: bestSha,
			LastCompletedIteration: iter, TotalCost: totalCost,
			MaxIterations: cfg.MaxIterations, Budget: cfg.MaxBudgetUSD,
		}); err != nil {
			return err
		}

		prevResults = iterResults

		if hooks != nil && len(hooks.PostIteration) > 0 {
			postIterEnv := []string{
				"SKILL_NAME=" + cfg.SkillName,
				fmt.Sprintf("ITERATION=%d", iter),
				fmt.Sprintf("ITER_SCORE=%g", iterScore),
				fmt.Sprintf("BEST_SCORE=%g", bestScore),
				fmt.Sprintf("IMPROVED=%v", improved),
			}
			if _, err := runner.RunHooks(hooks.PostIteration, hooksBaseDir, postIterEnv, rep); err != nil {
				return fmt.Errorf("post-iteration hook: %w", err)
			}
		}
	}

	return finalize(bestScore, bestSha, true)
}
