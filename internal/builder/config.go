package builder

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/pelletier/go-toml/v2"
	"github.com/sergi/go-diff/diffmatchpatch"
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
	Build       string   `toml:"build"`
}

// TargetSection defines the [target(.*)] section
type TargetSection struct {
	Lib     bool              `toml:"lib"`
	Sources []string          `toml:"sources"`
	Headers []string          `toml:"headers"`
	Defines map[string]string `toml:"defines"`
	Links   []string          `toml:"links"`
	Cflags  []string          `toml:"cflags"`
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

// unmarshalConditionalSection is a helper to parse, evaluate and merge multiple sections with conditional logic
func unmarshalConditionalSection[T any](rawCfg map[string]any, name string, dst *T, env ConfigEnv) error {
	sectionData, ok := rawCfg[name]
	if !ok {
		return nil
	}

	sectionMap, ok := sectionData.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid [%s] section format: expected a table", name)
	}

	baseFields := make(map[string]any)
	conditionalFields := make(map[string]map[string]any)

	for key, val := range sectionMap {
		if subMap, ok := val.(map[string]any); ok {
			_, err := expr.Compile(key, expr.Env(env))
			if err == nil {
				conditionalFields[key] = subMap
			} else {
				baseFields[key] = val
			}
		} else {
			baseFields[key] = val
		}
	}

	if len(baseFields) > 0 {
		if err := toml.Unmarshal([]byte(mustMarshal(baseFields)), dst); err != nil {
			return fmt.Errorf("failed to parse base [%s] section: %w", name, err)
		}
	}

	for expression, condMap := range conditionalFields {
		program, err := expr.Compile(expression, expr.Env(env))
		if err != nil {
			return fmt.Errorf("failed to compile expression for [%s.%q]: %w", name, expression, err)
		}

		result, err := expr.Run(program, env)
		if err != nil {
			return fmt.Errorf("failed to run expression for [%s.%q]: %w", name, expression, err)
		}

		// merge sections if the result is true
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

var exprRegex = regexp.MustCompile(`\{\{(.+?)\}\}`)

// evaluateString finds and evaluates all {{...}} expressions in a string
func evaluateString(s string, env ConfigEnv) (string, error) {
	matches := exprRegex.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s, nil
	}

	var builder strings.Builder
	lastIndex := 0

	for _, matchIndexes := range matches {
		fullMatchStart := matchIndexes[0]
		fullMatchEnd := matchIndexes[1]
		expressionStart := matchIndexes[2]
		expressionEnd := matchIndexes[3]

		builder.WriteString(s[lastIndex:fullMatchStart])

		expression := strings.TrimSpace(s[expressionStart:expressionEnd])
		program, err := expr.Compile(expression, expr.Env(env))
		if err != nil {
			return "", fmt.Errorf("failed to compile expression %q: %w", expression, err)
		}

		result, err := expr.Run(program, env)
		if err != nil {
			return "", fmt.Errorf("failed to run expression %q: %w", expression, err)
		}

		builder.WriteString(fmt.Sprintf("%v", result))
		lastIndex = fullMatchEnd
	}

	builder.WriteString(s[lastIndex:])

	return builder.String(), nil
}

// processExpressions recursively walks the parsed TOML data and evaluates expressions in strings
func processExpressions(data any, env ConfigEnv) (any, error) {
	switch v := data.(type) {
	case map[string]any:
		for key, val := range v {
			processedVal, err := processExpressions(val, env)
			if err != nil {
				return nil, err
			}
			v[key] = processedVal
		}
		return v, nil
	case []any:
		for i, item := range v {
			processedItem, err := processExpressions(item, env)
			if err != nil {
				return nil, err
			}
			v[i] = processedItem
		}
		return v, nil
	case string:
		return evaluateString(v, env)
	default:
		return data, nil
	}
}

func ParseConfig(rdr io.Reader, env ConfigEnv) (*Config, error) {
	var rawConfig map[string]any
	dec := toml.NewDecoder(rdr)
	if err := dec.Decode(&rawConfig); err != nil {
		if derr, ok := err.(*toml.DecodeError); ok {
			return nil, errors.New(derr.String())
		}
		return nil, err
	}

	processedConfig, err := processExpressions(rawConfig, env)
	if err != nil {
		return nil, fmt.Errorf("error processing expressions in config: %w", err)
	}
	rawConfig = processedConfig.(map[string]any)

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
func ParseConfigFromFile(path string, env ConfigEnv) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseConfig(bufio.NewReader(f), env)
}

//
// expr-lang helpers
//

func (cfg Config) RunBuildScript(env ConfigEnv) error {
	if cfg.Package.Build == "" {
		return nil
	}

	program, err := expr.Compile(cfg.Package.Build, expr.Env(env))
	if err != nil {
		return fmt.Errorf("failed to compile build script for package %q: %w", cfg.Package.Name, err)
	}
	result, err := expr.Run(program, env)
	if err != nil {
		return fmt.Errorf("failed to run build script for package %q: %w", cfg.Package.Name, err)
	}

	if result, ok := result.(bool); !ok || !result {
		return fmt.Errorf("build script for package %q returned false\n%s", cfg.Package.Name, cfg.Package.Build)
	}

	return nil
}

type ConfigEnv struct {
	TargetOS   string            `expr:"target_os"`
	TargetArch string            `expr:"target_arch"`
	Environ    map[string]string `expr:"environ"`
	basedir    string
}

func NewConfigEnv(basedir string) ConfigEnv {
	environ := make(map[string]string)
	for _, e := range os.Environ() {
		if i := strings.Index(e, "="); i >= 0 {
			environ[e[:i]] = e[i+1:]
		}
	}

	return ConfigEnv{
		TargetOS:   runtime.GOOS,
		TargetArch: runtime.GOARCH,
		Environ:    environ,
		basedir:    basedir,
	}
}

func (env ConfigEnv) Patch(path, patchText string) bool {
	fullPath := filepath.Join(env.basedir, path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		panic(err)
	}
	origText := string(data)

	dmp := diffmatchpatch.New()
	patches, err := dmp.PatchFromText(patchText)
	if err != nil {
		panic(err)
	}
	patchedText, results := dmp.PatchApply(patches, origText)
	for _, ok := range results {
		if ok {
			goto applied
		}
	}
	return false // nothing was applied, nothing to write

applied:
	err = os.WriteFile(fullPath, []byte(patchedText), 0644)
	if err != nil {
		panic(err)
	}

	return true
}

func (env ConfigEnv) ReadFile(path string) (string, error) {
	fullPath := filepath.Join(env.basedir, path)
	_, err := filepath.Rel(env.basedir, fullPath)
	if err != nil {
		panic(fmt.Sprintf("path %q is outside of package directory %q", path, env.basedir))
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		panic(err)
	}

	return string(data), nil
}
