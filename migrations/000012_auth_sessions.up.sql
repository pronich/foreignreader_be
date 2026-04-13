CREATE TABLE auth_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('apple', 'google')),
    refresh_token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoke_reason TEXT,
    last_used_at TIMESTAMPTZ NOT NULL,
    replaced_by_session_id UUID REFERENCES auth_sessions (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_auth_sessions_refresh_token_hash UNIQUE (refresh_token_hash)
);

CREATE INDEX idx_auth_sessions_user_id ON auth_sessions (user_id);

CREATE INDEX idx_auth_sessions_expires_at ON auth_sessions (expires_at);
