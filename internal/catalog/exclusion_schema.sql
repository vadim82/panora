CREATE TABLE image_exclusions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    image_id INTEGER NOT NULL UNIQUE REFERENCES images(id) ON DELETE RESTRICT,
    content_hash TEXT CHECK (content_hash IS NULL OR length(content_hash) = 64),
    excluded_at_ms INTEGER NOT NULL
);
PRAGMA user_version = 4;
