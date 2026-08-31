package migrations

import "embed"

// FS embeds all SQL migration scripts into the Go binary.
//
//go:embed sql/*.sql
var FS embed.FS
