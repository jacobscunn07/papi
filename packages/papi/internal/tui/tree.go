package tui

import (
	"fmt"
	"strings"

	"papi/internal/runs"
	"papi/internal/types"
)

type nodeKind int

const (
	kindRun nodeKind = iota
	kindIteration
	kindScenario
	kindEval
	kindFile
)

type row struct {
	key        string
	depth      int
	kind       nodeKind
	name       string
	score      float64 // -1 = none
	badge      string
	expandable bool
	expanded   bool
	live       bool

	run      *runs.Run
	iter     *runs.Iteration
	prevIter *runs.Iteration
	scen     *runs.Scenario
	eval     *types.EvalResult
	file     *runs.File
}

func runKey(ts string) string             { return "r:" + ts }
func iterKey(rk string, idx int) string    { return fmt.Sprintf("%s/i:%d", rk, idx) }
func scenKey(ik, id string) string         { return ik + "/s:" + id }
func evalKey(sk, id string) string         { return sk + "/e:" + id }
func fileKey(sk, label string) string      { return sk + "/f:" + label }

// buildRows flattens the visible tree given the current expansion state. The live
// run (if any) is listed first, then past runs newest-first.
func (m *model) buildRows() []row {
	var rows []row

	emitRun := func(r *runs.Run, live bool) {
		rk := runKey(r.Timestamp)
		expanded := m.expanded[rk]
		badge := ""
		if live {
			badge = m.liveStatus[rk]
		}
		rows = append(rows, row{
			key: rk, depth: 0, kind: kindRun,
			name:       "run " + r.Timestamp,
			score:      r.BestScore(),
			badge:      badge,
			expandable: len(r.Iterations) > 0,
			expanded:   expanded,
			live:       live,
			run:        r,
		})
		if !expanded {
			return
		}
		for idx := range r.Iterations {
			it := &r.Iterations[idx]
			var prev *runs.Iteration
			if idx > 0 {
				prev = &r.Iterations[idx-1]
			}
			m.emitIteration(&rows, rk, r, it, prev, live)
		}
	}

	if m.live != nil {
		emitRun(m.live, true)
	}
	for i := len(m.pastRuns) - 1; i >= 0; i-- {
		emitRun(&m.pastRuns[i], false)
	}
	return rows
}

func (m *model) emitIteration(rows *[]row, rk string, r *runs.Run, it *runs.Iteration, prev *runs.Iteration, live bool) {
	ik := iterKey(rk, it.Index)
	expanded := m.expanded[ik]
	name := fmt.Sprintf("iteration-%03d", it.Index)
	if it.Index == 0 {
		name += " (baseline)"
	}
	badge := m.liveStatus[ik]
	if badge == "" && prev != nil && it.Score >= 0 && prev.Score >= 0 {
		delta := (it.Score - prev.Score) * 100
		sign := "+"
		if delta < 0 {
			sign = ""
		}
		badge = fmt.Sprintf("Δ%s%.1f", sign, delta)
	}
	*rows = append(*rows, row{
		key: ik, depth: 1, kind: kindIteration,
		name:       name,
		score:      it.Score,
		badge:      badge,
		expandable: len(it.Scenarios) > 0,
		expanded:   expanded,
		live:       live,
		iter:       it,
		prevIter:   prev,
		run:        r,
	})
	if !expanded {
		return
	}
	for si := range it.Scenarios {
		sc := &it.Scenarios[si]
		m.emitScenario(rows, ik, sc, live)
	}
}

func (m *model) emitScenario(rows *[]row, ik string, sc *runs.Scenario, live bool) {
	sk := scenKey(ik, sc.ID)
	expanded := m.expanded[sk]
	badge := m.liveStatus[sk]
	if badge == "" {
		if sc.Score >= 0 && !sc.Invoked {
			badge = "not invoked"
		}
	}
	*rows = append(*rows, row{
		key: sk, depth: 2, kind: kindScenario,
		name:       sc.ID,
		score:      sc.Score,
		badge:      badge,
		expandable: len(sc.Transcripts)+len(sc.Files)+len(sc.Result.EvalResults) > 0,
		expanded:   expanded,
		live:       live,
		scen:       sc,
	})
	if !expanded {
		return
	}
	for ti := range sc.Transcripts {
		f := &sc.Transcripts[ti]
		*rows = append(*rows, row{
			key: fileKey(sk, f.Label), depth: 3, kind: kindFile,
			name: f.Label, score: -1, file: f, scen: sc,
		})
	}
	for fi := range sc.Files {
		f := &sc.Files[fi]
		*rows = append(*rows, row{
			key: fileKey(sk, f.Label), depth: 3, kind: kindFile,
			name: "≣ " + f.Label, score: -1, file: f, scen: sc,
		})
	}
	for ei := range sc.Result.EvalResults {
		ev := &sc.Result.EvalResults[ei]
		flags := ""
		if ev.Required {
			flags += " [req]"
		}
		if ev.IsLLMJudge {
			flags += " [llm]"
		}
		*rows = append(*rows, row{
			key: evalKey(sk, ev.EvalID), depth: 3, kind: kindEval,
			name: ev.Name, score: ev.Score, badge: flags, eval: ev, scen: sc,
		})
	}
}

// renderRow renders a single tree row to a string of the given width.
func renderRow(r row, selected bool, width int) string {
	indent := strings.Repeat("  ", r.depth)
	arrow := "  "
	if r.expandable {
		if r.expanded {
			arrow = "▾ "
		} else {
			arrow = "▸ "
		}
	}

	scorePart := ""
	if r.score >= 0 {
		scorePart = fmt.Sprintf("  %.1f", r.score*100)
	}
	badge := r.badge
	if r.live && r.kind == kindRun {
		badge = "● live " + badge
	}

	plain := indent + arrow + r.name + scorePart
	if badge != "" {
		plain += "  " + badge
	}
	if len(plain) > width && width > 1 {
		plain = plain[:width-1] + "…"
	}

	if selected {
		if len(plain) < width {
			plain += strings.Repeat(" ", width-len(plain))
		}
		return selectedRowStyle.Render(plain)
	}

	// Non-selected: colorize score and badge.
	line := mutedStyle.Render(indent+arrow) + r.name
	if scorePart != "" {
		line += scoreStyle(r.score).Render(scorePart)
	}
	if badge != "" {
		bs := mutedStyle
		if r.live {
			bs = liveBadgeStyle
		}
		line += "  " + bs.Render(badge)
	}
	return line
}
