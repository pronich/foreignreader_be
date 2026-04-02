-- Stable cross-device book identity: rename book_id -> book_fingerprint (data preserved).
ALTER TABLE reading_positions RENAME COLUMN book_id TO book_fingerprint;

ALTER INDEX idx_reading_positions_user_book RENAME TO idx_reading_positions_user_book_fingerprint;

ALTER TABLE reading_positions RENAME CONSTRAINT uq_reading_positions_user_book TO uq_reading_positions_user_fingerprint;
