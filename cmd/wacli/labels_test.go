package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureOutput runs fn with os.Stdout/os.Stderr redirected to files and
// returns what each one got.
func captureOutput(t *testing.T, fn func() error) (string, string) {
	t.Helper()
	dir := t.TempDir()
	stdoutPath := filepath.Join(dir, "stdout")
	stderrPath := filepath.Join(dir, "stderr")

	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatal(err)
	}
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutFile, stderrFile
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
	}()

	runErr := fn()

	if err := stdoutFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrFile.Close(); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatalf("writeLabelsJSON: %v", runErr)
	}

	stdout, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(stdout), string(stderr)
}

func decodeLabelsPayload(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, raw)
	}
	return payload
}

func TestLabelsJSONReportsSyncError(t *testing.T) {
	list := []labelJSON{{ID: "1", Name: "Clientes", Color: 0}}
	stdout, stderr := captureOutput(t, func() error {
		return writeLabelsJSON(list, errors.New("app state key not available"))
	})

	payload := decodeLabelsPayload(t, stdout)
	if payload["success"] != true {
		t.Fatalf("success = %v", payload["success"])
	}
	if got := payload["sync_error"]; got != "app state key not available" {
		t.Fatalf("sync_error = %v", got)
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("data = %#v", payload["data"])
	}
	if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, "app state key") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestLabelsJSONWithoutSyncErrorKeepsEnvelope(t *testing.T) {
	stdout, stderr := captureOutput(t, func() error {
		return writeLabelsJSON([]labelJSON{{ID: "2", Name: "Mayoristas"}}, nil)
	})

	if stderr != "" {
		t.Fatalf("stderr should stay empty, got %q", stderr)
	}
	payload := decodeLabelsPayload(t, stdout)
	if _, present := payload["sync_error"]; present {
		t.Fatalf("sync_error should be absent: %v", payload)
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("data = %#v", payload["data"])
	}
	if payload["error"] != nil {
		t.Fatalf("error = %v", payload["error"])
	}
}

func TestLabelsJSONEmptyAccountIsAnEmptyList(t *testing.T) {
	stdout, _ := captureOutput(t, func() error {
		return writeLabelsJSON(nil, nil)
	})

	data, ok := decodeLabelsPayload(t, stdout)["data"].([]any)
	if !ok {
		t.Fatalf("data is not a list: %q", stdout)
	}
	if len(data) != 0 {
		t.Fatalf("data = %#v", data)
	}
}
