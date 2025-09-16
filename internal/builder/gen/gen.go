package gen

type Generator interface {
	SetCompiler(cc, cxx string)
	AddTarget(name, basedir string, sources, dependencies []string, isLib bool, cflags, ldflags []string)
	Generate() string
	BuildFile() string
	Invoke(buildDir string) error
}
