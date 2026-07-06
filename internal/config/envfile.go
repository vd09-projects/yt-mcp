package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

// LoadEnvFile loads KEY=VALUE pairs from a dotenv-style file into the process
// environment BEFORE config parsing, so the ${VAR} references in config.json
// resolve against them. It uses godotenv.Overload, so values in the file WIN
// over any variable already present in the shell environment (file-wins): a
// single env file fully describes a channel's credentials, and switching
// channels is just pointing --env-file at a different path.
//
// Secrets live in these files, so LoadEnvFile never logs a key or value. If the
// file is group- or world-readable it emits a path-only warning (not fatal) —
// the operator may have deliberate reasons, but loose permissions on a file of
// OAuth secrets deserve a nudge.
func LoadEnvFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		// Wrap without the underlying value; os.Stat errors only carry the
		// path, never file contents.
		return fmt.Errorf("read env file %s: %w", path, err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		log.Printf("warning: env file %s is group/world-readable (%#o); it holds secrets — consider chmod 600", path, perm)
	}
	if err := godotenv.Overload(path); err != nil {
		// godotenv can echo the offending line (and thus a secret) through its
		// error on a malformed file. Never surface that — report the path only.
		return fmt.Errorf("parse env file %s: malformed KEY=VALUE content", path)
	}
	return nil
}
