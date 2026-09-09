// Package migrations embeds the ordered *.sql migration files.
package migrations

import "embed"

// FS holds the migration SQL files, applied in lexical filename order.
//
//go:embed *.sql
var FS embed.FS
