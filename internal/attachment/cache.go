package attachment

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const cacheDirEnv = "GIT_CONFLUENCE_CACHE_DIR"

type cache struct {
	root string
}

func openCache() (cache, error) {
	root := os.Getenv(cacheDirEnv)
	if root == "" {
		output, err := exec.Command("git", "rev-parse", "--git-path", "confluence").Output()
		if err != nil {
			return cache{}, fmt.Errorf("locate Git Confluence cache: %w", err)
		}
		root = strings.TrimSpace(string(output))
	}
	if root == "" {
		return cache{}, fmt.Errorf("Git Confluence cache path is empty")
	}
	return cache{root: root}, nil
}

func (c cache) lookupPointer(pointer []byte) (string, bool) {
	data, err := os.ReadFile(filepath.Join(c.root, "by-pointer", PointerOID(pointer)))
	if err != nil {
		return "", false
	}
	oid := strings.TrimSpace(string(data))
	if !validOID(oid) {
		return "", false
	}
	path := filepath.Join(c.root, "objects", oid)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

func (c cache) pointerForContent(oid, worktreePath string) ([]byte, bool) {
	if !validOID(oid) {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(c.root, "by-content", oid, pathOID(worktreePath)))
	if err != nil || !IsPointer(data) {
		return nil, false
	}
	return data, true
}

func (c cache) store(tempPath, contentOID, worktreePath string, pointer []byte) (string, error) {
	for _, dir := range []string{"objects", "by-pointer", "by-content"} {
		if err := os.MkdirAll(filepath.Join(c.root, dir), 0o700); err != nil {
			return "", err
		}
	}
	objectPath := filepath.Join(c.root, "objects", contentOID)
	if err := os.Rename(tempPath, objectPath); err != nil {
		if _, statErr := os.Stat(objectPath); statErr != nil {
			return "", err
		}
		_ = os.Remove(tempPath)
	}
	if err := c.remember(contentOID, worktreePath, pointer); err != nil {
		return "", err
	}
	return objectPath, nil
}

func (c cache) remember(contentOID, worktreePath string, pointer []byte) error {
	contentDir := filepath.Join(c.root, "by-content", contentOID)
	if err := os.MkdirAll(contentDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(c.root, "by-pointer"), 0o700); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(contentDir, pathOID(worktreePath)), pointer); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(c.root, "by-pointer", PointerOID(pointer)), []byte(contentOID+"\n"))
}

func pathOID(worktreePath string) string {
	hash := sha256.Sum256([]byte(filepath.ToSlash(worktreePath)))
	return hex.EncodeToString(hash[:])
}

func (c cache) newTemp() (*os.File, error) {
	if err := os.MkdirAll(c.root, 0o700); err != nil {
		return nil, err
	}
	return os.CreateTemp(c.root, "download-*")
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

func validOID(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
