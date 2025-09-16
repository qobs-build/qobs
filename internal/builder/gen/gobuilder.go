package gen

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/zeozeozeo/qobs/internal/msg"
	"golang.org/x/sync/errgroup"
)

// BuildState represents the state of a build target for incremental builds
type BuildState struct {
	Sources      map[string]string `json:"sources,omitempty"`      // source file -> hash
	Dependencies map[string]string `json:"dependencies,omitempty"` // dependency string -> hash
	Cflags       []string          `json:"cflags,omitempty"`       // compilation flags
	Ldflags      []string          `json:"ldflags,omitempty"`      // linker flags
}

// compileJob represents a single compilation job
type compileJob struct {
	src    string
	obj    string
	cflags []string
	isCxx  bool
	cc     string
}

// linkJob represents a linking job
type linkJob struct {
	objs    []string
	deps    []string
	out     string
	ldflags []string
	isLib   bool
	isCxx   bool
	cc      string
}

type GoBuilder struct {
	cc, cxx    string
	targets    map[string]buildUnit
	buildDir   string
	stateFile  string
	buildState map[string]*BuildState
	jobs       int
}

func NewGoBuilder() *GoBuilder {
	return &GoBuilder{
		targets:    make(map[string]buildUnit),
		buildState: make(map[string]*BuildState),
		jobs:       runtime.NumCPU(),
	}
}

func (g *GoBuilder) SetCompiler(cc, cxx string) {
	g.cc, g.cxx = cc, cxx
}

func (g *GoBuilder) BuildFile() string {
	return "qobs_build_state.json"
}

// AddTarget adds a package (library or executable) to the build graph
func (g *GoBuilder) AddTarget(name, basedir string, sources, dependencies []string, isLib bool, cflags, ldflags []string) {
	targetSources := make([]sourceFile, 0, len(sources))
	for _, srcPath := range sources {
		rel, err := filepath.Rel(basedir, srcPath)
		if err != nil {
			rel = filepath.Base(srcPath)
			msg.Warn("source file %s is outside of base directory %s", srcPath, basedir)
		}

		objPath := filepath.Join("QobsFiles", name+".dir", rel+".obj")
		targetSources = append(targetSources, sourceFile{src: srcPath, obj: objPath, isCxx: isCxx(srcPath)})
	}

	g.targets[name] = buildUnit{
		name:         name,
		isLib:        isLib,
		sources:      targetSources,
		dependencies: dependencies,
		cflags:       cflags,
		ldflags:      ldflags,
		basedir:      basedir,
	}
}

func (g *GoBuilder) Generate() string {
	return "" // no build file needed
}

// Invoke performs the actual build
func (g *GoBuilder) Invoke(buildDir string) error {
	g.buildDir = buildDir
	g.stateFile = filepath.Join(buildDir, g.BuildFile())

	if err := g.loadBuildState(); err != nil {
		msg.Warn("failed to load build state: %v", err)
	}

	for _, target := range g.targets {
		if err := g.buildTarget(target); err != nil {
			return fmt.Errorf("failed to build target %s: %w", target.name, err)
		}
	}

	if err := g.saveBuildState(); err != nil {
		msg.Warn("failed to save build state: %v", err)
	}

	return nil
}

// loadBuildState loads the previous build state from disk
func (g *GoBuilder) loadBuildState() error {
	f, err := os.Open(g.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no previous state, that's fine
		}
		return err
	}
	defer f.Close()
	return json.NewDecoder(bufio.NewReader(f)).Decode(&g.buildState)
}

// saveBuildState saves the current build state to disk
func (g *GoBuilder) saveBuildState() error {
	data, err := json.MarshalIndent(g.buildState, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(g.stateFile, data, 0644)
}

// fileHash computes the SHA256 hash of a file
func fileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// needsRebuild checks if a target needs to be rebuilt
func (g *GoBuilder) needsRebuild(target buildUnit) (bool, error) {
	state, exists := g.buildState[target.name]
	if !exists {
		return true, nil // no build state
	}

	// check if flags changed
	if !slices.Equal(state.Cflags, target.cflags) || !slices.Equal(state.Ldflags, target.ldflags) {
		return true, nil
	}

	// check if output exists
	outputPath := filepath.Join(g.buildDir, target.name)
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return true, nil
	}

	// check hashes
	for _, src := range target.sources {
		hash, err := fileHash(src.src)
		if err != nil {
			return true, err
		}

		if prevHash, exists := state.Sources[src.src]; !exists || prevHash != hash {
			return true, nil
		}
	}

	// check dependency hashes
	for _, dep := range target.dependencies {
		depPath := filepath.Join(g.buildDir, dep)
		hash, err := fileHash(depPath)
		if err != nil {
			return true, err
		}

		if prevHash, exists := state.Dependencies[dep]; !exists || prevHash != hash {
			return true, nil
		}
	}

	return false, nil
}

