// Package migrations embeds the goose SQL migration files into the binary
// so deployments are self-migrating without shipping the directory alongside.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
