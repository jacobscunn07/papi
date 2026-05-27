package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"papi/internal/types"
)

func runClaude(args []string, cwd string, extraEnv []string) (*types.ClaudeJsonOutput, error) {
	cmd := exec.Command("claude", args...)
	cmd.Dir = cwd
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("claude exec: %w\nstderr: %s", err, stderr.String())
	}

	var out types.ClaudeJsonOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("parse claude output: %w\nraw: %s", err, stdout.String())
	}
	return &out, nil
}

// RunHooks executes each script in order, chaining env vars — each script
// receives the accumulated vars emitted by all prior scripts.
func RunHooks(scripts []string, baseDir string, extraEnv []string) ([]string, error) {
	var accumulated []string
	for _, script := range scripts {
		newVars, err := RunHook(script, baseDir, append(extraEnv, accumulated...))
		if err != nil {
			return nil, err
		}
		accumulated = append(accumulated, newVars...)
	}
	return accumulated, nil
}

// resolveHookRunner returns the command to execute the hook script based on its extension.
func resolveHookRunner(scriptPath string) []string {
	switch filepath.Ext(scriptPath) {
	case ".py":
		return []string{"python3"}
	case ".js":
		return []string{"node"}
	case ".ts":
		return []string{"tsx"}
	case ".go":
		return []string{"go", "run"}
	default:
		return []string{"sh"}
	}
}

// RunHook executes a hook script and returns any KEY=VALUE lines from its stdout
// as env vars to inject into subsequent commands.
func RunHook(scriptPath, baseDir string, extraEnv []string) ([]string, error) {
	abs := filepath.Join(baseDir, scriptPath)
	runner := resolveHookRunner(abs)
	args := append(runner[1:], abs)
	cmd := exec.Command(runner[0], args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("hook %q: %w", scriptPath, err)
	}
	var env []string
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "=") {
			env = append(env, line)
		}
	}
	return env, nil
}

// detectInvocation returns true if the transcript contains a slash command invocation.
func detectInvocation(skillName, transcript string) bool {
	lower := strings.ToLower(strings.TrimSpace(transcript))
	slash := "/" + strings.ToLower(skillName)
	return strings.HasPrefix(lower, slash) ||
		strings.Contains(lower, "use "+slash) ||
		strings.Contains(lower, "invoke "+slash) ||
		strings.Contains(lower, "calling "+slash) ||
		strings.Contains(lower, "\n"+slash)
}

// Invocation Phase: runs Claude with only the skill name and description (no skill body).
// Tests whether the description alone is specific enough to trigger the skill.
func runInvocationCheck(scenario types.Scenario, skillName, skillDescription, tmpDir, model string, extraEnv []string) (*types.ClaudeJsonOutput, error) {
	systemPrompt := "You are a command dispatcher. You have one available command:\n\n" +
		"COMMAND: /" + skillName + "\n" +
		"TRIGGER: " + skillDescription + "\n\n" +
		"RULE: If the user's task matches the TRIGGER, respond with ONLY \"/" + skillName + "\" " +
		"and nothing else. If it does not match, answer normally without mentioning the command."

	return runClaude([]string{
		"-p", scenario.Prompt,
		"--model", model,
		"--system-prompt", systemPrompt,
		"--output-format", "json",
		"--no-session-persistence",
	}, tmpDir, extraEnv)
}

// Quality Phase: runs Claude with the full SKILL.md loaded via --plugin-dir.
// Executes the actual task and produces the output to be evaluated. Skipped if the invocation phase did not invoke the skill.
func runQualityCheck(scenario types.Scenario, skillDir, tmpDir, model string, extraEnv []string) (*types.ClaudeJsonOutput, error) {
	return runClaude([]string{
		"-p", scenario.Prompt,
		"--model", model,
		"--plugin-dir", skillDir,
		"--output-format", "json",
		"--no-session-persistence",
		"--dangerously-skip-permissions",
		"--add-dir", tmpDir,
	}, tmpDir, extraEnv)
}

// RunScenario executes all three phases for a scenario and returns the EvalContext.
//
// Phase 1 — Invocation: description-only check to see if the skill would be triggered.
// Phase 2 — Quality:    full skill execution to produce task output (skipped if not invoked).
// Phase 3 — Assessment: handled by the caller (scorer.ScoreScenario) against the returned EvalContext.
func RunScenario(
	scenario types.Scenario,
	skillName, skillDescription, skillContent, skillDir, workDir string,
	scenarioModel, qualityModel string,
	hooks *types.Hooks,
	hooksBaseDir string,
) (ctx types.EvalContext, totalCostUSD float64, durationMs int64, err error) {
	start := time.Now()

	var extraEnv []string
	if hooks != nil && len(hooks.PreScenario) > 0 {
		extraEnv, err = RunHooks(hooks.PreScenario, hooksBaseDir, nil)
		if err != nil {
			return ctx, 0, 0, err
		}
	}

	if err = os.MkdirAll(workDir, 0755); err != nil {
		return ctx, 0, 0, fmt.Errorf("mkdir workdir: %w", err)
	}

	for relPath, content := range scenario.Fixtures {
		dest := filepath.Join(workDir, relPath)
		if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return ctx, 0, 0, err
		}
		if err = os.WriteFile(dest, []byte(content), 0644); err != nil {
			return ctx, 0, 0, err
		}
	}

	invocationOut, err := runInvocationCheck(scenario, skillName, skillDescription, workDir, scenarioModel, extraEnv)
	if err != nil {
		return ctx, 0, 0, fmt.Errorf("invocation: %w", err)
	}

	invoked := detectInvocation(skillName, invocationOut.Result)
	shouldInvoke := scenario.ShouldInvoke == nil || *scenario.ShouldInvoke

	var qualityOut *types.ClaudeJsonOutput
	if shouldInvoke && invoked {
		qualityOut, err = runQualityCheck(scenario, skillDir, workDir, qualityModel, extraEnv)
		if err != nil {
			return ctx, 0, 0, fmt.Errorf("quality: %w", err)
		}
		if hooks != nil && len(hooks.PostQuality) > 0 {
			if _, hookErr := RunHooks(hooks.PostQuality, hooksBaseDir, []string{"WORK_DIR=" + workDir}); hookErr != nil {
				return ctx, 0, 0, fmt.Errorf("post-quality hook: %w", hookErr)
			}
		}
	}

	totalCostUSD = invocationOut.TotalCostUSD
	if qualityOut != nil {
		totalCostUSD += qualityOut.TotalCostUSD
	}

	qualityTranscript := ""
	if qualityOut != nil {
		qualityTranscript = qualityOut.Result
	}

	ctx = types.EvalContext{
		Scenario:             scenario,
		InvocationTranscript: invocationOut.Result,
		InvocationOutput:     invocationOut,
		QualityTranscript:    qualityTranscript,
		QualityOutput:        qualityOut,
		SkillName:            skillName,
		SkillDescription:     skillDescription,
		SkillContent:         skillContent,
		SkillDir:             skillDir,
		WorkDir:              workDir,
		Invoked:              invoked,
	}

	return ctx, totalCostUSD, time.Since(start).Milliseconds(), nil
}
