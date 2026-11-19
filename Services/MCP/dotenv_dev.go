//go:build !production

package MCP

// devDotEnvEnabled lets a source checkout read its own .env automatically, so
// OAuth connectors work when running the app with `go run` or `task dev`.
func devDotEnvEnabled() bool { return true }
