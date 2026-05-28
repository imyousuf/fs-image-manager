// Package config loads the application's INI configuration (see
// docs/specs/_contracts.md §7) and exposes typed getters plus the library
// alias registry. It is intentionally small: parsing, validation and
// tilde/`$HOME` expansion of library roots.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-ini/ini"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// DefaultConfigFilePath is used when no -config flag is supplied.
const DefaultConfigFilePath = "image-manager.cfg"

// Default values for optional keys.
const (
	defaultListener        = ":8080"
	defaultDBPath          = "./fsim.db"
	defaultCacheDir        = "./cache"
	defaultDebounceSeconds = 300
	defaultRekognitionAWS  = "imyousuf"
	defaultRekognitionRgn  = "us-east-1"
)

// Config is the parsed, validated application configuration.
type Config struct {
	libraries []catalog.Library

	listener     string
	authToken    string
	dbPath       string
	cacheDir     string
	debounce     time.Duration
	workerSecret string

	ollamaURL             string
	rekognitionProfile    string
	rekognitionRegion     string
	rekognitionCollection string
}

// Libraries returns the configured libraries in config order. The returned
// slice is a copy; mutating it does not affect the Config. For a Config loaded
// via LoadWorker it is always empty (the worker has no local libraries).
func (c *Config) Libraries() []catalog.Library {
	out := make([]catalog.Library, len(c.libraries))
	copy(out, c.libraries)
	return out
}

// Listener returns the HTTP listen address (e.g. ":8080").
func (c *Config) Listener() string { return c.listener }

// AuthToken returns the bearer token for the public API; empty means open.
func (c *Config) AuthToken() string { return c.authToken }

// DBPath returns the sqlite database file path.
func (c *Config) DBPath() string { return c.dbPath }

// CacheDir returns the derivative cache directory.
func (c *Config) CacheDir() string { return c.cacheDir }

// IngestDebounce returns the trailing-debounce quiet period for ingestion.
func (c *Config) IngestDebounce() time.Duration { return c.debounce }

// WorkerSecret returns the shared secret guarding the internal job API.
func (c *Config) WorkerSecret() string { return c.workerSecret }

// OllamaURL returns the configured Ollama base URL (may be empty).
func (c *Config) OllamaURL() string { return c.ollamaURL }

// RekognitionProfile returns the AWS profile used for Rekognition.
func (c *Config) RekognitionProfile() string { return c.rekognitionProfile }

// RekognitionRegion returns the AWS region used for Rekognition.
func (c *Config) RekognitionRegion() string { return c.rekognitionRegion }

// RekognitionCollection returns the Rekognition face collection id (may be empty).
func (c *Config) RekognitionCollection() string { return c.rekognitionCollection }

// Load reads and validates the config file at path for the media server
// (serve/scan/warm-cache). It requires a non-empty [libraries] section and
// verifies each library root exists on disk. An empty path falls back to
// DefaultConfigFilePath.
func Load(path string) (*Config, error) {
	return load(path, true)
}

// LoadWorker reads the config file at path for the enrich-worker, which runs on
// a separate machine (e.g. the GPU box) that has NO local copy of the media
// libraries — it reaches media only through the job API (see TECH_SPEC §3).
// It is identical to Load except it does NOT require or stat the [libraries]
// section, so the worker starts even though the library roots don't exist
// locally. Libraries() therefore returns empty for a worker config; the worker
// consumes [ai], [worker] shared_secret, [database] and [cache] instead.
func LoadWorker(path string) (*Config, error) {
	return load(path, false)
}

func load(path string, requireLibraries bool) (*Config, error) {
	if path == "" {
		path = DefaultConfigFilePath
	}
	f, err := ini.InsensitiveLoad(path)
	if err != nil {
		return nil, fmt.Errorf("config: load %q: %w", path, err)
	}
	return parse(f, requireLibraries)
}

