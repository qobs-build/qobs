package builder

import (
	"bufio"
	"errors"
	"io"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Package      PackageSection    `toml:"package"`
	Target       TargetSection     `toml:"target"`
	Dependencies map[string]string `toml:"dependencies"`
}

// PackageSection defines the [package] section
type PackageSection struct {
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	Authors     []string `toml:"authors"`
}

// TargetSection defines the [target] section
type TargetSection struct {
	Lib     bool     `toml:"lib"`
	Sources []string `toml:"sources"`
	Headers []string `toml:"headers"`
}

// ParseConfig parses and validates a config file from a reader
func ParseConfig(rdr io.Reader) (*Config, error) {
	dec := toml.NewDecoder(rdr)
	dec.DisallowUnknownFields()
	cfg := new(Config)
	if err := dec.Decode(cfg); err != nil {
		if derr, ok := err.(*toml.DecodeError); ok {
			return nil, errors.New(derr.String())
		}
		return nil, err
	}
	return cfg, nil
}

// ParseConfigFromFile parses and validates a config file from a filepath
func ParseConfigFromFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseConfig(bufio.NewReader(f))
}
