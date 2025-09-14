package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zeozeozeo/qobs/internal/msg"
)

// sourceFile represents a single source file and its corresponding object file path
type sourceFile struct {
	src string
	obj string
}

// ninjaTarget represents a single unit to be built (a library or an executable)
type ninjaTarget struct {
	name         string
	isLib        bool
	sources      []sourceFile
	dependencies []string
}

type NinjaGen struct {
	cflags, ldflags, cc string
	targets             map[string]ninjaTarget
}

func (g *NinjaGen) SetCompiler(cflags, ldflags, cc string) {
	g.cflags, g.ldflags, g.cc = cflags, ldflags, cc
}

func (g *NinjaGen) BuildFile() string { return "build.ninja" }

var ninjaPathEscaper = strings.NewReplacer(":", "$:", " ", "$ ")

func quote(s string) string { return ninjaPathEscaper.Replace(s) }

// AddTarget adds a package (library or executable) to the build graph
func (g *NinjaGen) AddTarget(name, basedir string, sources, dependencies []string, isLib bool) {
	if g.targets == nil {
		g.targets = make(map[string]ninjaTarget)
	}

	targetSources := make([]sourceFile, len(sources))
	for i, srcPath := range sources {
		rel, err := filepath.Rel(basedir, srcPath)
		if err != nil {
			rel = filepath.Base(srcPath)
			msg.Warn("source file %s is outside of base directory %s", srcPath, basedir)
		}

		objPath := quote(filepath.ToSlash(filepath.Join("QobsFiles", name+".dir", rel))) + ".obj"
		targetSources[i] = sourceFile{src: srcPath, obj: objPath}
	}

	g.targets[name] = ninjaTarget{
		name:         name,
		isLib:        isLib,
		sources:      targetSources,
		dependencies: dependencies,
	}
}

func (g *NinjaGen) Generate() string {
	var sb strings.Builder

	writeln(&sb, "ninja_required_version = 1.1")
	writeln(&sb, "cflags = ", g.cflags)
	writeln(&sb, "ldflags = ", g.ldflags)
	writeln(&sb, "cc = ", g.cc)
	writeln(&sb)

	// gen rules
	write(&sb,
		`rule cc
  command = $cc $cflags -c $in -o $out
  description = CC $out
`)
	write(&sb,
		`rule link
  command = $cc $ldflags -o $out $in
  description = LINK $out
`)
	write(&sb,
		`rule ar
  command = ar rcs $out $in
  description = AR $out
`)
	writeln(&sb)

	// build object files
	for _, target := range g.targets {
		for _, source := range target.sources {
			writeln(&sb, "build ", source.obj, ": cc ", quote(source.src))
		}
	}
	writeln(&sb)

	// ar/link
	for _, target := range g.targets {
		write(&sb, "build ", target.name, ": ")
		if target.isLib {
			write(&sb, "ar")
		} else {
			write(&sb, "link")
		}

		// add the object files and dependencies of this project
		for _, source := range target.sources {
			write(&sb, " ", source.obj)
		}
		for _, dep := range target.dependencies {
			write(&sb, " ", dep)
		}
		writeln(&sb)
	}

	return sb.String()
}

func (g *NinjaGen) Invoke(buildDir string) error {
	cmd := exec.Command("ninja", "-C", buildDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
