package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hkwi/git-confluence/internal/attachment"
	"github.com/hkwi/git-confluence/internal/confluence"
	"github.com/hkwi/git-confluence/internal/logging"
)

func filterClean(path string, input io.Reader, output, errorOutput io.Writer, maxInput int64, maxDepth int) error {
	if isAttachmentPath(path) {
		return attachment.Clean(input, path, output)
	}
	data, err := readLimitedInput(input, maxInput)
	if err != nil {
		return err
	}
	logger := logging.New(errorOutput).With(
		"app", "git-confluence", "path", path,
		"filter", "clean", "direction", "markdown_to_confluence_storage",
		"purpose", "normalize_for_git",
	)
	logger.Info("clean filter started", "bytes", len(data))
	storage, err := confluence.MarkdownToStorageWithMaxDepth(string(data), maxDepth)
	if err != nil {
		return err
	}
	_, err = io.WriteString(output, storage)
	if err == nil {
		logger.Info("clean filter completed", "bytes", len(storage))
	}
	return err
}

func filterSmudge(path string, input io.Reader, output, errorOutput io.Writer, maxInput int64, maxDepth int) error {
	data, err := readLimitedInput(input, maxInput)
	if err != nil {
		return err
	}
	if isAttachmentPath(path) || attachment.IsPointer(data) {
		return attachment.Smudge(data, path, output, errorOutput)
	}
	logger := logging.New(errorOutput).With(
		"app", "git-confluence", "path", path,
		"filter", "smudge", "direction", "confluence_storage_to_markdown",
		"purpose", "materialize_worktree",
	)
	logger.Info("smudge filter started", "bytes", len(data))
	markdown, err := confluence.StorageToMarkdownWithMaxDepth(string(data), maxDepth)
	if err != nil {
		return err
	}
	_, err = io.WriteString(output, markdown)
	if err == nil {
		logger.Info("smudge filter completed", "bytes", len(markdown))
	}
	return err
}

func isAttachmentPath(path string) bool {
	path = "/" + strings.Trim(filepath.ToSlash(path), "/") + "/"
	return strings.Contains(path, "/attachments/")
}

func installFilter(args []string) error {
	scope := "--global"
	if len(args) > 1 {
		return fmt.Errorf("install accepts at most one scope")
	}
	if len(args) == 1 {
		scope = args[0]
	}
	if scope != "--global" && scope != "--local" {
		return fmt.Errorf("install scope must be --global or --local")
	}
	settings := [][2]string{
		{"filter.confluence.clean", "git-confluence filter-clean %f"},
		{"filter.confluence.smudge", "git-confluence filter-smudge %f"},
		{"filter.confluence.required", "true"},
	}
	for _, setting := range settings {
		cmd := exec.Command("git", "config", scope, setting[0], setting[1])
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git config %s: %w: %s", setting[0], err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func pullFiles(paths []string) error {
	if err := installFilter([]string{"--local"}); err != nil {
		return err
	}
	if len(paths) == 0 {
		output, err := exec.Command("git", "ls-files", "-z").Output()
		if err != nil {
			return fmt.Errorf("list tracked files: %w", err)
		}
		for _, path := range strings.Split(string(output), "\x00") {
			if path != "" && isAttachmentPath(path) {
				paths = append(paths, path)
			}
		}
	}
	if len(paths) == 0 {
		return nil
	}
	pathspecs := make([]string, 0, len(paths))
	for index, path := range paths {
		clean := filepath.Clean(path)
		if clean != path || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || !isAttachmentPath(clean) {
			return fmt.Errorf("%s is not an attachment path", path)
		}
		paths[index] = clean
		pathspecs = append(pathspecs, ":(literal)"+filepath.ToSlash(clean))
	}
	diffArgs := append([]string{"diff", "--quiet", "--"}, pathspecs...)
	if err := exec.Command("git", diffArgs...).Run(); err != nil {
		return fmt.Errorf("attachment working files have local changes; refusing to overwrite")
	}
	cachedDiffArgs := append([]string{"diff", "--cached", "--quiet", "HEAD", "--"}, pathspecs...)
	if err := exec.Command("git", cachedDiffArgs...).Run(); err != nil {
		return fmt.Errorf("attachment index entries have staged changes; refusing to overwrite")
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("prepare attachment %s for materialization: %w", path, err)
		}
	}
	const batchSize = 100
	for start := 0; start < len(paths); start += batchSize {
		end := start + batchSize
		if end > len(paths) {
			end = len(paths)
		}
		args := append([]string{"checkout", "HEAD", "--"}, pathspecs[start:end]...)
		cmd := exec.Command("git", args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("materialize attachments: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("verify materialized attachment %s: %w", path, err)
		}
		if attachment.IsPointer(data) {
			return fmt.Errorf("attachment %s was not materialized; configure a Confluence PAT or check the download error", path)
		}
	}
	return nil
}
