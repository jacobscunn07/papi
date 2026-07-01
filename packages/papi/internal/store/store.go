// Package store is the SQLite persistence layer for the research loop. It owns the
// single database at .papi/papi.db and is the source of truth for all run history:
// runs, iterations, per-scenario results, eval rows, and the live log stream.
//
// Only fixtures and skill-generated output files stay on the filesystem (under the
// run's iteration/scenario work dirs); everything structured or textual that used
// to live in state.json / results.json / evals.json / experiment.txt / logs.jsonl
// and the per-scenario transcripts is stored here instead.
//
// Score conventions match the rest of the app: iteration and run scores are stored
// 0..100 (like types.RunState.BestScore); per-scenario and per-eval scores are
// stored 0..1.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"papi/internal/types"

	_ "modernc.org/sqlite"
)

// Store wraps the database connection and remembers the repo root so callers can
// derive on-disk work-dir paths for generated files.
type Store struct {
	db       *sql.DB
	repoRoot string
}

// DBPath returns the on-disk location of the database for a repo root.
func DBPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".papi", "papi.db")
}

// Open opens (creating if needed) the database under repoRoot/.papi and applies
// the schema. WAL mode plus a busy timeout let a live run write while the TUI
// reads concurrently.
func Open(repoRoot string) (*Store, error) {
	dbPath := DBPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create .papi dir: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db, repoRoot: repoRoot}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// RepoRoot returns the repo root the store was opened against.
func (s *Store) RepoRoot() string { return s.repoRoot }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ensureRunID returns the row id for (skill, timestamp), creating a minimal run
// row if none exists yet. The minimal row carries last_completed_iteration=-1 so
// it is not treated as a resumable/visible run until a real checkpoint is written.
func ensureRunID(q interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}, skill, ts string) (int64, error) {
	if _, err := q.Exec(`INSERT OR IGNORE INTO runs(skill, timestamp) VALUES(?, ?)`, skill, ts); err != nil {
		return 0, err
	}
	var id int64
	err := q.QueryRow(`SELECT id FROM runs WHERE skill=? AND timestamp=?`, skill, ts).Scan(&id)
	return id, err
}

// UpsertRun persists the run-level checkpoint (formerly state.json). BestScore is
// 0..100, matching types.RunState.
func (s *Store) UpsertRun(st types.RunState) error {
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		INSERT INTO runs(skill, timestamp, best_score, best_sha, last_completed_iteration,
			total_cost, max_iterations, budget, done, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(skill, timestamp) DO UPDATE SET
			best_score=excluded.best_score,
			best_sha=excluded.best_sha,
			last_completed_iteration=excluded.last_completed_iteration,
			total_cost=excluded.total_cost,
			max_iterations=excluded.max_iterations,
			budget=excluded.budget,
			done=excluded.done,
			updated_at=excluded.updated_at`,
		st.Skill, st.Timestamp, st.BestScore, st.BestSha, st.LastCompletedIteration,
		st.TotalCost, st.MaxIterations, st.Budget, b2i(st.Done), st.UpdatedAt)
	return err
}

// GetRunState reads the checkpoint for one run. ok is false if no checkpoint has
// been written yet (no row, or only a minimal placeholder row).
func (s *Store) GetRunState(skill, ts string) (st types.RunState, ok bool, err error) {
	var done int
	row := s.db.QueryRow(`
		SELECT skill, timestamp, best_score, best_sha, last_completed_iteration,
			total_cost, max_iterations, budget, done, updated_at
		FROM runs WHERE skill=? AND timestamp=?`, skill, ts)
	err = row.Scan(&st.Skill, &st.Timestamp, &st.BestScore, &st.BestSha, &st.LastCompletedIteration,
		&st.TotalCost, &st.MaxIterations, &st.Budget, &done, &st.UpdatedAt)
	if err == sql.ErrNoRows {
		return st, false, nil
	}
	if err != nil {
		return st, false, err
	}
	st.Done = done != 0
	// A real run has an explicit checkpoint (max_iterations > 0, set by UpsertRun).
	// The minimal placeholder rows from ensureRunID have max_iterations = 0 and are
	// not real, resumable runs — even if a partial baseline iteration was saved.
	return st, st.MaxIterations > 0, nil
}

// RunStates returns the checkpointed runs for a skill, oldest first by numeric
// timestamp. Only runs with an explicit checkpoint (max_iterations > 0) are
// returned; minimal placeholder rows from ensureRunID are excluded.
func (s *Store) RunStates(skill string) ([]types.RunState, error) {
	rows, err := s.db.Query(`
		SELECT skill, timestamp, best_score, best_sha, last_completed_iteration,
			total_cost, max_iterations, budget, done, updated_at
		FROM runs
		WHERE skill=? AND max_iterations > 0
		ORDER BY CAST(timestamp AS INTEGER), timestamp`, skill)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.RunState
	for rows.Next() {
		var st types.RunState
		var done int
		if err := rows.Scan(&st.Skill, &st.Timestamp, &st.BestScore, &st.BestSha,
			&st.LastCompletedIteration, &st.TotalCost, &st.MaxIterations, &st.Budget,
			&done, &st.UpdatedAt); err != nil {
			return nil, err
		}
		st.Done = done != 0
		out = append(out, st)
	}
	return out, rows.Err()
}

// RunTimestamps returns the timestamps of a skill's runs that have at least one
// iteration, oldest first.
func (s *Store) RunTimestamps(skill string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT r.timestamp FROM runs r
		WHERE r.skill=? AND EXISTS(SELECT 1 FROM iterations i WHERE i.run_id=r.id)
		ORDER BY CAST(r.timestamp AS INTEGER), r.timestamp`, skill)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ts string
		if err := rows.Scan(&ts); err != nil {
			return nil, err
		}
		out = append(out, ts)
	}
	return out, rows.Err()
}

