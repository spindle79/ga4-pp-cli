-- 0001_init.sql — base schema for ga4-pp-cli's local data layer.
--
-- Tables are GA4-shaped and intentionally narrow: we only persist the columns
-- the existing `pages` / `drift` / `watch` / `funnel` commands and the new
-- `search` / `sql` / `traffic-anomalies` / `bot-traffic` compound commands
-- actually read. Anything richer is left to a future migration.

CREATE TABLE IF NOT EXISTS properties (
    account_id   TEXT,
    property_id  TEXT NOT NULL PRIMARY KEY,
    name         TEXT,
    time_zone    TEXT,
    currency     TEXT,
    updated_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS dimensions (
    property_id  TEXT NOT NULL,
    api_name     TEXT NOT NULL,
    ui_name      TEXT,
    description  TEXT,
    category     TEXT,
    custom       INTEGER NOT NULL DEFAULT 0,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY(property_id, api_name)
);

CREATE TABLE IF NOT EXISTS metrics (
    property_id  TEXT NOT NULL,
    api_name     TEXT NOT NULL,
    ui_name      TEXT,
    description  TEXT,
    type         TEXT,
    category     TEXT,
    custom       INTEGER NOT NULL DEFAULT 0,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY(property_id, api_name)
);

CREATE TABLE IF NOT EXISTS pages_daily (
    property_id              TEXT NOT NULL,
    date                     TEXT NOT NULL,          -- YYYY-MM-DD
    page_path                TEXT NOT NULL,
    page_title               TEXT,
    sessions                 REAL NOT NULL DEFAULT 0,
    screen_page_views        REAL NOT NULL DEFAULT 0,
    engaged_sessions         REAL NOT NULL DEFAULT 0,
    total_users              REAL NOT NULL DEFAULT 0,
    engagement_rate          REAL NOT NULL DEFAULT 0,
    average_session_duration REAL NOT NULL DEFAULT 0,
    conversions              REAL NOT NULL DEFAULT 0,
    updated_at               TEXT NOT NULL,
    PRIMARY KEY(property_id, date, page_path)
);

CREATE INDEX IF NOT EXISTS idx_pages_daily_property_date
    ON pages_daily(property_id, date);
CREATE INDEX IF NOT EXISTS idx_pages_daily_page_path
    ON pages_daily(page_path);

CREATE TABLE IF NOT EXISTS sync_state (
    property_id  TEXT NOT NULL,
    scope        TEXT NOT NULL,                       -- 'schema' | 'pages' | 'properties'
    last_date    TEXT,                                -- YYYY-MM-DD when applicable
    last_run_at  TEXT NOT NULL,
    row_count    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(property_id, scope)
);

-- FTS5 mirrors. Populated by triggers below so callers only have to write to
-- the canonical tables. content='dimensions' / 'metrics' lets us avoid
-- duplicating the row body — rowid links back to the source row.
CREATE VIRTUAL TABLE IF NOT EXISTS dimensions_fts USING fts5(
    api_name, ui_name, description,
    content='dimensions', content_rowid='rowid'
);

CREATE VIRTUAL TABLE IF NOT EXISTS metrics_fts USING fts5(
    api_name, ui_name, description,
    content='metrics', content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS dimensions_ai AFTER INSERT ON dimensions BEGIN
    INSERT INTO dimensions_fts(rowid, api_name, ui_name, description)
    VALUES (new.rowid, new.api_name, new.ui_name, new.description);
END;
CREATE TRIGGER IF NOT EXISTS dimensions_ad AFTER DELETE ON dimensions BEGIN
    INSERT INTO dimensions_fts(dimensions_fts, rowid, api_name, ui_name, description)
    VALUES ('delete', old.rowid, old.api_name, old.ui_name, old.description);
END;
CREATE TRIGGER IF NOT EXISTS dimensions_au AFTER UPDATE ON dimensions BEGIN
    INSERT INTO dimensions_fts(dimensions_fts, rowid, api_name, ui_name, description)
    VALUES ('delete', old.rowid, old.api_name, old.ui_name, old.description);
    INSERT INTO dimensions_fts(rowid, api_name, ui_name, description)
    VALUES (new.rowid, new.api_name, new.ui_name, new.description);
END;

CREATE TRIGGER IF NOT EXISTS metrics_ai AFTER INSERT ON metrics BEGIN
    INSERT INTO metrics_fts(rowid, api_name, ui_name, description)
    VALUES (new.rowid, new.api_name, new.ui_name, new.description);
END;
CREATE TRIGGER IF NOT EXISTS metrics_ad AFTER DELETE ON metrics BEGIN
    INSERT INTO metrics_fts(metrics_fts, rowid, api_name, ui_name, description)
    VALUES ('delete', old.rowid, old.api_name, old.ui_name, old.description);
END;
CREATE TRIGGER IF NOT EXISTS metrics_au AFTER UPDATE ON metrics BEGIN
    INSERT INTO metrics_fts(metrics_fts, rowid, api_name, ui_name, description)
    VALUES ('delete', old.rowid, old.api_name, old.ui_name, old.description);
    INSERT INTO metrics_fts(rowid, api_name, ui_name, description)
    VALUES (new.rowid, new.api_name, new.ui_name, new.description);
END;
