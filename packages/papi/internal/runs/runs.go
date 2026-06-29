// Package runs reads back run history from the store (.papi/papi.db) so the TUI
// can browse past and in-progress runs. Per-scenario transcripts and the SKILL.md
// snapshot live in the store; only fixtures and skill-generated output files are
// read from the on-disk work-dir tree under .papi/skills/<skill>/runs/<ts>/.
//
// All scores returned by this package are normalized to the 0..1 range.
package runs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"papi/internal/store"
	"papi/internal/types"
)

// Skill describes a skill discovered under skills/ and the state of its runs.
type Skill struct {
	Name       string
	Runnable   bool      // has a .papi/skills/<name>/scenarios directory
	LastRun    string    // timestamp of most recent run, "" if none
	BestScore  float64   // best iteration score of the most recent run, -1 if none
	Trajectory []float64 // per-iteration scores of the most recent run, for a mini sparkline
}

// Run is a single timestamped run with its iterations.
type Run struct {
	Timestamp  string
	Dir        string
	Iterations []Iteration
	State      *types.RunState // run-level checkpoint, nil if absent
	Logs       []LogEntry      // persisted log lines
}

// LogEntry is one persisted log line scoped to a node in the run hierarchy.
type LogEntry struct {
	Iter       int    // iteration index; -1 = run-level
	ScenarioID string // "" = not scenario-specific
	EvalID     string // "" = not eval-specific
	Text       string
}

// Resumable reports whether this run can be continued: it has a checkpoint, has
// not reached its natural end, and still has iterations left.
func (r Run) Resumable() bool {
	return r.State != nil && !r.State.Done &&
		r.State.LastCompletedIteration < r.State.MaxIterations
}

// BestScore returns the highest iteration score in the run (0..1), or -1.
func (r Run) BestScore() float64 {
	best := -1.0
	for _, it := range r.Iterations {
		if it.Score > best {
			best = it.Score
		}
	}
	return best
}

// Duration returns the run's total execution time as the sum of its iteration
// durations (milliseconds).
func (r Run) Duration() int64 {
	var total int64
	for _, it := range r.Iterations {
		total += it.DurationMs
	}
	return total
}

// Iteration is one iteration within a run (index 0 = baseline).
type Iteration struct {
	Index      int
	Dir        string
	Score      float64 // 0..1
	DurationMs int64   // total execution time of the iteration
	Experiment string  // research agent's description of the change (iter > 0)
	Scenarios  []Scenario
	skillMd    string
}

// SkillMd returns the SKILL.md snapshot that ran for this iteration.
func (it *Iteration) SkillMd() string { return it.skillMd }

// SetSkillMd records the SKILL.md snapshot for this iteration. Used by the TUI to
// populate a live iteration (built from progress events, not the store).
func (it *Iteration) SetSkillMd(s string) { it.skillMd = s }

// Scenario is one scenario's result within an iteration.
type Scenario struct {
	ID          string
	Dir         string
	Score       float64 // 0..1
	Invoked     bool
	Result      types.ScenarioRunResult
	Transcripts []File // prompt / invocation / response (in-store text)
	Files       []File // skill-generated output files (on disk)
}

// File is an openable artifact in the tree. Transcripts carry their text inline
// (from the store); generated output files are read from Path on demand.
type File struct {
	Label   string
	Path    string
	content string
	inline  bool
}

// Content returns the file contents (inline for transcripts, read from disk for
// generated files).
func (f File) Content() string {
	if f.inline {
		return f.content
	}
	b, _ := os.ReadFile(f.Path)
	return string(b)
}

func inlineFile(label, content string) File {
	return File{Label: label, content: content, inline: true}
}

// runDir is the on-disk work-dir root for a run (parent of its iteration dirs).
func runDir(repoRoot, skill, ts string) string {
	return filepath.Join(repoRoot, ".papi", "skills", skill, "runs", ts)
}

func iterDir(repoRoot, skill, ts string, idx int) string {
	return filepath.Join(runDir(repoRoot, skill, ts), fmt.Sprintf("iteration-%03d", idx))
}

