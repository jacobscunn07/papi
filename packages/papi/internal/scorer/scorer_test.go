package scorer

import (
	"errors"
	"testing"
	"time"

	"papi/internal/types"
)

// stubEval is a minimal types.Eval for testing.
type stubEval struct {
	id      string
	llm     bool
	result  types.EvalResult
	evalErr error
	delay   time.Duration
}

func (s stubEval) ID() string       { return s.id }
func (s stubEval) Name() string     { return s.id }
func (s stubEval) IsLLMJudge() bool { return s.llm }
func (s stubEval) Evaluate(types.EvalContext) (types.EvalResult, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.evalErr != nil {
		return types.EvalResult{}, s.evalErr
	}
	return s.result, nil
}

func TestScoreScenario_FailedEvalDoesNotAbort(t *testing.T) {
	ctx := types.EvalContext{Scenario: types.Scenario{ID: "s1"}}
	evals := []types.Eval{
		stubEval{id: "broken", evalErr: errors.New(`exec: "tsx": executable file not found in $PATH`)},
		stubEval{id: "ok", result: types.EvalResult{EvalID: "ok", Name: "ok", Score: 1.0}},
	}

	results, score, err := ScoreScenario(0, ctx, evals, 0.3, 0.7, nil, "", nil)
	if err != nil {
		t.Fatalf("expected no error (failure should be absorbed), got %v", err)
	}
	if len(results) != len(evals) {
		t.Fatalf("expected %d results aligned with evals, got %d", len(evals), len(results))
	}

	broken := results[0]
	if broken.EvalID != "broken" || broken.Score != 0 {
		t.Errorf("broken eval = %+v, want EvalID=broken Score=0", broken)
	}
	if broken.Reasoning == "" {
		t.Errorf("broken eval should carry the exec error as reasoning, got empty")
	}
	if broken.Required {
		t.Errorf("a failed eval must not act as a required gate")
	}

	// Both evals are non-LLM: (0 + 1) / 2 = 0.5.
	if score != 0.5 {
		t.Errorf("score = %v, want 0.5", score)
	}
}

func TestScoreScenario_RecordsEvalDuration(t *testing.T) {
	ctx := types.EvalContext{Scenario: types.Scenario{ID: "s1"}}
	evals := []types.Eval{
		stubEval{id: "slow", result: types.EvalResult{EvalID: "slow", Name: "slow", Score: 1.0}, delay: 5 * time.Millisecond},
		stubEval{id: "broken", evalErr: errors.New("boom"), delay: 5 * time.Millisecond},
	}

	results, _, err := ScoreScenario(0, ctx, evals, 0.3, 0.7, nil, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range results {
		if r.DurationMs <= 0 {
			t.Errorf("eval %q DurationMs = %d, want > 0", r.EvalID, r.DurationMs)
		}
	}
}
