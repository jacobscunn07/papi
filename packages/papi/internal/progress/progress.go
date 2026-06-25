// Package progress defines the event model the research loop emits as it runs,
// plus reporters that consume those events. The headless CLI uses CLIReporter
// (which reproduces the original stdout output); the TUI uses ChannelReporter to
// stream events into a bubbletea program.
package progress

import "papi/internal/types"

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
// Iter 0 is the baseline (Delta is not meaningful).
type IterationDone struct {
	Iter     int
	Score    float64
	Delta    float64
	Improved bool
	Cost     float64
	Results  []types.ScenarioRunResult
}

// RunDone is emitted once at the end of a run.
type RunDone struct {
	Best  float64
	Cost  float64
	Tag   string
	Error string
}

// LogLine carries an arbitrary status line (cost notices, reverts, etc.).
type LogLine struct{ Text string }

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

// NopReporter discards all events.
type NopReporter struct{}

func (NopReporter) Emit(Event) {}
