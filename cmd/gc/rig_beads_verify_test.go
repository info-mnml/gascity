package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/doctor"
)

// stubBdConfigRunner records each invocation and returns scripted responses.
// Each entry in `responses` is keyed by the args joined with " ".
type stubBdConfigRunner struct {
	responses map[string]stubResponse
	calls     []stubCall
}

type stubResponse struct {
	out []byte
	err error
}

type stubCall struct {
	dir  string
	args []string
}

func (s *stubBdConfigRunner) run(dir string, args ...string) ([]byte, error) {
	s.calls = append(s.calls, stubCall{dir: dir, args: append([]string{}, args...)})
	key := strings.Join(args, " ")
	resp, ok := s.responses[key]
	if !ok {
		return nil, fmt.Errorf("stub: no scripted response for %q", key)
	}
	return resp.out, resp.err
}

func newStubBdConfigRunner(responses map[string]stubResponse) *stubBdConfigRunner {
	return &stubBdConfigRunner{responses: responses}
}

func TestMergeRigCustomTypes(t *testing.T) {
	cases := []struct {
		name     string
		existing []string
		required []string
		want     []string
	}{
		{
			name:     "empty existing yields required",
			existing: nil,
			required: []string{"a", "b"},
			want:     []string{"a", "b"},
		},
		{
			name:     "preserves user types and appends missing required",
			existing: []string{"custom-foo", "molecule"},
			required: []string{"molecule", "spec", "convergence"},
			want:     []string{"custom-foo", "molecule", "spec", "convergence"},
		},
		{
			name:     "dedupes duplicates",
			existing: []string{"a", "a", "b"},
			required: []string{"c"},
			want:     []string{"a", "b", "c"},
		},
		{
			name:     "drops empty entries and trims whitespace",
			existing: []string{" a ", "", "  ", "b"},
			required: []string{"c"},
			want:     []string{"a", "b", "c"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeRigCustomTypes(tc.existing, tc.required)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("mergeRigCustomTypes(%v, %v) = %v, want %v",
					tc.existing, tc.required, got, tc.want)
			}
		})
	}
}

// TestVerifyRigBeadsConfig_NoOpWhenAlreadyComplete confirms that a store with
// the expected issue_prefix and the full required types list registered does
// not trigger any writes.
func TestVerifyRigBeadsConfig_NoOpWhenAlreadyComplete(t *testing.T) {
	stub := newStubBdConfigRunner(map[string]stubResponse{
		"config get --json issue_prefix": {
			out: []byte(`{"key":"issue_prefix","value":"fe"}`),
		},
		"config get --json types.custom": {
			out: []byte(fmt.Sprintf(`{"key":"types.custom","value":%q}`,
				strings.Join(doctor.RequiredCustomTypes, ","))),
		},
	})

	if err := verifyAndCompleteRigBeadsConfig("/some/rig", "fe", stub.run); err != nil {
		t.Fatalf("verifyAndCompleteRigBeadsConfig() err = %v, want nil", err)
	}

	// Only the two reads — no writes when state already matches.
	if got := len(stub.calls); got != 2 {
		t.Fatalf("call count = %d, want 2 (only reads); calls=%v", got, stub.calls)
	}
	for _, c := range stub.calls {
		if c.args[0] == "config" && c.args[1] == "set" {
			t.Errorf("unexpected write call: %v", c.args)
		}
	}
}

// TestVerifyRigBeadsConfig_SetsMissingIssuePrefix confirms that an unset
// issue_prefix is written by the verification step.
func TestVerifyRigBeadsConfig_SetsMissingIssuePrefix(t *testing.T) {
	stub := newStubBdConfigRunner(map[string]stubResponse{
		"config get --json issue_prefix": {
			out: []byte(`{"key":"issue_prefix","value":""}`),
		},
		"config set issue_prefix fe": {
			out: []byte("ok"),
		},
		"config get --json types.custom": {
			out: []byte(fmt.Sprintf(`{"key":"types.custom","value":%q}`,
				strings.Join(doctor.RequiredCustomTypes, ","))),
		},
	})

	if err := verifyAndCompleteRigBeadsConfig("/some/rig", "fe", stub.run); err != nil {
		t.Fatalf("verifyAndCompleteRigBeadsConfig() err = %v, want nil", err)
	}

	wantWrite := []string{"config", "set", "issue_prefix", "fe"}
	if !hasCallWith(stub.calls, wantWrite) {
		t.Errorf("expected write call %v, calls=%v", wantWrite, stub.calls)
	}
}

// TestVerifyRigBeadsConfig_SetsMissingCustomTypes confirms missing required
// types are added (and any user-defined types preserved).
func TestVerifyRigBeadsConfig_SetsMissingCustomTypes(t *testing.T) {
	stub := newStubBdConfigRunner(map[string]stubResponse{
		"config get --json issue_prefix": {
			out: []byte(`{"key":"issue_prefix","value":"fe"}`),
		},
		"config get --json types.custom": {
			// Empty — types.custom never registered.
			out: []byte(`{"key":"types.custom","value":""}`),
		},
		"config set types.custom " + strings.Join(doctor.RequiredCustomTypes, ","): {
			out: []byte("ok"),
		},
	})

	if err := verifyAndCompleteRigBeadsConfig("/some/rig", "fe", stub.run); err != nil {
		t.Fatalf("verifyAndCompleteRigBeadsConfig() err = %v, want nil", err)
	}

	want := []string{"config", "set", "types.custom", strings.Join(doctor.RequiredCustomTypes, ",")}
	if !hasCallWith(stub.calls, want) {
		t.Errorf("expected write call %v, calls=%v", want, stub.calls)
	}
}

