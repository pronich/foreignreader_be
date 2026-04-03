ALTER TABLE entitlements
    DROP CONSTRAINT entitlements_source_check;

ALTER TABLE entitlements
    ADD CONSTRAINT entitlements_source_check CHECK (source IN ('dev', 'apple_iap', 'external'));
