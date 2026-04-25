CREATE TABLE user_rate_prompt_state (
    user_id UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    last_attempt_at TIMESTAMPTZ NOT NULL,
    last_attempt_app_version TEXT NOT NULL CHECK (length(trim(last_attempt_app_version)) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_user_rate_prompt_state_last_attempt_at ON user_rate_prompt_state (last_attempt_at);
