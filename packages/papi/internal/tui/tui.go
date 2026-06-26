// Package tui implements the interactive terminal UI for papi: a skill picker
// plus a two-pane browser (collapsible tree + detail) that watches a live run and
// browses past runs from the .papi/skills/<skill>/runs directory.
package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
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
	skillName     string
	skillRunnable bool
	pastRuns      []runs.Run
	live          *runs.Run
	liveActive    bool
	scenarioIDs   []string

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
	logs     []logEntry

	logModal   bool
	logModalVP viewport.Model

	events chan progress.Event
	done   chan error
	cancel context.CancelFunc

	width, height int
	treeWidth     int

	confirmStop      bool
	confirmStart     bool
	confirmResume    bool
	confirmQuit      bool
	confirmStartMsg  string
	confirmResumeMsg string
	resumeTs         string
	statusMsg        string
	err              error
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
		if m.logModal {
			w, h := m.logModalSize()
			m.logModalVP = viewport.New(w, h)
			m.setLogModalContent()
		}
		return m, nil

	case tea.KeyMsg:
		if m.confirmQuit {
			switch msg.String() {
			case "y", "Y", "ctrl+c": // second ctrl+c = force quit
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "n", "N", "esc":
				m.confirmQuit = false
			}
			return m, nil
		}
		if msg.String() == "ctrl+c" {
			if m.liveActive { // run in progress → confirm first
				m.confirmQuit = true
				return m, nil
			}
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
			// Selecting a skill only opens its runs read-only; starting a run is
			// an explicit, confirmed action ('r') inside the browse view.
			if item, ok := m.picker.SelectedItem().(skillItem); ok {
				m.openBrowse(item.s.Name, item.s.Runnable)
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

	if m.confirmStart {
		switch msg.String() {
		case "y", "Y":
			m.confirmStart = false
			return m, m.startRun(m.skillName)
		case "n", "N", "esc":
			m.confirmStart = false
		}
		return m, nil
	}

	if m.confirmResume {
		switch msg.String() {
		case "y", "Y":
			m.confirmResume = false
			return m, m.startResume(m.skillName, m.resumeTs)
		case "n", "N", "esc":
			m.confirmResume = false
		}
		return m, nil
	}

	if m.logModal {
		switch msg.String() {
		case "esc", "q", "enter":
			m.logModal = false
			return m, nil
		}
		var cmd tea.Cmd
		m.logModalVP, cmd = m.logModalVP.Update(msg)
		return m, cmd
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
	case "r":
		switch {
		case m.liveActive:
			m.statusMsg = "run already in progress"
		case !m.skillRunnable:
			m.statusMsg = "skill has no scenarios — not runnable"
		default:
			cfg, err := appconfig.Build(m.repoRoot, m.skillName)
			if err != nil {
				m.statusMsg = err.Error()
			} else {
				m.confirmStartMsg = fmt.Sprintf("%s — %d iterations, $%.2f budget",
					m.skillName, cfg.MaxIterations, cfg.MaxBudgetUSD)
				m.confirmStart = true
			}
		}
		return m, nil
	case "c":
		if r, ok := m.selectedResumableRun(); ok {
			m.resumeTs = r.Timestamp
			m.confirmResumeMsg = fmt.Sprintf("run %s — continue from iteration %d (best %.1f%%)",
				r.Timestamp, r.State.LastCompletedIteration+1, r.State.BestScore)
			m.confirmResume = true
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
		if msg.String() == "enter" {
			m.openLogModal()
			return m, nil
		}
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
				m.refreshLog()
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
	m.refreshLog()
}

// --- run lifecycle ---

func (m *model) startRun(skill string) tea.Cmd {
	cfg, err := appconfig.Build(m.repoRoot, skill)
	if err != nil {
		m.err = err
		return nil
	}
	m.openBrowse(skill, true)
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

// startResume continues an unfinished run (timestamp) instead of starting a fresh
// one. The resumed run is removed from pastRuns and re-seeded as the live run when
// the loop emits RunStarted.
func (m *model) startResume(skill, timestamp string) tea.Cmd {
	cfg, err := appconfig.Build(m.repoRoot, skill)
	if err != nil {
		m.err = err
		return nil
	}
	cfg.Resume = true
	cfg.ResumeTimestamp = timestamp

	m.openBrowse(skill, true)
	// Drop the run we're resuming from pastRuns so it isn't duplicated; it becomes
	// the live run once RunStarted arrives.
	filtered := m.pastRuns[:0]
	for _, r := range m.pastRuns {
		if r.Timestamp != timestamp {
			filtered = append(filtered, r)
		}
	}
	m.pastRuns = filtered

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

func (m *model) openBrowse(skill string, runnable bool) {
	m.mode = modeBrowse
	m.skillName = skill
	m.skillRunnable = runnable
	m.confirmStart = false
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

// selectedResumableRun returns the run for the currently selected tree node when
// that node is a (non-live) run node whose run can be resumed, and no run is active.
func (m *model) selectedResumableRun() (*runs.Run, bool) {
	if m.liveActive {
		return nil, false
	}
	runTs, iter, scen, _ := parseSelKey(m.selectedKey)
	if runTs == "" || iter != -1 || scen != "" {
		return nil, false
	}
	for i := range m.pastRuns {
		if m.pastRuns[i].Timestamp == runTs && m.pastRuns[i].Resumable() {
			return &m.pastRuns[i], true
		}
	}
	return nil, false
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
		if ev.ResumeFrom > 0 {
			// Resuming: load the already-completed iterations from disk so the run's
			// history stays visible while the remaining iterations stream in.
			runDir := filepath.Join(m.repoRoot, ".papi", "skills", m.skillName, "runs", ev.Timestamp)
			if r, err := runs.LoadRun(runDir); err == nil {
				m.live = &r
			} else {
				m.live = &runs.Run{Timestamp: ev.Timestamp}
			}
		} else {
			m.live = &runs.Run{Timestamp: ev.Timestamp}
		}
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
			it.DurationMs = ev.DurationMs
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
			runTs := ""
			if m.live != nil {
				runTs = m.live.Timestamp
			}
			m.appendLog(logEntry{
				runTs:      runTs,
				iter:       ev.Iter,
				scenarioID: ev.ScenarioID,
				evalID:     ev.EvalID,
				text:       t,
			})
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
	m.refreshLog()

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

// logModalSize returns the inner width/height of the fullscreen log viewport,
// leaving room for the modalBox border + Padding(1,2), a title line, and a hint line.
func (m *model) logModalSize() (w, h int) {
	w = m.width - 8
	if w < 20 {
		w = 20
	}
	// border(2) + vertical padding(2) + title(1) + blank(1) + blank(1) + hint(1) = 8
	h = m.height - 8
	if h < 3 {
		h = 3
	}
	return w, h
}

// openLogModal builds the fullscreen scrollable log overlay from the same
// node-filtered content as the strip, scrolled to the newest line.
func (m *model) openLogModal() {
	w, h := m.logModalSize()
	m.logModalVP = viewport.New(w, h)
	m.setLogModalContent()
	m.logModal = true
}

func (m *model) setLogModalContent() {
	content, shown := m.filteredLogContent()
	if shown == 0 {
		content = mutedStyle.Render("(no output)")
	}
	m.logModalVP.SetContent(content)
	m.logModalVP.GotoBottom()
}

// logEntry is one captured log line plus the run-hierarchy scope it belongs to,
// so the log panel can be filtered to the currently selected tree node.
type logEntry struct {
	runTs      string // m.live.Timestamp at append time ("" = global/pre-run)
	iter       int    // iteration index; -1 = run-level
	scenarioID string // "" = not scenario-specific
	evalID     string // "" = not eval-specific
	text       string
}

func (m *model) appendLog(e logEntry) {
	for _, l := range strings.Split(e.text, "\n") {
		le := e
		le.text = l
		m.logs = append(m.logs, le)
	}
	const maxLogs = 500
	if len(m.logs) > maxLogs {
		m.logs = m.logs[len(m.logs)-maxLogs:]
	}
	m.refreshLog()
}

// parseSelKey decodes a tree node key (r:ts/i:idx/s:id/e:evalid, f: segments
// ignored) into the scope it represents. iter is -1 when the key has no i:
// segment (the run node ⇒ no iteration constraint).
func parseSelKey(key string) (runTs string, iter int, scen, eval string) {
	iter = -1
	for _, part := range strings.Split(key, "/") {
		switch {
		case strings.HasPrefix(part, "r:"):
			runTs = part[2:]
		case strings.HasPrefix(part, "i:"):
			iter, _ = strconv.Atoi(part[2:])
		case strings.HasPrefix(part, "s:"):
			scen = part[2:]
		case strings.HasPrefix(part, "e:"):
			eval = part[2:]
		}
	}
	return
}

// refreshLog re-renders the log panel, filtered to the selected node's scope. An
// entry shows when it agrees with every constraint the node sets; a node higher
// in the hierarchy (e.g. the run) sets fewer constraints and so shows more.
func (m *model) refreshLog() {
	if m.logVP.Width == 0 {
		return
	}
	content, shown := m.filteredLogContent()
	if shown == 0 {
		m.logVP.SetContent(mutedStyle.Render("(no output)"))
		return
	}
	m.logVP.SetContent(content)
	m.logVP.GotoBottom()
}

// filteredLogContent renders the log lines visible for the selected node's scope
// (the same filter the strip uses), returning the joined text and the count shown.
// Shared by the inline strip (refreshLog) and the fullscreen log modal.
func (m *model) filteredLogContent() (string, int) {
	nodeRunTs, nodeIter, nodeScen, nodeEval := parseSelKey(m.selectedKey)
	var b strings.Builder
	shown := 0
	for _, e := range m.logs {
		if nodeIter >= 0 && e.iter != nodeIter {
			continue
		}
		if nodeScen != "" && e.scenarioID != nodeScen {
			continue
		}
		if nodeEval != "" && e.evalID != nodeEval {
			continue
		}
		// Past-run nodes have no logs; the live run shows its own; pre-run global
		// lines (no runTs) always show.
		if e.runTs != "" && nodeRunTs != "" && e.runTs != nodeRunTs {
			continue
		}
		if shown > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(formatLogLine(e))
		shown++
	}
	return b.String(), shown
}

// formatLogLine renders a log entry with compact, dimmed scope tags: [run <short>]
// always, then [iter N], [scenario], [eval] only when that level applies.
func formatLogLine(e logEntry) string {
	var tags []string
	if e.runTs != "" {
		tags = append(tags, "run "+shortRunTag(e.runTs))
	}
	if e.iter >= 0 {
		tags = append(tags, fmt.Sprintf("iter %d", e.iter))
	}
	if e.scenarioID != "" {
		tags = append(tags, e.scenarioID)
	}
	if e.evalID != "" {
		tags = append(tags, e.evalID)
	}
	if len(tags) == 0 {
		return e.text
	}
	prefix := ""
	for _, t := range tags {
		prefix += mutedStyle.Render("["+t+"]") + " "
	}
	return prefix + e.text
}

// shortRunTag abbreviates a long Unix-millis run timestamp to its last 4 digits.
func shortRunTag(ts string) string {
	if len(ts) > 4 {
		return ts[len(ts)-4:]
	}
	return ts
}

func (m *model) View() string {
	if m.err != nil {
		return "Error: " + m.err.Error() + "\n"
	}
	if m.mode == modePicker {
		return m.picker.View()
	}
	view := m.browseView()
	if m.confirmStart || m.confirmResume || m.confirmStop || m.confirmQuit {
		view = overlayCenter(view, m.modalView(), m.width, m.height)
	} else if m.logModal {
		view = overlayCenter(view, m.logModalView(), m.width, m.height)
	}
	return view
}

// modalView builds the centered confirmation dialog for starting or stopping a run.
func (m *model) modalView() string {
	title := "Start a run?"
	body := m.confirmStartMsg
	if m.confirmResume {
		title = "Continue run?"
		body = m.confirmResumeMsg
	}
	if m.confirmStop {
		title = "Stop run?"
		body = "Stop the running loop?"
	}
	if m.confirmQuit {
		title = "Stop run and quit?"
		body = "A run is in progress. Quitting will stop it."
	}

	width := 56
	if m.width-8 < width {
		width = m.width - 8
	}
	if width < 20 {
		width = 20
	}

	prompt := yesStyle.Render("[y] Yes") + "    " + noStyle.Render("[n] No") +
		"    " + mutedStyle.Render("(esc cancel)")
	inner := lipgloss.NewStyle().Width(width).Render(
		titleStyle.Render(title) + "\n\n" + body + "\n\n" + prompt)
	return modalBox.Render(inner)
}

// logModalView builds the centered, near-fullscreen scrollable log overlay.
func (m *model) logModalView() string {
	w, _ := m.logModalSize()
	title := titleStyle.Render("log · " + m.skillName)
	hint := mutedStyle.Render("↑/↓ pgup/pgdn scroll · esc close")
	inner := lipgloss.NewStyle().Width(w).Render(
		title + "\n\n" + m.logModalVP.View() + "\n\n" + hint)
	return modalBox.Render(inner)
}

// overlayCenter composites box centered over base (sized w×h) by replacing the
// center band of rows with the box's lines. Whole-row replacement keeps the
// surrounding tree/detail visible above and below without intra-line ANSI splicing.
func overlayCenter(base, box string, w, h int) string {
	lines := strings.Split(base, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	boxLines := strings.Split(box, "\n")
	left := (w - lipgloss.Width(box)) / 2
	if left < 0 {
		left = 0
	}
	top := (h - len(boxLines)) / 2
	if top < 0 {
		top = 0
	}
	pad := strings.Repeat(" ", left)
	for i, bl := range boxLines {
		row := top + i
		if row >= 0 && row < len(lines) {
			lines[row] = pad + bl
		}
	}
	return strings.Join(lines, "\n")
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
	keys := "↑/↓ move · space fold · enter open · tab pane · g live · q back"
	if m.focus == paneLog {
		keys += " · enter expand"
	}
	if m.liveActive {
		keys += " · s stop"
	} else if m.skillRunnable {
		keys += " · r run"
		if _, ok := m.selectedResumableRun(); ok {
			keys += " · c continue"
		}
	}
	line := footerStyle.Render(keys)
	if m.statusMsg != "" {
		line = footerStyle.Render(m.statusMsg) + "  " + line
	}
	return line
}
