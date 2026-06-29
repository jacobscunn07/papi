// Package progress defines the event model the research loop emits as it runs,
// plus reporters that consume those events. The headless CLI uses CLIReporter
// (which reproduces the original stdout output); the TUI uses ChannelReporter to
// stream events into a bubbletea program.
package progress

import (
	"fmt"

	"papi/internal/types"
)

// FmtDuration renders a millisecond duration compactly for display, e.g. "820ms",
// "4.2s", "3m05s", "1h02m". A non-positive duration renders as "—".
func FmtDuration(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	case ms < 3_600_000:
		s := ms / 1000
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	default:
		m := ms / 60_000
		return fmt.Sprintf("%dh%02dm", m/60, m%60)
	}
}

// Phase identifies which sub-step of a scenario is currently executing.
type Phase string

const (
	PhaseInvocation Phase = "invocation"
	PhaseQuality    Phase = "quality"
	PhaseScoring    Phase = "scoring"
)

// Event is the closed set of progress events emitted by loop.Run. Consumers
// switch on the concrete type.
type Event interface{ isEvent() }

// RunStarted is emitted once at the very start of a run.
type RunStarted struct {
	Skill         string
	Timestamp     string // run directory timestamp (Unix millis)
	MaxIterations int
	Budget        float64
	ScenarioIDs   []string
	ResumeFrom    int // 0 = fresh start; >0 = iterations 0..ResumeFrom-1 already completed
}

// IterationStarted is emitted at the start of each iteration (0 = baseline).
type IterationStarted struct {
	Iter int
	Best float64
}

// ResearchAgentDone is emitted after the research agent proposes a new SKILL.md.
type ResearchAgentDone struct {
	Iter        int
	Description string
	Cost        float64
}

// ScenarioStarted is emitted when a scenario begins running.
type ScenarioStarted struct {
	Iter int
	ID   string
}

// PhaseChanged is emitted when a scenario transitions between phases.
type PhaseChanged struct {
	Iter  int
	ID    string
	Phase Phase
}

// StreamChunk carries a chunk of streamed Claude output for the active phase.
type StreamChunk struct {
	Iter  int
	ID    string
	Phase Phase
	Text  string
}

// EvalDone is emitted after a single eval completes for a scenario.
type EvalDone struct {
	Iter       int
	ScenarioID string
	Eval       types.EvalResult
}

// ScenarioDone is emitted when a scenario (run + scoring) completes.
type ScenarioDone struct {
	Iter   int
	Result types.ScenarioRunResult
}

// IterationDone is emitted when an iteration completes and has been scored.
// Iter 0 is the baseline (Delta is not meaningful). SkillMd is the SKILL.md
// snapshot that ran this iteration, so the live TUI can diff it against the
// previous iteration without re-reading from the store.
type IterationDone struct {
	Iter       int
	Score      float64
	Delta      float64
	Improved   bool
	Cost       float64
	DurationMs int64
	Results    []types.ScenarioRunResult
	SkillMd    string
}

// RunDone is emitted once at the end of a run.
type RunDone struct {
	Best       float64
	Cost       float64
	DurationMs int64
	Tag        string
	Error      string
}

// LogLine carries an arbitrary status line (cost notices, reverts, etc.). The
// Iter/ScenarioID/EvalID fields scope the line to a node in the run hierarchy so
// consumers (the TUI) can filter logs by the selected node; they are normally
// populated by a scoped reporter (see WithScope) rather than at the emit site.
type LogLine struct {
	Text       string
	Iter       int    // iteration index; -1 when not tied to an iteration
	ScenarioID string // "" when not scenario-specific
	EvalID     string // "" when not eval-specific
}

func (RunStarted) isEvent()        {}
func (IterationStarted) isEvent()  {}
func (ResearchAgentDone) isEvent() {}
func (ScenarioStarted) isEvent()   {}
func (PhaseChanged) isEvent()      {}
func (StreamChunk) isEvent()       {}
func (EvalDone) isEvent()          {}
func (ScenarioDone) isEvent()      {}
func (IterationDone) isEvent()     {}
func (RunDone) isEvent()           {}
func (LogLine) isEvent()           {}

// Reporter consumes events emitted by the loop.
type Reporter interface {
	Emit(Event)
}

// ChannelReporter forwards events to a channel for the TUI to consume. Sends are
// blocking so no events are dropped; the consumer must keep draining.
type ChannelReporter struct {
	ch chan<- Event
}

// NewChannelReporter returns a reporter that forwards events to ch.
func NewChannelReporter(ch chan<- Event) *ChannelReporter {
	return &ChannelReporter{ch: ch}
}

func (r *ChannelReporter) Emit(e Event) { r.ch <- e }

// scopedReporter enriches LogLine events with hierarchy context (iteration,
// scenario, eval) before forwarding them to an inner reporter. All other event
// types pass through untouched, since they already carry their own identifiers.
type scopedReporter struct {
	inner      Reporter
	iter       int
	scenarioID string
	evalID     string
}

func (r scopedReporter) Emit(e Event) {
	if ll, ok := e.(LogLine); ok {
		ll.Iter, ll.ScenarioID, ll.EvalID = r.iter, r.scenarioID, r.evalID
		r.inner.Emit(ll)
		return
	}
	r.inner.Emit(e)
}

// WithScope returns a reporter that tags every LogLine it forwards with the given
// run-hierarchy scope. It unwraps any existing scope first so wrapping is always a
// single level over the true base reporter — a more-specific scope can never be
// clobbered by a less-specific outer one.
func WithScope(r Reporter, iter int, scenarioID, evalID string) Reporter {
	if s, ok := r.(scopedReporter); ok {
		r = s.inner
	}
	return scopedReporter{inner: r, iter: iter, scenarioID: scenarioID, evalID: evalID}
}

// NopReporter discards all events.
type NopReporter struct{}

func (NopReporter) Emit(Event) {}
