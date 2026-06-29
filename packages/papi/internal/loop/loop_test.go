package loop

import (
	"testing"

	"papi/internal/store"
	"papi/internal/types"
)

// seedRun writes a run-level checkpoint to the store the way the loop persists them.
func seedRun(t *testing.T, st *store.Store, state types.RunState) {
	t.Helper()
	if err := st.UpsertRun(state); err != nil {
		t.Fatal(err)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestFindResumableRun(t *testing.T) {
	const skill = "demo"
	cfg := &types.ResearchConfig{SkillName: skill, MaxIterations: 10, MaxBudgetUSD: 5.0}

	t.Run("picks newest eligible run", func(t *testing.T) {
		st := openStore(t)
		// Older run, eligible.
		seedRun(t, st, types.RunState{Skill: skill, Timestamp: "1000", LastCompletedIteration: 2, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})
		// Newer run, also eligible — should win.
		seedRun(t, st, types.RunState{Skill: skill, Timestamp: "2000", LastCompletedIteration: 5, TotalCost: 2.0, MaxIterations: 10, Budget: 5.0})

		got, err := findResumableRun(st, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Timestamp != "2000" {
			t.Fatalf("got run %s, want newest eligible 2000", got.Timestamp)
		}
	})

	t.Run("skips runs that exhausted iterations or budget", func(t *testing.T) {
		st := openStore(t)
		// Newest run reached max iterations → not eligible.
		seedRun(t, st, types.RunState{Skill: skill, Timestamp: "3000", LastCompletedIteration: 10, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})
		// Next run is over budget → not eligible.
		seedRun(t, st, types.RunState{Skill: skill, Timestamp: "2000", LastCompletedIteration: 4, TotalCost: 5.0, MaxIterations: 10, Budget: 5.0})
		// Oldest run is eligible → should be selected.
		seedRun(t, st, types.RunState{Skill: skill, Timestamp: "1000", LastCompletedIteration: 3, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})

		got, err := findResumableRun(st, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Timestamp != "1000" {
			t.Fatalf("got run %s, want only-eligible 1000", got.Timestamp)
		}
	})

	t.Run("honors an explicit resume timestamp", func(t *testing.T) {
		st := openStore(t)
		seedRun(t, st, types.RunState{Skill: skill, Timestamp: "1000", LastCompletedIteration: 2, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})
		seedRun(t, st, types.RunState{Skill: skill, Timestamp: "2000", LastCompletedIteration: 5, TotalCost: 2.0, MaxIterations: 10, Budget: 5.0})

		pinned := &types.ResearchConfig{SkillName: skill, MaxIterations: 10, MaxBudgetUSD: 5.0, ResumeTimestamp: "1000"}
		got, err := findResumableRun(st, pinned)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Timestamp != "1000" {
			t.Fatalf("got run %s, want pinned 1000", got.Timestamp)
		}
	})

	t.Run("errors when nothing is resumable", func(t *testing.T) {
		st := openStore(t)
		seedRun(t, st, types.RunState{Skill: skill, Timestamp: "1000", LastCompletedIteration: 10, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})

		if _, err := findResumableRun(st, cfg); err == nil {
			t.Fatal("expected an error when no run is resumable, got nil")
		}
	})

	t.Run("raising the iteration cap makes a completed run resumable again", func(t *testing.T) {
		st := openStore(t)
		seedRun(t, st, types.RunState{Skill: skill, Timestamp: "1000", LastCompletedIteration: 10, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})

		extended := &types.ResearchConfig{SkillName: skill, MaxIterations: 20, MaxBudgetUSD: 5.0}
		got, err := findResumableRun(st, extended)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Timestamp != "1000" {
			t.Fatalf("got run %s, want 1000 now-eligible under a higher iteration cap", got.Timestamp)
		}
	})
}
