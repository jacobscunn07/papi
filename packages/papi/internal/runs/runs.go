// Package runs reads back the on-disk artifacts the research loop writes under
// .papi/skills/<skill>/runs, so the TUI can browse past and in-progress runs.
//
// All scores returned by this package are normalized to the 0..1 range (the
// iteration-level score persisted in results.json is stored as 0..100 and is
// divided here).
package runs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"papi/internal/types"
)

// Skill describes a skill discovered under skills/ and the state of its runs.
type Skill struct {
	Name      string
	Runnable  bool    // has a .papi/skills/<name>/scenarios directory
	LastRun   string  // timestamp of most recent run, "" if none
	BestScore float64 // best iteration score of the most recent run, -1 if none
}

// Run is a single timestamped run directory containing iterations.
type Run struct {
	Timestamp  string
	Dir        string
	Iterations []Iteration
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

// Iteration is one iteration within a run (index 0 = baseline).
type Iteration struct {
	Index      int
	Dir        string
	Score      float64 // 0..1
	Experiment string  // research agent's description of the change (iter > 0)
	Scenarios  []Scenario
	skillMdRead bool
	skillMd     string
}

// SkillMd returns the SKILL.md snapshot that ran for this iteration.
func (it *Iteration) SkillMd() string {
	if !it.skillMdRead {
		b, _ := os.ReadFile(filepath.Join(it.Dir, "SKILL.md"))
		it.skillMd = string(b)
		it.skillMdRead = true
	}
	return it.skillMd
}

// Scenario is one scenario's result within an iteration.
type Scenario struct {
	ID          string
	Dir         string
	Score       float64 // 0..1
	Invoked     bool
	Result      types.ScenarioRunResult
	Transcripts []File // prompt / invocation / response
	Files       []File // skill-generated output files
}

// File is an openable artifact in the tree.
type File struct {
	Label string
	Path  string
}

// Content reads the file contents on demand.
func (f File) Content() string {
	b, _ := os.ReadFile(f.Path)
	return string(b)
}

type iterationResults struct {
	Score     float64                   `json:"score"`
	Scenarios []types.ScenarioRunResult `json:"scenarios"`
}

var transcriptOrder = []struct{ file, label string }{
	{"prompt.md", "prompt"},
	{"invocation.md", "invocation transcript"},
	{"response.md", "quality transcript"},
}

// ListSkills returns skills found under skills/, with run metadata for the picker.
func ListSkills(repoRoot string) ([]Skill, error) {
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
		if rs, err := ListRuns(repoRoot, name); err == nil && len(rs) > 0 {
			last := rs[len(rs)-1]
			s.LastRun = last.Timestamp
			s.BestScore = last.BestScore()
		}
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// ListRuns returns the runs for a skill, oldest first (by numeric timestamp).
func ListRuns(repoRoot, skill string) ([]Run, error) {
	runsDir := filepath.Join(repoRoot, ".papi", "skills", skill, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool {
		ai, _ := strconv.ParseInt(names[i], 10, 64)
		aj, _ := strconv.ParseInt(names[j], 10, 64)
		if ai != aj {
			return ai < aj
		}
		return names[i] < names[j]
	})

	runs := make([]Run, 0, len(names))
	for _, n := range names {
		run, err := LoadRun(filepath.Join(runsDir, n))
		if err != nil {
			continue
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// LoadRun reads a single run directory and its iterations.
func LoadRun(dir string) (Run, error) {
	run := Run{Timestamp: filepath.Base(dir), Dir: dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return run, err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "iteration-") {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "iteration-"))
		if err != nil {
			continue
		}
		it := LoadIteration(filepath.Join(dir, e.Name()), idx)
		run.Iterations = append(run.Iterations, it)
	}
	sort.Slice(run.Iterations, func(i, j int) bool { return run.Iterations[i].Index < run.Iterations[j].Index })
	return run, nil
}

// LoadIteration reads one iteration directory.
func LoadIteration(dir string, index int) Iteration {
	it := Iteration{Index: index, Dir: dir, Score: -1}

	if b, err := os.ReadFile(filepath.Join(dir, "experiment.txt")); err == nil {
		it.Experiment = strings.TrimSpace(string(b))
	}

	var res iterationResults
	if b, err := os.ReadFile(filepath.Join(dir, "results.json")); err == nil {
		if json.Unmarshal(b, &res) == nil {
			it.Score = res.Score / 100.0
		}
	}

	for _, sr := range res.Scenarios {
		sc := Scenario{
			ID:      sr.Scenario.ID,
			Dir:     filepath.Join(dir, sr.Scenario.ID),
			Score:   sr.ScenarioScore,
			Invoked: sr.Invoked,
			Result:  sr,
		}
		sc.Transcripts, sc.Files = scenarioArtifacts(sc.Dir)
		it.Scenarios = append(it.Scenarios, sc)
	}
	return it
}

// ScenarioArtifacts lists the transcript files and skill-generated output files
// present in a scenario directory. Exported for the TUI to populate live runs.
func ScenarioArtifacts(dir string) (transcripts, files []File) {
	return scenarioArtifacts(dir)
}

// scenarioArtifacts lists the transcript files and skill-generated output files
// present in a scenario directory.
func scenarioArtifacts(dir string) (transcripts, files []File) {
	known := map[string]bool{"evals.json": true}
	for _, t := range transcriptOrder {
		path := filepath.Join(dir, t.file)
		if _, err := os.Stat(path); err == nil {
			transcripts = append(transcripts, File{Label: t.label, Path: path})
			known[t.file] = true
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return transcripts, files
	}
	for _, e := range entries {
		if e.IsDir() || known[e.Name()] || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		files = append(files, File{Label: e.Name(), Path: filepath.Join(dir, e.Name())})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Label < files[j].Label })
	return transcripts, files
}
