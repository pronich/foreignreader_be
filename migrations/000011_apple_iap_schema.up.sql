-- Apple IAP schema (separate from Stripe tables).

-- Maps App Store product identifiers to internal product/entitlement codes.
CREATE TABLE apple_iap_products (
    apple_product_id TEXT PRIMARY KEY,
    product_code TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_apple_iap_products_product_code UNIQUE (product_code)
);

-- Tracks the latest known subscription state per original_transaction_id (subscription lineage).
CREATE TABLE apple_iap_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    apple_product_id TEXT NOT NULL REFERENCES apple_iap_products (apple_product_id),
    product_code TEXT NOT NULL,
    original_transaction_id TEXT NOT NULL,
    latest_transaction_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('active', 'expired', 'revoked', 'grace_period', 'billing_retry')),
    environment TEXT NOT NULL CHECK (environment IN ('sandbox', 'production')),
    purchased_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_apple_iap_subscriptions_original_tx UNIQUE (original_transaction_id),
    CONSTRAINT uq_apple_iap_subscriptions_latest_tx UNIQUE (latest_transaction_id)
);

CREATE INDEX idx_apple_iap_subscriptions_user_id ON apple_iap_subscriptions (user_id);
CREATE INDEX idx_apple_iap_subscriptions_apple_product_id ON apple_iap_subscriptions (apple_product_id);
CREATE INDEX idx_apple_iap_subscriptions_product_code ON apple_iap_subscriptions (product_code);
CREATE INDEX idx_apple_iap_subscriptions_status ON apple_iap_subscriptions (status);
CREATE INDEX idx_apple_iap_subscriptions_expires_at ON apple_iap_subscriptions (expires_at);

-- Idempotency + debug log for App Store Server Notifications (v2).
CREATE TABLE apple_iap_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_uuid TEXT NOT NULL UNIQUE,
    notification_type TEXT NOT NULL,
    subtype TEXT,
    original_transaction_id TEXT,
    transaction_id TEXT,
    signed_payload TEXT NOT NULL,
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_apple_iap_events_processed_at ON apple_iap_events (processed_at);
CREATE INDEX idx_apple_iap_events_original_transaction_id ON apple_iap_events (original_transaction_id);
CREATE INDEX idx_apple_iap_events_transaction_id ON apple_iap_events (transaction_id);

