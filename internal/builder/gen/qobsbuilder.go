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
	name    string
	objs    []string
	deps    []string
	out     string
	ldflags []string
	isLib   bool
	isCxx   bool
	cc      string
}

type QobsBuilder struct {
	cc, cxx    string
	targets    map[string]buildUnit
	buildDir   string
	stateFile  string
	buildState map[string]*BuildState
	jobs       int
	hashCache  map[string]string
}

func NewQobsBuilder() *QobsBuilder {
	return &QobsBuilder{
		targets:    make(map[string]buildUnit),
		buildState: make(map[string]*BuildState),
		jobs:       runtime.NumCPU(),
		hashCache:  make(map[string]string),
	}
}

func (g *QobsBuilder) SetCompiler(cc, cxx string) {
	g.cc, g.cxx = cc, cxx
}

func (g *QobsBuilder) BuildFile() string {
	return "qobs_build_state.json"
}

// AddTarget adds a package (library or executable) to the build graph
func (g *QobsBuilder) AddTarget(name, basedir string, sources, dependencies []string, isLib bool, cflags, ldflags []string) {
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

func (g *QobsBuilder) Generate() string {
	return "" // no build file needed
}

// Invoke performs the actual build
func (g *QobsBuilder) Invoke(buildDir string) error {
	g.buildDir = buildDir
	g.stateFile = filepath.Join(buildDir, g.BuildFile())

	if err := g.loadBuildState(); err != nil {
		msg.Warn("failed to load build state: %v", err)
	}

	sortedTargetNames, err := g.topologicalSortTargets()
	if err != nil {
		return err
	}

	compileJobs, linkJobs, err := g.planBuild(sortedTargetNames)
	if err != nil {
		return fmt.Errorf("build planning failed: %w", err)
	}

	if len(compileJobs) == 0 && len(linkJobs) == 0 {
		fmt.Println("qobs: no work to do.")
		return nil
	}

	if err := g.executeBuild(compileJobs, linkJobs); err != nil {
		return err
	}

	if err := g.saveBuildState(); err != nil {
		msg.Warn("failed to save build state: %v", err)
	}

	return nil
}

// planBuild determines which compile and link jobs are necessary
func (g *QobsBuilder) planBuild(sortedTargetNames []string) (allCompileJobs []compileJob, allLinkJobs []linkJob, err error) {
	rebuiltTargets := make(map[string]bool)

	for _, targetName := range sortedTargetNames {
		target := g.targets[targetName]
		oldState := g.buildState[targetName]
		needsRelink := false

		// reason 1 for relink: output file is missing
		outputPath := filepath.Join(g.buildDir, target.name)
		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			needsRelink = true
		}

		// reason 2 for relink: flags have changed
		if oldState != nil && (!slices.Equal(oldState.Cflags, target.cflags) || !slices.Equal(oldState.Ldflags, target.ldflags)) {
			needsRelink = true
		}

		// reason 3 for relink: a dependency was rebuilt
		for _, depName := range target.dependencies {
			if rebuiltTargets[depName] {
				needsRelink = true
				break
			}
			depPath := filepath.Join(g.buildDir, depName)
			hash, err := g.fileHash(depPath)
			if err != nil {
				if os.IsNotExist(err) {
					needsRelink = true
					break
				}
				return nil, nil, fmt.Errorf("failed to hash dependency %s: %w", depName, err)
			}
			if oldState == nil || oldState.Dependencies[depName] != hash {
				needsRelink = true
				break
			}
		}

		// determine which source files in this target are dirty
		var targetCompileJobs []compileJob
		for _, src := range target.sources {
			objPath := filepath.Join(g.buildDir, src.obj)

			// check if source is dirty
			isDirty, err := g.isSourceFileDirty(src, objPath, oldState)
			if err != nil {
				return nil, nil, fmt.Errorf("could not check status of %s: %w", src.src, err)
			}
			if isDirty {
				compiler := g.cc
				if src.isCxx {
					compiler = g.cxx
				}
				targetCompileJobs = append(targetCompileJobs, compileJob{
					src:    src.src,
					obj:    objPath,
					cflags: target.cflags,
					isCxx:  src.isCxx,
					cc:     compiler,
				})
			}
		}

		// reason 4 for relink: one or more of its source files were recompiled
		if len(targetCompileJobs) > 0 {
			allCompileJobs = append(allCompileJobs, targetCompileJobs...)
			needsRelink = true
		}

		if needsRelink {
			rebuiltTargets[target.name] = true
			linkJob, err := g.createLinkJob(target)
			if err != nil {
				return nil, nil, err
			}
			allLinkJobs = append(allLinkJobs, linkJob)
		}
	}

	return allCompileJobs, allLinkJobs, nil
}

// executeBuild runs the planned compile and link jobs and updates the build state
func (g *QobsBuilder) executeBuild(compileJobs []compileJob, linkJobs []linkJob) error {
	if err := runJobs(compileJobs, runCompileJob, g.jobs); err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}
	if err := runJobs(linkJobs, runLinkJob, g.jobs); err != nil {
		return fmt.Errorf("linking failed: %w", err)
	}

	for _, job := range linkJobs {
		target, ok := g.targets[job.name]
		if !ok {
			continue
		}
		if err := g.updateBuildState(target); err != nil {
			msg.Warn("failed to update build state for target %s: %v", target.name, err)
		}
	}

	return nil
}

// isSourceFileDirty checks if a single source file needs to be recompiled
func (g *QobsBuilder) isSourceFileDirty(src sourceFile, objPath string, state *BuildState) (bool, error) {
	if _, err := os.Stat(objPath); os.IsNotExist(err) {
		return true, nil
	}

	if state == nil {
		return true, nil
	}

	hash, err := g.fileHash(src.src)
	if err != nil {
		if os.IsNotExist(err) {
			return true, fmt.Errorf("source file %s not found", src.src)
		}
		return true, err
	}
	if prevHash, exists := state.Sources[src.src]; !exists || prevHash != hash {
		return true, nil
	}

	return false, nil
}