// ListSkills returns skills found under skills/, with run metadata for the picker.
func ListSkills(st *store.Store) ([]Skill, error) {
	repoRoot := st.RepoRoot()
	skillsDir := filepath.Join(repoRoot, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if _, err := os.Stat(filepath.Join(skillsDir, name, "SKILL.md")); err != nil {
			continue
		}
		s := Skill{Name: name, BestScore: -1}
		if fi, err := os.Stat(filepath.Join(repoRoot, ".papi", "skills", name, "scenarios")); err == nil && fi.IsDir() {
			s.Runnable = true
		}
		if rs, err := ListRuns(st, name); err == nil && len(rs) > 0 {
			last := rs[len(rs)-1]
			s.LastRun = last.Timestamp
			s.BestScore = last.BestScore()
			for i := range last.Iterations {
				s.Trajectory = append(s.Trajectory, last.Iterations[i].Score)
			}
		}
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// ListRuns returns the runs for a skill, oldest first (by numeric timestamp).
func ListRuns(st *store.Store, skill string) ([]Run, error) {
	timestamps, err := st.RunTimestamps(skill)
	if err != nil {
		return nil, err
	}
	runs := make([]Run, 0, len(timestamps))
	for _, ts := range timestamps {
		run, err := LoadRun(st, skill, ts)
		if err != nil {
			continue
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// LoadRun reads a single run and its iterations from the store.
func LoadRun(st *store.Store, skill, ts string) (Run, error) {
	repoRoot := st.RepoRoot()
	run := Run{Timestamp: ts, Dir: runDir(repoRoot, skill, ts)}

	if rs, ok, err := st.GetRunState(skill, ts); err == nil && ok {
		state := rs
		run.State = &state
	}
	run.Logs = loadLogs(st, skill, ts)

	iters, err := st.Iterations(skill, ts)
	if err != nil {
		return run, err
	}
	for _, meta := range iters {
		run.Iterations = append(run.Iterations, loadIteration(st, skill, ts, meta))
	}
	return run, nil
}

// loadLogs reads a run's persisted log lines, splitting each record's text into
// one LogEntry per line to match how live logs are stored.
func loadLogs(st *store.Store, skill, ts string) []LogEntry {
	rows, err := st.Logs(skill, ts)
	if err != nil {
		return nil
	}
	var out []LogEntry
	for _, r := range rows {
		for _, t := range strings.Split(r.Text, "\n") {
			out = append(out, LogEntry{Iter: r.Iter, ScenarioID: r.ScenarioID, EvalID: r.EvalID, Text: t})
		}
	}
	return out
}

// loadIteration builds one iteration's display data from its stored metadata and
// scenario results.
func loadIteration(st *store.Store, skill, ts string, meta store.IterRow) Iteration {
	dir := iterDir(st.RepoRoot(), skill, ts, meta.Index)
	it := Iteration{
		Index:      meta.Index,
		Dir:        dir,
		Score:      meta.Score,
		DurationMs: meta.DurationMs,
		Experiment: meta.Experiment,
		skillMd:    meta.SkillMd,
	}
	results, _ := st.ScenarioResults(skill, ts, meta.Index)
	for _, r := range results {
		workDir := filepath.Join(dir, r.Scenario.ID)
		sc := Scenario{
			ID:      r.Scenario.ID,
			Dir:     workDir,
			Score:   r.ScenarioScore,
			Invoked: r.Invoked,
			Result:  r,
		}
		sc.Transcripts, sc.Files = BuildScenarioArtifacts(workDir, r)
		it.Scenarios = append(it.Scenarios, sc)
	}
	return it
}

// BuildScenarioArtifacts returns the transcript nodes (inline, from the result)
// and the generated-file nodes (on disk, under workDir) for a scenario. It is used
// for both past runs and live runs.
func BuildScenarioArtifacts(workDir string, r types.ScenarioRunResult) (transcripts, files []File) {
	transcripts = append(transcripts, inlineFile("prompt", r.Scenario.Prompt))
	transcripts = append(transcripts, inlineFile("invocation transcript", r.InvocationTranscript))
	if r.QualityTranscript != "" {
		transcripts = append(transcripts, inlineFile("quality transcript", r.QualityTranscript))
	}

	entries, err := os.ReadDir(workDir)
	if err != nil {
		return transcripts, files
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		files = append(files, File{Label: e.Name(), Path: filepath.Join(workDir, e.Name())})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Label < files[j].Label })
	return transcripts, files
}
