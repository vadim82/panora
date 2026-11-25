CREATE TABLE cached_content (
    hash TEXT PRIMARY KEY CHECK (length(hash) = 64),
    path TEXT NOT NULL UNIQUE,
    bytes INTEGER NOT NULL CHECK (bytes > 0),
    width INTEGER NOT NULL CHECK (width > 0),
    height INTEGER NOT NULL CHECK (height > 0),
    last_used_ms INTEGER NOT NULL
);

CREATE TABLE image_content (
    image_id INTEGER PRIMARY KEY REFERENCES images(id) ON DELETE RESTRICT,
    hash TEXT NOT NULL REFERENCES cached_content(hash) ON DELETE RESTRICT
);
CREATE INDEX image_content_by_hash ON image_content(hash);
PRAGMA user_version = 2;
