package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

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
	case kindGroup:
		return m.detailGroup(r)
	case kindEval:
		return m.detailEval(r.eval)
	case kindFile:
		return r.file.Content()
	}
	return ""
}

func (m *model) detailRun(r *runs.Run) string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("run "+r.Timestamp))
	stat := []string{}
	if r.BestScore() >= 0 {
		stat = append(stat, "best "+scoreStyle(r.BestScore()).Render(fmt.Sprintf("%.1f%%", r.BestScore()*100)))
	}
	stat = append(stat, fmt.Sprintf("%d iterations", len(r.Iterations)), progress.FmtDuration(r.Duration()))
	fmt.Fprintln(&b, strings.Join(stat, mutedStyle.Render(" · ")))

	if len(r.Iterations) > 0 {
		fmt.Fprintf(&b, "\n%s\n", eyebrow("iterations"))
		var rows [][]string
		prev := -1.0
		for i := range r.Iterations {
			it := &r.Iterations[i]
			delta := ""
			if i > 0 && it.Score >= 0 && prev >= 0 {
				delta = fmt.Sprintf("%+.1f", (it.Score-prev)*100)
			}
			rows = append(rows, []string{
				fmt.Sprintf("iteration-%03d", it.Index),
				scoreCell(it.Score),
				mutedStyle.Render(delta),
				mutedStyle.Render(progress.FmtDuration(it.DurationMs)),
			})
			if it.Score >= 0 {
				prev = it.Score
			}
		}
		fmt.Fprint(&b, renderTable(
			[]string{"iteration", "score", "Δ", "time"},
			[]lipgloss.Position{lipgloss.Left, lipgloss.Right, lipgloss.Right, lipgloss.Right},
			rows))
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
		fmt.Fprintf(&b, "\n%s\n%s\n", eyebrow("experiment"), wrap(it.Experiment, m.wrapWidth()))
	}

	if len(it.Scenarios) > 0 {
		fmt.Fprintf(&b, "\n%s\n", eyebrow("scenarios"))
		var rows [][]string
		for i := range it.Scenarios {
			sc := &it.Scenarios[i]
			rows = append(rows, []string{sc.ID, scoreCell(sc.Score), mutedStyle.Render(progress.FmtDuration(sc.Result.DurationMs))})
		}
		fmt.Fprint(&b, renderTable(
			[]string{"scenario", "score", "time"},
			[]lipgloss.Position{lipgloss.Left, lipgloss.Right, lipgloss.Right},
			rows))
	}

	// SKILL.md diff vs previous iteration.
	fmt.Fprintf(&b, "\n%s\n", eyebrow("SKILL.md changes"))
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
			fmt.Fprintf(&b, "   %s", progress.FmtDuration(sc.Result.DurationMs))
		}
		fmt.Fprintln(&b)
	}

	// Live stream (if this scenario is currently producing output).
	if buf := m.streams[r.key]; buf != nil && buf.Len() > 0 {
		fmt.Fprintf(&b, "\n%s\n%s\n", eyebrow("live output"), buf.String())
	}

	if len(sc.Result.EvalResults) > 0 {
		fmt.Fprintf(&b, "\n%s\n", eyebrow("evals"))
		fmt.Fprint(&b, evalsTable(sc.Result.EvalResults))
	}
	return b.String()
}

// evalsTable renders the per-eval table (name+flags, score, time).
func evalsTable(evals []types.EvalResult) string {
	var rows [][]string
	for i := range evals {
		ev := &evals[i]
		flags := ""
		if ev.Required {
			flags += " [req]"
		}
		if ev.IsLLMJudge {
			flags += " [llm]"
		}
		rows = append(rows, []string{
			ev.Name + mutedStyle.Render(flags),
			scoreCell(ev.Score),
			mutedStyle.Render(progress.FmtDuration(ev.DurationMs)),
		})
	}
	return renderTable(
		[]string{"eval", "score", "time"},
		[]lipgloss.Position{lipgloss.Left, lipgloss.Right, lipgloss.Right},
		rows)
}

// detailGroup renders a short summary for a selected scenario section header.
func (m *model) detailGroup(r *row) string {
	sc := r.scen
	var b strings.Builder
	switch r.groupID {
	case "evals":
		fmt.Fprintln(&b, titleStyle.Render("evals"))
		fmt.Fprint(&b, evalsTable(sc.Result.EvalResults))
	case "transcripts":
		fmt.Fprintln(&b, titleStyle.Render("transcripts"))
		fmt.Fprint(&b, fileTable(sc.Transcripts))
	case "files":
		fmt.Fprintln(&b, titleStyle.Render("files"))
		fmt.Fprint(&b, fileTable(sc.Files))
	}
	fmt.Fprintf(&b, "\n%s\n", mutedStyle.Render("expand to view items"))
	return b.String()
}

// fileTable renders one row per file label.
func fileTable(files []runs.File) string {
	var rows [][]string
	for i := range files {
		rows = append(rows, []string{files[i].Label})
	}
	return renderTable([]string{"file"}, []lipgloss.Position{lipgloss.Left}, rows)
}

func (m *model) detailEval(ev *types.EvalResult) string {
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
		fmt.Fprintf(&b, "\n%s\n%s\n", eyebrow("reasoning"), wrap(ev.Reasoning, m.wrapWidth()))
	}
	return b.String()
}

// scoreCell renders a 0..1 score as a colored "%.1f" cell, or "" when absent.
func scoreCell(s float64) string {
	if s < 0 {
		return ""
	}
	return scoreStyle(s).Render(fmt.Sprintf("%.1f", s*100))
}

// renderTable renders rows in aligned columns under a muted header row. Cells may
// contain ANSI styling; column widths are measured with lipgloss.Width so color
// never breaks alignment. align[c]==lipgloss.Right right-justifies that column.
func renderTable(headers []string, align []lipgloss.Position, rows [][]string) string {
	n := len(headers)
	w := make([]int, n)
	for c := 0; c < n; c++ {
		w[c] = lipgloss.Width(headers[c])
	}
	for _, row := range rows {
		for c := 0; c < n && c < len(row); c++ {
			if x := lipgloss.Width(row[c]); x > w[c] {
				w[c] = x
			}
		}
	}
	pad := func(s string, width int, a lipgloss.Position) string {
		gap := width - lipgloss.Width(s)
		if gap < 0 {
			gap = 0
		}
		if a == lipgloss.Right {
			return strings.Repeat(" ", gap) + s
		}
		return s + strings.Repeat(" ", gap)
	}
	writeRow := func(b *strings.Builder, cells []string, style func(string) string) {
		b.WriteString("  ")
		for c := 0; c < n; c++ {
			if c > 0 {
				b.WriteString("  ")
			}
			cell := ""
			if c < len(cells) {
				cell = cells[c]
			}
			b.WriteString(style(pad(cell, w[c], align[c])))
		}
		b.WriteByte('\n')
	}

	var b strings.Builder
	writeRow(&b, headers, func(s string) string { return mutedStyle.Render(s) })
	for _, row := range rows {
		writeRow(&b, row, func(s string) string { return s })
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
