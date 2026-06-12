package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"git-confluence/internal/confluence"
)

const (
	defaultMaxInputBytes = int64(64 << 20)
	maxInputBytesEnv     = "GIT_CONFLUENCE_MAX_INPUT_BYTES"
	maxRecursionDepthEnv = "GIT_CONFLUENCE_MAX_RECURSION_DEPTH"
)

func main() {
	if len(os.Args) != 2 {
		usage()
		os.Exit(2)
	}

	maxInput, err := maxInputBytes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-confluence-filter: %v\n", err)
		os.Exit(2)
	}

	maxDepth, err := maxRecursionDepth()
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-confluence-filter: %v\n", err)
		os.Exit(2)
	}

	input, err := readLimitedInput(os.Stdin, maxInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-confluence-filter: %v\n", err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "clean":
		storage, err := confluence.MarkdownToStorageWithMaxDepth(string(input), maxDepth)
		if err != nil {
			fmt.Fprintf(os.Stderr, "git-confluence-filter: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(storage)
	case "smudge":
		markdown, err := confluence.StorageToMarkdownWithMaxDepth(string(input), maxDepth)
		if err != nil {
			fmt.Fprintf(os.Stderr, "git-confluence-filter: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(markdown)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: git-confluence-filter clean|smudge")
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
