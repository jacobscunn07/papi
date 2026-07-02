package runs

import (
	"testing"

	"papi/internal/store"
)

func TestLoadLogsRoundTrip(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const skill, ts = "demo", "1000"
	// Records as the LogTee appends them (note the multi-line text record).
	if err := st.AppendLog(skill, ts, -1, "", "", "run line"); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLog(skill, ts, 1, "scenA", "", "line one\nline two"); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLog(skill, ts, 1, "scenA", "eval1", "eval line"); err != nil {
		t.Fatal(err)
	}

	run, err := LoadRun(st, skill, ts)
	if err != nil {
		t.Fatal(err)
	}
	// Multi-line text → 2 entries, so 4 total.
	if len(run.Logs) != 4 {
		t.Fatalf("got %d log entries, want 4: %+v", len(run.Logs), run.Logs)
	}
	if run.Logs[1] != (LogEntry{Iter: 1, ScenarioID: "scenA", Text: "line one"}) {
		t.Errorf("entry[1] = %+v", run.Logs[1])
	}
	if run.Logs[3] != (LogEntry{Iter: 1, ScenarioID: "scenA", EvalID: "eval1", Text: "eval line"}) {
		t.Errorf("entry[3] = %+v", run.Logs[3])
	}

	// Unknown run → no logs, no error.
	empty, err := LoadRun(st, skill, "9999")
	if err != nil || empty.Logs != nil {
		t.Fatalf("expected empty logs, got %+v err=%v", empty.Logs, err)
	}
}
