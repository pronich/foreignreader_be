DROP INDEX IF EXISTS idx_analytics_events_platform;
ALTER TABLE analytics_events DROP COLUMN IF EXISTS platform;
