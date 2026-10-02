package file

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsExist(t *testing.T) {
	// Nonexistent file
	if IsExist("this_file_should_not_exist_12345.tmp") {
		t.Fatalf("expected false for nonexistent file")
	}

	// Temporary existing file
	tmpFile, err := os.CreateTemp("", "fnode_test_*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	if !IsExist(tmpFile.Name()) {
		t.Fatalf("expected true for existing temp file %s", tmpFile.Name())
	}

	// Nonexistent path under root
	nonexistentRootPath := filepath.Join(string(filepath.Separator), "root", ".local", "nonexistent_domain", "cert.crt")
	if IsExist(nonexistentRootPath) {
		t.Fatalf("expected false for nonexistent path %s", nonexistentRootPath)
	}
}
