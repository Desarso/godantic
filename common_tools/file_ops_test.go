package common_tools

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"
)

// mockDirEntry implements fs.DirEntry for testing
type mockDirEntry struct {
	name  string
	isDir bool
}

func (m mockDirEntry) Name() string               { return m.name }
func (m mockDirEntry) IsDir() bool                 { return m.isDir }
func (m mockDirEntry) Type() fs.FileMode           { return 0 }
func (m mockDirEntry) Info() (fs.FileInfo, error)  { return mockFileInfo{name: m.name, isDir: m.isDir}, nil }

type mockFileInfo struct {
	name  string
	isDir bool
}

func (m mockFileInfo) Name() string      { return m.name }
func (m mockFileInfo) Size() int64       { return 0 }
func (m mockFileInfo) Mode() fs.FileMode { return 0 }
func (m mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m mockFileInfo) IsDir() bool       { return m.isDir }
func (m mockFileInfo) Sys() interface{}  { return nil }

func setupFileMocks() func() {
	origRead := readFileFunc
	origWrite := writeFileFunc
	origMkdir := mkdirAllFunc
	origReadDir := readDirFunc

	return func() {
		readFileFunc = origRead
		writeFileFunc = origWrite
		mkdirAllFunc = origMkdir
		readDirFunc = origReadDir
	}
}

func TestReadFile(t *testing.T) {
	cleanup := setupFileMocks()
	defer cleanup()

	readFileFunc = func(name string) ([]byte, error) {
		if name == "test.txt" {
			return []byte("line1\nline2\nline3\nline4\nline5"), nil
		}
		return nil, fmt.Errorf("not found")
	}

	// Basic read
	result, err := ReadFile("test.txt", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "line1") {
		t.Error("expected line1 in output")
	}

	// With offset and limit
	result, err = ReadFile("test.txt", 2, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "line2\nline3" {
		t.Errorf("expected 'line2\\nline3', got %q", result)
	}

	// Empty path
	_, err = ReadFile("", 0, 0)
	if err == nil {
		t.Error("expected error for empty path")
	}

	// File not found
	_, err = ReadFile("missing.txt", 0, 0)
	if err == nil {
		t.Error("expected error for missing file")
	}

	// Offset beyond file
	result, err = ReadFile("test.txt", 100, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty for offset beyond file, got %q", result)
	}
}

func TestReadFileTruncation(t *testing.T) {
	cleanup := setupFileMocks()
	defer cleanup()

	bigContent := strings.Repeat("x", 60*1024)
	readFileFunc = func(name string) ([]byte, error) {
		return []byte(bigContent), nil
	}

	result, err := ReadFile("big.txt", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(result, "...(truncated at 50KB)") {
		t.Error("expected truncation suffix")
	}
}

func TestWriteFile(t *testing.T) {
	cleanup := setupFileMocks()
	defer cleanup()

	var writtenPath string
	var writtenData []byte
	mkdirAllFunc = func(path string, perm os.FileMode) error { return nil }
	writeFileFunc = func(name string, data []byte, perm os.FileMode) error {
		writtenPath = name
		writtenData = data
		return nil
	}

	result, err := WriteFile("out.txt", "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if writtenPath != "out.txt" {
		t.Errorf("expected out.txt, got %s", writtenPath)
	}
	if string(writtenData) != "hello world" {
		t.Error("content mismatch")
	}
	if !strings.Contains(result, "11 bytes") {
		t.Errorf("expected byte count in result: %s", result)
	}

	// Empty path
	_, err = WriteFile("", "data")
	if err == nil {
		t.Error("expected error for empty path")
	}

	// Mkdir failure
	mkdirAllFunc = func(path string, perm os.FileMode) error { return fmt.Errorf("perm denied") }
	_, err = WriteFile("dir/file.txt", "data")
	if err == nil {
		t.Error("expected error for mkdir failure")
	}
}

func TestEditFile(t *testing.T) {
	cleanup := setupFileMocks()
	defer cleanup()

	fileContent := "hello world\nfoo bar\nbaz"
	readFileFunc = func(name string) ([]byte, error) {
		return []byte(fileContent), nil
	}
	var writtenData []byte
	writeFileFunc = func(name string, data []byte, perm os.FileMode) error {
		writtenData = data
		return nil
	}

	result, err := EditFile("test.txt", "foo bar", "REPLACED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "replaced 1") {
		t.Error("expected success message")
	}
	if !strings.Contains(string(writtenData), "REPLACED") {
		t.Error("expected replacement in output")
	}

	// Not found
	_, err = EditFile("test.txt", "nonexistent", "new")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Error("expected not found error")
	}

	// Multiple matches
	fileContent = "aaa\naaa\naaa"
	_, err = EditFile("test.txt", "aaa", "bbb")
	if err == nil || !strings.Contains(err.Error(), "3 times") {
		t.Error("expected multiple match error")
	}

	// Empty path
	_, err = EditFile("", "a", "b")
	if err == nil {
		t.Error("expected error for empty path")
	}

	// Empty oldText
	_, err = EditFile("test.txt", "", "b")
	if err == nil {
		t.Error("expected error for empty old_text")
	}
}

func TestListDirectory(t *testing.T) {
	cleanup := setupFileMocks()
	defer cleanup()

	readDirFunc = func(name string) ([]os.DirEntry, error) {
		if name == "testdir" {
			return []os.DirEntry{
				mockDirEntry{name: "file.txt", isDir: false},
				mockDirEntry{name: "subdir", isDir: true},
			}, nil
		}
		return nil, fmt.Errorf("not found")
	}

	result, err := ListDirectory("testdir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "file.txt") || !strings.Contains(result, "subdir/") {
		t.Errorf("unexpected output: %s", result)
	}

	// Empty dir
	readDirFunc = func(name string) ([]os.DirEntry, error) {
		return nil, nil
	}
	result, err = ListDirectory("empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "(empty directory)" {
		t.Errorf("expected empty directory message, got %q", result)
	}

	// Default path
	readDirFunc = func(name string) ([]os.DirEntry, error) {
		if name != "." {
			t.Errorf("expected '.', got %q", name)
		}
		return nil, nil
	}
	_, _ = ListDirectory("")

	// Error
	readDirFunc = func(name string) ([]os.DirEntry, error) {
		return nil, fmt.Errorf("access denied")
	}
	_, err = ListDirectory("nope")
	if err == nil {
		t.Error("expected error")
	}
}
