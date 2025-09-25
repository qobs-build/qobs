package builder

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/qobs-build/qobs/internal/builder/gen"
	"github.com/qobs-build/qobs/internal/msg"
)

var (
	errCantRunLib = errors.New("can't run a library target (target.lib is true)")
)

const (
	GeneratorNinja  = "ninja"
	GeneratorQobs   = "qobs"
	GeneratorVS2022 = "vs2022"
)

// Package represents a single component (root package or dependency) in the build graph
type Package struct {
	Name   string
	Path   string
	Config *Config
	IsRoot bool
}

// outputName returns the desired artifact name for this package (e.g., `my_app.exe` or `libmy_lib.a`)
func (p *Package) outputName() string {
	pkgName := p.Config.Package.Name
	if p.Config.Target.Lib {
		if runtime.GOOS == "windows" {
			return pkgName + ".lib"
		}
		return "lib" + pkgName + ".a"
	}
	if runtime.GOOS == "windows" {
		return pkgName + ".exe"
	}
	return pkgName
}

type Builder struct {
	cfg     *Config
	basedir string
	env     ConfigEnv
}

func NewBuilderInDirectory(path string, features []string, defaultFeatures bool) (*Builder, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	featureMap := make(map[string]bool)
	for _, feature := range features {
		featureMap[feature] = true
	}

	env := NewConfigEnvWithFeatures(path, featureMap)
	cfg, err := ParseConfigFromFile(filepath.Join(path, "Qobs.toml"), env, defaultFeatures)
	if err != nil {
		return nil, err
	}
	return &Builder{cfg: cfg, basedir: path, env: env}, nil
}

func (b *Builder) resolveBuildGraph(rootPath string, depsDir string) (map[string]*Package, error) {
	packages := make(map[string]*Package)
	depSpecs := make(map[string]Dependency)

	rootPackage := &Package{
		Name:   b.cfg.Package.Name,
		Path:   rootPath,
		Config: b.cfg,
		IsRoot: true,
	}
	packages[rootPackage.Name] = rootPackage

	// pass 1: resolve dependencies
	queue := make([]string, 0)
	for name, dep := range b.cfg.Dependencies {
		depSpecs[name] = dep
		queue = append(queue, name)
	}

	for i := 0; i < len(queue); i++ {
		depName := queue[i]
		if _, exists := packages[depName]; exists {
			continue
		}

		depSpec, ok := depSpecs[depName]
		if !ok {
			return nil, fmt.Errorf("internal error: dependency %q has no section", depName)
		}

		depPath := filepath.Join(depsDir, depName)

		// fetch dependency if it doesn't exist
		stat, err := os.Stat(depPath)
		if os.IsNotExist(err) || !stat.IsDir() {
			if err := os.MkdirAll(depPath, 0755); err != nil && !os.IsExist(err) {
				return nil, err
			}
			if _, err := fetchDependency(depSpec.Source, depPath); err != nil {
				return nil, fmt.Errorf("failed to fetch dependency %q: %w", depName, err)
			}
		}

		// parse config with no features
		env := NewConfigEnv(depPath)
		depConfig, err := ParseConfigFromFile(filepath.Join(depPath, "Qobs.toml"), env, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse initial config for dependency %q: %w", depName, err)
		}

		if depConfig.Package.Name != depName {
			msg.Warn("dependency %q has a mismatched package name: %q", depName, depConfig.Package.Name)
		}

		packages[depName] = &Package{
			Name:   depConfig.Package.Name,
			Path:   depPath,
			Config: depConfig,
		}

		for name, dep := range depConfig.Dependencies {
			if _, ok := depSpecs[name]; !ok {
				depSpecs[name] = dep
			}
			queue = append(queue, name)
		}
	}

	// pass 2: resolve features
	finalFeatures := make(map[string]map[string]bool)
	finalFeatures[b.cfg.Package.Name] = b.env.Features

	changed := true
	for changed {
		changed = false

		for pkgName, pkg := range packages {
			if pkg.IsRoot {
				continue
			}

			requestedFeatures := make(map[string]bool)
			useDefaultFeatures := false

			for _, parentPkg := range packages {
				if dep, isDependency := parentPkg.Config.Dependencies[pkgName]; isDependency {
					if dep.DefaultFeatures {
						useDefaultFeatures = true
					}
					for _, f := range dep.Features {
						requestedFeatures[f] = true
					}
					if parentPkg.Config.enabledDepFeatures != nil {
						for _, f := range parentPkg.Config.enabledDepFeatures[pkgName] {
							requestedFeatures[f] = true
						}
					}
				}
			}

			if !maps.Equal(finalFeatures[pkgName], requestedFeatures) {
				changed = true
				finalFeatures[pkgName] = requestedFeatures

				env := NewConfigEnvWithFeatures(pkg.Path, requestedFeatures)
				newConfig, err := ParseConfigFromFile(filepath.Join(pkg.Path, "Qobs.toml"), env, useDefaultFeatures)
				if err != nil {
					return nil, fmt.Errorf("failed to parse config for package %q: %w", pkgName, err)
				}
				pkg.Config = newConfig
			}
		}
	}

	return packages, nil
}

