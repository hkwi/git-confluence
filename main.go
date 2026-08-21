package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"

	"github.com/hkwi/git-confluence/internal/confluence"
	"github.com/hkwi/git-confluence/internal/logging"
)

const (
	appName              = "git-confluence"
	defaultMaxInputBytes = int64(64 << 20)
	maxInputBytesEnv     = "GIT_CONFLUENCE_MAX_INPUT_BYTES"
	maxRecursionDepthEnv = "GIT_CONFLUENCE_MAX_RECURSION_DEPTH"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "help":
		fmt.Print(helpOutput())
		return
	case "version", "--version", "-version":
		fmt.Print(versionOutput())
		return
	case "install":
		if err := installFilter(os.Args[2:]); err != nil {
			logError(err)
			os.Exit(1)
		}
		return
	case "pull":
		if err := pullFiles(os.Args[2:]); err != nil {
			logError(err)
			os.Exit(1)
		}
		return
	}

	maxInput, err := maxInputBytes()
	if err != nil {
		logError(err)
		os.Exit(2)
	}

	maxDepth, err := maxRecursionDepth()
	if err != nil {
		logError(err)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "clean":
		input, err := readLimitedInput(os.Stdin, maxInput)
		if err != nil {
			fail(err)
		}
		storage, err := confluence.MarkdownToStorageWithMaxDepth(string(input), maxDepth)
		if err != nil {
			fail(err)
		}
		fmt.Print(storage)
	case "smudge":
		input, err := readLimitedInput(os.Stdin, maxInput)
		if err != nil {
			fail(err)
		}
		markdown, err := confluence.StorageToMarkdownWithMaxDepth(string(input), maxDepth)
		if err != nil {
			fail(err)
		}
		fmt.Print(markdown)
	case "filter-clean":
		if len(os.Args) != 3 {
			fail(fmt.Errorf("filter-clean requires a pathname"))
		}
		if err := filterClean(os.Args[2], os.Stdin, os.Stdout, os.Stderr, maxInput, maxDepth); err != nil {
			fail(err)
		}
	case "filter-smudge":
		if len(os.Args) != 3 {
			fail(fmt.Errorf("filter-smudge requires a pathname"))
		}
		if err := filterSmudge(os.Args[2], os.Stdin, os.Stdout, os.Stderr, maxInput, maxDepth); err != nil {
			fail(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, helpOutput())
}

func helpOutput() string {
	return fmt.Sprintf("usage: %s clean|smudge|filter-clean <path>|filter-smudge <path>|install [--global|--local]|pull [path...]|version|help\n", appName)
}

func fail(err error) {
	logError(err)
	os.Exit(1)
}

func logError(err error) {
	logging.New(os.Stderr).Error(err.Error(), "app", appName)
}

func versionOutput() string {
	return fmt.Sprintf("%s %s\ncommit: %s\nbuilt: %s\n", appName, releaseVersion(), commit, date)
}

func releaseVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return version
	}
	return info.Main.Version
}

func maxInputBytes() (int64, error) {
	value := os.Getenv(maxInputBytesEnv)
	if value == "" {
		return defaultMaxInputBytes, nil
	}
	maxInput, err := strconv.ParseInt(value, 10, 64)
	if err != nil || maxInput < 1 {
		return 0, fmt.Errorf("%s must be a positive byte count", maxInputBytesEnv)
	}
	return maxInput, nil
}

func maxRecursionDepth() (int, error) {
	value := os.Getenv(maxRecursionDepthEnv)
	if value == "" {
		return confluence.DefaultMarkdownRecursionDepth, nil
	}
	maxDepth, err := strconv.Atoi(value)
	if err != nil || maxDepth < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", maxRecursionDepthEnv)
	}
	return maxDepth, nil
}

func readLimitedInput(r io.Reader, maxBytes int64) ([]byte, error) {
	input, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	if int64(len(input)) > maxBytes {
		return nil, fmt.Errorf("input exceeds %d bytes; set %s to a larger byte count if this page is expected",
			maxBytes, maxInputBytesEnv)
	}
	return input, nil
}
