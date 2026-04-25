CREATE TABLE analytics_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_name TEXT NOT NULL CHECK (length(trim(event_name)) > 0),
    anonymous_id TEXT NOT NULL CHECK (length(trim(anonymous_id)) > 0),
    user_id UUID REFERENCES users (id) ON DELETE SET NULL,
    session_id TEXT NOT NULL CHECK (length(trim(session_id)) > 0),
    app_version TEXT NOT NULL CHECK (length(trim(app_version)) > 0),
    properties JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_analytics_events_event_name ON analytics_events (event_name);
CREATE INDEX idx_analytics_events_anonymous_id ON analytics_events (anonymous_id);
CREATE INDEX idx_analytics_events_user_id ON analytics_events (user_id);
CREATE INDEX idx_analytics_events_session_id ON analytics_events (session_id);
CREATE INDEX idx_analytics_events_occurred_at ON analytics_events (occurred_at);
CREATE INDEX idx_analytics_events_received_at ON analytics_events (received_at);