// SaveIteration writes one iteration and its scenario + eval rows in a single
// transaction (replacing any previous rows for that iteration). score is 0..1 and
// is stored as 0..100; per-scenario and per-eval scores are stored as-is (0..1).
func (s *Store) SaveIteration(skill, ts string, idx int, score float64, durationMs int64, experiment, skillMd string, results []types.ScenarioRunResult) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	runID, err := ensureRunID(tx, skill, ts)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(`
		INSERT INTO iterations(run_id, idx, score, duration_ms, experiment, skill_md)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, idx) DO UPDATE SET
			score=excluded.score, duration_ms=excluded.duration_ms,
			experiment=excluded.experiment, skill_md=excluded.skill_md`,
		runID, idx, score*100, durationMs, experiment, skillMd); err != nil {
		return err
	}
	var iterID int64
	if err := tx.QueryRow(`SELECT id FROM iterations WHERE run_id=? AND idx=?`, runID, idx).Scan(&iterID); err != nil {
		return err
	}
	// Replace existing scenarios (cascades to their evals) so re-running an
	// iteration is idempotent.
	if _, err := tx.Exec(`DELETE FROM scenarios WHERE iteration_id=?`, iterID); err != nil {
		return err
	}

	for sOrd, r := range results {
		defJSON, _ := json.Marshal(r.Scenario)
		res, err := tx.Exec(`
			INSERT INTO scenarios(iteration_id, ord, scenario_id, scenario_def, score, invoked,
				invocation_output, quality_output, invocation_transcript, quality_transcript, duration_ms)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			iterID, sOrd, r.Scenario.ID, string(defJSON), r.ScenarioScore, b2i(r.Invoked),
			marshalOutput(r.InvocationOutput), marshalOutput(r.QualityOutput),
			r.InvocationTranscript, r.QualityTranscript, r.DurationMs)
		if err != nil {
			return err
		}
		scenRowID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		for eOrd, ev := range r.EvalResults {
			if _, err := tx.Exec(`
				INSERT INTO evals(scenario_row_id, ord, eval_id, name, score, reasoning, required, is_llm_judge, duration_ms)
				VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				scenRowID, eOrd, ev.EvalID, ev.Name, ev.Score, ev.Reasoning,
				b2i(ev.Required), b2i(ev.IsLLMJudge), ev.DurationMs); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// marshalOutput returns the JSON for a nullable ClaudeJsonOutput, or nil so the
// column stores SQL NULL.
func marshalOutput(o *types.ClaudeJsonOutput) any {
	if o == nil {
		return nil
	}
	b, _ := json.Marshal(o)
	return string(b)
}

// IterRow is iteration metadata for the run browser. Score is 0..1.
type IterRow struct {
	Index      int
	Score      float64
	DurationMs int64
	Experiment string
	SkillMd    string
}

// Iterations returns a run's iterations ordered by index, scores normalized to 0..1.
func (s *Store) Iterations(skill, ts string) ([]IterRow, error) {
	rows, err := s.db.Query(`
		SELECT i.idx, i.score, i.duration_ms, i.experiment, i.skill_md
		FROM iterations i
		JOIN runs r ON r.id = i.run_id
		WHERE r.skill=? AND r.timestamp=?
		ORDER BY i.idx`, skill, ts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IterRow
	for rows.Next() {
		var it IterRow
		if err := rows.Scan(&it.Index, &it.Score, &it.DurationMs, &it.Experiment, &it.SkillMd); err != nil {
			return nil, err
		}
		it.Score /= 100.0
		out = append(out, it)
	}
	return out, rows.Err()
}

// ScenarioResults reconstructs the full ScenarioRunResult list for one iteration,
// in scenario order. This is the inverse of what SaveIteration persisted and is
// used both to rebuild the research prompt on resume and to render the TUI.
func (s *Store) ScenarioResults(skill, ts string, idx int) ([]types.ScenarioRunResult, error) {
	rows, err := s.db.Query(`
		SELECT sc.id, sc.scenario_def, sc.score, sc.invoked,
			sc.invocation_output, sc.quality_output,
			sc.invocation_transcript, sc.quality_transcript, sc.duration_ms
		FROM scenarios sc
		JOIN iterations i ON i.id = sc.iteration_id
		JOIN runs r ON r.id = i.run_id
		WHERE r.skill=? AND r.timestamp=? AND i.idx=?
		ORDER BY sc.ord`, skill, ts, idx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scenRow struct {
		id     int64
		result types.ScenarioRunResult
	}
	var scens []scenRow
	for rows.Next() {
		var (
			id           int64
			defJSON      string
			score        float64
			invoked      int
			invOut       sql.NullString
			qualOut      sql.NullString
			invTrans     string
			qualTrans    string
			durationMs   int64
		)
		if err := rows.Scan(&id, &defJSON, &score, &invoked, &invOut, &qualOut, &invTrans, &qualTrans, &durationMs); err != nil {
			return nil, err
		}
		var r types.ScenarioRunResult
		_ = json.Unmarshal([]byte(defJSON), &r.Scenario)
		r.ScenarioScore = score
		r.Invoked = invoked != 0
		r.InvocationOutput = unmarshalOutput(invOut)
		r.QualityOutput = unmarshalOutput(qualOut)
		r.InvocationTranscript = invTrans
		r.QualityTranscript = qualTrans
		r.DurationMs = durationMs
		scens = append(scens, scenRow{id: id, result: r})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]types.ScenarioRunResult, len(scens))
	for i, sr := range scens {
		evals, err := s.evalsFor(sr.id)
		if err != nil {
			return nil, err
		}
		sr.result.EvalResults = evals
		out[i] = sr.result
	}
	return out, nil
}

func (s *Store) evalsFor(scenRowID int64) ([]types.EvalResult, error) {
	rows, err := s.db.Query(`
		SELECT eval_id, name, score, reasoning, required, is_llm_judge, duration_ms
		FROM evals WHERE scenario_row_id=? ORDER BY ord`, scenRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.EvalResult
	for rows.Next() {
		var ev types.EvalResult
		var required, llm int
		if err := rows.Scan(&ev.EvalID, &ev.Name, &ev.Score, &ev.Reasoning, &required, &llm, &ev.DurationMs); err != nil {
			return nil, err
		}
		ev.Required = required != 0
		ev.IsLLMJudge = llm != 0
		out = append(out, ev)
	}
	return out, rows.Err()
}

func unmarshalOutput(ns sql.NullString) *types.ClaudeJsonOutput {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	var o types.ClaudeJsonOutput
	if json.Unmarshal([]byte(ns.String), &o) != nil {
		return nil
	}
	return &o
}

// AppendLog records one log line scoped to a node in the run hierarchy. The run
// row is created on demand so logs emitted before the baseline checkpoint are not
// lost.
func (s *Store) AppendLog(skill, ts string, iter int, scenarioID, evalID, text string) error {
	runID, err := ensureRunID(s.db, skill, ts)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO logs(run_id, iter, scenario_id, eval_id, text)
		VALUES(?, ?, ?, ?, ?)`, runID, iter, scenarioID, evalID, text)
	return err
}

// LogRow is one persisted log line.
type LogRow struct {
	Iter       int
	ScenarioID string
	EvalID     string
	Text       string
}

// Logs returns a run's log lines in insertion order.
func (s *Store) Logs(skill, ts string) ([]LogRow, error) {
	rows, err := s.db.Query(`
		SELECT l.iter, l.scenario_id, l.eval_id, l.text
		FROM logs l
		JOIN runs r ON r.id = l.run_id
		WHERE r.skill=? AND r.timestamp=?
		ORDER BY l.id`, skill, ts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LogRow
	for rows.Next() {
		var lr LogRow
		if err := rows.Scan(&lr.Iter, &lr.ScenarioID, &lr.EvalID, &lr.Text); err != nil {
			return nil, err
		}
		out = append(out, lr)
	}
	return out, rows.Err()
}

// PurgeOldRuns deletes all but the newest maxRuns runs (by timestamp) for a skill,
// cascading to their iterations/scenarios/evals/logs. It returns the timestamps of
// the deleted runs so the caller can remove their on-disk work-dir trees. maxRuns
// <= 0 keeps everything.
func (s *Store) PurgeOldRuns(skill string, maxRuns int) ([]string, error) {
	if maxRuns <= 0 {
		return nil, nil
	}
	timestamps, err := s.RunTimestamps(skill) // oldest first
	if err != nil {
		return nil, err
	}
	if len(timestamps) <= maxRuns {
		return nil, nil
	}
	purge := timestamps[:len(timestamps)-maxRuns]
	for _, ts := range purge {
		if _, err := s.db.Exec(`DELETE FROM runs WHERE skill=? AND timestamp=?`, skill, ts); err != nil {
			return nil, err
		}
	}
	return purge, nil
}
