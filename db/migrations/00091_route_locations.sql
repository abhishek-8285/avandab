-- +goose Up
-- 00091 Standardized route locations (gap #46): geocoded coordinates for
-- route endpoints, stored side-table so the sqlc-generated routes queries
-- stay untouched. Rows are written best-effort by RouteService when a
-- forward geocoder is configured; NULL/missing row = free-text-only route.
CREATE TABLE IF NOT EXISTS route_locations (
    route_id    TEXT PRIMARY KEY REFERENCES routes(id) ON DELETE CASCADE,
    source_lat  REAL NOT NULL,
    source_lng  REAL NOT NULL,
    source_name TEXT,
    dest_lat    REAL NOT NULL,
    dest_lng    REAL NOT NULL,
    dest_name   TEXT,
    geocoded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS route_locations;
