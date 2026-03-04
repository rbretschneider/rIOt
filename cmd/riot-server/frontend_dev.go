//go:build !embed_frontend

package main

import "io/fs"

// In dev mode, no frontend is embedded. Run `cd web && npm run dev` separately.
var frontendFS fs.FS = nil
