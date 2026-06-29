package progress

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// persistedLog is the on-disk shape of a LogLine, written one JSON object per line
// to a run's logs.jsonl so past runs can replay their output in the TUI.
type persistedLog struct {
	Iter       int    `json:"iter"`
	ScenarioID string `json:"scenarioId,omitempty"`
	EvalID     string `json:"evalId,omitempty"`
	Text       string `json:"text"`
}

// LogTee forwards every event to an inner reporter and additionally appends
// LogLine events to a JSONL file. The file is opened lazily on the first log line
// (creating its directory) so an unused run directory is never created. It must
// wrap the raw base reporter with WithScope layered on top, so each LogLine it
// sees already carries its final iteration/scenario/eval scope.
type LogTee struct {
	inner Reporter
	path  string

	mu  sync.Mutex
	w   io.WriteCloser
	enc *json.Encoder
}

// NewLogTee returns a LogTee that persists LogLine events to path and forwards
// all events to inner.
func NewLogTee(inner Reporter, path string) *LogTee {
	return &LogTee{inner: inner, path: path}
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
	if t.enc == nil {
		if err := os.MkdirAll(filepath.Dir(t.path), 0755); err != nil {
			return
		}
		f, err := os.OpenFile(t.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		t.w = f
		t.enc = json.NewEncoder(f)
	}
	_ = t.enc.Encode(persistedLog{
		Iter:       ll.Iter,
		ScenarioID: ll.ScenarioID,
		EvalID:     ll.EvalID,
		Text:       ll.Text,
	})
}

// Close closes the underlying file if it was opened.
func (t *LogTee) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.w != nil {
		err := t.w.Close()
		t.w, t.enc = nil, nil
		return err
	}
	return nil
}
