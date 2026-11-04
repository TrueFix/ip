package assets

import "embed"

// Files contains versioned country metadata and flag assets; see THIRD_PARTY.md.
//
//go:embed countries.json subdivisions.json flags/*.svg zone.tab
var Files embed.FS
