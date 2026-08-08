package main

import "testing"

// pnpmGlobalBinCheck/uvJupyterlabCheck themselves are not tested here, for
// the same reason evalCheck's "is this really on PATH" branches aren't:
// they shell out to whatever pnpm/uv happen to be on the machine running
// `go test`, which is exactly the kind of environment-dependent behavior a
// unit test should not assert on. The pure halves below carry the real
// logic and are what a bug would actually live in.

func TestPnpmGlobalBinResult(t *testing.T) {
	if r := pnpmGlobalBinResult(false, ""); r.state != checkBad {
		t.Errorf("unreachable global bin dir: state = %v, want checkBad", r.state)
	}
	if r := pnpmGlobalBinResult(true, "/Users/x/Library/pnpm/bin"); r.state != checkOK {
		t.Errorf("reachable global bin dir: state = %v, want checkOK", r.state)
	} else if r.path != "/Users/x/Library/pnpm/bin" {
		t.Errorf("path = %q, want the resolved dir echoed back", r.path)
	}
}

func TestUvToolListHas(t *testing.T) {
	// The exact shape `uv tool list` produces: one "<name> v<version>" line
	// per tool, followed by one "- <exposed command>" line per binary it
	// puts on PATH.
	const output = `kaggle v2.2.4
- kaggle
open-webui v0.10.2
- open-webui
jupyterlab v4.5.0
- jupyter
- jupyter-lab
`
	cases := map[string]bool{
		"jupyterlab": true,
		"kaggle":     true,
		"open-webui": true,
		// An exposed command can share a name with an unrelated package.
		// Only the package line (no leading "-") should count.
		"jupyter":     false,
		"jupyter-lab": false,
		"missing":     false,
	}
	for name, want := range cases {
		if got := uvToolListHas(output, name); got != want {
			t.Errorf("uvToolListHas(_, %q) = %v, want %v", name, got, want)
		}
	}

	if uvToolListHas("", "jupyterlab") {
		t.Error("empty uv tool list reported jupyterlab present")
	}
}

func TestUvJupyterlabResult(t *testing.T) {
	present := "jupyterlab v4.5.0\n- jupyter\n"
	if r := uvJupyterlabResult(present); r.state != checkOK {
		t.Errorf("jupyterlab present: state = %v, want checkOK", r.state)
	}

	absent := "kaggle v2.2.4\n- kaggle\n"
	r := uvJupyterlabResult(absent)
	if r.state != checkBad {
		t.Errorf("jupyterlab absent: state = %v, want checkBad", r.state)
	}
	if r.path == "" {
		t.Error("a checkBad row with no path gives no way to fix it")
	}
}

// ── grouping and the "i" key ───────────────────────────────

func TestPackageChecksAreGroupedApartFromTools(t *testing.T) {
	for n := range packageCheckNames {
		if g := checkGroup(n); g != "Packages" {
			t.Errorf("checkGroup(%q) = %q, want Packages", n, g)
		}
	}
	if g := checkGroup("pnpm"); g == "Packages" {
		t.Error("the tool-presence check for pnpm itself was grouped under Packages")
	}
}

// A package-health failure is a PATH problem or a missing `uv tool install`,
// neither of which "i" can fix by installing a formula — letting one through
// would generate `brew install "pnpm global bin"`.
func TestInstallKeyIgnoresPackageFailures(t *testing.T) {
	m := doctorModel{
		repo: t.TempDir(),
		checks: []checkResult{
			{name: "pnpm global bin", state: checkBad},
			{name: "uv tool: jupyterlab", state: checkBad},
		},
	}
	if _, _, ok := m.buildInstall(); ok {
		t.Error("buildInstall offered to install package-health failures as formulas")
	}
}
