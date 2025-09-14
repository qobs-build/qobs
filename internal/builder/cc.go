package builder

import (
	"os"
	"os/exec"
)

// TODO: zig cc
var (
	commonCCompilers   = []string{"clang", "gcc", "icx", "icc", "tcc", "cl"}
	commonCxxCompilers = []string{"clang++", "g++", "clang", "gcc", "icpx", "icx", "icpc", "icc", "cl"}
)

// findCompiler attempts to find a suitable C or C++ compiler on the system
func findCompiler(needCxx bool) string {
	cc := os.Getenv("CC")
	cxx := os.Getenv("CXX")

	if needCxx && cxx != "" {
		return cxx
	}
	if !needCxx && cc != "" {
		return cc
	}

	if cxx != "" {
		return cxx
	}
	if cc != "" {
		return cc
	}

	var compilersToTry []string
	if needCxx {
		compilersToTry = commonCxxCompilers
	} else {
		compilersToTry = commonCCompilers
	}

	for _, compiler := range compilersToTry {
		path, err := exec.LookPath(compiler)
		if err == nil {
			return path
		}
	}

	return ""
}