func (b *Builder) collectFiles(pkg *Package, patterns []string, stripFilename bool) ([]string, error) {
	var files []string
	var stripmap map[string]struct{}
	if stripFilename {
		stripmap = map[string]struct{}{}
	}
	fsys := os.DirFS(pkg.Path)

	var globparams []doublestar.GlobOption
	if !stripFilename {
		globparams = append(globparams, doublestar.WithFilesOnly())
	}

	for _, pat := range patterns {
		if filepath.IsAbs(pat) {
			files = append(files, filepath.Clean(pat))
			continue
		}
		matches, err := doublestar.Glob(fsys, pat, globparams...)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			absPath, err := filepath.Abs(filepath.Join(pkg.Path, match))
			if err != nil {
				return nil, fmt.Errorf("while globbing directory %s: %w", match, err)
			}
			if stripFilename {
				if stat, err := os.Stat(absPath); err == nil && !stat.IsDir() {
					stripmap[filepath.Dir(filepath.Clean(absPath))] = struct{}{} // this is a file, we need directories
				} else {
					stripmap[absPath] = struct{}{}
				}
			} else {
				files = append(files, filepath.Clean(absPath))
			}
		}
	}

	if stripFilename {
		for dir := range stripmap {
			files = append(files, dir)
		}
	}

	return files, nil
}

func createGenerator(generator string) gen.Generator {
	switch generator {
	case GeneratorNinja:
		return &gen.NinjaGen{}
	case GeneratorQobs:
		return gen.NewQobsBuilder()
	case GeneratorVS2022:
		return gen.NewVS2022Gen()
	default:
		panic("createGenerator: unreachable")
	}
}

func (b *Builder) makeCflags(profile string) ([]string, error) {
	if prof, ok := b.cfg.Profile[profile]; ok {
		var cflags []string
		optLevel := prof.OptLevel.String()
		if optLevel != "" {
			cflags = append(cflags, "-O"+optLevel)
		}
		return cflags, nil
	}
	return nil, fmt.Errorf("unknown profile %q, known profiles: %s", profile, strings.Join(b.cfg.Profiles(), ", "))
}

