ALTER TABLE analytics_events
ADD COLUMN platform TEXT NOT NULL CHECK (platform IN ('iphone', 'ipad'));

CREATE INDEX idx_analytics_events_platform ON analytics_events (platform);
