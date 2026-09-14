package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// LoadDotEnv reads simple KEY=VALUE pairs from the given file and sets them as
// process environment variables, but only for keys that are not already set in
// the real environment. This mirrors typical dotenv behaviour: an explicitly
// exported variable (e.g. via `$env:LLM_API_KEY=...`) always wins over the file.
//
// It is a best-effort convenience for local development. A missing file is not
// an error, so calling it unconditionally on startup is safe. Secrets still
// live only in the environment/file and are never hard-coded.
//
// Supported syntax:
//   - blank lines and lines starting with '#' are ignored
//   - KEY=VALUE, with surrounding whitespace trimmed from both sides
//   - an optional leading "export " prefix is stripped
//   - single- or double-quoted values have their surrounding quotes removed
//
// Inline "# comment" after a value is intentionally NOT supported: the value is
// taken verbatim, so a URL containing '#' is preserved.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set env %s (line %d): %w", key, lineNo, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

// parseDotEnvLine parses a single line into a key/value pair. ok is false for
// blank lines, comments, or lines without a valid "KEY=VALUE" shape.
func parseDotEnvLine(raw string) (key, value string, ok bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")

	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return "", "", false
	}

	key = strings.TrimSpace(line[:eq])
	value = strings.TrimSpace(line[eq+1:])
	value = unquote(value)
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

// unquote removes a single matching pair of surrounding single or double quotes.
func unquote(v string) string {
	if len(v) < 2 {
		return v
	}
	first, last := v[0], v[len(v)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}