// createLinkJob constructs a linkJob for a given buildUnit
func (g *QobsBuilder) createLinkJob(target buildUnit) (linkJob, error) {
	objects := make([]string, len(target.sources))
	for i, src := range target.sources {
		objects[i] = filepath.Join(g.buildDir, src.obj)
	}

	dependencies := make([]string, len(target.dependencies))
	for i, dep := range target.dependencies {
		dependencies[i] = filepath.Join(g.buildDir, dep)
	}

	isCxx := g.hasCxxInTarget(target)
	var linker string
	if isCxx {
		linker = g.cxx
	} else {
		linker = g.cc
	}

	return linkJob{
		name:    target.name,
		objs:    objects,
		deps:    dependencies,
		out:     filepath.Join(g.buildDir, target.name),
		ldflags: target.ldflags,
		isLib:   target.isLib,
		isCxx:   isCxx,
		cc:      linker,
	}, nil
}

func (g *QobsBuilder) topologicalSortTargets() ([]string, error) {
	graph := make(map[string][]string) // target -> targets that depend on it
	inDegree := make(map[string]int)   // target -> dependency count

	for name := range g.targets {
		graph[name] = []string{}
		inDegree[name] = 0
	}

	// build graph
	for name, target := range g.targets {
		for _, depName := range target.dependencies {
			if _, ok := g.targets[depName]; !ok {
				return nil, fmt.Errorf("target `%s` lists a non-existent dependency: `%s`", name, depName)
			}

			graph[depName] = append(graph[depName], name)
			inDegree[name]++
		}
	}

	// queue of targets with indegree of 0
	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}
	slices.Sort(queue)

	var sortedOrder []string

	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		sortedOrder = append(sortedOrder, u)

		slices.Sort(graph[u])

		// for each target v that depends on u
		for _, v := range graph[u] {
			inDegree[v]--
			// if v no longer has any unmet dependencies, add it to the queue
			if inDegree[v] == 0 {
				queue = append(queue, v)
			}
		}
	}

	// check cycles
	if len(sortedOrder) != len(g.targets) {
		var cycleNodes []string
		for name, degree := range inDegree {
			if degree > 0 {
				cycleNodes = append(cycleNodes, name)
			}
		}
		slices.Sort(cycleNodes)
		return nil, fmt.Errorf("dependency cycle detected involving targets: %v", cycleNodes)
	}

	return sortedOrder, nil
}

// loadBuildState loads the previous build state from disk
func (g *QobsBuilder) loadBuildState() error {
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
func (g *QobsBuilder) saveBuildState() error {
	data, err := json.MarshalIndent(g.buildState, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(g.stateFile, data, 0644)
}

// fileHash computes the SHA256 hash of a file with an in-memory cache
func (g *QobsBuilder) fileHash(path string) (string, error) {
	if hash, ok := g.hashCache[path]; ok {
		return hash, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	hexHash := hex.EncodeToString(hash.Sum(nil))
	g.hashCache[path] = hexHash
	return hexHash, nil
}

// hasCxxInTarget checks if target or its dependencies have C++ sources
func (g *QobsBuilder) hasCxxInTarget(target buildUnit) bool {
	for _, src := range target.sources {
		if src.isCxx {
			return true
		}
	}

	// TODO: cache this?
	for _, depName := range target.dependencies {
		if depTarget, exists := g.targets[depName]; exists {
			if g.hasCxxInTarget(depTarget) {
				return true
			}
		}
	}

	return false
}

// runJobs runs jobs in parallel
func runJobs[T any](jobs []T, jobfunc func(job T) error, limit int) error {
	if len(jobs) == 0 {
		return nil
	}

	eg, _ := errgroup.WithContext(context.Background())
	eg.SetLimit(limit)

	for _, job := range jobs {
		eg.Go(func() error {
			return jobfunc(job)
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

	fmt.Printf("CC %s\n", job.src)
	return cmd.Run()
}

// runLinkJob runs a single linking job
func runLinkJob(job linkJob) error {
	var cmd *exec.Cmd
	if job.isLib {
		args := []string{"rcs", job.out}
		args = append(args, job.objs...)

		cmd = exec.Command("ar", args...)
		fmt.Printf("AR %s\n", job.out)
	} else {
		args := []string{"-o", job.out}
		args = append(args, job.objs...)
		args = append(args, job.deps...)
		args = append(args, job.ldflags...)

		cmd = exec.Command(job.cc, args...)
		fmt.Printf("LINK %s\n", job.out)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// updateBuildState updates the build state for a target after a successful build
func (g *QobsBuilder) updateBuildState(target buildUnit) error {
	state := &BuildState{
		Sources:      make(map[string]string),
		Dependencies: make(map[string]string),
		Cflags:       slices.Clone(target.cflags),
		Ldflags:      slices.Clone(target.ldflags),
	}

	// hash source files
	for _, src := range target.sources {
		hash, err := g.fileHash(src.src)
		if err != nil {
			return fmt.Errorf("failed to hash source file %s: %w", src.src, err)
		}
		state.Sources[src.src] = hash
	}

	// hash dependencies
	for _, dep := range target.dependencies {
		depPath := filepath.Join(g.buildDir, dep)
		hash, err := g.fileHash(depPath)
		if err != nil {
			msg.Warn("could not hash dependency %s for state update: %v", dep, err)
			continue
		}
		state.Dependencies[dep] = hash
	}

	g.buildState[target.name] = state
	return nil
}
