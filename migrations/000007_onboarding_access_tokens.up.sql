CREATE TABLE onboarding_access_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash TEXT NOT NULL,
    device_session_id TEXT,
    platform TEXT,
    app_version TEXT NOT NULL,
    scope TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'revoked', 'expired')),
    issued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_ip TEXT,
    last_used_at TIMESTAMPTZ,
    last_used_ip TEXT,
    CONSTRAINT uq_onboarding_access_tokens_token_hash UNIQUE (token_hash)
);

CREATE INDEX idx_onboarding_access_tokens_expires_at ON onboarding_access_tokens (expires_at);
CREATE INDEX idx_onboarding_access_tokens_status ON onboarding_access_tokens (status);
