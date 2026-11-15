package selfupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidTag(t *testing.T) {
	for _, ok := range []string{"0.0.22", "v0.0.22", "0.0.22-pre", "0.0.13-pre-1"} {
		if !ValidTag(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "latest", "../x", "1.2.3/../../a", "1.2.3 4", "1.2.3?x=1", "v", "-pre"} {
		if ValidTag(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestSameVersion(t *testing.T) {
	if !SameVersion("v0.0.22", "0.0.22") || SameVersion("0.0.22-pre", "0.0.22") {
		t.Fatal("SameVersion")
	}
}

func TestAssetName(t *testing.T) {
	cases := map[[3]string]string{
		{"linux", "amd64", ""}: "FNode-linux-64.zip",
		{"linux", "arm64", ""}: "FNode-linux-arm64-v8a.zip",
		{"linux", "arm", "7"}:  "FNode-linux-arm32-v7.zip",
	}
	for in, want := range cases {
		got, err := AssetName(in[0], in[1], in[2])
		if err != nil || got != want {
			t.Errorf("%v: got %q, %v", in, got, err)
		}
	}
	if _, err := AssetName("windows", "amd64", ""); err == nil {
		t.Error("windows must be refused")
	}
}

func TestParseSHA256(t *testing.T) {
	h := strings.Repeat("ab", 32)
	got, err := ParseSHA256("MD5= 123\nSHA2-256= " + h + "\nSHA1= x\n")
	if err != nil || got != h {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err = ParseSHA256("MD5= 123\n"); err == nil {
		t.Fatal("missing sha256 must fail")
	}
}

func makeZip(t *testing.T, name, body string) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, _ := zw.Create(name)
	w.Write([]byte(body))
	zw.Close()
	return b.Bytes()
}

func setup(t *testing.T, zipBytes []byte, dgst string) (exe string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("remote update is Linux only")
	}
	asset, _ := AssetName(runtime.GOOS, runtime.GOARCH, buildGOARM())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/0.0.9/" + asset:
			w.Write(zipBytes)
		case "/0.0.9/" + asset + ".dgst":
			w.Write([]byte(dgst))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldBase, oldExe := releaseBase, exePath
	t.Cleanup(func() { releaseBase, exePath = oldBase, oldExe })
	releaseBase = srv.URL
	dir := t.TempDir()
	exe = filepath.Join(dir, "FNode")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	exePath = func() (string, error) { return exe, nil }
	return exe
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func TestApplyReplacesBinaryAndKeepsOld(t *testing.T) {
	z := makeZip(t, "FNode", "#!/bin/sh\necho 0.0.9\n")
	exe := setup(t, z, "SHA2-256= "+sum(z)+"\n")
	ver, err := Apply(context.Background(), "0.0.9")
	if err != nil || ver != "0.0.9" {
		t.Fatalf("Apply: %q, %v", ver, err)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old" {
		t.Fatalf("old binary not kept: %q", b)
	}
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "0.0.9") {
		t.Fatalf("new binary not in place: %q", b)
	}
	if es, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".fnode-update-*")); len(es) != 0 {
		t.Fatalf("staging dir left behind: %v", es)
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	z := makeZip(t, "FNode", "#!/bin/sh\necho 0.0.9\n")
	exe := setup(t, z, "SHA2-256= "+strings.Repeat("0", 64)+"\n")
	if _, err := Apply(context.Background(), "0.0.9"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want checksum error, got %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatal("binary must stay untouched")
	}
}

func TestApplyRejectsBrokenBinary(t *testing.T) {
	z := makeZip(t, "FNode", "#!/bin/sh\nexit 1\n")
	exe := setup(t, z, "SHA2-256= "+sum(z)+"\n")
	if _, err := Apply(context.Background(), "0.0.9"); err == nil {
		t.Fatal("a binary that does not run must be refused")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatal("binary must stay untouched")
	}
}

func TestApplyDisabledAndBadTag(t *testing.T) {
	Disabled = true
	if _, err := Apply(context.Background(), "0.0.9"); err == nil {
		t.Fatal("disabled must refuse")
	}
	Disabled = false
	if _, err := Apply(context.Background(), "../etc"); err == nil {
		t.Fatal("bad tag must refuse")
	}
}

func TestCompare(t *testing.T) {
	order := []string{"0.0.21", "0.0.22-pre", "0.0.22-pre-1", "0.0.22-pre-2", "0.0.22-pre-9", "0.0.22-pre-10", "v0.0.22", "0.0.23-pre", "0.0.23-pre-1", "0.0.23", "0.1.0"}
	for i := range order {
		for j := range order {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := Compare(order[i], order[j]); got != want {
				t.Errorf("Compare(%q, %q) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	if Compare("v0.0.22", "0.0.22") != 0 {
		t.Error("leading v must be ignored")
	}
	for _, tag := range []string{"0.0.22-pre-1", "0.0.22-pre-10", "0.0.22-pre"} {
		if !ValidTag(tag) {
			t.Errorf("%s must be valid", tag)
		}
	}
	if Compare("0.0.22-pre-prev3", "0.0.22-pre-3") != 0 || Compare("0.0.22-pre-prev", "0.0.22-pre") != 0 {
		t.Error("the old -prevN spelling reads as -N")
	}
}

func TestRollbackRestoresOldBinaryOnlyWhenPending(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "FNode")
	oldExe, oldBase := exePath, releaseBase
	t.Cleanup(func() { exePath, releaseBase = oldExe, oldBase })
	exePath = func() (string, error) { return exe, nil }
	os.WriteFile(exe, []byte("new"), 0o755)
	os.WriteFile(exe+".old", []byte("old"), 0o755)

	if Pending() {
		t.Fatal("no marker yet")
	}
	os.WriteFile(exe+".updating", []byte("0.1.0\n"), 0o644)
	if !Pending() {
		t.Fatal("marker must be seen")
	}
	if err := Rollback("config migration failed: boom"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("old binary not restored: %q", b)
	}
	if Pending() {
		t.Error("marker must be gone after a rollback")
	}
	if b, _ := os.ReadFile(exe + ".rollback-reason"); !strings.Contains(string(b), "boom") {
		t.Errorf("reason not stored: %q", b)
	}
	ReportRollback()
	if _, err := os.Stat(exe + ".rollback-reason"); err == nil {
		t.Error("the reason is reported once, then removed")
	}
	if err := Rollback("again"); err == nil {
		t.Error("without a previous binary a rollback must fail")
	}
}

func TestConfirmClearsPending(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "FNode")
	oldExe := exePath
	t.Cleanup(func() { exePath = oldExe })
	exePath = func() (string, error) { return exe, nil }
	os.WriteFile(exe+".updating", []byte("x"), 0o644)
	Confirm()
	if Pending() {
		t.Fatal("Confirm must clear the marker")
	}
}
