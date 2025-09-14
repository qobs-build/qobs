package builder

import (
	"errors"
	"net/url"
	"os"
	"strings"

	"github.com/go-git/go-git/v6"
)

var depShortcuts = map[string]string{
	"gh:": "https://github.com/",
	"gl:": "https://gitlab.com/",
	"bb:": "https://bitbucket.org/",
	"sr:": "https://sr.ht/",
	"cb:": "https://codeberg.org/",
}

const gitPrefix = "git:"

var (
	errIllegalDep = errors.New("empty or illegal dependency string")
)

func fetchDependency(dep string, toWhere string) (string, error) {
	if dep == "" {
		return "", errIllegalDep
	}

	// check for `git:` prefix, e.g. git:https://github.com/zeozeozeo/qobs.git
	if strings.HasPrefix(dep, gitPrefix) {
		return cloneGitRepo(dep[len(gitPrefix):], toWhere)
	}

	// check for shortcut prefix, e.g. gh:zeozeozeo/qobs
	for shortcut, url := range depShortcuts {
		if strings.HasPrefix(dep, shortcut) {
			return cloneGitRepo(url+dep[len(shortcut):], toWhere)
		}
	}

	// if it's a URL, it should be an archive
	if isURL(dep) {
		return downloadAndExtractArchive(dep, toWhere)
	}

	// otherwise it's a path
	return dep, nil
}

func isURL(maybeURL string) bool {
	u, err := url.Parse(maybeURL)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func cloneGitRepo(url, toWhere string) (string, error) {
	_, err := git.PlainClone(toWhere, &git.CloneOptions{
		URL:               url,
		Progress:          os.Stdout,
		Depth:             1,                                  // --depth=1
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth, // --recursive
		ShallowSubmodules: true,                               // --shallow-submodules
	})
	return toWhere, err
}

func downloadAndExtractArchive(url, toWhere string) (string, error) {
	panic("TODO")
}
