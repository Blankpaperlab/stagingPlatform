CREATE TABLE sessions (
    session_name TEXT PRIMARY KEY,
    parent_session_name TEXT,
    current_snapshot_id TEXT,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (parent_session_name) REFERENCES sessions(session_name) ON DELETE SET NULL
);

CREATE TABLE session_snapshots (
    snapshot_id TEXT PRIMARY KEY,
    session_name TEXT NOT NULL,
    parent_snapshot_id TEXT,
    source_run_id TEXT,
    state_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (session_name) REFERENCES sessions(session_name) ON DELETE CASCADE,
    FOREIGN KEY (parent_snapshot_id) REFERENCES session_snapshots(snapshot_id) ON DELETE SET NULL,
    FOREIGN KEY (source_run_id) REFERENCES runs(run_id) ON DELETE SET NULL
);

CREATE INDEX idx_sessions_parent_session_name ON sessions(parent_session_name);
CREATE INDEX idx_sessions_current_snapshot_id ON sessions(current_snapshot_id);
CREATE INDEX idx_session_snapshots_session_created_at ON session_snapshots(session_name, created_at DESC);
CREATE INDEX idx_session_snapshots_parent_id ON session_snapshots(parent_snapshot_id);
