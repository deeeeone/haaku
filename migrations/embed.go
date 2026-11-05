// Package migrations carries the schema migrations inside the binary.
//
// The SQL lives under sql/ as Goose-annotated files and is embedded at build
// time, so the binary always carries the schema it expects. The server applies
// them at startup (see internal/database.Migrate) rather than depending on a
// human remembering to run a tool.
package migrations

import "embed"

// FS holds every migration under sql/. internal/database applies them with
// `fs.Sub(FS, "sql")`, so the file names are the migration names Goose sorts
// and versions by.
//
//go:embed sql/*.sql
var FS embed.FS
