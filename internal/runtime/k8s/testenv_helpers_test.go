package k8s

import (
	"testing"
)

// clearDoltAndCityEnv empties the GC_DOLT_* / GC_K8S_DOLT_* / GC_CITY_PATH /
// GC_BIN env vars for the duration of the test so the child scripts spawned
// via runControllerScriptDeploy and runBeadsScript (which inherit the test
// process's env through `os.Environ()`) do not observe a leak from the
// developer's shell. Each test's opts.Env continues to declare its own
// desired state, which overrides the emptied values when cmd.Env is flattened.
//
// GC_BIN matters because gc-controller-k8s resolves it via
// `GC_BIN="${GC_BIN:-gc}"` and then invokes `$GC_BIN config show --city …`.
// If a developer's shell exports GC_BIN to the real binary, the script
// bypasses the fake `gc` planted on PATH and parses the developer's actual
// resolved-config output instead of the test fixture.
//
// Shell scripts read these vars via `${VAR:-…}` / `[ -n "$VAR" ]` patterns, so
// an empty string is treated the same as unset — good enough to make the tests
// deterministic without needing a raw os.Unsetenv + manual cleanup.
func clearDoltAndCityEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"GC_DOLT_HOST",
		"GC_DOLT_PORT",
		"GC_K8S_DOLT_HOST",
		"GC_K8S_DOLT_PORT",
		"GC_CITY_PATH",
		"GC_BIN",
	} {
		t.Setenv(name, "")
	}
}
