package tui

import (
	"fmt"
	"strings"

	"papi/internal/progress"
	"papi/internal/runs"
	"papi/internal/types"
)

// detailContent renders the right-hand pane for the selected row.
func (m *model) detailContent(r *row) string {
	if r == nil {
		return mutedStyle.Render("Nothing selected.")
	}
	switch r.kind {
	case kindRun:
		return m.detailRun(r.run)
	case kindIteration:
		return m.detailIteration(r)
	case kindScenario:
		return m.detailScenario(r)
	case kindEval:
		return detailEval(r.eval)
	case kindFile:
		return r.file.Content()
	}
	return ""
}

func (m *model) detailRun(r *runs.Run) string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("run "+r.Timestamp))
	if r.BestScore() >= 0 {
		fmt.Fprintf(&b, "best score: %s\n", scoreStyle(r.BestScore()).Render(fmt.Sprintf("%.1f%%", r.BestScore()*100)))
	}
	fmt.Fprintf(&b, "iterations: %d   duration: %s\n\n", len(r.Iterations), progress.FmtDuration(r.Duration()))
	for i := range r.Iterations {
		it := &r.Iterations[i]
		label := fmt.Sprintf("iteration-%03d", it.Index)
		fmt.Fprintf(&b, "  %-22s %s   %s\n", label, scoreStyle(it.Score).Render(fmt.Sprintf("%.1f", it.Score*100)), mutedStyle.Render(progress.FmtDuration(it.DurationMs)))
	}
	return b.String()
}

func (m *model) detailIteration(r *row) string {
	it := r.iter
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render(fmt.Sprintf("iteration-%03d", it.Index)))
	if it.Score >= 0 {
		fmt.Fprintf(&b, "score: %s", scoreStyle(it.Score).Render(fmt.Sprintf("%.1f%%", it.Score*100)))
		if r.prevIter != nil && r.prevIter.Score >= 0 {
			delta := (it.Score - r.prevIter.Score) * 100
			fmt.Fprintf(&b, "   Δ %+.1f", delta)
		}
		if it.DurationMs > 0 {
			fmt.Fprintf(&b, "   %s", progress.FmtDuration(it.DurationMs))
		}
		fmt.Fprintln(&b)
	}
	if st := m.liveStatus[r.key]; st != "" {
		fmt.Fprintf(&b, "status: %s\n", liveBadgeStyle.Render(st))
	}
	if it.Experiment != "" {
		fmt.Fprintf(&b, "\n%s\n%s\n", titleStyle.Render("experiment"), wrap(it.Experiment, 72))
	}

	if len(it.Scenarios) > 0 {
		fmt.Fprintf(&b, "\n%s\n", titleStyle.Render("scenarios"))
		for i := range it.Scenarios {
			sc := &it.Scenarios[i]
			fmt.Fprintf(&b, "  %-30s %s   %s\n", sc.ID, scoreStyle(sc.Score).Render(fmt.Sprintf("%.1f", sc.Score*100)), mutedStyle.Render(progress.FmtDuration(sc.Result.DurationMs)))
		}
	}

	// SKILL.md diff vs previous iteration.
	fmt.Fprintf(&b, "\n%s\n", titleStyle.Render("SKILL.md changes"))
	if r.prevIter == nil {
		fmt.Fprintln(&b, mutedStyle.Render("(baseline — no previous iteration to diff against)"))
	} else {
		diff := runs.DiffSkillMd(r.prevIter.SkillMd(), it.SkillMd())
		if strings.TrimSpace(diff) == "" {
			fmt.Fprintln(&b, mutedStyle.Render("(no changes to SKILL.md)"))
		} else {
			fmt.Fprint(&b, colorizeDiff(diff))
		}
	}
	return b.String()
}

func (m *model) detailScenario(r *row) string {
	sc := r.scen
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render(sc.ID))

	if st := m.liveStatus[r.key]; st != "" {
		fmt.Fprintf(&b, "status: %s\n", liveBadgeStyle.Render(st))
	}
	if sc.Score >= 0 {
		invoked := "invoked"
		if !sc.Invoked {
			invoked = "not invoked"
		}
		fmt.Fprintf(&b, "score: %s   %s", scoreStyle(sc.Score).Render(fmt.Sprintf("%.1f%%", sc.Score*100)), invoked)
		if sc.Result.DurationMs > 0 {
			fmt.Fprintf(&b, "   %.1fs", float64(sc.Result.DurationMs)/1000)
		}
		fmt.Fprintln(&b)
	}

	// Live stream (if this scenario is currently producing output).
	if buf := m.streams[r.key]; buf != nil && buf.Len() > 0 {
		fmt.Fprintf(&b, "\n%s\n%s\n", titleStyle.Render("live output"), buf.String())
	}

	if len(sc.Result.EvalResults) > 0 {
		fmt.Fprintf(&b, "\n%s\n", titleStyle.Render("evals"))
		for i := range sc.Result.EvalResults {
			ev := &sc.Result.EvalResults[i]
			flags := ""
			if ev.Required {
				flags += " [req]"
			}
			if ev.IsLLMJudge {
				flags += " [llm]"
			}
			fmt.Fprintf(&b, "  %-26s %s   %s%s\n", ev.Name, scoreStyle(ev.Score).Render(fmt.Sprintf("%.0f", ev.Score*100)), mutedStyle.Render(progress.FmtDuration(ev.DurationMs)), mutedStyle.Render(flags))
		}
	}
	return b.String()
}

func detailEval(ev *types.EvalResult) string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render(ev.Name))
	fmt.Fprintf(&b, "score: %s   duration: %s\n", scoreStyle(ev.Score).Render(fmt.Sprintf("%.1f", ev.Score*100)), progress.FmtDuration(ev.DurationMs))
	flags := []string{}
	if ev.Required {
		flags = append(flags, "required")
	}
	if ev.IsLLMJudge {
		flags = append(flags, "llm judge")
	}
	if len(flags) > 0 {
		fmt.Fprintf(&b, "%s\n", mutedStyle.Render(strings.Join(flags, " · ")))
	}
	if ev.Reasoning != "" {
		fmt.Fprintf(&b, "\n%s\n%s\n", titleStyle.Render("reasoning"), wrap(ev.Reasoning, 72))
	}
	return b.String()
}

func colorizeDiff(diff string) string {
	var b strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			fmt.Fprintln(&b, hunkLineStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			fmt.Fprintln(&b, addLineStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			fmt.Fprintln(&b, delLineStyle.Render(line))
		default:
			fmt.Fprintln(&b, line)
		}
	}
	return b.String()
}

// wrap word-wraps text to width columns.
func wrap(s string, width int) string {
	var out strings.Builder
	for _, para := range strings.Split(s, "\n") {
		line := 0
		for _, word := range strings.Fields(para) {
			if line > 0 && line+1+len(word) > width {
				out.WriteByte('\n')
				line = 0
			} else if line > 0 {
				out.WriteByte(' ')
				line++
			}
			out.WriteString(word)
			line += len(word)
		}
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}
