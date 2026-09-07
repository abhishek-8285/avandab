package db

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS

// MigrationsPG is the Postgres port of the migration chain, version-locked
// 1:1 with Migrations (same version numbers and base names). Files that are
// pure portable SQL are identical; the rest are faithful PG rewrites
// (TIMESTAMPTZ, IDENTITY, plpgsql triggers, ON CONFLICT). Each file carries
// a header noting its port status. The sqlite chain is frozen — PG work
// happens here, never there.
//
//go:embed migrations_pg/*.sql
var MigrationsPG embed.FS
