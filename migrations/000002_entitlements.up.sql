CREATE TABLE entitlements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    product_code TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'expired', 'revoked')),
    source TEXT NOT NULL CHECK (source IN ('dev', 'apple_iap', 'external')),
    starts_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_entitlements_user_id ON entitlements (user_id);
CREATE INDEX idx_entitlements_user_product ON entitlements (user_id, product_code);
