package attachment

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	skipSmudgeEnv             = "GIT_CONFLUENCE_SKIP_SMUDGE"
	maxAttachmentEnv          = "GIT_CONFLUENCE_MAX_ATTACHMENT_BYTES"
	defaultMaxAttachmentBytes = int64(1 << 30)
	maxPointerBytes           = int64(64 << 10)
)

func Smudge(pointerData []byte, worktreePath string, output, errorOutput io.Writer) error {
	pointer, err := ParsePointer(pointerData)
	if err != nil {
		return err
	}
	canonical := pointer.Canonical()
	if os.Getenv(skipSmudgeEnv) != "" {
		_, err = output.Write(canonical)
		return err
	}
	cache, err := openCache()
	if err != nil {
		return err
	}
	if path, ok := cache.lookupPointer(canonical); ok {
		if err := cache.remember(filepath.Base(path), worktreePath, canonical); err != nil {
			return err
		}
		return copyFile(path, output)
	}
	pat := resolvePAT()
	if pat == "" {
		fmt.Fprintln(errorOutput, "git-confluence: Confluence PAT is not configured; leaving attachment pointer in working tree")
		_, err = output.Write(canonical)
		return err
	}
	path, err := download(cache, pointer, canonical, worktreePath, pat)
	if err != nil {
		fmt.Fprintf(errorOutput, "git-confluence: attachment %q: %v; leaving attachment pointer in working tree\n", worktreePath, err)
		_, writeErr := output.Write(canonical)
		return writeErr
	}
	return copyFile(path, output)
}

func Clean(input io.Reader, worktreePath string, output io.Writer) error {
	maxBytes, err := maxAttachmentBytes()
	if err != nil {
		return err
	}
	hash := sha256.New()
	buffer := make([]byte, 128<<10)
	prefix := make([]byte, 0, maxPointerBytes)
	var total int64
	for {
		read, readErr := input.Read(buffer)
		if read > 0 {
			total += int64(read)
			if total > maxBytes {
				return fmt.Errorf("attachment exceeds %d bytes; set %s to a larger byte count", maxBytes, maxAttachmentEnv)
			}
			_, _ = hash.Write(buffer[:read])
			if int64(len(prefix)) < maxPointerBytes {
				remaining := int(maxPointerBytes - int64(len(prefix)))
				keep := read
				if keep > remaining {
					keep = remaining
				}
				prefix = append(prefix, buffer[:keep]...)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if total <= maxPointerBytes && IsPointer(prefix) {
		pointer, err := ParsePointer(prefix)
		if err != nil {
			return err
		}
		_, err = output.Write(pointer.Canonical())
		return err
	}
	cache, err := openCache()
	if err != nil {
		return err
	}
	oid := hex.EncodeToString(hash.Sum(nil))
	pointer, ok := cache.pointerForContent(oid, worktreePath)
	if !ok {
		return fmt.Errorf("attachment content is not a downloaded Confluence version; attachments are read-only")
	}
	_, err = output.Write(pointer)
	return err
}

func download(cache cache, pointer Pointer, canonical []byte, worktreePath, pat string) (string, error) {
	downloadURL, err := pointer.DownloadURL()
	if err != nil {
		return "", err
	}
	request, err := http.NewRequest(http.MethodGet, downloadURL.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+pat)
	request.Header.Set("User-Agent", "git-confluence/dev")
	client := &http.Client{Timeout: 5 * time.Minute}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != downloadURL.Scheme || req.URL.Host != downloadURL.Host {
			return fmt.Errorf("attachment redirect points outside Confluence origin")
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download attachment %s version %d: %w", pointer.AttachmentID, pointer.AttachmentVersion, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("download attachment %s version %d: HTTP %d", pointer.AttachmentID, pointer.AttachmentVersion, response.StatusCode)
	}
	maxBytes, err := maxAttachmentBytes()
	if err != nil {
		return "", err
	}
	temp, err := cache.newTemp()
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(response.Body, maxBytes+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written > maxBytes {
		return "", fmt.Errorf("attachment exceeds %d bytes; set %s to a larger byte count", maxBytes, maxAttachmentEnv)
	}
	if pointer.Size > 0 && written != pointer.Size {
		return "", fmt.Errorf("attachment size mismatch: pointer has %d bytes, downloaded %d", pointer.Size, written)
	}
	oid := hex.EncodeToString(hash.Sum(nil))
	return cache.store(tempPath, oid, worktreePath, canonical)
}

func copyFile(path string, output io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(output, bufio.NewReader(file))
	return err
}

func resolvePAT() string {
	for _, name := range []string{"CONFLUENCE_PAT", "GIT_REMOTE_CONFLUENCE_PAT"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	for _, key := range []string{"confluence.pat", "remote.confluence.pat"} {
		output, err := exec.Command("git", "config", "--get", key).Output()
		if err == nil && strings.TrimSpace(string(output)) != "" {
			return strings.TrimSpace(string(output))
		}
	}
	return ""
}

func maxAttachmentBytes() (int64, error) {
	value := os.Getenv(maxAttachmentEnv)
	if value == "" {
		return defaultMaxAttachmentBytes, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%s must be a positive byte count", maxAttachmentEnv)
	}
	return parsed, nil
}
