PRAGMA foreign_keys = ON;

CREATE TABLE images (
    id INTEGER PRIMARY KEY,
    provider_id TEXT NOT NULL,
    identity_scope TEXT NOT NULL,
    provider_image_id TEXT NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    first_seen_ms INTEGER NOT NULL,
    last_seen_ms INTEGER NOT NULL,
    UNIQUE (provider_id, identity_scope, provider_image_id),
    UNIQUE (id, provider_id)
);

CREATE TABLE discovery_progress (
    provider_id TEXT NOT NULL,
    input_id TEXT NOT NULL,
    settings_revision TEXT NOT NULL,
    scan_id TEXT NOT NULL,
    cursor_json TEXT CHECK (cursor_json IS NULL OR json_valid(cursor_json)),
    complete INTEGER NOT NULL DEFAULT 0 CHECK (complete IN (0, 1)),
    updated_at_ms INTEGER NOT NULL,
    PRIMARY KEY (provider_id, input_id),
    CHECK (complete = 0 OR cursor_json IS NULL)
);

CREATE TABLE input_images (
    provider_id TEXT NOT NULL,
    input_id TEXT NOT NULL,
    settings_revision TEXT NOT NULL,
    image_id INTEGER NOT NULL,
    candidate_json TEXT NOT NULL CHECK (json_valid(candidate_json)),
    first_seen_ms INTEGER NOT NULL,
    last_seen_ms INTEGER NOT NULL,
    PRIMARY KEY (provider_id, input_id, settings_revision, image_id),
    FOREIGN KEY (image_id, provider_id)
        REFERENCES images(id, provider_id) ON DELETE RESTRICT
);
CREATE INDEX input_images_by_image ON input_images(image_id);

CREATE TABLE playback_history (
    id INTEGER PRIMARY KEY,
    image_id INTEGER NOT NULL,
    provider_id TEXT NOT NULL,
    input_id TEXT NOT NULL,
    settings_revision TEXT NOT NULL,
    path TEXT NOT NULL,
    accepted_at_ms INTEGER NOT NULL,
    FOREIGN KEY (provider_id, input_id, settings_revision, image_id)
        REFERENCES input_images(provider_id, input_id, settings_revision, image_id)
        ON DELETE RESTRICT
);

CREATE TABLE playback_state (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    paused INTEGER NOT NULL DEFAULT 0 CHECK (paused IN (0, 1)),
    history_id INTEGER REFERENCES playback_history(id) ON DELETE RESTRICT,
    last_action_ms INTEGER
);

CREATE TABLE pending_apply (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    image_id INTEGER NOT NULL,
    provider_id TEXT NOT NULL,
    input_id TEXT NOT NULL,
    settings_revision TEXT NOT NULL,
    path TEXT NOT NULL,
    history_id INTEGER REFERENCES playback_history(id) ON DELETE RESTRICT,
    started_at_ms INTEGER NOT NULL,
    FOREIGN KEY (provider_id, input_id, settings_revision, image_id)
        REFERENCES input_images(provider_id, input_id, settings_revision, image_id)
        ON DELETE RESTRICT
);

INSERT INTO playback_state(singleton, paused) VALUES (1, 0);
PRAGMA user_version = 1;
