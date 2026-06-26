package scorer

import (
	"fmt"
	"time"

	"papi/internal/progress"
	"papi/internal/runner"
	"papi/internal/types"
)

// Assessment Phase: runs all evals (built-in + custom) against the EvalContext produced by the
// invocation and quality phases, then aggregates them into a per-scenario score.
// If any required eval scores 0, the scenario contributes 0 regardless of others.
// For negative tests (shouldInvoke=false), content evals are skipped and scored N/A (1.0).
// Non-required evals are split into two categories: LLM judge and non-LLM judge.
// Final score = llmWeight*llmCategoryScore + nonLLMWeight*nonLLMCategoryScore.
// If one category is empty, the other carries 100% of the score.
func ScoreScenario(iter int, ctx types.EvalContext, evals []types.Eval, llmWeight, nonLLMWeight float64, hooks *types.Hooks, hooksBaseDir string, rep progress.Reporter) ([]types.EvalResult, float64, error) {
	shouldInvoke := ctx.Scenario.ShouldInvoke == nil || *ctx.Scenario.ShouldInvoke
	results := make([]types.EvalResult, 0, len(evals))

	for i, e := range evals {
		if hooks != nil && len(hooks.PreEval) > 0 {
			evalEnv := []string{
				"EVAL_ID=" + e.ID(),
				"EVAL_NAME=" + e.Name(),
				"SCENARIO_ID=" + ctx.Scenario.ID,
				"WORK_DIR=" + ctx.WorkDir,
			}
			if _, err := runner.RunHooks(hooks.PreEval, hooksBaseDir, evalEnv, rep); err != nil {
				return nil, 0, fmt.Errorf("pre-eval hook: %w", err)
			}
		}

		evalStart := time.Now()
		r, err := e.Evaluate(ctx)
		evalMs := time.Since(evalStart).Milliseconds()
		if err != nil {
			// A failed eval (e.g. its runner isn't installed, a non-zero exit, or
			// unparseable output) is recorded as a 0-scoring result with the error
			// captured as the reasoning, rather than aborting the whole run. It is
			// not treated as a required gate, so one broken eval can't zero the
			// scenario via the required short-circuit.
			if rep != nil {
				progress.WithScope(rep, iter, ctx.Scenario.ID, e.ID()).
					Emit(progress.LogLine{Text: fmt.Sprintf("eval %s failed: %v", e.ID(), err)})
			}
			results = append(results, types.EvalResult{
				EvalID:     e.ID(),
				Name:       e.Name(),
				Score:      0,
				Reasoning:  fmt.Sprintf("eval failed to run: %v", err),
				IsLLMJudge: e.IsLLMJudge(),
				DurationMs: evalMs,
			})
			continue
		}

		if hooks != nil && len(hooks.PostEval) > 0 {
			evalEnv := []string{
				"EVAL_ID=" + e.ID(),
				"EVAL_NAME=" + e.Name(),
				"SCENARIO_ID=" + ctx.Scenario.ID,
				"WORK_DIR=" + ctx.WorkDir,
				fmt.Sprintf("SCORE=%g", r.Score),
			}
			if _, err := runner.RunHooks(hooks.PostEval, hooksBaseDir, evalEnv, rep); err != nil {
				return nil, 0, fmt.Errorf("post-eval hook: %w", err)
			}
		}
		r.IsLLMJudge = e.IsLLMJudge()
		r.DurationMs = evalMs
		results = append(results, r)

		if r.Required {
			// Short-circuit once the required gate resolves: skip remaining evals.
			// Required failure → content evals scored 0 (not run).
			// Negative test pass → content evals scored 1.0 (N/A, no output to evaluate).
			if r.Score == 0 || !shouldInvoke {
				skippedScore := 0.0
				skippedReason := "Not evaluated — a required eval did not pass."
				if !shouldInvoke && r.Score > 0 {
					skippedScore = 1.0
					skippedReason = "N/A — negative test: only skill invocation is evaluated."
				}
				for _, remaining := range evals[i+1:] {
					results = append(results, types.EvalResult{
						EvalID:     remaining.ID(),
						Name:       remaining.Name(),
						Score:      skippedScore,
						Reasoning:  skippedReason,
						IsLLMJudge: remaining.IsLLMJudge(),
					})
				}
				break
			}
		}
	}

	for _, r := range results {
		if r.Required && r.Score == 0 {
			return results, 0, nil
		}
	}

	var llmSum, llmTotal, nonLLMSum, nonLLMTotal float64
	for i, e := range evals {
		if e.IsLLMJudge() {
			llmSum += results[i].Score
			llmTotal++
		} else {
			nonLLMSum += results[i].Score
			nonLLMTotal++
		}
	}

	var llmScore, nonLLMScore float64
	if llmTotal > 0 {
		llmScore = llmSum / llmTotal
	}
	if nonLLMTotal > 0 {
		nonLLMScore = nonLLMSum / nonLLMTotal
	}

	var score float64
	switch {
	case llmTotal == 0:
		score = nonLLMScore
	case nonLLMTotal == 0:
		score = llmScore
	default:
		score = llmWeight*llmScore + nonLLMWeight*nonLLMScore
	}
	return results, score, nil
}

// AggregateScore computes a simple average across all scenario results.
func AggregateScore(results []types.ScenarioRunResult) float64 {
	if len(results) == 0 {
		return 0
	}
	var sum float64
	for _, r := range results {
		sum += r.ScenarioScore
	}
	return sum / float64(len(results))
}
