package builder

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

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
	Package            PackageSection            `toml:"package"`
	Target             TargetSection             `toml:"target"`
	Dependencies       map[string]Dependency     `toml:"dependencies"`
	Profile            map[string]ProfileSection `toml:"profile"`
	Features           FeaturesSection           `toml:"features"`
	enabledDepFeatures map[string][]string
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

type Dependency struct {
	Source          string   `toml:"dep"`
	DefaultFeatures bool     `toml:"default-features"`
	Features        []string `toml:"features"`
}

func (d *Dependency) UnmarshalTOML(v any) error {
	switch val := v.(type) {
	case string:
		d.Source = val
		d.DefaultFeatures = true
	case map[string]any:
		d.DefaultFeatures = true
		if df, ok := val["default-features"].(bool); ok {
			d.DefaultFeatures = df
		}
		if src, ok := val["dep"].(string); ok {
			d.Source = src
		} else {
			return errors.New("dependency table must contain a `dep` key with a source string")
		}
		if features, ok := val["features"].([]any); ok {
			for _, f := range features {
				if featureStr, ok := f.(string); ok {
					d.Features = append(d.Features, featureStr)
				}
			}
		}
	default:
		return fmt.Errorf("unexpected type for dependency: %T", v)
	}
	return nil
}

// FeaturesSection defines the [features] section
type FeaturesSection map[string][]string

func (f FeaturesSection) ResolveFeatures(requested []string, useDefault bool) (
	ownFeatures map[string]bool,
	depFeatures map[string][]string,
	err error,
) {
	ownFeatures = make(map[string]bool)
	depFeatures = make(map[string][]string)
	queue := slices.Clone(requested)

	if useDefault {
		if defaultFeatures, ok := f["default"]; ok {
			queue = append(queue, defaultFeatures...)
		}
	}

	for len(queue) > 0 {
		feature := queue[0]
		queue = queue[1:]

		// handle `dep/feature` syntax
		if parts := strings.SplitN(feature, "/", 2); len(parts) == 2 {
			depName, featureName := parts[0], parts[1]
			if !slices.Contains(depFeatures[depName], featureName) {
				depFeatures[depName] = append(depFeatures[depName], featureName)
			}
			continue
		}

		// feature is for the current package
		if _, exists := ownFeatures[feature]; exists {
			continue
		}
		ownFeatures[feature] = true

		// if this feature enables other features, add them to the queue
		if subFeatures, ok := f[feature]; ok {
			queue = append(queue, subFeatures...)
		}
	}

	return ownFeatures, depFeatures, nil
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
			_, err := expr.Compile(key, env.exprOptions()...)
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
		program, err := expr.Compile(expression, env.exprOptions()...)
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
		program, err := expr.Compile(expression, env.exprOptions()...)
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

func ParseConfig(rdr io.Reader, env ConfigEnv, defaultFeatures bool) (*Config, error) {
	var rawConfig map[string]any
	dec := toml.NewDecoder(rdr)
	if err := dec.Decode(&rawConfig); err != nil {
		if derr, ok := err.(*toml.DecodeError); ok {
			return nil, errors.New(derr.String())
		}
		return nil, err
	}

	// parse/resolve features
	var featuresSection FeaturesSection
	if err := unmarshalSection(rawConfig, "features", &featuresSection); err != nil {
		return nil, err
	}

	requestedFeatures := make([]string, 0, len(env.Features))
	for feature, enabled := range env.Features {
		if enabled {
			requestedFeatures = append(requestedFeatures, feature)
		}
	}
	enabledFeatures, depFeatures, err := featuresSection.ResolveFeatures(requestedFeatures, defaultFeatures)
	if err != nil {
		return nil, err
	}

	// add features to env and move on with the rest of the config
	env2 := env
	env2.Features = enabledFeatures

	// process exprs in strings (e.g. "{{ environ[...] }}")
	processedConfig, err := processExpressions(rawConfig, env2)
	if err != nil {
		return nil, fmt.Errorf("error processing expressions in config: %w", err)
	}
	rawConfig = processedConfig.(map[string]any)

	cfg := new(Config)
	cfg.Profile = defaultProfiles
	cfg.Features = featuresSection
	cfg.enabledDepFeatures = depFeatures

	if err := unmarshalSection(rawConfig, "package", &cfg.Package); err != nil {
		return nil, err
	}
	if err := unmarshalConditionalSection(rawConfig, "dependencies", &cfg.Dependencies, env2); err != nil {
		return nil, err
	}
	if err := unmarshalConditionalSection(rawConfig, "profile", &cfg.Profile, env2); err != nil {
		return nil, err
	}
	if err := unmarshalConditionalSection(rawConfig, "target", &cfg.Target, env2); err != nil {
		return nil, err
	}

	return cfg, nil
}

// ParseConfigFromFile parses and validates a config file from a filepath
func ParseConfigFromFile(path string, env ConfigEnv, defaultFeatures bool) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseConfig(bufio.NewReader(f), env, defaultFeatures)
}

//
// expr-lang helpers
//

func (cfg Config) RunBuildScript(env ConfigEnv) error {
	if cfg.Package.Build == "" {
		return nil
	}

	program, err := expr.Compile(cfg.Package.Build, env.exprOptions()...)
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
	Features   map[string]bool   `expr:"-"`
	basedir    string
}

func (e ConfigEnv) exprOptions() []expr.Option {
	return []expr.Option{
		expr.Env(e),
		expr.Function("feature", func(features ...any) (any, error) {
			for i, f := range features {
				ff, ok := f.(string)
				if !ok {
					return false, fmt.Errorf("argument %d must be string", i+1)
				}
				if !e.Features[ff] {
					return false, nil
				}
			}
			return true, nil
		}),
	}
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
		Features:   make(map[string]bool),
		basedir:    basedir,
	}
}

func NewConfigEnvWithFeatures(basedir string, features map[string]bool) ConfigEnv {
	env := NewConfigEnv(basedir)
	env.Features = features
	return env
}
