package tui

import (
	"strings"
	"testing"

	"papi/internal/progress"
	"papi/internal/runs"
)

// TestResearchAgentDoneDeliversSkillMd verifies the live iteration carries its
// proposed SKILL.md as soon as ResearchAgentDone arrives — before any
// IterationDone — so the diff renders correctly mid-iteration.
func TestResearchAgentDoneDeliversSkillMd(t *testing.T) {
	m := &model{
		mode:       modeBrowse,
		skillName:  "go-author",
		liveStatus: map[string]string{},
		streams:    map[string]*strings.Builder{},
		expanded:   map[string]bool{},
	}
	// Viewports are left at zero width so refreshDetail/refreshLog no-op in tests.
	m.applyEvent(progress.RunStarted{Skill: "go-author", Timestamp: "1000", ScenarioIDs: []string{"s1"}})
	m.applyEvent(progress.IterationStarted{Iter: 1})
	m.applyEvent(progress.ResearchAgentDone{Iter: 1, Description: "tweak", SkillMd: "---\nnew skill\n---"})

	it := m.liveIter(1)
	if it == nil {
		t.Fatal("expected live iteration 1")
	}
	if got := it.SkillMd(); got != "---\nnew skill\n---" {
		t.Fatalf("iteration snapshot not set from ResearchAgentDone: %q", got)
	}
	if it.Experiment != "tweak" {
		t.Fatalf("experiment = %q, want %q", it.Experiment, "tweak")
	}
}

// TestRunningFlagGatesSpinner verifies that only the iteration with a live-status
// entry is marked running (and thus animated), while a completed iteration of the
// same live run is not — even though it carries a static delta/duration badge.
func TestRunningFlagGatesSpinner(t *testing.T) {
	run := runs.Run{
		Timestamp: "1000",
		Iterations: []runs.Iteration{
			{Index: 0, Score: 0.8, DurationMs: 1500}, // completed
			{Index: 1, Score: -1},                    // running
		},
	}
	rk := runKey(run.Timestamp)
	m := &model{
		mode: modeBrowse, skillName: "demo", width: 96, height: 24,
		live: &run, liveActive: true,
		liveStatus: map[string]string{iterKey(rk, 1): "running"},
		streams:    map[string]*strings.Builder{},
		expanded:   map[string]bool{rk: true},
	}

	rows := m.buildRows()
	var done, running *row
	for i := range rows {
		if rows[i].kind != kindIteration {
			continue
		}
		switch rows[i].iter.Index {
		case 0:
			done = &rows[i]
		case 1:
			running = &rows[i]
		}
	}
	if done == nil || running == nil {
		t.Fatalf("expected both iteration rows, got %d rows", len(rows))
	}
	if done.running {
		t.Errorf("completed iteration-000 should not be running")
	}
	if !running.running {
		t.Errorf("active iteration-001 should be running")
	}

	const spin = "⣷"
	// Completed iteration has a non-empty (Δ/duration) badge but must NOT animate.
	if done.badge == "" {
		t.Fatalf("expected a static badge on the completed iteration")
	}
	if out := renderRow(*done, false, 96, spin); strings.Contains(out, spin) {
		t.Errorf("completed iteration must not show a spinner:\n%s", out)
	}
	// Active iteration must animate.
	if out := renderRow(*running, false, 96, spin); !strings.Contains(out, spin) {
		t.Errorf("active iteration must show a spinner:\n%s", out)
	}
}
