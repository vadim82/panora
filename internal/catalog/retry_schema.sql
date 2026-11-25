CREATE TABLE provider_retries (
    provider_id TEXT NOT NULL,
    input_id TEXT NOT NULL,
    settings_revision TEXT NOT NULL,
    scope TEXT NOT NULL CHECK (scope IN ('discovery','materialization','provider_metadata','provider_download')),
    image_id INTEGER NOT NULL CHECK (image_id >= 0),
    attempts INTEGER NOT NULL CHECK (attempts BETWEEN 1 AND 16),
    next_at_ms INTEGER NOT NULL CHECK (next_at_ms > 0),
    failure_stage TEXT NOT NULL,
    failure_code TEXT NOT NULL,
    PRIMARY KEY (provider_id,input_id,settings_revision,scope,image_id)
);
PRAGMA user_version = 3;
