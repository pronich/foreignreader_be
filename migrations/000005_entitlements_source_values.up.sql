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
        'friend_lifetime'
    ));
