CREATE TABLE reading_positions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id TEXT NOT NULL,
    chapter_id TEXT NOT NULL,
    character_offset INTEGER NOT NULL CHECK (character_offset >= 0),
    progress_fraction DOUBLE PRECISION,
    device_id TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_reading_positions_user_book UNIQUE (user_id, book_id)
);

CREATE INDEX idx_reading_positions_user_book ON reading_positions (user_id, book_id);
CREATE INDEX idx_reading_positions_updated_at ON reading_positions (updated_at);
