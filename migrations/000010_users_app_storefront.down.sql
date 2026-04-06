ALTER TABLE users
    DROP COLUMN IF EXISTS app_storefront,
    DROP COLUMN IF EXISTS app_storefront_updated_at;
