package conf

import (
	"os"
	"path/filepath"
	"testing"
)

// readScript reads a file of the FNode-script branch (install.sh, FNode.sh, ...).
// The scripts live on the separate `FNode-script` branch of the repository, so
// they are looked up in $FNODE_SCRIPT_DIR, then in the sibling checkout
// ../../FNode-script. When neither exists the calling test is skipped.
func readScript(t testing.TB, name string) ([]byte, error) {
	t.Helper()
	dirs := []string{filepath.Join("..", "..", "FNode-script")}
	if env := os.Getenv("FNODE_SCRIPT_DIR"); env != "" {
		dirs = append([]string{env}, dirs...)
	}
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			return raw, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	t.Skipf("FNode-script branch checkout not found (set FNODE_SCRIPT_DIR or clone it next to this repo); skipping %s", name)
	return nil, nil
}
