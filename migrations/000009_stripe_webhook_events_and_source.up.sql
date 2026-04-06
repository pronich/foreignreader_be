ALTER TABLE entitlements
    DROP CONSTRAINT entitlements_source_check;

ALTER TABLE entitlements
    ADD CONSTRAINT entitlements_source_check CHECK (source IN (
        'dev',
        'owner',
        'admin',
        'internal',
        'apple_iap',
        'external',
        'promo',
        'gift',
        'friend_lifetime',
        'stripe'
    ));

CREATE TABLE stripe_webhook_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stripe_event_id TEXT NOT NULL UNIQUE,
    event_type TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_stripe_webhook_events_processed_at ON stripe_webhook_events (processed_at);
