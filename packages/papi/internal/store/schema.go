package store

// schema is the full database schema, applied idempotently on every Open. New
// tables/columns are added here; SQLite ignores CREATE ... IF NOT EXISTS for
// objects that already exist, so this doubles as a lightweight migration.
const schema = `
CREATE TABLE IF NOT EXISTS runs (
	id                       INTEGER PRIMARY KEY AUTOINCREMENT,
	skill                    TEXT    NOT NULL,
	timestamp                TEXT    NOT NULL,
	best_score               REAL    NOT NULL DEFAULT 0,   -- 0..100
	best_sha                 TEXT    NOT NULL DEFAULT '',
	last_completed_iteration INTEGER NOT NULL DEFAULT -1,  -- -1 = no baseline checkpoint yet
	total_cost               REAL    NOT NULL DEFAULT 0,
	max_iterations           INTEGER NOT NULL DEFAULT 0,
	budget                   REAL    NOT NULL DEFAULT 0,
	done                     INTEGER NOT NULL DEFAULT 0,
	updated_at               TEXT    NOT NULL DEFAULT '',
	UNIQUE(skill, timestamp)
);

CREATE TABLE IF NOT EXISTS iterations (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	run_id      INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
	idx         INTEGER NOT NULL,
	score       REAL    NOT NULL DEFAULT 0,  -- 0..100
	duration_ms INTEGER NOT NULL DEFAULT 0,
	experiment  TEXT    NOT NULL DEFAULT '',
	skill_md    TEXT    NOT NULL DEFAULT '',
	UNIQUE(run_id, idx)
);

CREATE TABLE IF NOT EXISTS scenarios (
	id                    INTEGER PRIMARY KEY AUTOINCREMENT,
	iteration_id          INTEGER NOT NULL REFERENCES iterations(id) ON DELETE CASCADE,
	ord                   INTEGER NOT NULL DEFAULT 0,
	scenario_id           TEXT    NOT NULL,
	scenario_def          TEXT    NOT NULL DEFAULT '',  -- JSON of types.Scenario
	score                 REAL    NOT NULL DEFAULT 0,   -- 0..1
	invoked               INTEGER NOT NULL DEFAULT 0,
	invocation_output     TEXT,                          -- JSON of types.ClaudeJsonOutput, nullable
	quality_output        TEXT,                          -- JSON of types.ClaudeJsonOutput, nullable
	invocation_transcript TEXT    NOT NULL DEFAULT '',
	quality_transcript    TEXT    NOT NULL DEFAULT '',
	duration_ms           INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS evals (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	scenario_row_id INTEGER NOT NULL REFERENCES scenarios(id) ON DELETE CASCADE,
	ord             INTEGER NOT NULL DEFAULT 0,
	eval_id         TEXT    NOT NULL,
	name            TEXT    NOT NULL DEFAULT '',
	score           REAL    NOT NULL DEFAULT 0,  -- 0..1
	reasoning       TEXT    NOT NULL DEFAULT '',
	required        INTEGER NOT NULL DEFAULT 0,
	is_llm_judge    INTEGER NOT NULL DEFAULT 0,
	duration_ms     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS logs (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	run_id      INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
	iter        INTEGER NOT NULL DEFAULT -1,
	scenario_id TEXT    NOT NULL DEFAULT '',
	eval_id     TEXT    NOT NULL DEFAULT '',
	text        TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_iterations_run  ON iterations(run_id);
CREATE INDEX IF NOT EXISTS idx_scenarios_iter  ON scenarios(iteration_id);
CREATE INDEX IF NOT EXISTS idx_evals_scenario  ON evals(scenario_row_id);
CREATE INDEX IF NOT EXISTS idx_logs_run        ON logs(run_id);
`
