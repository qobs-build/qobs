package gen

// SourceFile represents a single source file and its corresponding object file path
type SourceFile struct {
	Src   string
	Obj   string // relative to build directory
	IsCxx bool   // C++ file
}

// buildUnit represents a single unit to be built (a library or an executable)
type buildUnit struct {
	name            string
	isLib           bool
	sources         []SourceFile
	dependencies    []string
	cflags, ldflags []string
	basedir         string
}

type Generator interface {
	SetCompiler(cc, cxx string)
	AddTarget(name, basedir string, sources []SourceFile, dependencies []string, isLib bool, cflags, ldflags []string)
	Generate() string
	BuildFile() string
	Invoke(buildDir string) error
}
