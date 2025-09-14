package builder

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
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

	// check for `git:` prefix, e.g. git:https://github.com/zeozeozeo/libhelloworld.git
	if strings.HasPrefix(dep, gitPrefix) {
		return cloneGitRepo(dep[len(gitPrefix):], toWhere)
	}

	// check for shortcut prefix, e.g. gh:zeozeozeo/libhelloworld
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

type gitURL struct {
	cleanURL    string
	branch      string
	commitOrTag string
}

// someone/something@master#0.1.0
// someone/something@feature-branch#12345abc
// someone/something#12345abc
func parseGitURL(rawURL string) (res gitURL) {
	parts := strings.SplitN(rawURL, "#", 2)
	baseURL := parts[0]
	if len(parts) == 2 {
		res.commitOrTag = parts[1]
	}

	parts = strings.SplitN(baseURL, "@", 2)
	res.cleanURL = parts[0]
	if len(parts) == 2 {
		res.branch = parts[1]
	}

	if !strings.HasSuffix(res.cleanURL, ".git") {
		res.cleanURL += ".git"
	}

	return
}

// cloneGitRepo clones a Git remote into the specified directory
func cloneGitRepo(url, toWhere string) (string, error) {
	parsedURL := parseGitURL(url)

	cloneOptions := &git.CloneOptions{
		URL:               parsedURL.cleanURL,
		Progress:          os.Stdout,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
	}

	if parsedURL.commitOrTag == "" {
		cloneOptions.Depth = 1 // we can do a shallow clone of the latest commit
	}

	if parsedURL.branch != "" {
		cloneOptions.ReferenceName = plumbing.NewBranchReferenceName(parsedURL.branch)
		cloneOptions.SingleBranch = true
	}

	repo, err := git.PlainClone(toWhere, cloneOptions)
	if err != nil {
		return toWhere, err
	}

	if parsedURL.commitOrTag != "" {
		w, err := repo.Worktree()
		if err != nil {
			return toWhere, fmt.Errorf("could not get worktree: %w", err)
		}

		revision := parsedURL.commitOrTag
		hash, err := repo.ResolveRevision(plumbing.Revision(revision))
		if err != nil {
			return toWhere, fmt.Errorf("could not resolve revision `%s`: %w", revision, err)
		}

		err = w.Checkout(&git.CheckoutOptions{
			Hash:  *hash,
			Force: true,
		})
		if err != nil {
			return toWhere, fmt.Errorf("failed to checkout `%s`: %w", revision, err)
		}
	}

	return toWhere, nil
}

func downloadAndExtractArchive(url, toWhere string) (string, error) {
	panic("TODO")
}
