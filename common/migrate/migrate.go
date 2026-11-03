package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/common/json5"
)

// Validator checks that the config at path (with its sing_origin.json) is
// accepted by the bundled sing-box core. It must not bind ports.
type Validator func(configPath string) error

// Result describes one Run.
type Result struct {
	Changes  []string // one line per converted option
	Backups  []string // backup files written
	Migrated bool     // the schema version was raised
}

// ErrRolledBack wraps the validation error when the migrated files failed the
// check and the previous files were put back.
var ErrRolledBack = errors.New("migrated config failed validation, previous files restored")

var schemaRe = regexp.MustCompile(`("SchemaVersion"\s*:\s*)\d+`)

// stamp is the timestamp in backup file names; tests pin it.
var stamp = func() string { return time.Now().Format("20060102-150405") }

// Run migrates the config at configPath when its SchemaVersion is below the
// current one: it converts sing_origin.json (the Cores[].OriginalPath files),
// writes a timestamped backup of every file it changes, logs one line per
// conversion and records SchemaVersion in config.json, so the next start does
// nothing. When validate rejects the result, every changed file is restored and
// the error wraps ErrRolledBack. config.json keeps its comments: only the
// SchemaVersion line is inserted into the text.
func Run(configPath string, validate Validator) (*Result, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg struct {
		SchemaVersion int `json:"SchemaVersion"`
		Cores         []struct {
			Type         string `json:"Type"`
			OriginalPath string `json:"OriginalPath"`
		} `json:"Cores"`
	}
	if err = decodeLoose(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	res := &Result{}
	if cfg.SchemaVersion >= SchemaVersion {
		return res, nil
	}

	type change struct {
		path string
		old  []byte
		new  []byte
	}
	var changes []change
	seen := map[string]bool{}
	for _, c := range cfg.Cores {
		p := c.OriginalPath
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(configPath), p)
		}
		old, rerr := os.ReadFile(p)
		if rerr != nil {
			continue // a missing origin file falls back to FNode's defaults at start
		}
		var d doc
		if derr := decodeLoose(old, &d); derr != nil {
			log.WithFields(log.Fields{"file": p, "err": derr}).Warn("Config migration: file is not readable JSON, left as it is")
			continue
		}
		lines := SingOptions(d)
		if len(lines) == 0 {
			continue
		}
		out, merr := json.MarshalIndent(d, "", "  ")
		if merr != nil {
			return nil, merr
		}
		for _, l := range lines {
			log.Infof("Config migration (%s): %s", filepath.Base(p), l)
			res.Changes = append(res.Changes, filepath.Base(p)+": "+l)
		}
		changes = append(changes, change{path: p, old: old, new: append(out, '\n')})
	}
	changes = append(changes, change{path: configPath, old: raw, new: setSchemaVersion(raw, SchemaVersion)})

	ts := stamp()
	var restore []func()
	rollback := func() {
		for i := len(restore) - 1; i >= 0; i-- {
			restore[i]()
		}
	}
	for _, c := range changes {
		bak := c.path + ".bak-" + ts
		if err = os.WriteFile(bak, c.old, 0o600); err != nil {
			rollback()
			return nil, fmt.Errorf("write backup %s: %w", bak, err)
		}
		res.Backups = append(res.Backups, bak)
		c := c
		restore = append(restore, func() { _ = writeFileAtomic(c.path, c.old) })
		if err = writeFileAtomic(c.path, c.new); err != nil {
			rollback()
			return nil, fmt.Errorf("write %s: %w", c.path, err)
		}
	}
	if validate != nil {
		if verr := validate(configPath); verr != nil {
			rollback()
			log.WithField("err", verr).Error("Config migration: the migrated config failed validation, previous files restored")
			return res, fmt.Errorf("%w: %v", ErrRolledBack, verr)
		}
	}
	res.Migrated = true
	log.Infof("Config migration: schema %d -> %d done (%d change(s), backups: %s)", cfg.SchemaVersion, SchemaVersion, len(res.Changes), strings.Join(res.Backups, ", "))
	return res, nil
}

// decodeLoose parses JSON5-ish text (comments, trailing commas) like the config loader.
func decodeLoose(raw []byte, v any) error {
	data, err := io.ReadAll(json5.NewTrimNodeReader(bytes.NewReader(raw)))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// setSchemaVersion sets "SchemaVersion": n in the config text without
// reformatting it (comments stay): an existing key is rewritten, otherwise the
// key is inserted after the opening brace.
func setSchemaVersion(raw []byte, n int) []byte {
	if schemaRe.Match(raw) {
		return schemaRe.ReplaceAll(raw, []byte(fmt.Sprintf("${1}%d", n)))
	}
	i := bytes.IndexByte(raw, '{')
	if i < 0 {
		return raw
	}
	rest := bytes.TrimLeft(raw[i+1:], " \t\r\n")
	comma := ","
	if len(rest) > 0 && rest[0] == '}' {
		comma = ""
	}
	ins := fmt.Sprintf("\n  \"SchemaVersion\": %d%s", n, comma)
	out := make([]byte, 0, len(raw)+len(ins))
	out = append(out, raw[:i+1]...)
	out = append(out, ins...)
	return append(out, raw[i+1:]...)
}

func writeFileAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// MigrateJSON applies the sing-box rules to one sing_origin.json text and
// returns the converted JSON (unchanged input comes back normalised).
func MigrateJSON(data []byte) ([]byte, error) {
	var d doc
	if err := decodeLoose(data, &d); err != nil {
		return nil, err
	}
	SingOptions(d)
	return json.MarshalIndent(d, "", "  ")
}

// ChangesJSON reports what SingOptions would change in a sing_origin.json text.
func ChangesJSON(data []byte) ([]string, error) {
	var d doc
	if err := decodeLoose(data, &d); err != nil {
		return nil, err
	}
	return SingOptions(d), nil
}
