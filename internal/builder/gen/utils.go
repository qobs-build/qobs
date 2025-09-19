package gen

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/heaths/go-vssetup"
)

func write(sb *strings.Builder, s ...string) {
	for _, str := range s {
		sb.WriteString(str)
	}
}
func writeln(sb *strings.Builder, s ...string) {
	for _, str := range s {
		sb.WriteString(str)
	}
	sb.WriteByte('\n')
}

func FindMsbuild() (string, error) {
	instances, err := vssetup.Instances(true)
	if err != nil {
		return "", err
	}

	for _, instance := range instances {
		defer instance.Close()

		packages, err := instance.Packages()
		if err != nil {
			continue
		}

		for _, pkg := range packages {
			if id, _ := pkg.ID(); id == "Microsoft.Component.MSBuild" {
				installPath, err := instance.InstallationPath()
				if err != nil {
					return "", err
				}
				return filepath.Join(installPath, "MSBuild", "Current", "Bin", "MSBuild.exe"), nil
			}
		}
	}

	return "", errors.New("msbuild.exe not found in any Visual Studio installation")
}
