CREATE TABLE runs (
    run_id TEXT PRIMARY KEY,
    session_name TEXT NOT NULL,
    mode TEXT NOT NULL,
    status TEXT NOT NULL,
    schema_version TEXT NOT NULL,
    sdk_version TEXT NOT NULL,
    runtime_version TEXT NOT NULL,
    scrub_policy_version TEXT NOT NULL,
    base_snapshot_id TEXT,
    agent_version TEXT,
    git_sha TEXT,
    started_at TEXT NOT NULL,
    ended_at TEXT
);

CREATE TABLE interactions (
    interaction_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    parent_interaction_id TEXT,
    sequence INTEGER NOT NULL,
    service TEXT NOT NULL,
    operation TEXT NOT NULL,
    protocol TEXT NOT NULL,
    streaming INTEGER NOT NULL DEFAULT 0,
    fallback_tier TEXT,
    request_json TEXT NOT NULL,
    scrub_report_json TEXT NOT NULL,
    extracted_entities_json TEXT,
    latency_ms INTEGER,
    FOREIGN KEY (run_id) REFERENCES runs(run_id) ON DELETE CASCADE,
    FOREIGN KEY (parent_interaction_id) REFERENCES interactions(interaction_id) ON DELETE SET NULL
);

CREATE TABLE events (
    event_id TEXT PRIMARY KEY,
    interaction_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    t_ms INTEGER NOT NULL,
    sim_t_ms INTEGER NOT NULL,
    type TEXT NOT NULL,
    data_json TEXT,
    nested_interaction_id TEXT,
    FOREIGN KEY (interaction_id) REFERENCES interactions(interaction_id) ON DELETE CASCADE
);

CREATE TABLE assertions (
    assertion_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    definition_json TEXT NOT NULL,
    result TEXT NOT NULL,
    failure_reason TEXT,
    evidence_json TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (run_id) REFERENCES runs(run_id) ON DELETE CASCADE
);

CREATE TABLE baselines (
    baseline_id TEXT PRIMARY KEY,
    session_name TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    git_sha TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (source_run_id) REFERENCES runs(run_id) ON DELETE CASCADE
);

CREATE TABLE scrub_salts (
    session_name TEXT PRIMARY KEY,
    salt_id TEXT NOT NULL,
    salt_encrypted BLOB NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_runs_session_started_at ON runs(session_name, started_at DESC);
CREATE INDEX idx_runs_status_started_at ON runs(status, started_at DESC);
CREATE INDEX idx_interactions_run_id ON interactions(run_id);
CREATE UNIQUE INDEX idx_interactions_run_sequence ON interactions(run_id, sequence);
CREATE INDEX idx_interactions_parent_id ON interactions(parent_interaction_id);
CREATE INDEX idx_events_interaction_id ON events(interaction_id);
CREATE UNIQUE INDEX idx_events_interaction_sequence ON events(interaction_id, sequence);
CREATE INDEX idx_assertions_run_id ON assertions(run_id);
CREATE INDEX idx_baselines_session_created_at ON baselines(session_name, created_at DESC);
CREATE UNIQUE INDEX idx_scrub_salts_salt_id ON scrub_salts(salt_id);
