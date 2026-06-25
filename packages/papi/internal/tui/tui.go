// Package tui implements the interactive terminal UI for papi: a skill picker
// plus a two-pane browser (collapsible tree + detail) that watches a live run and
// browses past runs from the .papi/skills/<skill>/runs directory.
package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"papi/internal/appconfig"
	"papi/internal/loop"
	"papi/internal/progress"
	"papi/internal/runs"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Run starts the TUI against the given repo root.
func Run(repoRoot string) error {
	m, err := newModel(repoRoot)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

type mode int

const (
	modePicker mode = iota
	modeBrowse
)

type focusPane int

const (
	paneTree focusPane = iota
	paneDetail
	paneLog
)

// logStripHeight is the number of content lines shown in the bottom log strip.
const logStripHeight = 4

type model struct {
	repoRoot string

	mode   mode
	picker list.Model

	// browse state
	skillName string
	pastRuns  []runs.Run
	live      *runs.Run
	liveActive bool
	scenarioIDs []string

	liveStatus map[string]string
	streams    map[string]*strings.Builder

	expanded    map[string]bool
	rows        []row
	cursor      int
	treeOffset  int
	follow      bool
	activeKey   string
	selectedKey string

	focus    focusPane
	detailVP viewport.Model
	logVP    viewport.Model
	logs     []string

	events chan progress.Event
	done   chan error
	cancel context.CancelFunc

	width, height int
	treeWidth     int

	confirmStop bool
	statusMsg   string
	err         error
}

type skillItem struct{ s runs.Skill }

func (i skillItem) Title() string { return i.s.Name }
func (i skillItem) Description() string {
	if !i.s.Runnable {
		return "no scenarios — not runnable"
	}
	if i.s.LastRun == "" {
		return "runnable · no runs yet"
	}
	return fmt.Sprintf("runnable · last run %s · best %.1f%%", i.s.LastRun, i.s.BestScore*100)
}
func (i skillItem) FilterValue() string { return i.s.Name }

func newModel(repoRoot string) (*model, error) {
	skills, err := runs.ListSkills(repoRoot)
	if err != nil {
		return nil, err
	}
	items := make([]list.Item, len(skills))
	for i, s := range skills {
		items[i] = skillItem{s}
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "papi — select a skill"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)

	return &model{
		repoRoot:   repoRoot,
		mode:       modePicker,
		picker:     l,
		liveStatus: map[string]string{},
		streams:    map[string]*strings.Builder{},
		expanded:   map[string]bool{},
	}, nil
}

func (m *model) Init() tea.Cmd { return nil }

// --- messages ---

type evMsg struct{ e progress.Event }
type runClosedMsg struct{ err error }

func waitForEvent(events chan progress.Event, done chan error) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-events
		if !ok {
			return runClosedMsg{err: <-done}
		}
		return evMsg{e}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.treeWidth = msg.Width * 2 / 5
		if m.treeWidth < 28 {
			m.treeWidth = 28
		}
		m.picker.SetSize(msg.Width, msg.Height-1)
		m.detailVP = viewport.New(m.detailInnerWidth(), m.paneInnerHeight())
		m.logVP = viewport.New(m.logInnerWidth(), m.logInnerHeight())
		m.refreshDetail(true)
		m.refreshLog()
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		if m.mode == modePicker {
			return m.updatePicker(msg)
		}
		return m.updateBrowse(msg)

	case evMsg:
		m.applyEvent(msg.e)
		return m, waitForEvent(m.events, m.done)

	case runClosedMsg:
		m.liveActive = false
		if msg.err != nil {
			m.statusMsg = "run ended: " + msg.err.Error()
		} else {
			m.statusMsg = "run complete"
		}
		if m.live != nil {
			m.liveStatus[runKey(m.live.Timestamp)] = "done"
		}
		m.rebuild(false)
		return m, nil
	}

	if m.mode == modePicker {
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the filter input is active, let the list consume keystrokes.
	if m.picker.FilterState() != list.Filtering {
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "enter":
			if item, ok := m.picker.SelectedItem().(skillItem); ok {
				if !item.s.Runnable {
					m.openBrowse(item.s.Name)
					return m, nil
				}
				return m, m.startRun(item.s.Name)
			}
		case "b":
			if item, ok := m.picker.SelectedItem().(skillItem); ok {
				m.openBrowse(item.s.Name)
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

func (m *model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmStop {
		switch msg.String() {
		case "y", "Y":
			if m.cancel != nil {
				m.cancel()
			}
			m.statusMsg = "stopping…"
			m.confirmStop = false
		case "n", "N", "esc":
			m.confirmStop = false
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "esc":
		if m.liveActive {
			m.statusMsg = "run in progress — press s to stop, or wait"
			return m, nil
		}
		m.mode = modePicker
		m.reloadSkills()
		return m, nil
	case "tab":
		m.focus = (m.focus + 1) % 3 // tree → detail → log → tree
		return m, nil
	case "s":
		if m.liveActive {
			m.confirmStop = true
		}
		return m, nil
	case "g":
		if m.activeKey != "" {
			m.follow = true
			m.expandAncestors(m.activeKey)
			m.rebuild(false)
			m.selectKey(m.activeKey)
		}
		return m, nil
	}

	if m.focus == paneDetail {
		var cmd tea.Cmd
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd
	}
	if m.focus == paneLog {
		var cmd tea.Cmd
		m.logVP, cmd = m.logVP.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.follow = false
			m.afterCursorMove()
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			m.follow = false
			m.afterCursorMove()
		}
	case "home":
		m.cursor = 0
		m.follow = false
		m.afterCursorMove()
	case "end":
		m.cursor = len(m.rows) - 1
		m.follow = false
		m.afterCursorMove()
	case " ", "enter":
		if m.cursor < len(m.rows) {
			r := m.rows[m.cursor]
			if r.expandable {
				m.expanded[r.key] = !m.expanded[r.key]
				m.rebuild(false)
				m.selectKey(r.key)
			} else if msg.String() == "enter" {
				m.focus = paneDetail
			}
		}
	}
	return m, nil
}

func (m *model) afterCursorMove() {
	if m.cursor < len(m.rows) {
		m.selectedKey = m.rows[m.cursor].key
	}
	m.ensureCursorVisible()
	m.refreshDetail(true)
}

// --- run lifecycle ---

func (m *model) startRun(skill string) tea.Cmd {
	cfg, err := appconfig.Build(m.repoRoot, skill)
	if err != nil {
		m.err = err
		return nil
	}
	m.openBrowse(skill)
	m.live = nil
	m.liveActive = true
	m.follow = true
	m.events = make(chan progress.Event, 128)
	m.done = make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	repoRoot := m.repoRoot
	events := m.events
	done := m.done
	go func() {
		err := loop.Run(ctx, cfg, repoRoot, progress.NewChannelReporter(events), true)
		done <- err
		close(events)
	}()
	return waitForEvent(events, done)
}

func (m *model) openBrowse(skill string) {
	m.mode = modeBrowse
	m.skillName = skill
	m.focus = paneTree
	m.cursor = 0
	m.treeOffset = 0
	m.statusMsg = ""
	m.logs = nil
	m.refreshLog()
	m.liveStatus = map[string]string{}
	m.streams = map[string]*strings.Builder{}
	m.expanded = map[string]bool{}
	rs, _ := runs.ListRuns(m.repoRoot, skill)
	m.pastRuns = rs
	// Expand the most recent past run by default.
	if len(rs) > 0 {
		m.expanded[runKey(rs[len(rs)-1].Timestamp)] = true
	}
	m.rebuild(true)
	m.refreshDetail(true)
}

func (m *model) reloadSkills() {
	skills, _ := runs.ListSkills(m.repoRoot)
	items := make([]list.Item, len(skills))
	for i, s := range skills {
		items[i] = skillItem{s}
	}
	m.picker.SetItems(items)
}

// --- event application ---

func (m *model) applyEvent(e progress.Event) {
	switch ev := e.(type) {
	case progress.RunStarted:
		m.scenarioIDs = ev.ScenarioIDs
		m.live = &runs.Run{Timestamp: ev.Timestamp}
		rk := runKey(ev.Timestamp)
		m.liveStatus[rk] = "running"
		m.expanded[rk] = true
		m.activeKey = rk

	case progress.IterationStarted:
		it := runs.Iteration{
			Index: ev.Iter,
			Dir:   m.iterDir(ev.Iter),
			Score: -1,
		}
		for _, id := range m.scenarioIDs {
			it.Scenarios = append(it.Scenarios, runs.Scenario{ID: id, Score: -1})
		}
		m.live.Iterations = append(m.live.Iterations, it)
		ik := iterKey(runKey(m.live.Timestamp), ev.Iter)
		m.liveStatus[ik] = "running"
		m.setActive(ik)

	case progress.ResearchAgentDone:
		if it := m.liveIter(ev.Iter); it != nil {
			it.Experiment = ev.Description
		}

	case progress.ScenarioStarted:
		sk := scenKey(iterKey(runKey(m.live.Timestamp), ev.Iter), ev.ID)
		m.liveStatus[sk] = "invocation…"
		m.setActive(sk)

	case progress.PhaseChanged:
		sk := scenKey(iterKey(runKey(m.live.Timestamp), ev.Iter), ev.ID)
		m.liveStatus[sk] = string(ev.Phase) + "…"

	case progress.StreamChunk:
		sk := scenKey(iterKey(runKey(m.live.Timestamp), ev.Iter), ev.ID)
		buf := m.streams[sk]
		if buf == nil {
			buf = &strings.Builder{}
			m.streams[sk] = buf
		}
		buf.WriteString(ev.Text)
		m.liveStatus[sk] = string(ev.Phase) + "…"

	case progress.EvalDone:
		if sc := m.liveScenario(ev.Iter, ev.ScenarioID); sc != nil {
			sc.Result.EvalResults = append(sc.Result.EvalResults, ev.Eval)
		}

	case progress.ScenarioDone:
		if sc := m.liveScenario(ev.Iter, ev.Result.Scenario.ID); sc != nil {
			sc.Score = ev.Result.ScenarioScore
			sc.Invoked = ev.Result.Invoked
			sc.Result = ev.Result
			sc.Transcripts, sc.Files = runs.ScenarioArtifacts(filepath.Join(m.iterDir(ev.Iter), ev.Result.Scenario.ID))
		}
		sk := scenKey(iterKey(runKey(m.live.Timestamp), ev.Iter), ev.Result.Scenario.ID)
		delete(m.liveStatus, sk)
		delete(m.streams, sk)

	case progress.IterationDone:
		if it := m.liveIter(ev.Iter); it != nil {
			it.Score = ev.Score
		}
		delete(m.liveStatus, iterKey(runKey(m.live.Timestamp), ev.Iter))

	case progress.RunDone:
		m.liveActive = false
		m.liveStatus[runKey(m.live.Timestamp)] = "done"
		if ev.Error != "" {
			m.statusMsg = "error: " + ev.Error
		}

	case progress.LogLine:
		if t := strings.TrimSpace(ev.Text); t != "" {
			m.appendLog(t)
		}
	}

	keepTop := m.selectedKey
	m.rebuild(false)
	if m.follow && m.activeKey != "" {
		m.expandAncestors(m.activeKey)
		m.rebuild(false)
		m.selectKey(m.activeKey)
	} else {
		m.selectKey(keepTop)
	}
	m.refreshDetail(false)

	// Keep streamed output in view while following the live node.
	if _, ok := e.(progress.StreamChunk); ok && m.follow {
		m.detailVP.GotoBottom()
	}
}

func (m *model) setActive(key string) {
	m.activeKey = key
	if m.follow {
		m.expandAncestors(key)
	}
}

func (m *model) liveIter(idx int) *runs.Iteration {
	if m.live == nil {
		return nil
	}
	for i := range m.live.Iterations {
		if m.live.Iterations[i].Index == idx {
			return &m.live.Iterations[i]
		}
	}
	return nil
}

func (m *model) liveScenario(iter int, id string) *runs.Scenario {
	it := m.liveIter(iter)
	if it == nil {
		return nil
	}
	for i := range it.Scenarios {
		if it.Scenarios[i].ID == id {
			return &it.Scenarios[i]
		}
	}
	return nil
}

func (m *model) iterDir(iter int) string {
	return filepath.Join(m.repoRoot, ".papi", "skills", m.skillName, "runs",
		m.live.Timestamp, fmt.Sprintf("iteration-%03d", iter))
}

// --- tree helpers ---

func (m *model) rebuild(resetCursor bool) {
	m.rows = m.buildRows()
	if resetCursor {
		m.cursor = 0
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor < len(m.rows) {
		m.selectedKey = m.rows[m.cursor].key
	}
	m.ensureCursorVisible()
}

func (m *model) selectKey(key string) {
	for i, r := range m.rows {
		if r.key == key {
			m.cursor = i
			m.selectedKey = key
			m.ensureCursorVisible()
			return
		}
	}
}

// expandAncestors expands every ancestor of key so it becomes visible.
func (m *model) expandAncestors(key string) {
	parts := strings.Split(key, "/")
	acc := ""
	for i, p := range parts {
		if i == 0 {
			acc = p
		} else {
			acc += "/" + p
		}
		if i < len(parts)-1 {
			m.expanded[acc] = true
		}
	}
}

func (m *model) ensureCursorVisible() {
	h := m.paneInnerHeight()
	if h <= 0 {
		return
	}
	if m.cursor < m.treeOffset {
		m.treeOffset = m.cursor
	}
	if m.cursor >= m.treeOffset+h {
		m.treeOffset = m.cursor - h + 1
	}
	if m.treeOffset < 0 {
		m.treeOffset = 0
	}
}

// refreshDetail re-renders the detail pane for the selected row. When force is
// true (selection changed) the viewport scrolls back to the top; otherwise the
// scroll position is preserved (e.g. while live output streams in).
func (m *model) refreshDetail(force bool) {
	if m.detailVP.Width == 0 {
		return
	}
	var content string
	if m.cursor < len(m.rows) {
		r := m.rows[m.cursor]
		content = m.detailContent(&r)
	}
	m.detailVP.SetContent(content)
	if force {
		m.detailVP.GotoTop()
	}
}

// --- layout / view ---

func (m *model) detailInnerWidth() int {
	w := m.width - m.treeWidth - 4
	if w < 10 {
		w = 10
	}
	return w
}

func (m *model) paneInnerHeight() int {
	// header(1) + body border(2) + log strip(title 1 + logStripHeight + border 2) + footer(1)
	h := m.height - 7 - logStripHeight
	if h < 3 {
		h = 3
	}
	return h
}

func (m *model) logInnerWidth() int {
	w := m.width - 2
	if w < 10 {
		w = 10
	}
	return w
}

func (m *model) logInnerHeight() int { return logStripHeight }

func (m *model) appendLog(line string) {
	for _, l := range strings.Split(line, "\n") {
		m.logs = append(m.logs, l)
	}
	const maxLogs = 500
	if len(m.logs) > maxLogs {
		m.logs = m.logs[len(m.logs)-maxLogs:]
	}
	m.refreshLog()
	m.logVP.GotoBottom()
}

func (m *model) refreshLog() {
	if m.logVP.Width == 0 {
		return
	}
	if len(m.logs) == 0 {
		m.logVP.SetContent(mutedStyle.Render("(no output)"))
		return
	}
	m.logVP.SetContent(strings.Join(m.logs, "\n"))
}

func (m *model) View() string {
	if m.err != nil {
		return "Error: " + m.err.Error() + "\n"
	}
	if m.mode == modePicker {
		return m.picker.View()
	}
	return m.browseView()
}

func (m *model) browseView() string {
	h := m.paneInnerHeight()

	// Tree pane.
	var tb strings.Builder
	end := m.treeOffset + h
	if end > len(m.rows) {
		end = len(m.rows)
	}
	for i := m.treeOffset; i < end; i++ {
		tb.WriteString(renderRow(m.rows[i], i == m.cursor && m.focus == paneTree, m.treeWidth))
		tb.WriteByte('\n')
	}
	treePane := lipgloss.NewStyle().Width(m.treeWidth).Height(h).Render(tb.String())
	detailPane := lipgloss.NewStyle().Width(m.detailInnerWidth()).Height(h).Render(m.detailVP.View())

	treeBox := paneInactiveBorder
	detailBox := paneInactiveBorder
	if m.focus == paneTree {
		treeBox = paneActiveBorder
	} else {
		detailBox = paneActiveBorder
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, treeBox.Render(treePane), detailBox.Render(detailPane))

	// Bottom log strip.
	logBox := paneInactiveBorder
	if m.focus == paneLog {
		logBox = paneActiveBorder
	}
	logTitle := lipgloss.NewStyle().Width(m.logInnerWidth()).Render(mutedStyle.Render("log"))
	logContent := lipgloss.NewStyle().Width(m.logInnerWidth()).Height(m.logInnerHeight()).Render(m.logVP.View())
	logStrip := logBox.Render(logTitle + "\n" + logContent)

	header := titleStyle.Render("papi · " + m.skillName)
	if m.liveActive {
		header += "  " + liveBadgeStyle.Render("● running")
	}

	return header + "\n" + body + "\n" + logStrip + "\n" + m.footer()
}

func (m *model) footer() string {
	if m.confirmStop {
		return footerStyle.Render("Stop the running loop? (y/n)")
	}
	keys := "↑/↓ move · space fold · enter open · tab pane · g live · q back"
	if m.liveActive {
		keys += " · s stop"
	}
	line := footerStyle.Render(keys)
	if m.statusMsg != "" {
		line = footerStyle.Render(m.statusMsg) + "  " + line
	}
	return line
}
