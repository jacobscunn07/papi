package store

import (
	"reflect"
	"testing"

	"papi/internal/types"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func boolPtr(b bool) *bool { return &b }

func TestSaveAndReconstructIteration(t *testing.T) {
	st := open(t)
	const skill, ts = "demo", "1000"

	results := []types.ScenarioRunResult{
		{
			Scenario:             types.Scenario{ID: "a", Prompt: "do a", Tags: []string{"x"}, ShouldInvoke: boolPtr(true)},
			Invoked:              true,
			InvocationOutput:     &types.ClaudeJsonOutput{Result: "inv", TotalCostUSD: 0.01},
			QualityOutput:        &types.ClaudeJsonOutput{Result: "qual"},
			ScenarioScore:        0.8,
			DurationMs:           1200,
			InvocationTranscript: "invocation text",
			QualityTranscript:    "quality text",
			EvalResults: []types.EvalResult{
				{EvalID: "skill-used", Name: "skill used", Score: 1, Required: true, DurationMs: 5},
				{EvalID: "quality", Name: "quality", Score: 0.6, IsLLMJudge: true, Reasoning: "ok"},
			},
		},
		{
			Scenario:      types.Scenario{ID: "b", Prompt: "do b"},
			Invoked:       false,
			ScenarioScore: 0.0,
			DurationMs:    300,
		},
	}

	if err := st.SaveIteration(skill, ts, 0, 0.4, 1500, "", "SKILL v0", results); err != nil {
		t.Fatalf("save iteration 0: %v", err)
	}

	// Iteration metadata: score stored 0..100, read back 0..1.
	iters, err := st.Iterations(skill, ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(iters) != 1 || iters[0].Index != 0 {
		t.Fatalf("iterations = %+v", iters)
	}
	if iters[0].Score != 0.4 || iters[0].DurationMs != 1500 || iters[0].SkillMd != "SKILL v0" {
		t.Fatalf("iter meta = %+v", iters[0])
	}

	got, err := st.ScenarioResults(skill, ts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, results) {
		t.Fatalf("reconstructed results differ:\n got=%+v\nwant=%+v", got, results)
	}
}

func TestSaveIterationIsIdempotent(t *testing.T) {
	st := open(t)
	const skill, ts = "demo", "1000"
	mk := func(score float64) []types.ScenarioRunResult {
		return []types.ScenarioRunResult{{Scenario: types.Scenario{ID: "a"}, ScenarioScore: score, Invoked: true}}
	}
	if err := st.SaveIteration(skill, ts, 1, 0.5, 10, "first", "v1", mk(0.5)); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveIteration(skill, ts, 1, 0.7, 20, "second", "v2", mk(0.7)); err != nil {
		t.Fatal(err)
	}
	iters, _ := st.Iterations(skill, ts)
	if len(iters) != 1 || iters[0].Score != 0.7 || iters[0].Experiment != "second" {
		t.Fatalf("re-saved iteration not replaced: %+v", iters)
	}
	got, _ := st.ScenarioResults(skill, ts, 1)
	if len(got) != 1 || got[0].ScenarioScore != 0.7 {
		t.Fatalf("scenarios not replaced: %+v", got)
	}
}

func TestRunStatesFilterAndOrder(t *testing.T) {
	st := open(t)
	const skill = "demo"
	// A logged-only run (no checkpoint) must not appear as a resumable run.
	if err := st.AppendLog(skill, "500", -1, "", "", "early"); err != nil {
		t.Fatal(err)
	}
	must := func(ts string, lci int) {
		if err := st.UpsertRun(types.RunState{Skill: skill, Timestamp: ts, LastCompletedIteration: lci, MaxIterations: 10}); err != nil {
			t.Fatal(err)
		}
	}
	must("2000", 3)
	must("1000", 1)

	states, err := st.RunStates(skill)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 || states[0].Timestamp != "1000" || states[1].Timestamp != "2000" {
		t.Fatalf("run states (oldest first, checkpointed only) = %+v", states)
	}
}

func TestPurgeOldRuns(t *testing.T) {
	st := open(t)
	const skill = "demo"
	for _, ts := range []string{"1000", "2000", "3000"} {
		if err := st.SaveIteration(skill, ts, 0, 0.5, 10, "", "v", []types.ScenarioRunResult{{Scenario: types.Scenario{ID: "a"}}}); err != nil {
			t.Fatal(err)
		}
	}
	purged, err := st.PurgeOldRuns(skill, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(purged, []string{"1000"}) {
		t.Fatalf("purged = %+v, want [1000]", purged)
	}
	remaining, _ := st.RunTimestamps(skill)
	if !reflect.DeepEqual(remaining, []string{"2000", "3000"}) {
		t.Fatalf("remaining = %+v, want [2000 3000]", remaining)
	}
}
