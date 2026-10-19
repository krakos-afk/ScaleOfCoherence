// Package scaleco exposes embedded assets shared across the server. The SQL
// migrations are compiled into the binary so a deployed image only needs the
// binary and the frontend directory.
package scaleco

import "embed"

// Migrations holds the golang-migrate SQL files, applied at startup or via
// the -migrate-only flag.
//
//go:embed migrations/*.sql
var Migrations embed.FS
