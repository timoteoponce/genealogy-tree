// Package config loads all runtime configuration from environment variables.
//
// The application intentionally has no config file: everything is set through
// env vars so that Docker, docker-compose and the CLI subcommands behave
// identically. Documented in README ("Configuration").
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the fully-resolved application configuration.
type Config struct {
	// DataDir is the directory that contains the SQLite database and
	// anything else that must survive restarts. Default: ".data" (hidden in
	// the repo root; matches the mise.toml dev env, so `mise dev` and a
	// directly-run binary always open the SAME database file).
	DataDir string
	// DBPath is the full path of the SQLite database file.
	DBPath string
	// Port the HTTP server listens on. Default: 8080.
	Port int
	// CookieSecure sets the Secure attribute on session cookies.
	// Automatically true when the request is served... never guessed:
	// set APP_COOKIE_SECURE=true when running behind a TLS-terminating proxy.
	CookieSecure bool
}

// Load reads configuration from the environment, applying defaults.
// It never fails on a specific value being absent — only on malformed values.
func Load() (Config, error) {
	c := Config{}

	c.DataDir = getenv("DATA_DIR", ".data")
	c.DataDir = strings.TrimSuffix(c.DataDir, "/")
	c.DBPath = filepath.Join(c.DataDir, "familytree.db")

	port := getenv("PORT", "8080")
	p, err := strconv.Atoi(port)
	if err != nil || p <= 0 || p > 65535 {
		return c, fmt.Errorf("invalid PORT %q: must be a number 1-65535", port)
	}
	c.Port = p

	switch strings.ToLower(getenv("APP_COOKIE_SECURE", "false")) {
	case "1", "true", "yes", "on":
		c.CookieSecure = true
	case "", "0", "false", "no", "off":
		c.CookieSecure = false
	default:
		return c, fmt.Errorf("invalid APP_COOKIE_SECURE %q: use true/false", getenv("APP_COOKIE_SECURE", ""))
	}

	return c, nil
}

// getenv returns the environment variable value or the fallback.
func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}
