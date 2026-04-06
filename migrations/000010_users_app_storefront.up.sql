ALTER TABLE users
    ADD COLUMN app_storefront TEXT,
    ADD COLUMN app_storefront_updated_at TIMESTAMPTZ;
