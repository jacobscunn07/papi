package loop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"papi/internal/types"
)

// writeRunState lays down a run directory with a state.json checkpoint under the
// repo's .papi/skills/<skill>/runs/<ts>/ tree, the way the loop persists them.
func writeRunState(t *testing.T, repoRoot, skill, ts string, st types.RunState) {
	t.Helper()
	dir := filepath.Join(repoRoot, ".papi", "skills", skill, "runs", ts)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), b, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFindResumableRun(t *testing.T) {
	const skill = "demo"
	cfg := &types.ResearchConfig{SkillName: skill, MaxIterations: 10, MaxBudgetUSD: 5.0}

	t.Run("picks newest eligible run", func(t *testing.T) {
		root := t.TempDir()
		// Older run, eligible.
		writeRunState(t, root, skill, "1000", types.RunState{Timestamp: "1000", LastCompletedIteration: 2, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})
		// Newer run, also eligible — should win.
		writeRunState(t, root, skill, "2000", types.RunState{Timestamp: "2000", LastCompletedIteration: 5, TotalCost: 2.0, MaxIterations: 10, Budget: 5.0})

		st, err := findResumableRun(root, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.Timestamp != "2000" {
			t.Fatalf("got run %s, want newest eligible 2000", st.Timestamp)
		}
	})

	t.Run("skips runs that exhausted iterations or budget", func(t *testing.T) {
		root := t.TempDir()
		// Newest run reached max iterations → not eligible.
		writeRunState(t, root, skill, "3000", types.RunState{Timestamp: "3000", LastCompletedIteration: 10, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})
		// Next run is over budget → not eligible.
		writeRunState(t, root, skill, "2000", types.RunState{Timestamp: "2000", LastCompletedIteration: 4, TotalCost: 5.0, MaxIterations: 10, Budget: 5.0})
		// Oldest run is eligible → should be selected.
		writeRunState(t, root, skill, "1000", types.RunState{Timestamp: "1000", LastCompletedIteration: 3, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})

		st, err := findResumableRun(root, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.Timestamp != "1000" {
			t.Fatalf("got run %s, want only-eligible 1000", st.Timestamp)
		}
	})

	t.Run("honors an explicit resume timestamp", func(t *testing.T) {
		root := t.TempDir()
		writeRunState(t, root, skill, "1000", types.RunState{Timestamp: "1000", LastCompletedIteration: 2, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})
		writeRunState(t, root, skill, "2000", types.RunState{Timestamp: "2000", LastCompletedIteration: 5, TotalCost: 2.0, MaxIterations: 10, Budget: 5.0})

		pinned := &types.ResearchConfig{SkillName: skill, MaxIterations: 10, MaxBudgetUSD: 5.0, ResumeTimestamp: "1000"}
		st, err := findResumableRun(root, pinned)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.Timestamp != "1000" {
			t.Fatalf("got run %s, want pinned 1000", st.Timestamp)
		}
	})

	t.Run("errors when nothing is resumable", func(t *testing.T) {
		root := t.TempDir()
		writeRunState(t, root, skill, "1000", types.RunState{Timestamp: "1000", LastCompletedIteration: 10, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})

		if _, err := findResumableRun(root, cfg); err == nil {
			t.Fatal("expected an error when no run is resumable, got nil")
		}
	})

	t.Run("raising the iteration cap makes a completed run resumable again", func(t *testing.T) {
		root := t.TempDir()
		writeRunState(t, root, skill, "1000", types.RunState{Timestamp: "1000", LastCompletedIteration: 10, TotalCost: 1.0, MaxIterations: 10, Budget: 5.0})

		extended := &types.ResearchConfig{SkillName: skill, MaxIterations: 20, MaxBudgetUSD: 5.0}
		st, err := findResumableRun(root, extended)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.Timestamp != "1000" {
			t.Fatalf("got run %s, want 1000 now-eligible under a higher iteration cap", st.Timestamp)
		}
	})
}
