//go:build embed_frontend

package main

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embeddedFrontend embed.FS

var frontendFS fs.FS = embeddedFrontend
