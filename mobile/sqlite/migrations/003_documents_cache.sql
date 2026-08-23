-- migration: 003_documents_cache
-- Offline cache of driver/vehicle document metadata (expiry tracking +
-- local photo URIs) so the vault screen works without network.

CREATE TABLE IF NOT EXISTS documents_cache (
  id TEXT PRIMARY KEY,
  owner_type TEXT CHECK(owner_type IN ('driver','vehicle')),
  owner_id TEXT NOT NULL,
  doc_type TEXT NOT NULL,
  expiry_date TEXT,
  local_uri TEXT,
  synced INTEGER NOT NULL DEFAULT 0
);

-- down: DROP TABLE IF EXISTS documents_cache;
