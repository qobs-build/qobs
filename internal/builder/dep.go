package builder

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/zeozeozeo/qobs/internal/msg"
)

var depShortcuts = map[string]string{
	"gh:": "https://github.com/",
	"gl:": "https://gitlab.com/",
	"bb:": "https://bitbucket.org/",
	"sr:": "https://sr.ht/",
	"cb:": "https://codeberg.org/",
}

var (
	errIllegalDep = errors.New("empty or illegal dependency string")
)

func fetchDependency(dep string, toWhere string) (string, error) {
	if dep == "" {
		return "", errIllegalDep
	}

	// check for `git:` prefix, e.g. git:https://github.com/zeozeozeo/libhelloworld.git
	const gitPrefix = "git:"
	if strings.HasPrefix(dep, gitPrefix) {
		return cloneGitRepo(dep[len(gitPrefix):], toWhere)
	}
	// or suffix
	if strings.HasSuffix(dep, ".git") {
		return cloneGitRepo(dep, toWhere)
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

type indentWriter struct {
	Indent    int
	W         io.Writer
	didIndent bool
}

func (w *indentWriter) Write(p []byte) (n int, err error) {
	for _, c := range p {
		if !w.didIndent {
			w.W.Write([]byte(strings.Repeat(" ", w.Indent)))
			w.didIndent = true
		}
		w.W.Write([]byte{c})
		if c == '\n' || c == '\r' {
			w.didIndent = false
		}
	}
	return len(p), nil
}

// cloneGitRepo clones a Git remote into the specified directory
func cloneGitRepo(url, toWhere string) (string, error) {
	parsedURL := parseGitURL(url)

	cloneOptions := &git.CloneOptions{
		URL:               parsedURL.cleanURL,
		Progress:          &indentWriter{Indent: 4, W: os.Stdout},
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
	}

	if parsedURL.commitOrTag == "" {
		cloneOptions.Depth = 1 // we can do a shallow clone of the latest commit
	}

	if parsedURL.branch != "" {
		cloneOptions.ReferenceName = plumbing.NewBranchReferenceName(parsedURL.branch)
		cloneOptions.SingleBranch = true
	}

	fmt.Printf("  %s %s\n", color.HiGreenString("Cloning"), parsedURL.cleanURL)

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

// determineArchiveFormat checks the archive format using the file magic, Content-Type and the URL suffix
func determineArchiveFormat(filePath string, resp *http.Response, originalURL string) (string, error) {
	// check magic
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	header := make([]byte, 4)
	_, err = file.Read(header)
	if err != nil && err != io.EOF {
		return "", err
	}

	if bytes.Equal(header, []byte{0x50, 0x4b, 0x03, 0x04}) {
		return "zip", nil
	}
	if bytes.Equal(header[:2], []byte{0x1f, 0x8b}) {
		return "tar.gz", nil
	}

	// fallback to mimetype
	contentType := resp.Header.Get("Content-Type")
	switch contentType {
	case "application/zip", "application/x-zip-compressed":
		return "zip", nil
	case "application/gzip", "application/x-gzip", "application/x-tar":
		return "tar.gz", nil
	}

	// fallback to URL suffix
	u, err := url.Parse(originalURL)
	if err == nil {
		ext := path.Ext(u.Path)
		switch ext {
		case ".zip":
			return "zip", nil
		case ".tgz", ".tar.gz":
			return "tar.gz", nil
		}
	}

	return "", errors.New("unknown or unsupported archive format")
}

// downloadAndExtractArchive downloads and extracts an archive
func downloadAndExtractArchive(downloadURL, toWhere string) (string, error) {
	cleanURL := downloadURL
	var expectedMD5 string
	if parts := strings.SplitN(downloadURL, "#MD5=", 2); len(parts) == 2 {
		cleanURL = parts[0]
		expectedMD5 = parts[1]
	}

	fmt.Printf("  %s %s\n", color.HiGreenString("Fetching"), cleanURL)

	resp, err := http.Get(cleanURL)
	if err != nil {
		return "", fmt.Errorf("failed to download from url %s: %w", cleanURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download from url %s: status code %d", cleanURL, resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(toWhere, "archive-*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	archivePath := tmpFile.Name()
	defer os.Remove(archivePath)

	hash := md5.New()

	pb := &msg.ProgressBar{
		Total:  resp.ContentLength,
		Indent: 1,
		W:      os.Stdout,
		Start:  time.Now(),
	}

	_, err = io.Copy(io.MultiWriter(tmpFile, hash, pb), resp.Body)
	if err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to write to temporary file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temporary file: %w", err)
	}
	pb.Finish()

	if expectedMD5 != "" {
		calculatedMD5 := hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(expectedMD5, calculatedMD5) {
			return "", fmt.Errorf("MD5 checksum mismatch for %s: expected %s, got %s", cleanURL, expectedMD5, calculatedMD5)
		}
	}

	format, err := determineArchiveFormat(archivePath, resp, downloadURL)
	if err != nil {
		return "", err
	}

	var extractErr error
	switch format {
	case "zip":
		extractErr = unzip(archivePath, toWhere)
	case "tar.gz":
		extractErr = untar(archivePath, toWhere)
	}

	if extractErr != nil {
		return "", fmt.Errorf("failed to extract archive: %w", extractErr)
	}

	return toWhere, nil
}

// unzip extracts a zip archive to a destination directory
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	var rootDir string
	if len(r.File) > 0 {
		firstPath := r.File[0].Name
		isSingleRoot := true
		if r.File[0].FileInfo().IsDir() {
			rootDir = firstPath
			for _, f := range r.File {
				if !strings.HasPrefix(f.Name, rootDir) {
					isSingleRoot = false
					break
				}
			}
		} else {
			isSingleRoot = false
		}
		if !isSingleRoot {
			rootDir = ""
		}
	}

	for _, f := range r.File {
		name := f.Name
		if rootDir != "" {
			name = strings.TrimPrefix(name, rootDir)
		}
		if name == "" {
			continue
		}

		fpath := filepath.Join(dest, name)

		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}

// untar extracts a tar.gz archive to a destination directory
func untar(src, dest string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	var rootDir string
	firstEntry := true

	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if firstEntry {
			if header.Typeflag == tar.TypeDir {
				rootDir = header.Name
			}
			firstEntry = false
		} else {
			if rootDir != "" && !strings.HasPrefix(header.Name, rootDir) {
				rootDir = ""
			}
		}

		name := header.Name
		if rootDir != "" {
			name = strings.TrimPrefix(name, rootDir)
		}
		if name == "" {
			continue
		}

		target := filepath.Join(dest, name)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", target)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			f.Close()
			if err != nil {
				return err
			}
		}
	}
}