// Build resolves the entire dependency graph and then invokes the generator (or builder)
func (b *Builder) Build(profile, generator string) error {
	buildDir := filepath.Join(b.basedir, "build")
	depsDir := filepath.Join(buildDir, "_deps")
	if err := os.MkdirAll(depsDir, 0755); err != nil {
		return err
	}

	globalCflags, err := b.makeCflags(profile)
	if err != nil {
		return err
	}

	// resolve buildgraph
	packages, err := b.resolveBuildGraph(b.basedir, depsDir)
	if err != nil {
		return fmt.Errorf("failed to resolve dependency graph: %w", err)
	}

	g := createGenerator(generator)
	var rootPkg *Package

	// add targets
	for _, pkg := range packages {
		if pkg.IsRoot {
			rootPkg = pkg
		}

		// collect files for the package
		sources, err := b.collectFiles(pkg, pkg.Config.Target.Sources, false)
		if err != nil {
			return fmt.Errorf("failed to collect sources for %s: %w", pkg.Name, err)
		}

		// collect own headers
		ownHeaders, err := b.collectFiles(pkg, pkg.Config.Target.Headers, true)
		if err != nil {
			return fmt.Errorf("failed to collect headers for %s: %w", pkg.Name, err)
		}

		// determine the outputs of its dependencies
		var depOutputs []string
		cflags := slices.Clone(globalCflags)

		cflags = append(cflags, pkg.Config.Target.Cflags...)

		// add own include paths to cflags
		for _, includePath := range ownHeaders {
			cflags = append(cflags, "-I"+includePath)
		}

		for depName := range pkg.Config.Dependencies {
			dep, ok := packages[depName]
			if !ok {
				return fmt.Errorf("internal error: resolved dependency %q not found in package map", depName)
			}

			depHeaders, err := b.collectFiles(dep, dep.Config.Target.Headers, true)
			if err != nil {
				return fmt.Errorf("failed to collect headers for dependency %q: %w", dep.Name, err)
			}
			for _, includePath := range depHeaders {
				cflags = append(cflags, "-I"+includePath)
			}

			// don't produce link artifacts for header-only deps
			if dep.Config.Target.HeaderOnly {
				continue
			}

			if !dep.Config.Target.Lib {
				return fmt.Errorf("package %q depends on %q, which is not a library (target.lib = false)", pkg.Name, dep.Name)
			}

			depOutputs = append(depOutputs, dep.outputName())
		}

		// build ldflags
		var ldflags []string

		seen := make(map[string]bool)
		var collectLinks func(string)
		collectLinks = func(name string) {
			if seen[name] {
				return
			}
			seen[name] = true
			dep, ok := packages[name]
			if !ok {
				return
			}
			for _, lib := range dep.Config.Target.Links {
				ldflags = append(ldflags, "-l"+lib)
			}
			for child := range dep.Config.Dependencies {
				collectLinks(child)
			}
		}

		for depName := range pkg.Config.Dependencies {
			collectLinks(depName)
		}

		for define, v := range pkg.Config.Target.Defines {
			if v != "" {
				cflags = append(cflags, "-D"+define+"="+v) // TODO: escape this?
			} else {
				cflags = append(cflags, "-D"+define)
			}
		}

		for _, lib := range pkg.Config.Target.Links {
			ldflags = append(ldflags, "-l"+lib)
		}

		if err := pkg.Config.RunBuildScript(b.env); err != nil {
			return err
		}

		if !pkg.Config.Target.HeaderOnly {
			g.AddTarget(
				pkg.outputName(),
				pkg.Path,
				sources,
				depOutputs,
				pkg.Config.Target.Lib,
				cflags,
				ldflags,
			)
		}
	}

	if rootPkg == nil {
		return errors.New("internal error: root package not found after graph resolution")
	}

	// generate the buildfile
	g.SetCompiler(findCompiler(false), findCompiler(true))

	out := g.Generate()
	if out != "" {
		buildFile := filepath.Join(buildDir, g.BuildFile())
		if err = os.WriteFile(buildFile, []byte(out), 0644); err != nil {
			return err
		}
	}

	if err := g.Invoke(buildDir); err != nil {
		return err
	}

	return nil
}

func (b *Builder) BuildAndRun(args []string, profile, generator string) error {
	if b.cfg.Target.Lib {
		return errCantRunLib
	}

	if err := b.Build(profile, generator); err != nil {
		return err
	}

	outputName := b.cfg.Package.Name
	if runtime.GOOS == "windows" {
		outputName += ".exe"
	}

	cmd := exec.Command(filepath.Join(b.basedir, "build", outputName), args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
