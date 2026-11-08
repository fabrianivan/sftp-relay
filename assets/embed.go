// Package assets embeds the application icon.
package assets

import _ "embed"

// Icon is the 256×256 app icon used for the window, tray and dashboard.
//
//go:embed icon-256.png
var Icon []byte
