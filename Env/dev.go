//go:build !production

package Env

// Source checkouts load .env automatically so the app works with `go run` and
// `task dev` without extra shell setup.
func allowed() bool { return true }
