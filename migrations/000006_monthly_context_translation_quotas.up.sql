CREATE TABLE monthly_context_translation_quotas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    period_key TEXT NOT NULL,
    monthly_limit INTEGER NOT NULL CHECK (monthly_limit >= 0),
    used_count INTEGER NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_monthly_context_translation_quotas_user_period UNIQUE (user_id, period_key),
    CONSTRAINT chk_monthly_context_translation_quotas_period_key CHECK (period_key ~ '^\d{4}-\d{2}$')
);

CREATE INDEX idx_monthly_context_translation_quotas_user_id ON monthly_context_translation_quotas (user_id);
