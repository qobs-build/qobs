package builder

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strconv"

	"github.com/expr-lang/expr"
	"github.com/pelletier/go-toml/v2"
)

var defaultProfiles = map[string]ProfileSection{
	"release": {
		OptLevel: intOrString{Value: 3},
	},
	"debug": {
		OptLevel: intOrString{Value: ""}, // no -O
	},
}

type Config struct {
	Package      PackageSection            `toml:"package"`
	Target       TargetSection             `toml:"target"`
	Dependencies map[string]string         `toml:"dependencies"`
	Profile      map[string]ProfileSection `toml:"profile"`
}

func (c Config) Profiles() []string {
	profiles := make([]string, 0, len(c.Profile))
	for k := range c.Profile {
		profiles = append(profiles, k)
	}
	slices.Sort(profiles)
	return profiles
}

type intOrString struct {
	Value any
}

func (o *intOrString) UnmarshalTOML(v any) error {
	switch val := v.(type) {
	case int64:
		o.Value = int(val)
	case string:
		o.Value = val
	default:
		return fmt.Errorf("unexpected type: %T", v)
	}
	return nil
}

func (o *intOrString) String() string {
	if o == nil || o.Value == nil {
		return ""
	}

	switch v := o.Value.(type) {
	case int:
		return strconv.Itoa(v)
	case string:
		return v
	default:
		return ""
	}
}

// ProfileSection defines the [profile.*] section
type ProfileSection struct {
	OptLevel intOrString `toml:"opt-level"`
}

// PackageSection defines the [package] section
type PackageSection struct {
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	Authors     []string `toml:"authors"`
}

// TargetSection defines the [target(.*)] section
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

// unmarshalSection is a helper to parse sections without conditional logic
func unmarshalSection(rawCfg map[string]any, name string, dst any) error {
	if data, ok := rawCfg[name]; ok {
		if err := toml.Unmarshal([]byte(mustMarshal(data)), dst); err != nil {
			return fmt.Errorf("failed to parse [%s] section: %w", name, err)
		}
	}
	return nil
}

// unmarshalSection is a helper to parse, evaluate and merge multiple sections with conditional logic
func unmarshalConditionalSection[T any](rawCfg map[string]any, name string, dst *T, env map[string]any) error {
	sectionData, ok := rawCfg[name]
	if !ok {
		return nil
	}

	sectionMap, ok := sectionData.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid [%s] section format: expected a table", name)
	}

	if err := toml.Unmarshal([]byte(mustMarshal(sectionMap)), dst); err != nil {
		return fmt.Errorf("failed to parse base [%s] section: %w", name, err)
	}

	for expression, val := range sectionMap {
		condMap, isMap := val.(map[string]any)
		if !isMap {
			continue
		}

		program, err := expr.Compile(expression, expr.Env(env))
		if err != nil {
			return fmt.Errorf("failed to compile expression for [%s.%q]: %w", name, expression, err)
		}

		result, err := expr.Run(program, env)
		if err != nil {
			return fmt.Errorf("failed to run expression for [%s.%q]: %w", name, expression, err)
		}

		// merge sections if result is true
		if matched, ok := result.(bool); !ok || !matched {
			continue
		}

		var condSection T
		if err := toml.Unmarshal([]byte(mustMarshal(condMap)), &condSection); err != nil {
			return fmt.Errorf("failed to parse conditional section [%s.%q]: %w", name, expression, err)
		}
		if err := mergeStructs(dst, condSection); err != nil {
			return fmt.Errorf("failed to merge conditional section [%s.%q]: %w", name, expression, err)
		}
	}

	return nil
}

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
	cfg.Profile = defaultProfiles

	if err := unmarshalSection(rawConfig, "package", &cfg.Package); err != nil {
		return nil, err
	}
	if err := unmarshalConditionalSection(rawConfig, "dependencies", &cfg.Dependencies, env); err != nil {
		return nil, err
	}
	if err := unmarshalConditionalSection(rawConfig, "profile", &cfg.Profile, env); err != nil {
		return nil, err
	}
	if err := unmarshalConditionalSection(rawConfig, "target", &cfg.Target, env); err != nil {
		return nil, err
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

func NewConfigEnv() map[string]any {
	// TODO
	return map[string]any{
		"target_os":   runtime.GOOS,
		"target_arch": runtime.GOARCH,
	}
}
