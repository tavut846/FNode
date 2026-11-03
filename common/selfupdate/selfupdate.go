// Package selfupdate replaces the running FNode binary with a release asset
// downloaded from FNode's own GitHub releases. It is triggered by the panel's
// custom_node.update event (CustomNode plugin), never by a URL the panel sends:
// the panel names only the release tag, the repository is fixed here.
package selfupdate

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	maxZipBytes    = 300 << 20
	maxBinaryBytes = 500 << 20
	serviceName    = "FNode"
)

var (
	// Disabled turns remote updates off (config "DisableRemoteUpdate").
	Disabled bool

	// releaseBase is the fixed download root; tests point it at a local server.
	releaseBase = "https://github.com/tavut846/FNode/releases/download"
	exePath     = func() (string, error) {
		p, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(p)
	}
	running atomic.Bool

	// tagRe keeps the tag a single URL path segment: 0.0.22, v0.0.22, 0.0.22-pre, 0.0.22-pre-1.
	preSuffixRe = regexp.MustCompile(`^pre(?:-prev)?-?([0-9]*)$`)
	tagRe = regexp.MustCompile(`^v?[0-9]+(\.[0-9]+){1,3}(-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)
)

// ValidTag reports whether tag looks like an FNode release tag.
func ValidTag(tag string) bool { return len(tag) <= 64 && tagRe.MatchString(tag) }

// SameVersion compares two version strings ignoring a leading "v".
func SameVersion(a, b string) bool {
	return strings.TrimPrefix(strings.TrimSpace(a), "v") == strings.TrimPrefix(strings.TrimSpace(b), "v")
}

// Compare orders two release versions (leading "v" ignored): 0.0.22-pre (= pre-0)
// < 0.0.22-pre-1 < 0.0.22-pre-10 < 0.0.22 < 0.0.23-pre-1; the number is compared
// numerically and the old "-prevN" spelling is read as -N. Returns -1, 0 or 1.
func Compare(a, b string) int {
	ab, as := splitVersion(a)
	bb, bs := splitVersion(b)
	if c := compareNums(ab, bb); c != 0 {
		return c
	}
	ar, an := preRank(as)
	br, bn := preRank(bs)
	switch {
	case ar != br:
		if ar < br {
			return -1
		}
		return 1
	case an != bn:
		if an < bn {
			return -1
		}
		return 1
	}
	if ar > 0 {
		return 0
	}
	return strings.Compare(as, bs)
}

func splitVersion(v string) (base, suffix string) {
	v = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), "v"), "V")
	base, suffix, _ = strings.Cut(v, "-")
	return
}

func compareNums(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// preRank ranks a suffix: none (a stable release) is highest; "pre-N" is the
// N-th pre-release of its base (the old "pre" counts as 0, the old "pre-prevN"
// as N); anything else sorts below them by its text.
func preRank(suffix string) (rank, n int) {
	if suffix == "" {
		return 2, 0
	}
	if m := preSuffixRe.FindStringSubmatch(suffix); m != nil {
		n, _ = strconv.Atoi(m[1])
		return 1, n
	}
	return 0, 0
}

// AssetName is the release zip for a platform, matching .github/build/friendly-filenames.json.
func AssetName(goos, goarch, goarm string) (string, error) {
	if goos != "linux" {
		return "", fmt.Errorf("remote update supports Linux only (this is %s)", goos)
	}
	if goarch == "arm" {
		switch goarm {
		case "5", "6", "7":
			return "FNode-linux-arm32-v" + goarm + ".zip", nil
		}
		return "", errors.New("unknown ARM variant, update by hand")
	}
	names := map[string]string{
		"amd64":   "linux-64",
		"386":     "linux-32",
		"arm64":   "linux-arm64-v8a",
		"s390x":   "linux-s390x",
		"riscv64": "linux-riscv64",
	}
	n, ok := names[goarch]
	if !ok {
		return "", fmt.Errorf("no release asset for linux/%s, update by hand", goarch)
	}
	return "FNode-" + n + ".zip", nil
}

func buildGOARM() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "GOARM" {
				return s.Value
			}
		}
	}
	return ""
}

// ParseSHA256 reads the SHA-256 from an `openssl dgst` file as the release
// workflow writes it ("SHA2-256= <hex>" or "SHA256= <hex>").
func ParseSHA256(dgst string) (string, error) {
	for _, line := range strings.Split(dgst, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(k), "-", ""))
		if k != "SHA256" && k != "SHA2256" {
			continue
		}
		v = strings.ToLower(strings.TrimSpace(v))
		if len(v) == 64 {
			if _, err := hex.DecodeString(v); err == nil {
				return v, nil
			}
		}
	}
	return "", errors.New("no SHA-256 line in the checksum file")
}

func fetch(ctx context.Context, url string, dst io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("GET %s: larger than %d bytes", url, limit)
	}
	return nil
}

// Apply downloads release tag, verifies its SHA-256, checks that the new binary
// runs, keeps the current binary as <exe>.old and puts the new one in its place.
// It does not restart; call Restart afterwards.
func Apply(ctx context.Context, tag string) (string, error) {
	if Disabled {
		return "", errors.New("remote update is disabled in the config (DisableRemoteUpdate)")
	}
	if !ValidTag(tag) {
		return "", fmt.Errorf("invalid release tag %q", tag)
	}
	if !running.CompareAndSwap(false, true) {
		return "", errors.New("an update is already running")
	}
	defer running.Store(false)

	asset, err := AssetName(runtime.GOOS, runtime.GOARCH, buildGOARM())
	if err != nil {
		return "", err
	}
	exe, err := exePath()
	if err != nil {
		return "", fmt.Errorf("locate the running binary: %w", err)
	}
	// same directory, so the final rename stays on one filesystem
	tmp, err := os.MkdirTemp(filepath.Dir(exe), ".fnode-update-")
	if err != nil {
		return "", fmt.Errorf("create a staging directory next to %s: %w", exe, err)
	}
	defer os.RemoveAll(tmp)

	base := releaseBase + "/" + tag + "/" + asset
	log.Infof("Update: downloading %s", base)
	zipPath := filepath.Join(tmp, "release.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	err = fetch(ctx, base, zf, maxZipBytes)
	if cerr := zf.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	var dg strings.Builder
	if err = fetch(ctx, base+".dgst", &dg, 64<<10); err != nil {
		return "", fmt.Errorf("download checksum: %w", err)
	}
	want, err := ParseSHA256(dg.String())
	if err != nil {
		return "", err
	}
	got, err := fileSHA256(zipPath)
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf("checksum mismatch (expected %s, got %s): nothing was changed", want, got)
	}

	newBin := filepath.Join(tmp, "FNode.new")
	if err = extractBinary(zipPath, newBin); err != nil {
		return "", err
	}
	vctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(vctx, newBin, "version", "--short").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "", fmt.Errorf("the downloaded binary does not run on this machine: %v", err)
	}
	newVer := strings.TrimSpace(string(out))

	old := exe + ".old"
	_ = os.Remove(old)
	if err = os.Rename(exe, old); err != nil {
		return "", fmt.Errorf("keep the old binary: %w", err)
	}
	if err = os.Rename(newBin, exe); err != nil {
		_ = os.Rename(old, exe)
		return "", fmt.Errorf("put the new binary in place (old one restored): %w", err)
	}
	// the new binary clears this once its start-up checks (config migration) pass;
	// while it exists a failing check puts the old binary back (Rollback)
	if werr := os.WriteFile(pendingPath(exe), []byte(newVer+"\n"), 0o644); werr != nil {
		log.Warnf("Update: could not write the pending marker, an automatic rollback is not possible: %v", werr)
	}
	log.Infof("Update: installed %s at %s (previous binary kept as %s)", newVer, exe, old)
	return newVer, nil
}

func pendingPath(exe string) string  { return exe + ".updating" }
func rollbackNote(exe string) string { return exe + ".rollback-reason" }

// Pending reports whether this binary was installed by an update that has not
// yet passed its start-up checks.
func Pending() bool {
	exe, err := exePath()
	if err != nil {
		return false
	}
	_, err = os.Stat(pendingPath(exe))
	return err == nil
}

// Confirm marks the pending update as good: the old binary is no longer a
// rollback target.
func Confirm() {
	if exe, err := exePath(); err == nil {
		_ = os.Remove(pendingPath(exe))
	}
}

// Rollback puts the previous binary (<exe>.old) back after the freshly updated
// one failed a start-up check, and stores the reason for the next start to
// report (ReportRollback). It does not restart; call Restart afterwards.
func Rollback(reason string) error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	old := exe + ".old"
	if _, err = os.Stat(old); err != nil {
		return fmt.Errorf("no previous binary to roll back to: %w", err)
	}
	if err = os.Rename(old, exe); err != nil {
		return fmt.Errorf("restore the previous binary: %w", err)
	}
	_ = os.Remove(pendingPath(exe))
	_ = os.WriteFile(rollbackNote(exe), []byte(reason+"\n"), 0o644)
	log.Errorf("Update rolled back to the previous FNode binary: %s", reason)
	return nil
}

// ReportRollback logs (at error level, so the panel's log view gets it) why the
// last update was rolled back, once.
func ReportRollback() {
	exe, err := exePath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(rollbackNote(exe))
	if err != nil {
		return
	}
	_ = os.Remove(rollbackNote(exe))
	log.Errorf("The last FNode update was rolled back: %s", strings.TrimSpace(string(b)))
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func extractBinary(zipPath, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open the release zip: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "FNode" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		n, err := io.Copy(out, io.LimitReader(rc, maxBinaryBytes+1))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if n > maxBinaryBytes || n == 0 {
			return errors.New("the FNode binary in the zip has an unexpected size")
		}
		return os.Chmod(dst, 0o755)
	}
	return errors.New("the release zip has no FNode binary")
}

// Restart asks systemd to restart the FNode service (the new binary starts).
// Without systemd it only logs: restart by hand with `FNode restart`.
func Restart() {
	if p, err := exec.LookPath("systemctl"); err == nil {
		if err = exec.Command(p, "restart", "--no-block", serviceName).Run(); err == nil {
			return
		}
		log.Warnf("Update: systemctl restart failed: %v", err)
	}
	log.Warn("Update: installed, but FNode could not restart itself: run `FNode restart` to start the new version")
}

// Run applies release tag and restarts. current is the running version;
// the same version is skipped.
func Run(tag, current string) {
	if SameVersion(tag, current) {
		log.Infof("Update: already on %s, nothing to do", current)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	log.Infof("Update: panel asked to update FNode %s -> %s", current, tag)
	ver, err := Apply(ctx, tag)
	if err != nil {
		log.Errorf("Update failed, FNode %s keeps running: %v", current, err)
		return
	}
	log.Infof("Update: FNode %s installed, restarting now", ver)
	time.Sleep(2 * time.Second) // let the log line reach the panel
	Restart()
}
