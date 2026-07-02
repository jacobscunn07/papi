package progress

import (
	"sync"

	"papi/internal/store"
)

// LogTee forwards every event to an inner reporter and additionally persists
// LogLine events to the store, so past runs can replay their output in the TUI.
// It must wrap the raw base reporter with WithScope layered on top, so each
// LogLine it sees already carries its final iteration/scenario/eval scope.
type LogTee struct {
	inner Reporter
	st    *store.Store
	skill string
	ts    string

	mu sync.Mutex
}

// NewLogTee returns a LogTee that persists LogLine events for run (skill, ts) to
// st and forwards all events to inner.
func NewLogTee(inner Reporter, st *store.Store, skill, ts string) *LogTee {
	return &LogTee{inner: inner, st: st, skill: skill, ts: ts}
}

func (t *LogTee) Emit(e Event) {
	if ll, ok := e.(LogLine); ok {
		t.write(ll)
	}
	t.inner.Emit(e)
}

func (t *LogTee) write(ll LogLine) {
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.st.AppendLog(t.skill, t.ts, ll.Iter, ll.ScenarioID, ll.EvalID, ll.Text)
}

// Close is a no-op; the store owns the connection lifecycle. It is kept so callers
// can defer it like the previous file-backed tee.
func (t *LogTee) Close() error { return nil }
