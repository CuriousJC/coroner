// Package config loads coroner's optional configuration file.
//
// A config is never required. When none is found, Load returns a zero Config and
// no error, and coroner runs on its built-in defaults.
//
// Resolution order:
//
//  1. the path given to -config, which must exist or Load fails
//  2. coroner.yaml beside the executable
//  3. no config
//
// Resolving relative to the executable rather than the working directory means
// the same binary behaves the same way wherever it is invoked from, which is
// what you want for a tool that gets put on PATH.
package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FileName is the config file looked for beside the executable.
const FileName = "coroner.yaml"

// Config is the whole config file. Every field is optional.
type Config struct {
	Defaults Defaults `yaml:"defaults"`
	Embed    Embed    `yaml:"embed"`

	// Path records where this config came from, for reporting. Not read from
	// the file itself.
	Path string `yaml:"-"`
}

// Defaults override coroner's built-in flag defaults. An explicitly supplied
// flag still wins; see the precedence handling in cmd/coroner.
type Defaults struct {
	// Source is the root holding source directories. `coroner digest` with no
	// -source digests every corpus under it.
	Source string `yaml:"source"`

	// Digested is where the searchable corpus is written and read.
	Digested string `yaml:"digested"`

	Hits    int  `yaml:"hits"`
	Workers int  `yaml:"workers"`
	Verbose bool `yaml:"verbose"`
}

// Embed points at ollama.
type Embed struct {
	// BaseURL is where ollama listens. Empty means the default.
	BaseURL string `yaml:"base_url"`

	// Model is the embedding model.
	//
	// Changing this invalidates every vector already stored, and coroner will
	// refuse to search a corpus built with a different one rather than compare
	// vectors from two unrelated spaces. Changing it means re-digesting
	// everything.
	Model string `yaml:"model"`
}

// Loaded reports whether a config file was actually found and read.
func (c *Config) Loaded() bool {
	return c != nil && c.Path != ""
}

// DefaultPath returns the config path beside the executable.
func DefaultPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locating the executable: %w", err)
	}
	return filepath.Join(filepath.Dir(exe), FileName), nil
}

// Load reads the config. An explicit path is required to exist, on the grounds
// that being told to use a specific file and silently not using it is worse than
// failing. The default path is allowed to be absent.
func Load(explicitPath string) (*Config, error) {
	if explicitPath != "" {
		return readFile(explicitPath)
	}

	defaultPath, err := DefaultPath()
	if err != nil {
		// Not being able to locate our own executable is not a reason to refuse
		// to run; it just means there is no config to find.
		return &Config{}, nil
	}

	cfg, err := readFile(defaultPath)
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func readFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening config %s: %w", path, err)
	}
	defer f.Close()

	var cfg Config
	dec := yaml.NewDecoder(f)

	// Reject unknown fields. A typo in a config file that silently does nothing
	// is a bad afternoon; better to say so at startup.
	dec.KnownFields(true)

	// An empty file decodes to io.EOF. That is a valid, if pointless, config.
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	cfg.Path = path
	return &cfg, nil
}
