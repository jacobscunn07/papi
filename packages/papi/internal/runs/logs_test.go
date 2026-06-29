package runs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLogsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Records as the LogTee writes them (note the multi-line text record).
	jsonl := `{"iter":-1,"text":"run line"}
{"iter":1,"scenarioId":"scenA","text":"line one\nline two"}
{"iter":1,"scenarioId":"scenA","evalId":"eval1","text":"eval line"}
`
	if err := os.WriteFile(filepath.Join(dir, "logs.jsonl"), []byte(jsonl), 0644); err != nil {
		t.Fatal(err)
	}
	run, err := LoadRun(dir)
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

	// Missing file → no logs, no error.
	empty, err := LoadRun(t.TempDir())
	if err != nil || empty.Logs != nil {
		t.Fatalf("expected empty logs, got %+v err=%v", empty.Logs, err)
	}
}
