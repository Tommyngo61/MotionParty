// Package migrations embeds the controller's SQL migrations.
//
// They live at the repository root rather than under internal/ so they are
// easy to read and review as SQL — the schema is the part of this system a
// reviewer is most likely to want to look at without opening Go code — while
// still being compiled into the binary. The controller migrates itself: there
// is no separate migration image to keep in sync with the app image, and no
// hand-run SQL.
package migrations

import "embed"

// FS holds every .sql migration, in goose's numbered format.
//
//go:embed *.sql
var FS embed.FS

// Dir is the path within FS that goose is pointed at.
const Dir = "."
