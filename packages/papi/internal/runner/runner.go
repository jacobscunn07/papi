package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"papi/internal/progress"
	"papi/internal/types"
)

// StreamSink receives chunks of streamed Claude output for a given phase. When a
// non-nil sink is provided, claude is run with --output-format stream-json and
// assistant text is forwarded as it arrives. When nil, claude is run buffered
// (--output-format json) with no streaming.
type StreamSink func(phase progress.Phase, text string)

func runClaude(ctx context.Context, args []string, cwd string, extraEnv []string, phase progress.Phase, sink StreamSink) (*types.ClaudeJsonOutput, error) {
	if sink == nil {
		return runClaudeBuffered(ctx, args, cwd, extraEnv)
	}
	return runClaudeStreaming(ctx, args, cwd, extraEnv, phase, sink)
}

func runClaudeBuffered(ctx context.Context, args []string, cwd string, extraEnv []string) (*types.ClaudeJsonOutput, error) {
	full := append(append([]string{}, args...), "--output-format", "json")
	cmd := exec.CommandContext(ctx, "claude", full...)
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

func runClaudeStreaming(ctx context.Context, args []string, cwd string, extraEnv []string, phase progress.Phase, sink StreamSink) (*types.ClaudeJsonOutput, error) {
	full := append(append([]string{}, args...), "--output-format", "stream-json", "--verbose")
	cmd := exec.CommandContext(ctx, "claude", full...)
	cmd.Dir = cwd
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var final *types.ClaudeJsonOutput
	reader := bufio.NewReader(stdoutPipe)
	for {
		line, rerr := reader.ReadString('\n')
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			handleStreamLine(trimmed, phase, sink, &final)
		}
		if rerr != nil {
			break
		}
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("claude exec: %w\nstderr: %s", err, stderr.String())
	}
	if final == nil {
		return nil, fmt.Errorf("claude stream ended without a result event\nstderr: %s", stderr.String())
	}
	return final, nil
}

// handleStreamLine parses one NDJSON line from claude's stream-json output,
// forwarding assistant text to the sink and capturing the terminal result event.
func handleStreamLine(line string, phase progress.Phase, sink StreamSink, final **types.ClaudeJsonOutput) {
	var env struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
	}
	if json.Unmarshal([]byte(line), &env) != nil {
		return
	}
	switch env.Type {
	case "assistant":
		var msg struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if len(env.Message) > 0 && json.Unmarshal(env.Message, &msg) == nil && sink != nil {
			for _, c := range msg.Content {
				if c.Type == "text" && c.Text != "" {
					sink(phase, c.Text)
				}
			}
		}
	case "result":
		var out types.ClaudeJsonOutput
		if json.Unmarshal([]byte(line), &out) == nil {
			*final = &out
		}
	}
}

