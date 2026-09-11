-- Mirror marker for 00149_identity_sequence_resync (PG-only repair, A7 tail
-- proof). SQLite INTEGER PRIMARY KEY auto-assigns max+1, so the PG
-- identity-sequence desync cannot occur here and roles are runtime-seeded;
-- this file exists solely to keep the 1:1 sqlite/PG filename sets identical.
-- +goose Up
SELECT 1;

-- +goose Down
SELECT 1;
