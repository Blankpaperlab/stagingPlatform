CREATE TABLE session_clocks (
    session_name TEXT PRIMARY KEY,
    sim_time TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_name) REFERENCES sessions(session_name) ON DELETE CASCADE
);

CREATE TABLE scheduled_events (
    event_id TEXT PRIMARY KEY,
    session_name TEXT NOT NULL,
    service TEXT NOT NULL,
    topic TEXT NOT NULL,
    delivery_mode TEXT NOT NULL,
    due_at TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    delivered_at TEXT,
    FOREIGN KEY (session_name) REFERENCES sessions(session_name) ON DELETE CASCADE
);

CREATE INDEX idx_scheduled_events_session_status_mode_due ON scheduled_events(session_name, status, delivery_mode, due_at, event_id);
CREATE INDEX idx_scheduled_events_session_created_at ON scheduled_events(session_name, created_at);
CREATE INDEX idx_session_clocks_updated_at ON session_clocks(updated_at);
