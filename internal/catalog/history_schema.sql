ALTER TABLE playback_history ADD COLUMN public_ref TEXT NOT NULL DEFAULT '';
UPDATE playback_history SET public_ref=lower(hex(randomblob(16)));
CREATE UNIQUE INDEX playback_history_by_reference ON playback_history(public_ref);
PRAGMA user_version = 5;
