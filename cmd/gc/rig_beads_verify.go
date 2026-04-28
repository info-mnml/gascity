package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gastownhall/gascity/internal/doctor"
)

// providerNeedsRigBeadsConfigVerify reports whether the bd-config verification
// step applies to the current beads provider. The verification shells out to
// `bd config get/set`, which only makes sense for bd-backed stores. File-
// backed stores do not have a database layer to drift from on-disk config.
func providerNeedsRigBeadsConfigVerify(cityPath string) bool {
	return beadsProvider(cityPath) != "file"
}

// rigBeadsConfigRunner runs `bd <args>` in the given directory and returns
// stdout. Injectable for tests; production uses defaultRigBeadsConfigRunner.
type rigBeadsConfigRunner func(dir string, args ...string) ([]byte, error)

// defaultRigBeadsConfigRunner shells out to the bd binary with cwd set to dir
// so bd auto-discovers the rig's .beads store. BEADS_DOLT_AUTO_START=0
// suppresses bd's built-in Dolt auto-start so a discovery miss returns a
// surfaceable error instead of spawning a rogue local server (matches
// bdRuntimeEnv).
var defaultRigBeadsConfigRunner rigBeadsConfigRunner = func(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("bd", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BEADS_DOLT_AUTO_START=0")
	return cmd.Output()
}

// verifyAndCompleteRigBeadsConfig is the Go-side completion check for bd-backed
// rig initialization. It re-applies issue_prefix and types.custom on the bd
// store when they are missing or wrong.
//
// Background: gc-beads-bd's init op sets these two keys with `|| true` so the
// init does not fail outright on transient Dolt write failures. The visible
// symptom is a freshly-added rig where `bd create` returns "database not
// initialized" and types.custom is absent — both observed across nine rigs
// in mnml on 2026-04-28 (gt-qjs).
//
// The function is idempotent: when both keys already match the expected
// values it issues no writes. On any bd failure it returns a typed error
// rather than swallowing it, so `gc rig add` can fail loudly instead of
// reporting "Initialized beads database" against a half-initialized store.
func verifyAndCompleteRigBeadsConfig(dir, prefix string, runner rigBeadsConfigRunner) error {
	if runner == nil {
		runner = defaultRigBeadsConfigRunner
	}

	current, err := readBdConfigJSONValue(runner, dir, "issue_prefix")
	if err != nil {
		return fmt.Errorf("reading issue_prefix from bd config at %s: %w", dir, err)
	}
	if current != prefix {
		if _, err := runner(dir, "config", "set", "issue_prefix", prefix); err != nil {
			return fmt.Errorf("setting issue_prefix=%q at %s: %w", prefix, dir, err)
		}
	}

	rawTypes, err := readBdConfigJSONValue(runner, dir, "types.custom")
	if err != nil {
		return fmt.Errorf("reading types.custom from bd config at %s: %w", dir, err)
	}
	var existing []string
	if rawTypes != "" {
		existing = strings.Split(rawTypes, ",")
	}
	merged := mergeRigCustomTypes(existing, doctor.RequiredCustomTypes)
	if !equalTrimmedStringSlices(existing, merged) {
		if _, err := runner(dir, "config", "set", "types.custom", strings.Join(merged, ",")); err != nil {
			return fmt.Errorf("setting types.custom at %s: %w", dir, err)
		}
	}
	return nil
}

// readBdConfigJSONValue invokes `bd config get --json <key>` and returns the
// trimmed value. Empty (unset key) yields "" with no error so callers can
// branch on emptiness without a sentinel check.
func readBdConfigJSONValue(runner rigBeadsConfigRunner, dir, key string) (string, error) {
	out, err := runner(dir, "config", "get", "--json", key)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Value string `json:"value"`
	}
	if jerr := json.Unmarshal(out, &parsed); jerr != nil {
		return "", fmt.Errorf("parsing %s JSON: %w", key, jerr)
	}
	return strings.TrimSpace(parsed.Value), nil
}

// mergeRigCustomTypes returns the union of existing and required, in order:
// existing entries first (preserving user order), then any required entries
// not already present. Whitespace-only entries are dropped and duplicates
// are removed. Mirrors internal/doctor.mergeCustomTypes — duplicated here to
// avoid widening the doctor package's public surface for a single caller.
func mergeRigCustomTypes(existing, required []string) []string {
	seen := make(map[string]bool, len(existing)+len(required))
	merged := make([]string, 0, len(existing)+len(required))
	for _, t := range existing {
		trimmed := strings.TrimSpace(t)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		merged = append(merged, trimmed)
	}
	for _, req := range required {
		if seen[req] {
			continue
		}
		seen[req] = true
		merged = append(merged, req)
	}
	return merged
}

func equalTrimmedStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}
