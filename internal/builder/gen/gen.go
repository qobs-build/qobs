package gen

type Generator interface {
	SetFiles(files []string, basedir string)
	SetCompiler(cflags, ldflags, cc string)
	SetOutput(binname string)
	Generate() string
	BuildFile() string
	Invoke(path string) error
}
