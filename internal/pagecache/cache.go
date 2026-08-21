package pagecache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const cacheDirEnv = "GIT_CONFLUENCE_CACHE_DIR"

type Cache struct {
	root string
}

func Open() (Cache, error) {
	root := os.Getenv(cacheDirEnv)
	if root == "" {
		output, err := exec.Command("git", "rev-parse", "--git-path", "confluence").Output()
		if err != nil {
			return Cache{}, fmt.Errorf("locate Git Confluence cache: %w", err)
		}
		root = strings.TrimSpace(string(output))
	}
	if root == "" {
		return Cache{}, fmt.Errorf("Git Confluence cache path is empty")
	}
	return Cache{root: filepath.Join(root, "pages")}, nil
}

func (c Cache) Remember(markdown []byte, worktreePath string, storage []byte) error {
	path := c.mappingPath(markdown, worktreePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, storage)
}

func (c Cache) Original(markdown []byte, worktreePath string) ([]byte, bool, error) {
	data, err := os.ReadFile(c.mappingPath(markdown, worktreePath))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func MatchesIndex(worktreePath string, data []byte) bool {
	object := ":" + filepath.ToSlash(worktreePath)
	indexed, err := exec.Command("git", "cat-file", "blob", object).Output()
	return err == nil && bytes.Equal(indexed, data)
}

func (c Cache) mappingPath(markdown []byte, worktreePath string) string {
	contentHash := sha256.Sum256(markdown)
	pathHash := sha256.Sum256([]byte(filepath.ToSlash(worktreePath)))
	return filepath.Join(c.root, "by-content", hex.EncodeToString(contentHash[:]), hex.EncodeToString(pathHash[:]))
}

func writeAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "mapping-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
