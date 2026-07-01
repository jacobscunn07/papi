// Package tui implements the interactive terminal UI for papi: a skill picker
// plus a two-pane browser (collapsible tree + detail) that watches a live run and
// browses past runs from the .papi/skills/<skill>/runs directory.
package tui

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"papi/internal/appconfig"
	"papi/internal/loop"
	"papi/internal/progress"
	"papi/internal/runs"
	"papi/internal/store"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Run starts the TUI against the given repo root.
func Run(repoRoot string) error {
	st, err := store.Open(repoRoot)
	if err != nil {
		return err
	}
	defer st.Close()
	m, err := newModel(repoRoot, st)
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
	store    *store.Store

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

	spinnerIdx int // animates while a run is live
	totalIters int // configured max iterations for the live run

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

// skillDelegate renders each picker row in the instrument identity: a bold name
// over a muted meta line carrying the best score and a mini score-trajectory
// sparkline of the skill's most recent run.
type skillDelegate struct{}

func (skillDelegate) Height() int                         { return 2 }
func (skillDelegate) Spacing() int                        { return 1 }
func (skillDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (skillDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(skillItem)
	if !ok {
		return
	}
	s := it.s
	width := m.Width()
	clip := lipgloss.NewStyle().MaxWidth(width)

	// Title line — selected rows get the same cyan bar as the browse tree.
	var title string
	if index == m.Index() {
		bar := "  " + s.Name
		if w := lipgloss.Width(bar); w < width {
			bar += strings.Repeat(" ", width-w)
		}
		title = selectedRowStyle.Render(bar)
	} else {
		title = "  " + valueStyle.Render(s.Name)
	}

	// Meta line.
	var meta string
	switch {
	case !s.Runnable:
		meta = mutedStyle.Render("not runnable")
	case s.LastRun == "":
		meta = mutedStyle.Render("runnable · no runs yet")
	default:
		meta = mutedStyle.Render("best ") +
			scoreStyle(s.BestScore).Render(fmt.Sprintf("%.1f%%", s.BestScore*100))
		if spark := sparkline(s.Trajectory, nil); spark != "" {
			meta += "  " + spark
		}
		meta += mutedStyle.Render("  last run " + s.LastRun)
	}

	fmt.Fprint(w, clip.Render(title)+"\n"+clip.Render("    "+meta))
}

// pickerView frames the skill list with the same header/footer language as the
// browse screen so the two screens share one identity.
func (m *model) pickerView() string {
	title := lipgloss.NewStyle().Foreground(colorAccent).Render("▌") +
		valueStyle.Render(" papi") + mutedStyle.Render(" · select a skill")
	runnable := 0
	for _, it := range m.picker.Items() {
		if s, ok := it.(skillItem); ok && s.s.Runnable {
			runnable++
		}
	}
	// Second header line keeps the picker the same height as the browse ribbon, so
	// the panel doesn't jump a row when switching screens.
	tagline := mutedStyle.Render("  self-improving skills via scored autoresearch")
	panel := titledPanel("skills", fmt.Sprintf("%d runnable", runnable), m.picker.View(), m.width-2, true)
	help := footerStyle.Render("↑/↓ move · / filter · enter open · q quit")
	return title + "\n" + tagline + "\n" + panel + "\n" + help
}

func newModel(repoRoot string, st *store.Store) (*model, error) {
	skills, err := runs.ListSkills(st)
	if err != nil {
		return nil, err
	}
	items := make([]list.Item, len(skills))
	for i, s := range skills {
		items[i] = skillItem{s}
	}
	l := list.New(items, skillDelegate{}, 0, 0)
	l.SetShowTitle(false) // we render our own instrument header
	l.SetShowHelp(false)  // and our own footer
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	// Fold the list chrome into the instrument palette.
	l.Styles.FilterPrompt = l.Styles.FilterPrompt.Foreground(colorAccent)
	l.Styles.FilterCursor = l.Styles.FilterCursor.Foreground(colorAccent)
	l.Styles.NoItems = l.Styles.NoItems.Foreground(colorMuted)
	l.Styles.ActivePaginationDot = l.Styles.ActivePaginationDot.Foreground(colorAccent)
	l.Styles.InactivePaginationDot = l.Styles.InactivePaginationDot.Foreground(colorBorder)

	return &model{
		repoRoot:   repoRoot,
		store:      st,
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
type spinnerTickMsg struct{}

// tickSpinner schedules the next spinner frame.
func tickSpinner() tea.Cmd {
	return tea.Tick(time.Second/10, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

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
		pickerH := msg.Height - 5 // identity + tagline + top border + bottom border + footer
		if pickerH < 1 {
			pickerH = 1
		}
		pickerW := msg.Width - 2 // list sits inside the panel's left/right borders
		if pickerW < 1 {
			pickerW = 1
		}
		m.picker.SetSize(pickerW, pickerH)
		m.detailVP = viewport.New(m.detailInnerWidth(), m.paneInnerHeight())
		m.logVP = viewport.New(m.detailInnerWidth(), m.logInnerHeight())
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

	case spinnerTickMsg:
		if !m.liveActive {
			return m, nil // stop ticking once the run ends
		}
		m.spinnerIdx++
		return m, tickSpinner()

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
			ts := m.live.Timestamp
			// Reload from the store so the just-stopped run moves from m.live into
			// pastRuns; its persisted Done=false checkpoint makes it resumable, so 'c'
			// continues it immediately without leaving the view. Disk writes are done by
			// now (the channel closes after loop.Run returns).
			if rs, err := runs.ListRuns(m.store, m.skillName); err == nil {
				m.pastRuns = rs
				m.live = nil
				m.rebuild(false)
				m.selectKey(runKey(ts)) // keep the run node selected so 'c' works
			} else {
				m.liveStatus[runKey(ts)] = "done"
				m.rebuild(false)
			}
		} else {
			m.rebuild(false)
		}
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
	m.totalIters = cfg.MaxIterations
	m.events = make(chan progress.Event, 128)
	m.done = make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	repoRoot := m.repoRoot
	st := m.store
	events := m.events
	done := m.done
	go func() {
		err := loop.Run(ctx, cfg, repoRoot, st, progress.NewChannelReporter(events), true)
		done <- err
		close(events)
	}()
	return tea.Batch(waitForEvent(events, done), tickSpinner())
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
	m.totalIters = cfg.MaxIterations
	m.events = make(chan progress.Event, 128)
	m.done = make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	repoRoot := m.repoRoot
	st := m.store
	events := m.events
	done := m.done
	go func() {
		err := loop.Run(ctx, cfg, repoRoot, st, progress.NewChannelReporter(events), true)
		done <- err
		close(events)
	}()
	return tea.Batch(waitForEvent(events, done), tickSpinner())
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
	rs, _ := runs.ListRuns(m.store, skill)
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
	skills, _ := runs.ListSkills(m.store)
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
			if r, err := runs.LoadRun(m.store, m.skillName, ev.Timestamp); err == nil {
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
			it.SetSkillMd(ev.SkillMd)
		}

	case progress.ScenarioStarted:
		sk := scenKey(iterKey(runKey(m.live.Timestamp), ev.Iter), ev.ID)
		m.liveStatus[sk] = "invocation"
		m.setActive(sk)

	case progress.PhaseChanged:
		sk := scenKey(iterKey(runKey(m.live.Timestamp), ev.Iter), ev.ID)
		m.liveStatus[sk] = string(ev.Phase)

	case progress.StreamChunk:
		sk := scenKey(iterKey(runKey(m.live.Timestamp), ev.Iter), ev.ID)
		buf := m.streams[sk]
		if buf == nil {
			buf = &strings.Builder{}
			m.streams[sk] = buf
		}
		buf.WriteString(ev.Text)
		m.liveStatus[sk] = string(ev.Phase)

	case progress.EvalDone:
		if sc := m.liveScenario(ev.Iter, ev.ScenarioID); sc != nil {
			sc.Result.EvalResults = append(sc.Result.EvalResults, ev.Eval)
		}

	case progress.ScenarioDone:
		if sc := m.liveScenario(ev.Iter, ev.Result.Scenario.ID); sc != nil {
			sc.Score = ev.Result.ScenarioScore
			sc.Invoked = ev.Result.Invoked
			sc.Result = ev.Result
			sc.Transcripts, sc.Files = runs.BuildScenarioArtifacts(filepath.Join(m.iterDir(ev.Iter), ev.Result.Scenario.ID), ev.Result)
		}
		sk := scenKey(iterKey(runKey(m.live.Timestamp), ev.Iter), ev.Result.Scenario.ID)
		delete(m.liveStatus, sk)
		delete(m.streams, sk)

	case progress.IterationDone:
		if it := m.liveIter(ev.Iter); it != nil {
			it.Score = ev.Score
			it.DurationMs = ev.DurationMs
			it.SetSkillMd(ev.SkillMd)
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
	h := m.treeInnerHeight()
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

// paneInnerHeight is the activity (detail) pane's content height — it shares the
// right column with the logs panel, so the log strip's rows are subtracted.
func (m *model) paneInnerHeight() int {
	// header(2) + activity border(2) + logs(logStripHeight + border 2) + footer(1)
	h := m.height - 7 - logStripHeight
	if h < 3 {
		h = 3
	}
	return h
}

// treeInnerHeight is the runs rail's content height — it spans the full body, so
// only the header(2), its own border(2), and footer(1) are subtracted.
func (m *model) treeInnerHeight() int {
	h := m.height - 5
	if h < 3 {
		h = 3
	}
	return h
}

// wrapWidth is the column width for word-wrapped prose in the detail pane.
func (m *model) wrapWidth() int {
	w := m.detailInnerWidth() - 1
	if w < 20 {
		w = 20
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
		m.logVP.SetContent(mutedStyle.Render("no output for this selection"))
		return
	}
	m.logVP.SetContent(content)
	m.logVP.GotoBottom()
}

// filteredLogContent renders the log lines visible for the selected node's scope
// (the same filter the strip uses), returning the joined text and the count shown.
// Shared by the inline strip (refreshLog) and the fullscreen log modal.
// logEntriesForRun returns the log entries to filter for the given run: the live
// in-memory buffer for the active run (or when no run is scoped), otherwise the
// selected past run's persisted logs converted to logEntry.
func (m *model) logEntriesForRun(runTs string) []logEntry {
	if runTs == "" || (m.live != nil && runTs == m.live.Timestamp) {
		return m.logs
	}
	for i := range m.pastRuns {
		if m.pastRuns[i].Timestamp == runTs {
			src := m.pastRuns[i].Logs
			out := make([]logEntry, 0, len(src))
			for _, le := range src {
				out = append(out, logEntry{
					runTs:      runTs,
					iter:       le.Iter,
					scenarioID: le.ScenarioID,
					evalID:     le.EvalID,
					text:       le.Text,
				})
			}
			return out
		}
	}
	return m.logs
}

func (m *model) filteredLogContent() (string, int) {
	nodeRunTs, nodeIter, nodeScen, nodeEval := parseSelKey(m.selectedKey)
	var b strings.Builder
	shown := 0
	for _, e := range m.logEntriesForRun(nodeRunTs) {
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
		return m.pickerView()
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
	// Runs rail spans the full body height; activity + logs share the right column.
	treeH := m.treeInnerHeight()
	detailH := m.paneInnerHeight()

	var tb strings.Builder
	end := m.treeOffset + treeH
	if end > len(m.rows) {
		end = len(m.rows)
	}
	for i := m.treeOffset; i < end; i++ {
		tb.WriteString(renderRow(m.rows[i], i == m.cursor && m.focus == paneTree, m.treeWidth, m.spinner()))
		tb.WriteByte('\n')
	}
	treePane := lipgloss.NewStyle().Width(m.treeWidth).Height(treeH).Render(tb.String())
	runsPanel := titledPanel("runs", "", treePane, m.treeWidth, m.focus == paneTree)

	// Right column: activity over logs (logs scoped to the selected node).
	detailPane := lipgloss.NewStyle().Width(m.detailInnerWidth()).Height(detailH).Render(m.detailVP.View())
	activityPanel := titledPanel("activity", "", detailPane, m.detailInnerWidth(), m.focus == paneDetail)

	logContent := lipgloss.NewStyle().Width(m.detailInnerWidth()).Height(m.logInnerHeight()).Render(m.logVP.View())
	logsPanel := titledPanel("logs", m.logScope(), logContent, m.detailInnerWidth(), m.focus == paneLog)

	right := lipgloss.JoinVertical(lipgloss.Left, activityPanel, logsPanel)
	body := lipgloss.JoinHorizontal(lipgloss.Top, runsPanel, right)

	return m.headerRibbon() + "\n" + body + "\n" + m.footer()
}

// logScope is a short label for the logs panel border showing what the logs are
// filtered to — the selected node's narrowest scope.
func (m *model) logScope() string {
	runTs, iter, scen, eval := parseSelKey(m.selectedKey)
	scope := ""
	switch {
	case eval != "":
		scope = eval
	case scen != "":
		scope = scen
	case iter >= 0:
		scope = fmt.Sprintf("iter %d", iter)
	case runTs != "":
		scope = "run " + shortRunTag(runTs)
	}
	if len(scope) > 18 {
		scope = scope[:17] + "…"
	}
	return scope
}

// spinner returns the current braille spinner frame.
func (m *model) spinner() string {
	return spinnerFrames[m.spinnerIdx%len(spinnerFrames)]
}

// currentRun is the run the header readout describes: the live run if any,
// otherwise the run owning the selected tree node.
func (m *model) currentRun() *runs.Run {
	if m.live != nil {
		return m.live
	}
	runTs, _, _, _ := parseSelKey(m.selectedKey)
	for i := range m.pastRuns {
		if m.pastRuns[i].Timestamp == runTs {
			return &m.pastRuns[i]
		}
	}
	return nil
}

// iterScores returns each iteration's score and whether it survived selection,
// for the trajectory sparkline.
func iterScores(r *runs.Run) (scores []float64, kept []bool) {
	best := -1.0
	for i := range r.Iterations {
		s := r.Iterations[i].Score
		scores = append(scores, s)
		survived := i == 0 || (s >= 0 && s > best) // baseline is the starting point
		kept = append(kept, survived)
		if s >= 0 && s > best {
			best = s
		}
	}
	return scores, kept
}

// headerRibbon renders the two-line instrument readout: identity + BEST/Δ on
// line one, the score trajectory sparkline + live progress on line two.
func (m *model) headerRibbon() string {
	run := m.currentRun()

	title := lipgloss.NewStyle().Foreground(colorAccent).Render("▌") +
		valueStyle.Render(" papi") + mutedStyle.Render(" · "+m.skillName)

	right := ""
	if run != nil && run.BestScore() >= 0 {
		best := run.BestScore()
		right = mutedStyle.Render("BEST ") + scoreStyle(best).Bold(true).Render(fmt.Sprintf("%.1f%%", best*100))
		if len(run.Iterations) > 0 {
			if base := run.Iterations[0].Score; base >= 0 {
				d := (best - base) * 100
				arrow := "▲"
				if d < -0.05 {
					arrow = "▼"
				} else if d <= 0.05 {
					arrow = "·"
				}
				right += "  " + scoreStyle(best).Render(fmt.Sprintf("%s %+.1f vs baseline", arrow, d))
			}
		}
	}
	// Pulsing TAILING badge while we're following a live run.
	if m.follow && m.liveActive {
		chip := tailChipOff
		if (m.spinnerIdx/5)%2 == 0 {
			chip = tailChipOn
		}
		if right != "" {
			right += "   "
		}
		right += chip.Render(" ● TAILING ")
	}
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line1 := title + strings.Repeat(" ", gap) + right

	// Line 2: baseline anchor + sparkline + live/idle status.
	var line2 strings.Builder
	line2.WriteByte(' ')
	if run != nil && len(run.Iterations) > 0 {
		if base := run.Iterations[0].Score; base >= 0 {
			line2.WriteString(mutedStyle.Render(fmt.Sprintf("%.1f ", base*100)))
		}
		scores, kept := iterScores(run)
		line2.WriteString(sparkline(scores, kept))
		line2.WriteString("  ")
	}
	if status := m.liveProgress(); status != "" {
		line2.WriteString(status)
	} else if !m.liveActive && run != nil {
		line2.WriteString(mutedStyle.Render(fmt.Sprintf("%d iterations", len(run.Iterations))))
	}

	return line1 + "\n" + line2.String()
}

// liveProgress summarizes the running iteration: which iteration, the active
// phase with a spinner, and scenario progress within the iteration.
func (m *model) liveProgress() string {
	if !m.liveActive || m.live == nil || len(m.live.Iterations) == 0 {
		return ""
	}
	it := &m.live.Iterations[len(m.live.Iterations)-1]
	done, running := 0, 0
	phase := ""
	for i := range it.Scenarios {
		sc := &it.Scenarios[i]
		if sc.Score >= 0 {
			done++
		}
		if st := m.liveStatus[scenKey(iterKey(runKey(m.live.Timestamp), it.Index), sc.ID)]; st != "" {
			phase = st
			running = 1
		}
	}
	parts := []string{mutedStyle.Render(fmt.Sprintf("iter %d/%d", it.Index, m.totalIters))}
	if phase != "" {
		parts = append(parts, liveBadgeStyle.Render(m.spinner()+" "+phase))
	} else {
		parts = append(parts, liveBadgeStyle.Render(m.spinner()+" running"))
	}
	if total := len(m.scenarioIDs); total > 0 {
		cur := done + running
		if cur > total {
			cur = total
		}
		parts = append(parts, mutedStyle.Render(fmt.Sprintf("scenario %d/%d", cur, total)))
	}
	return strings.Join(parts, mutedStyle.Render(" · "))
}

func (m *model) footer() string {
	// Focus now lives on the panel titles, so the footer is just keys.
	nav := "↑/↓ move · space fold · enter open · tab pane · g tail"
	if m.focus == paneLog {
		nav += " · enter expand"
	}
	actions := "q back"
	if m.liveActive {
		actions = "s stop · " + actions
	} else if m.skillRunnable {
		if _, ok := m.selectedResumableRun(); ok {
			actions = "c continue · " + actions
		}
		actions = "r run · " + actions
	}
	line := footerStyle.Render(nav) + footerStyle.Render("   "+actions)
	if m.statusMsg != "" {
		line = liveBadgeStyle.Render(m.statusMsg) + "  " + line
	}
	return line
}
