//go:build production

package Env

import "os"

// A shipped app must never pick up a stray developer .env from its launch
// directory. Staging builds can opt in explicitly with ARGUS_LOAD_DOTENV=1.
func allowed() bool { return os.Getenv("ARGUS_LOAD_DOTENV") == "1" }
