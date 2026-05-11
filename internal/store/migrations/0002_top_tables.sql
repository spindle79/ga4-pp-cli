-- 0002_top_tables.sql — three daily-mirror tables for the `top` command tree.
--
-- The pattern matches pages_daily exactly: a property/date row key plus the
-- dimensions you actually pivot on, then a small set of numeric metric
-- columns. Critical: every requested dimension must appear in the PRIMARY
-- KEY (after property_id, date), otherwise UPSERT collapses rows and SUMs
-- come out wrong (see commit 64b413c for the pages_daily version of this
-- bug). Add descriptive metadata via a separate side-table later if needed.

CREATE TABLE IF NOT EXISTS acquisition_daily (
    property_id      TEXT NOT NULL,
    date             TEXT NOT NULL,          -- YYYY-MM-DD
    session_source   TEXT NOT NULL,
    session_medium   TEXT NOT NULL,
    session_campaign TEXT NOT NULL,
    sessions         REAL NOT NULL DEFAULT 0,
    total_users      REAL NOT NULL DEFAULT 0,
    new_users        REAL NOT NULL DEFAULT 0,
    engaged_sessions REAL NOT NULL DEFAULT 0,
    conversions      REAL NOT NULL DEFAULT 0,
    total_revenue    REAL NOT NULL DEFAULT 0,
    updated_at       TEXT NOT NULL,
    PRIMARY KEY(property_id, date, session_source, session_medium, session_campaign)
);

CREATE INDEX IF NOT EXISTS idx_acquisition_daily_property_date
    ON acquisition_daily(property_id, date);
CREATE INDEX IF NOT EXISTS idx_acquisition_daily_source
    ON acquisition_daily(session_source);

CREATE TABLE IF NOT EXISTS events_daily (
    property_id          TEXT NOT NULL,
    date                 TEXT NOT NULL,      -- YYYY-MM-DD
    event_name           TEXT NOT NULL,
    page_path            TEXT NOT NULL,
    event_count          REAL NOT NULL DEFAULT 0,
    event_count_per_user REAL NOT NULL DEFAULT 0,
    event_value          REAL NOT NULL DEFAULT 0,
    total_users          REAL NOT NULL DEFAULT 0,
    conversions          REAL NOT NULL DEFAULT 0,
    updated_at           TEXT NOT NULL,
    PRIMARY KEY(property_id, date, event_name, page_path)
);

CREATE INDEX IF NOT EXISTS idx_events_daily_property_date
    ON events_daily(property_id, date);
CREATE INDEX IF NOT EXISTS idx_events_daily_event_name
    ON events_daily(event_name);

CREATE TABLE IF NOT EXISTS devices_geo_daily (
    property_id              TEXT NOT NULL,
    date                     TEXT NOT NULL,  -- YYYY-MM-DD
    device_category          TEXT NOT NULL,
    country                  TEXT NOT NULL,
    sessions                 REAL NOT NULL DEFAULT 0,
    total_users              REAL NOT NULL DEFAULT 0,
    engaged_sessions         REAL NOT NULL DEFAULT 0,
    average_session_duration REAL NOT NULL DEFAULT 0,
    screen_page_views        REAL NOT NULL DEFAULT 0,
    updated_at               TEXT NOT NULL,
    PRIMARY KEY(property_id, date, device_category, country)
);

CREATE INDEX IF NOT EXISTS idx_devices_geo_daily_property_date
    ON devices_geo_daily(property_id, date);
CREATE INDEX IF NOT EXISTS idx_devices_geo_daily_device
    ON devices_geo_daily(device_category);
CREATE INDEX IF NOT EXISTS idx_devices_geo_daily_country
    ON devices_geo_daily(country);
