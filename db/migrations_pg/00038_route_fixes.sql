-- PG port of 00038_route_fixes.sql | status: PORTABLE | flags: none
-- +goose Up
-- Fix 1: reverse_distance/reverse_standard_fare already exist (migration 00031)
--         but were never wired into queries. This migration adds the missing
--         columns that the code layer needs.

-- Fix 2: tenant_id on routes (every other table has it)
ALTER TABLE routes ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '1';
-- Fix 3: normalized columns for case-insensitive duplicate prevention
ALTER TABLE routes ADD COLUMN source_normalized TEXT NOT NULL DEFAULT '';
ALTER TABLE routes ADD COLUMN dest_normalized TEXT NOT NULL DEFAULT '';
-- Direction: oneway | bidirectional
ALTER TABLE routes ADD COLUMN direction TEXT NOT NULL DEFAULT 'oneway';
-- Soft-delete / disable support
ALTER TABLE routes ADD COLUMN is_active INTEGER NOT NULL DEFAULT 1;
-- Backfill normalized columns from existing data
UPDATE routes SET
    source_normalized = LOWER(TRIM(source)),
    dest_normalized   = LOWER(TRIM(destination));
-- Unique index: one route per (tenant, source, destination) ignoring case/whitespace
CREATE UNIQUE INDEX IF NOT EXISTS idx_routes_tenant_normalized
    ON routes (tenant_id, source_normalized, dest_normalized);
-- +goose Down
DROP INDEX IF EXISTS idx_routes_tenant_normalized;
