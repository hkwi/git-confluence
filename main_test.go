package main

import (
	"strings"
	"testing"
)

func TestReadLimitedInputAcceptsInputAtLimit(t *testing.T) {
	input, err := readLimitedInput(strings.NewReader("abcd"), 4)
	if err != nil {
		t.Fatalf("readLimitedInput returned error: %v", err)
	}
	if string(input) != "abcd" {
		t.Fatalf("unexpected input: %q", input)
	}
}

func TestReadLimitedInputRejectsInputOverLimit(t *testing.T) {
	_, err := readLimitedInput(strings.NewReader("abcde"), 4)
	if err == nil {
		t.Fatal("readLimitedInput returned nil error")
	}
	if !strings.Contains(err.Error(), maxInputBytesEnv) {
		t.Fatalf("error should mention override env var: %v", err)
	}
}

func TestMaxInputBytesFromEnv(t *testing.T) {
	t.Setenv(maxInputBytesEnv, "123")

	maxInput, err := maxInputBytes()
	if err != nil {
		t.Fatalf("maxInputBytes returned error: %v", err)
	}
	if maxInput != 123 {
		t.Fatalf("unexpected max input: %d", maxInput)
	}
}

func TestMaxInputBytesRejectsInvalidEnv(t *testing.T) {
	t.Setenv(maxInputBytesEnv, "0")

	_, err := maxInputBytes()
	if err == nil {
		t.Fatal("maxInputBytes returned nil error")
	}
}
