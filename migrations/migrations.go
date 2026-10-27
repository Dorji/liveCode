// Package migrations embeds SQL migration files into the binary so they do
// not have to be shipped or mounted separately at runtime.
package migrations

import _ "embed"

// InitSQL contains the full idempotent schema (CREATE TABLE IF NOT EXISTS).
//
//go:embed 0001_init.sql
var InitSQL string