// TestVerifyRigBeadsConfig_PreservesUserTypes confirms that user-defined
// custom types are kept when adding the required ones.
func TestVerifyRigBeadsConfig_PreservesUserTypes(t *testing.T) {
	merged := mergeRigCustomTypes([]string{"custom-foo"}, doctor.RequiredCustomTypes)
	stub := newStubBdConfigRunner(map[string]stubResponse{
		"config get --json issue_prefix": {
			out: []byte(`{"key":"issue_prefix","value":"fe"}`),
		},
		"config get --json types.custom": {
			out: []byte(`{"key":"types.custom","value":"custom-foo"}`),
		},
		"config set types.custom " + strings.Join(merged, ","): {
			out: []byte("ok"),
		},
	})

	if err := verifyAndCompleteRigBeadsConfig("/some/rig", "fe", stub.run); err != nil {
		t.Fatalf("verifyAndCompleteRigBeadsConfig() err = %v, want nil", err)
	}

	want := []string{"config", "set", "types.custom", strings.Join(merged, ",")}
	if !hasCallWith(stub.calls, want) {
		t.Errorf("expected write call %v, calls=%v", want, stub.calls)
	}
}

// TestVerifyRigBeadsConfig_ErrorOnReadFailure confirms that a bd config get
// failure surfaces a typed error instead of being swallowed (the bug behavior
// in the wrapper script).
func TestVerifyRigBeadsConfig_ErrorOnReadFailure(t *testing.T) {
	wantErr := errors.New("dolt server unreachable")
	stub := newStubBdConfigRunner(map[string]stubResponse{
		"config get --json issue_prefix": {err: wantErr},
	})

	err := verifyAndCompleteRigBeadsConfig("/some/rig", "fe", stub.run)
	if err == nil {
		t.Fatal("verifyAndCompleteRigBeadsConfig() err = nil, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want it to wrap %v", err, wantErr)
	}
}

// TestVerifyRigBeadsConfig_ErrorOnWriteFailure confirms a bd config set
// failure surfaces a typed error.
func TestVerifyRigBeadsConfig_ErrorOnWriteFailure(t *testing.T) {
	wantErr := errors.New("permission denied")
	stub := newStubBdConfigRunner(map[string]stubResponse{
		"config get --json issue_prefix": {
			out: []byte(`{"key":"issue_prefix","value":""}`),
		},
		"config set issue_prefix fe": {err: wantErr},
	})

	err := verifyAndCompleteRigBeadsConfig("/some/rig", "fe", stub.run)
	if err == nil {
		t.Fatal("verifyAndCompleteRigBeadsConfig() err = nil, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want it to wrap %v", err, wantErr)
	}
}

// TestVerifyRigBeadsConfig_RunsInTargetDir confirms that bd commands are
// executed with cwd set to the rig directory so bd discovers the right
// .beads/ store.
func TestVerifyRigBeadsConfig_RunsInTargetDir(t *testing.T) {
	stub := newStubBdConfigRunner(map[string]stubResponse{
		"config get --json issue_prefix": {
			out: []byte(`{"key":"issue_prefix","value":"fe"}`),
		},
		"config get --json types.custom": {
			out: []byte(fmt.Sprintf(`{"key":"types.custom","value":%q}`,
				strings.Join(doctor.RequiredCustomTypes, ","))),
		},
	})

	want := "/path/to/myrig"
	if err := verifyAndCompleteRigBeadsConfig(want, "fe", stub.run); err != nil {
		t.Fatalf("verifyAndCompleteRigBeadsConfig() err = %v, want nil", err)
	}
	for _, c := range stub.calls {
		if c.dir != want {
			t.Errorf("call dir = %q, want %q (call=%v)", c.dir, want, c.args)
		}
	}
}

func hasCallWith(calls []stubCall, args []string) bool {
	for _, c := range calls {
		if reflect.DeepEqual(c.args, args) {
			return true
		}
	}
	return false
}

// TestProviderNeedsRigBeadsConfigVerify confirms that the bd-config
// verification step is gated correctly: it must run for bd-backed providers
// (the failure mode in gt-qjs) and skip for file-backed and unset providers
// where there is no database layer to drift from on-disk config.
func TestProviderNeedsRigBeadsConfigVerify(t *testing.T) {
	cases := []struct {
		name       string
		gcBeadsEnv string
		want       bool
	}{
		{name: "file backend skipped", gcBeadsEnv: "file", want: false},
		// Unset GC_BEADS defaults to "bd" via rawBeadsProvider, so verification
		// runs by default — matches the production path the bug was filed
		// against (mnml rigs that never set GC_BEADS).
		{name: "unset backend defaults to bd verified", gcBeadsEnv: "", want: true},
		{name: "bd backend verified", gcBeadsEnv: "bd", want: true},
		{name: "exec backend verified", gcBeadsEnv: "exec:/some/script.sh", want: true},
	}
	cityDir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GC_BEADS", tc.gcBeadsEnv)
			got := providerNeedsRigBeadsConfigVerify(cityDir)
			if got != tc.want {
				t.Errorf("providerNeedsRigBeadsConfigVerify(GC_BEADS=%q) = %v, want %v",
					tc.gcBeadsEnv, got, tc.want)
			}
		})
	}
}