func parse(f *ini.File, requireLibraries bool) (*Config, error) {
	c := &Config{
		listener:           defaultListener,
		dbPath:             defaultDBPath,
		cacheDir:           defaultCacheDir,
		debounce:           defaultDebounceSeconds * time.Second,
		rekognitionProfile: defaultRekognitionAWS,
		rekognitionRegion:  defaultRekognitionRgn,
	}

	if requireLibraries {
		if err := parseLibraries(f, c); err != nil {
			return nil, err
		}
	}
	parseHTTP(f, c)
	parseAuth(f, c)
	parseDatabase(f, c)
	parseCache(f, c)
	if err := parseIngest(f, c); err != nil {
		return nil, err
	}
	parseWorker(f, c)
	parseAI(f, c)
	return c, nil
}

func parseLibraries(f *ini.File, c *Config) error {
	sec := f.Section("libraries")
	keys := sec.Keys()
	if len(keys) == 0 {
		return errors.New("config: [libraries] section is empty; at least one alias=path is required")
	}
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		alias := k.Name()
		if _, dup := seen[alias]; dup {
			return fmt.Errorf("config: duplicate library alias %q", alias)
		}
		seen[alias] = struct{}{}

		root, err := expandPath(k.String())
		if err != nil {
			return fmt.Errorf("config: library %q: %w", alias, err)
		}
		info, err := os.Stat(root)
		if err != nil {
			return fmt.Errorf("config: library %q root %q: %w", alias, root, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("config: library %q root %q is not a directory", alias, root)
		}
		c.libraries = append(c.libraries, catalog.Library{
			Alias: alias,
			Name:  titleFor(alias),
			Root:  root,
		})
	}
	return nil
}

func parseHTTP(f *ini.File, c *Config) {
	if v := f.Section("http").Key("listener").String(); v != "" {
		c.listener = v
	}
}

func parseAuth(f *ini.File, c *Config) {
	c.authToken = strings.TrimSpace(f.Section("auth").Key("token").String())
}

func parseDatabase(f *ini.File, c *Config) {
	if v := f.Section("database").Key("path").String(); v != "" {
		if expanded, err := expandPath(v); err == nil {
			c.dbPath = expanded
		}
	}
}

func parseCache(f *ini.File, c *Config) {
	if v := f.Section("cache").Key("dir").String(); v != "" {
		if expanded, err := expandPath(v); err == nil {
			c.cacheDir = expanded
		}
	}
}

func parseIngest(f *ini.File, c *Config) error {
	key := f.Section("ingest").Key("debounce_seconds")
	if key.String() == "" {
		return nil
	}
	secs, err := key.Int()
	if err != nil {
		return fmt.Errorf("config: [ingest] debounce_seconds: %w", err)
	}
	if secs < 0 {
		return errors.New("config: [ingest] debounce_seconds must be >= 0")
	}
	c.debounce = time.Duration(secs) * time.Second
	return nil
}

func parseWorker(f *ini.File, c *Config) {
	c.workerSecret = strings.TrimSpace(f.Section("worker").Key("shared_secret").String())
}

func parseAI(f *ini.File, c *Config) {
	ai := f.Section("ai")
	c.ollamaURL = strings.TrimSpace(ai.Key("ollama_url").String())
	if v := strings.TrimSpace(ai.Key("rekognition_profile").String()); v != "" {
		c.rekognitionProfile = v
	}
	if v := strings.TrimSpace(ai.Key("rekognition_region").String()); v != "" {
		c.rekognitionRegion = v
	}
	c.rekognitionCollection = strings.TrimSpace(ai.Key("rekognition_collection").String())
}

// expandPath expands a leading "~" or "~/" (and "$HOME") to the user's home
// directory and returns an absolute, cleaned path.
func expandPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("empty path")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	} else if strings.HasPrefix(p, "$HOME") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand $HOME: %w", err)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "$HOME"))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// titleFor derives a human-friendly library name from its alias (e.g.
// "pictures" -> "Pictures").
func titleFor(alias string) string {
	if alias == "" {
		return alias
	}
	return strings.ToUpper(alias[:1]) + alias[1:]
}
