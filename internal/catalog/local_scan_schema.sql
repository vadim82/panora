ALTER TABLE discovery_progress ADD COLUMN scan_epoch INTEGER NOT NULL DEFAULT 0;
ALTER TABLE discovery_progress ADD COLUMN complete_epoch INTEGER NOT NULL DEFAULT 0;
ALTER TABLE input_images ADD COLUMN seen_epoch INTEGER NOT NULL DEFAULT 0;
PRAGMA user_version = 6;