// buildTarget builds a single target
func (g *GoBuilder) buildTarget(target buildUnit) error {
	needsRebuild, err := g.needsRebuild(target)
	if err != nil {
		return err
	}

	if !needsRebuild {
		return nil
	}

	objDir := filepath.Join(g.buildDir, "QobsFiles", target.name+".dir")
	if err := os.MkdirAll(objDir, 0755); err != nil {
		return fmt.Errorf("failed to create object directory: %w", err)
	}

	// compile sources
	compileJobs := make([]compileJob, len(target.sources))

	for i, src := range target.sources {
		objPath := filepath.Join(g.buildDir, src.obj)
		compiler := g.cc
		if src.isCxx {
			compiler = g.cxx
		}

		compileJobs[i] = compileJob{
			src:    src.src,
			obj:    objPath,
			cflags: target.cflags,
			isCxx:  src.isCxx,
			cc:     compiler,
		}
	}

	if err := g.runCompileJobs(compileJobs); err != nil {
		return err
	}

	// link
	objects := make([]string, len(target.sources))
	for i, src := range target.sources {
		objects[i] = filepath.Join(g.buildDir, src.obj)
	}

	dependencies := make([]string, len(target.dependencies))
	for i, dep := range target.dependencies {
		dependencies[i] = filepath.Join(g.buildDir, dep)
	}

	outputPath := filepath.Join(g.buildDir, target.name)
	linkJob := linkJob{
		objs:    objects,
		deps:    dependencies,
		out:     outputPath,
		ldflags: target.ldflags,
		isLib:   target.isLib,
		isCxx:   g.hasCxxInTarget(target),
		cc:      g.cc,
	}
	if linkJob.isCxx {
		linkJob.cc = g.cxx
	}

	if err := g.runLinkJob(linkJob); err != nil {
		return err
	}

	if err := g.updateBuildState(target); err != nil {
		return err
	}

	return nil
}

// hasCxxInTarget checks if target or its dependencies have C++ sources
func (g *GoBuilder) hasCxxInTarget(target buildUnit) bool {
	for _, src := range target.sources {
		if src.isCxx {
			return true
		}
	}

	for _, depName := range target.dependencies {
		if depTarget, exists := g.targets[depName]; exists {
			for _, src := range depTarget.sources {
				if src.isCxx {
					return true
				}
			}
		}
	}

	return false
}

// runCompileJobs runs compilation jobs in parallel
func (g *GoBuilder) runCompileJobs(jobs []compileJob) error {
	if len(jobs) == 0 {
		return nil
	}

	eg, _ := errgroup.WithContext(context.Background())
	eg.SetLimit(g.jobs)

	for _, job := range jobs {
		eg.Go(func() error {
			return runCompileJob(job)
		})
	}

	return eg.Wait()
}

// runCompileJob runs a single compilation job
func runCompileJob(job compileJob) error {
	if err := os.MkdirAll(filepath.Dir(job.obj), 0755); err != nil {
		return fmt.Errorf("failed to create object directory: %w", err)
	}

	args := make([]string, 0, len(job.cflags)+4)
	args = append(args, job.cflags...)
	args = append(args, "-c", job.src, "-o", job.obj)

	cmd := exec.Command(job.cc, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("CC %s\n", job.obj)
	return cmd.Run()
}

// runLinkJob runs a linking job
func (g *GoBuilder) runLinkJob(job linkJob) error {
	if job.isLib {
		args := []string{"rcs", job.out}
		args = append(args, job.objs...)
		args = append(args, job.deps...)

		cmd := exec.Command("ar", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		fmt.Printf("AR %s\n", job.out)
		return cmd.Run()
	} else {
		args := []string{"-o", job.out}
		args = append(args, job.objs...)
		args = append(args, job.deps...)
		args = append(args, job.ldflags...)

		cmd := exec.Command(job.cc, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		fmt.Printf("LINK %s\n", job.out)
		return cmd.Run()
	}
}

// updateBuildState updates the build state for a target
func (g *GoBuilder) updateBuildState(target buildUnit) error {
	state := &BuildState{
		Sources:      make(map[string]string),
		Dependencies: make(map[string]string),
		Cflags:       target.cflags,
		Ldflags:      target.ldflags,
	}

	// hash source files
	for _, src := range target.sources {
		hash, err := fileHash(src.src)
		if err != nil {
			return fmt.Errorf("failed to hash source file %s: %w", src.src, err)
		}
		state.Sources[src.src] = hash
	}

	// hash dependencies
	for _, dep := range target.dependencies {
		depPath := filepath.Join(g.buildDir, dep)
		hash, err := fileHash(depPath)
		if err != nil {
			return fmt.Errorf("failed to hash dependency %s: %w", dep, err)
		}
		state.Dependencies[dep] = hash
	}

	g.buildState[target.name] = state
	return nil
}
