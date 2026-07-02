package progress

import (
	"testing"

	"papi/internal/store"
)

type captureReporter struct{ events []Event }

func (c *captureReporter) Emit(e Event) { c.events = append(c.events, e) }

// TestLogTeeScoping verifies that a LogTee wrapped by WithScope persists each
// LogLine to the store with its final iteration/scenario/eval scope, and still
// forwards every event to the inner reporter unchanged.
func TestLogTeeScoping(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	capt := &captureReporter{}
	tee := NewLogTee(capt, st, "demo", "1000")

	// Mirror the loop's wrapping: tee wraps the raw reporter, WithScope on top.
	runRep := WithScope(tee, -1, "", "")
	runRep.Emit(LogLine{Text: "run line"})
	WithScope(runRep, 1, "", "").Emit(LogLine{Text: "iter line"})
	WithScope(runRep, 1, "scenA", "").Emit(LogLine{Text: "scen line"})
	WithScope(runRep, 1, "scenA", "eval1").Emit(LogLine{Text: "eval line"})
	runRep.Emit(RunDone{}) // non-log event: forwarded, not persisted

	if err := tee.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Inner reporter sees all 5 events with scope applied to the log lines.
	if len(capt.events) != 5 {
		t.Fatalf("inner got %d events, want 5", len(capt.events))
	}
	if ll, ok := capt.events[2].(LogLine); !ok || ll.ScenarioID != "scenA" || ll.Iter != 1 {
		t.Fatalf("inner scen line not scoped: %+v", capt.events[2])
	}

	// In the store: exactly the 4 log lines, each with the right scope.
	want := []store.LogRow{
		{Iter: -1, Text: "run line"},
		{Iter: 1, Text: "iter line"},
		{Iter: 1, ScenarioID: "scenA", Text: "scen line"},
		{Iter: 1, ScenarioID: "scenA", EvalID: "eval1", Text: "eval line"},
	}
	got, err := st.Logs("demo", "1000")
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("wrote %d records, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
