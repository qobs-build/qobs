package index

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/qobs-build/qobs/internal/msg"
)

const (
	IndexFilename = "qobs_index.json"
	indexRepoURL  = "https://github.com/qobs-build/index.git"
	indexBranch   = "main"
)

type Index struct {
	// on windows: %LocalAppData%/qobs/index
	// on linux: ~/.cache/qobs/index
	basePath string
	// dependency URL -> path in index
	Deps map[string]string
}

func ParseIndex(rdr io.Reader, basePath string) (*Index, error) {
	var deps map[string]string
	if err := json.NewDecoder(bufio.NewReader(rdr)).Decode(&deps); err != nil {
		return nil, err
	}
	return &Index{Deps: deps, basePath: basePath}, nil
}

func (index Index) Save(basePath string) error {
	path := filepath.Join(basePath, IndexFilename)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	bufw := bufio.NewWriter(f)
	defer bufw.Flush()

	enc := json.NewEncoder(bufw)
	enc.SetIndent("", "  ")
	return enc.Encode(index.Deps)
}

func FetchIndex(basePath string) (*Index, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(basePath, ".git")); os.IsNotExist(err) {
		fmt.Printf("  %s qobs index\n", color.HiGreenString("Fetching"))
		_, err := git.PlainClone(basePath, &git.CloneOptions{
			URL:           indexRepoURL,
			ReferenceName: plumbing.NewBranchReferenceName(indexBranch),
			SingleBranch:  true,
			Depth:         1,
			Progress:      &msg.IndentWriter{Indent: "    ", W: os.Stdout},
		})
		if err != nil {
			return nil, err
		}
	} else {
		repo, err := git.PlainOpen(basePath)
		if err != nil {
			return nil, err
		}
		w, err := repo.Worktree()
		if err != nil {
			return nil, err
		}
		err = w.Pull(&git.PullOptions{
			RemoteName:    "origin",
			ReferenceName: plumbing.NewBranchReferenceName(indexBranch),
			SingleBranch:  true,
			Depth:         1,
			Progress:      os.Stdout,
		})
		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return nil, err
		}
	}

	return ParseIndexInPath(basePath)
}

func ParseIndexInPath(basePath string) (*Index, error) {
	path := filepath.Join(basePath, IndexFilename)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseIndex(bufio.NewReader(f), basePath)
}

func LoadOrFetchIndex(basePath string) (*Index, error) {
	path := filepath.Join(basePath, IndexFilename)

	if _, err := os.Stat(path); err == nil {
		return ParseIndexInPath(basePath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	return FetchIndex(basePath)
}

var globalIndex *Index

func GetIndexAnyhow() (*Index, error) {
	if globalIndex != nil {
		return globalIndex, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	index, err := LoadOrFetchIndex(filepath.Join(cacheDir, "qobs", "index"))
	if err != nil {
		return nil, err
	}
	globalIndex = index
	return index, err
}

// Copy copies all files from the related index entry (if any) to the destination path `destPath`
func (index Index) Copy(destPath, url string) error {
	path, ok := index.Deps[url]
	if !ok {
		return errors.New("dependency not found in index")
	}

	fromPath := filepath.Join(index.basePath, path)
	return os.CopyFS(destPath, os.DirFS(fromPath))
}

func (idx *Index) SetDep(url, path string) {
	if idx.Deps == nil {
		idx.Deps = make(map[string]string)
	}
	idx.Deps[url] = path
}

func (idx *Index) HasDep(url string) bool {
	_, exists := idx.Deps[url]
	return exists
}

func (idx *Index) RemoveDep(url string) bool {
	if idx.Deps == nil {
		return false
	}
	if _, ok := idx.Deps[url]; ok {
		delete(idx.Deps, url)
		return true
	}
	return false
}

func UpdateGlobalIndex() (*Index, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return FetchIndex(filepath.Join(cacheDir, "qobs", "index"))
}
