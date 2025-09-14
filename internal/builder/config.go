package builder

import (
	"bufio"
	"errors"
	"io"
	"maps"
	"os"
	"runtime"

	"github.com/expr-lang/expr"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Package      PackageSection           `toml:"package"`
	RawTarget    map[string]TargetSection `toml:"target"`
	Target       TargetSection            `toml:"-"`
	Dependencies map[string]string        `toml:"dependencies"`
}

// PackageSection defines the [package] section
type PackageSection struct {
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	Authors     []string `toml:"authors"`
}

// TargetSection defines the [target] section
type TargetSection struct {
	Lib     bool              `toml:"lib"`
	Sources []string          `toml:"sources"`
	Headers []string          `toml:"headers"`
	Defines map[string]string `toml:"defines"`
}

func (t *TargetSection) merge(other TargetSection) {
	t.Lib = t.Lib || other.Lib
	t.Sources = append(t.Sources, other.Sources...)
	t.Headers = append(t.Headers, other.Headers...)
	if t.Defines == nil {
		t.Defines = make(map[string]string)
	}
	maps.Copy(t.Defines, other.Defines)
}

// ParseConfig parses and validates a config file from a reader
func ParseConfig(rdr io.Reader, env map[string]any) (*Config, error) {
	dec := toml.NewDecoder(rdr)
	dec.DisallowUnknownFields()
	cfg := new(Config)
	if err := dec.Decode(cfg); err != nil {
		if derr, ok := err.(*toml.DecodeError); ok {
			return nil, errors.New(derr.String())
		}
		return nil, err
	}
	err := cfg.evaluate(env)
	return cfg, err
}

// ParseConfigFromFile parses and validates a config file from a filepath
func ParseConfigFromFile(path string, env map[string]any) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseConfig(bufio.NewReader(f), env)
}

func (cfg *Config) evaluate(env map[string]any) error {
	for expression, target := range cfg.RawTarget {
		program, err := expr.Compile(expression, expr.Env(env))
		if err != nil {
			return err
		}

		result, err := expr.Run(program, env)
		if err != nil {
			return err
		}

		if matched, ok := result.(bool); !ok || !matched {
			continue
		}

		// merge the sections
		cfg.Target.merge(target)
	}
	return nil
}

func NewConfigEnv() map[string]any {
	// TODO
	return map[string]any{
		"target_os":   runtime.GOOS,
		"target_arch": runtime.GOARCH,
	}
}
