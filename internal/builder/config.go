package builder

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"

	"github.com/expr-lang/expr"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Package      PackageSection           `toml:"package"`
	RawTarget    map[string]TargetSection `toml:"target"`
	Target       TargetSection            `toml:"target"`
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
	Links   []string          `toml:"links"`
}

// mergeStructs merges the fields of the src struct into the dst struct
func mergeStructs(dst, src any) error {
	dstVal := reflect.ValueOf(dst)
	if dstVal.Kind() != reflect.Pointer || dstVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("dst must be a pointer to a struct")
	}

	dstElem := dstVal.Elem()
	srcVal := reflect.ValueOf(src)

	if srcVal.Kind() == reflect.Pointer {
		srcVal = srcVal.Elem()
	}

	if srcVal.Kind() != reflect.Struct {
		return fmt.Errorf("src must be a struct or a pointer to a struct")
	}

	if dstElem.Type() != srcVal.Type() {
		return fmt.Errorf("dst and src must be of the same struct type")
	}

	for i := range srcVal.NumField() {
		srcField := srcVal.Field(i)
		dstField := dstElem.Field(i)

		if !dstField.CanSet() {
			continue
		}

		switch dstField.Kind() {
		case reflect.Slice:
			if !srcField.IsNil() {
				dstField.Set(reflect.AppendSlice(dstField, srcField))
			}
		case reflect.Map:
			if !srcField.IsNil() {
				if dstField.IsNil() {
					dstField.Set(reflect.MakeMap(dstField.Type()))
				}
				for _, key := range srcField.MapKeys() {
					dstField.SetMapIndex(key, srcField.MapIndex(key))
				}
			}
		case reflect.Bool:
			dstField.SetBool(dstField.Bool() || srcField.Bool())
		default:
			if !srcField.IsZero() {
				dstField.Set(srcField)
			}
		}
	}

	return nil
}

func mustMarshal(v any) string {
	b, err := toml.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ParseConfig parses and validates a config file from a reader
func ParseConfig(rdr io.Reader, env map[string]any) (*Config, error) {
	var rawConfig map[string]any
	dec := toml.NewDecoder(rdr)
	if err := dec.Decode(&rawConfig); err != nil {
		if derr, ok := err.(*toml.DecodeError); ok {
			return nil, errors.New(derr.String())
		}
		return nil, err
	}

	cfg := new(Config)

	// TODO: this is fucking ugly, idk if we can use reflection for the expression thing
	if pkg, ok := rawConfig["package"]; ok {
		if err := toml.Unmarshal([]byte(mustMarshal(pkg)), &cfg.Package); err != nil {
			return nil, err
		}
	}
	if deps, ok := rawConfig["dependencies"]; ok {
		if err := toml.Unmarshal([]byte(mustMarshal(deps)), &cfg.Dependencies); err != nil {
			return nil, err
		}
	}

	if targetData, ok := rawConfig["target"]; ok {
		targetMap, ok := targetData.(map[string]any)
		if !ok {
			return nil, errors.New("invalid [target] section format")
		}

		if err := toml.Unmarshal([]byte(mustMarshal(targetMap)), &cfg.Target); err != nil {
			return nil, err
		}

		cfg.RawTarget = make(map[string]TargetSection)
		for key, val := range targetMap {
			if _, isMap := val.(map[string]any); isMap {
				var conditionalTarget TargetSection
				if err := toml.Unmarshal([]byte(mustMarshal(val)), &conditionalTarget); err != nil {
					return nil, err
				}
				cfg.RawTarget[key] = conditionalTarget
			}
		}
	}

	if len(cfg.RawTarget) > 0 {
		if err := cfg.evaluate(env); err != nil {
			return nil, err
		}
	}

	return cfg, nil
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
		if err := mergeStructs(&cfg.Target, target); err != nil {
			return err
		}
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