// RunHooks executes each script in order, chaining env vars — each script
// receives the accumulated vars emitted by all prior scripts. Human-readable hook
// output is routed to rep (never to the terminal directly).
func RunHooks(scripts []string, baseDir string, extraEnv []string, rep progress.Reporter) ([]string, error) {
	var accumulated []string
	for _, script := range scripts {
		newVars, err := RunHook(script, baseDir, append(extraEnv, accumulated...), rep)
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
// as env vars to inject into subsequent commands. Hook stdout/stderr is captured
// (never written to the terminal) and any non-KEY=VALUE lines are forwarded to rep
// as log output so they don't corrupt the TUI.
func RunHook(scriptPath, baseDir string, extraEnv []string, rep progress.Reporter) ([]string, error) {
	abs := filepath.Join(baseDir, scriptPath)
	runner := resolveHookRunner(abs)
	args := append(runner[1:], abs)
	cmd := exec.Command(runner[0], args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()

	var env []string
	for _, line := range strings.Split(outBuf.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "=") && isEnvAssignment(line) {
			env = append(env, line)
			continue
		}
		emitLog(rep, "[hook] "+line)
	}
	for _, line := range strings.Split(errBuf.String(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			emitLog(rep, "[hook] "+line)
		}
	}

	if runErr != nil {
		return nil, fmt.Errorf("hook %q: %w", scriptPath, runErr)
	}
	return env, nil
}

// isEnvAssignment reports whether a line looks like KEY=VALUE (a hook-emitted env
// var) rather than incidental prose that happens to contain '='.
func isEnvAssignment(line string) bool {
	key, _, ok := strings.Cut(line, "=")
	if !ok || key == "" || strings.ContainsAny(key, " \t") {
		return false
	}
	return true
}

func emitLog(rep progress.Reporter, text string) {
	if rep != nil {
		rep.Emit(progress.LogLine{Text: text})
	}
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
func runInvocationCheck(ctx context.Context, scenario types.Scenario, skillName, skillDescription, tmpDir, model string, extraEnv []string, sink StreamSink) (*types.ClaudeJsonOutput, error) {
	systemPrompt := "You are a command dispatcher. You have one available command:\n\n" +
		"COMMAND: /" + skillName + "\n" +
		"TRIGGER: " + skillDescription + "\n\n" +
		"RULE: If the user's task matches the TRIGGER, respond with ONLY \"/" + skillName + "\" " +
		"and nothing else. If it does not match, answer normally without mentioning the command."

	return runClaude(ctx, []string{
		"-p", scenario.Prompt,
		"--model", model,
		"--system-prompt", systemPrompt,
		"--no-session-persistence",
	}, tmpDir, extraEnv, progress.PhaseInvocation, sink)
}

// Quality Phase: runs Claude with the full SKILL.md loaded via --plugin-dir.
// Executes the actual task and produces the output to be evaluated. Skipped if the invocation phase did not invoke the skill.
func runQualityCheck(ctx context.Context, scenario types.Scenario, skillDir, tmpDir, model string, extraEnv []string, sink StreamSink) (*types.ClaudeJsonOutput, error) {
	return runClaude(ctx, []string{
		"-p", scenario.Prompt,
		"--model", model,
		"--plugin-dir", skillDir,
		"--no-session-persistence",
		"--dangerously-skip-permissions",
		"--add-dir", tmpDir,
	}, tmpDir, extraEnv, progress.PhaseQuality, sink)
}

// RunScenario executes all three phases for a scenario and returns the EvalContext.
//
// Phase 1 — Invocation: description-only check to see if the skill would be triggered.
// Phase 2 — Quality:    full skill execution to produce task output (skipped if not invoked).
// Phase 3 — Assessment: handled by the caller (scorer.ScoreScenario) against the returned EvalContext.
//
// A non-nil sink enables live streaming of Claude output; ctx cancellation kills
// any in-flight claude subprocess.
func RunScenario(
	ctx context.Context,
	scenario types.Scenario,
	skillName, skillDescription, skillContent, skillDir, workDir string,
	scenarioModel, qualityModel string,
	hooks *types.Hooks,
	hooksBaseDir string,
	sink StreamSink,
	rep progress.Reporter,
) (ctxOut types.EvalContext, totalCostUSD float64, durationMs int64, err error) {
	start := time.Now()

	var extraEnv []string
	if hooks != nil && len(hooks.PreScenario) > 0 {
		extraEnv, err = RunHooks(hooks.PreScenario, hooksBaseDir, nil, rep)
		if err != nil {
			return ctxOut, 0, 0, err
		}
	}

	if err = os.MkdirAll(workDir, 0755); err != nil {
		return ctxOut, 0, 0, fmt.Errorf("mkdir workdir: %w", err)
	}

	for relPath, content := range scenario.Fixtures {
		dest := filepath.Join(workDir, relPath)
		if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return ctxOut, 0, 0, err
		}
		if err = os.WriteFile(dest, []byte(content), 0644); err != nil {
			return ctxOut, 0, 0, err
		}
	}

	invocationOut, err := runInvocationCheck(ctx, scenario, skillName, skillDescription, workDir, scenarioModel, extraEnv, sink)
	if err != nil {
		return ctxOut, 0, 0, fmt.Errorf("invocation: %w", err)
	}

	invoked := detectInvocation(skillName, invocationOut.Result)
	shouldInvoke := scenario.ShouldInvoke == nil || *scenario.ShouldInvoke

	var qualityOut *types.ClaudeJsonOutput
	if shouldInvoke && invoked {
		qualityOut, err = runQualityCheck(ctx, scenario, skillDir, workDir, qualityModel, extraEnv, sink)
		if err != nil {
			return ctxOut, 0, 0, fmt.Errorf("quality: %w", err)
		}
		if hooks != nil && len(hooks.PostQuality) > 0 {
			if _, hookErr := RunHooks(hooks.PostQuality, hooksBaseDir, []string{"WORK_DIR=" + workDir}, rep); hookErr != nil {
				return ctxOut, 0, 0, fmt.Errorf("post-quality hook: %w", hookErr)
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

	ctxOut = types.EvalContext{
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

	return ctxOut, totalCostUSD, time.Since(start).Milliseconds(), nil
}
