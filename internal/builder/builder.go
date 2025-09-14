package builder

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/zeozeozeo/qobs/internal/builder/gen"
	"github.com/zeozeozeo/qobs/internal/msg"
)

var (
	errCantRunLib = errors.New("can't run a library target (target.lib is true)")
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
}

func NewBuilderInDirectory(path string) (*Builder, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	cfg, err := ParseConfigFromFile(filepath.Join(path, "Qobs.toml"))
	if err != nil {
		return nil, err
	}
	return &Builder{cfg: cfg, basedir: path}, nil
}

func resolveBuildGraph(rootPath string, depsDir string) (map[string]*Package, error) {
	packages := make(map[string]*Package)
	var queue []string
	processed := make(map[string]bool)

	// process the root package first
	rootConfig, err := ParseConfigFromFile(filepath.Join(rootPath, "Qobs.toml"))
	if err != nil {
		return nil, err
	}
	rootPackage := &Package{
		Name:   rootConfig.Package.Name,
		Path:   rootPath,
		Config: rootConfig,
		IsRoot: true,
	}
	packages[rootPackage.Name] = rootPackage
	processed[rootPackage.Name] = true

	// populate the initial queue
	for name := range rootPackage.Config.Dependencies {
		queue = append(queue, name)
	}

	for len(queue) > 0 {
		depName := queue[0]
		queue = queue[1:]

		if _, exists := processed[depName]; exists {
			continue
		}

		// we need to find the dependency source from a package that lists it
		var depSource string
		for _, p := range packages {
			if src, ok := p.Config.Dependencies[depName]; ok {
				depSource = src
				break
			}
		}
		if depSource == "" {
			return nil, fmt.Errorf("could not find source for dependency: %s", depName)
		}

		depPath := filepath.Join(depsDir, depName)

		// fetch dependency if it doesn't exist
		stat, err := os.Stat(depPath)
		if os.IsNotExist(err) || !stat.IsDir() {
			if err := os.MkdirAll(depPath, 0755); err != nil && !os.IsExist(err) {
				return nil, err
			}
			if _, err := fetchDependency(depSource, depPath); err != nil {
				return nil, fmt.Errorf("failed to fetch dependency %s: %w", depName, err)
			}
		}

		// parse its config and add it to the graph
		depConfig, err := ParseConfigFromFile(filepath.Join(depPath, "Qobs.toml"))
		if err != nil {
			return nil, fmt.Errorf("failed to parse config for dependency %s: %w", depName, err)
		}

		if depConfig.Package.Name != depName {
			msg.Warn("dependency '%s' has a mismatched package name: `%s`", depName, depConfig.Package.Name)
		}

		depPkg := &Package{
			Name:   depConfig.Package.Name,
			Path:   depPath,
			Config: depConfig,
		}
		packages[depPkg.Name] = depPkg
		processed[depPkg.Name] = true

		// add its dependencies to the queue
		for name := range depPkg.Config.Dependencies {
			if !processed[name] {
				queue = append(queue, name)
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
				stripmap[filepath.Dir(absPath)] = struct{}{}
			} else {
				files = append(files, absPath)
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

func includeCflags(paths []string) string {
	var cflagsb strings.Builder
	for _, includePath := range paths {
		cflagsb.WriteByte(' ')
		cflagsb.WriteString(`-I"`)
		cflagsb.WriteString(includePath)
		cflagsb.WriteByte('"')
	}
	return cflagsb.String()
}

// Build resolves the entire dependency graph and then generates a single build file
func (b *Builder) Build() error {
	buildDir := filepath.Join(b.basedir, "build")
	depsDir := filepath.Join(buildDir, "_deps")
	if err := os.MkdirAll(depsDir, 0755); err != nil {
		return err
	}

	// resolve buildgraph
	packages, err := resolveBuildGraph(b.basedir, depsDir)
	if err != nil {
		return fmt.Errorf("failed to resolve dependency graph: %w", err)
	}

	g := &gen.NinjaGen{}
	allIncludePaths := []string{}
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
		headers, err := b.collectFiles(pkg, pkg.Config.Target.Headers, true)
		if err != nil {
			return fmt.Errorf("failed to collect headers for %s: %w", pkg.Name, err)
		}
		allIncludePaths = append(allIncludePaths, headers...)

		// determine the outputs of its dependencies
		var depOutputs []string
		for depName := range pkg.Config.Dependencies {
			dep, ok := packages[depName]
			if !ok {
				return fmt.Errorf("internal error: resolved dependency `%s` not found in package map", depName)
			}
			if !dep.Config.Target.Lib {
				return fmt.Errorf("package `%s` depends on `%s`, which is not a library (target.lib = false)", pkg.Name, dep.Name)
			}
			depOutputs = append(depOutputs, dep.outputName())
		}

		g.AddTarget(
			pkg.outputName(),
			pkg.Path,
			sources,
			depOutputs,
			pkg.Config.Target.Lib,
		)
	}

	if rootPkg == nil {
		return errors.New("internal error: root package not found after graph resolution")
	}

	// generate the buildfile
	cflags := includeCflags(allIncludePaths)
	g.SetCompiler(cflags, "", findCompiler(false), findCompiler(true))

	out := g.Generate()
	buildFile := filepath.Join(buildDir, g.BuildFile())
	if err = os.WriteFile(buildFile, []byte(out), 0644); err != nil {
		return err
	}

	if err := g.Invoke(buildDir); err != nil {
		return err
	}

	return nil
}

func (b *Builder) BuildAndRun(args []string) error {
	if b.cfg.Target.Lib {
		return errCantRunLib
	}

	if err := b.Build(); err != nil {
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
