//go:build production

package MCP

import "os"

// A packaged app must never pick up a developer's .env from whatever directory
// it happens to be launched in. Staging builds can still opt in explicitly.
func devDotEnvEnabled() bool { return os.Getenv("ARGUS_LOAD_DOTENV") == "1" }
