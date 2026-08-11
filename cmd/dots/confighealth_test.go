package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	dotfiles "github.com/TowardInfinity/dotfiles"
)

// policyBody is what the repo's config.policy.toml contains, for the purposes
// of these tests. Its exact content does not matter — only that the managed
// block is compared against it byte for byte.
const policyBody = `model = "gpt-5.6-terra"
model_reasoning_effort = "medium"

[agents]
enabled = true
`

// fixture builds a fake HOME containing ~/.codex/config.toml, and optionally a
// checkout containing the policy file. Returns (repo, cleanup-free t.Setenv
// already applied).
func fixture(t *testing.T, configBody string, mode os.FileMode, withRepo bool) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	if configBody != "" {
		dir := filepath.Join(home, ".codex")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(p, []byte(configBody), 0o600); err != nil {
			t.Fatal(err)
		}
		// WriteFile applies the umask, so set the mode we actually want
		// explicitly — otherwise the 0644 case silently becomes 0644&^umask.
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}

	if !withRepo {
		return ""
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "common", "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, policyRel), []byte(policyBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// withBlock wraps the policy body in the managed-block markers the way
// merge-toml-block.py does, plus some machine-owned content around it.
func withBlock(inner string) string {
	return "notify = []\n\n" + mergeBegin + "\n" + inner + mergeEnd +
		"\n\n[projects.\"/home/me/x\"]\ntrust_level = \"trusted\"\n"
}

func find(t *testing.T, results []checkResult, name string) checkResult {
	t.Helper()
	for _, r := range results {
		if r.name == name {
			return r
		}
	}
	t.Fatalf("no %q row in %v", name, names(results))
	return checkResult{}
}

func names(rs []checkResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.name
	}
	return out
}

func TestConfigHealthValid(t *testing.T) {
	repo := fixture(t, withBlock(policyBody), 0o600, true)
	got := configChecks(repo)

	for _, n := range []string{"codex config", "codex mode", "managed block"} {
		if r := find(t, got, n); r.state != checkOK {
			t.Errorf("%s: state = %v (%s), want checkOK", n, r.state, r.path)
		}
	}
}

func TestConfigHealthMissingFile(t *testing.T) {
	repo := fixture(t, "", 0o600, true)
	got := configChecks(repo)

	if r := find(t, got, "codex config"); r.state != checkBad {
		t.Errorf("missing config: state = %v, want checkBad", r.state)
	}
	// One row, not three. Cascading failures for a single cause are what train
	// you to skim doctor's output.
	for _, n := range []string{"codex mode", "managed block"} {
		for _, r := range got {
			if r.name == n {
				t.Errorf("missing config also emitted a %q row; expected it to be suppressed", n)
			}
		}
	}
}

func TestConfigHealthMalformedTOML(t *testing.T) {
	repo := fixture(t, withBlock(policyBody)+"\nthis is not = = toml\n", 0o600, true)
	got := configChecks(repo)

	r := find(t, got, "codex config")
	if r.state != checkBad {
		t.Errorf("malformed TOML: state = %v, want checkBad", r.state)
	}
	if !strings.Contains(r.path, "does not parse") {
		t.Errorf("malformed TOML: detail = %q, want it to mention parsing", r.path)
	}
}

func TestConfigHealthWideMode(t *testing.T) {
	repo := fixture(t, withBlock(policyBody), 0o644, true)
	got := configChecks(repo)

	r := find(t, got, "codex mode")
	if r.state != checkBad {
		t.Errorf("0644 config: state = %v, want checkBad", r.state)
	}
	if !strings.Contains(r.path, "0644") {
		t.Errorf("0644 config: detail = %q, want it to name the actual mode", r.path)
	}
	// The file still parses and the block still matches — a mode problem must
	// not be reported as content drift.
	if r := find(t, got, "managed block"); r.state != checkOK {
		t.Errorf("managed block: state = %v, want checkOK (only the mode is wrong)", r.state)
	}
}

func TestConfigHealthDuplicateMarkers(t *testing.T) {
	body := withBlock(policyBody) + "\n" + mergeBegin + "\nx = 1\n" + mergeEnd + "\n"
	repo := fixture(t, body, 0o600, true)

	r := find(t, configChecks(repo), "managed block")
	if r.state != checkBad {
		t.Errorf("duplicate markers: state = %v, want checkBad", r.state)
	}
	if !strings.Contains(r.path, "2 begin") {
		t.Errorf("duplicate markers: detail = %q, want the marker counts", r.path)
	}
}

func TestConfigHealthMisorderedMarkers(t *testing.T) {
	body := "notify = []\n\n" + mergeEnd + "\n" + policyBody + mergeBegin + "\n"
	repo := fixture(t, body, 0o600, true)

	r := find(t, configChecks(repo), "managed block")
	if r.state != checkBad {
		t.Errorf("misordered markers: state = %v, want checkBad", r.state)
	}
	if !strings.Contains(r.path, "precedes") {
		t.Errorf("misordered markers: detail = %q, want it to say the order is wrong", r.path)
	}
}

func TestConfigHealthPolicyDrift(t *testing.T) {
	// Structurally perfect, but the block no longer matches the repo — the
	// exact failure that would otherwise be invisible.
	drifted := strings.Replace(policyBody, "medium", "high", 1)
	repo := fixture(t, withBlock(drifted), 0o600, true)

	r := find(t, configChecks(repo), "managed block")
	if r.state != checkBad {
		t.Errorf("policy drift: state = %v, want checkBad", r.state)
	}
	if !strings.Contains(r.path, "drifted") {
		t.Errorf("policy drift: detail = %q, want it to say drifted", r.path)
	}
}

// A --copy install has no checkout. Structure is still checked; the comparison
// against the policy source becomes unavailable, not failed — otherwise doctor
// exits non-zero on a machine working exactly as designed.
func TestConfigHealthCopyModeHasNoCheckout(t *testing.T) {
	fixture(t, withBlock(policyBody), 0o600, false)
	got := configChecks("")

	r := find(t, got, "managed block")
	if r.state != checkWarn {
		t.Errorf("copy mode: state = %v, want checkWarn", r.state)
	}
	if !strings.Contains(r.path, "no checkout") {
		t.Errorf("copy mode: detail = %q, want it to explain why", r.path)
	}
	// Everything answerable is still answered.
	for _, n := range []string{"codex config", "codex mode"} {
		if r := find(t, got, n); r.state != checkOK {
			t.Errorf("copy mode: %s = %v, want checkOK", n, r.state)
		}
	}
}

// ── --online ──────────────────────────────────────────────────

// redirectServer mimics GitHub's /releases/latest/download/<asset>: a redirect
// whose target carries the version tag.
func redirectServer(t *testing.T, tag string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tag == "" {
			w.WriteHeader(http.StatusOK) // no Location at all
			return
		}
		w.Header().Set("Location",
			"https://example.invalid/releases/download/"+tag+"/"+filepath.Base(r.URL.Path))
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestOnlineCurrent(t *testing.T) {
	s := redirectServer(t, "v1.2.3")
	t.Setenv("DOTS_RELEASE_BASE", s.URL)
	old := version
	version = "v1.2.3"
	t.Cleanup(func() { version = old })

	if r := onlineCheck(); r.state != checkOK {
		t.Errorf("current version: state = %v (%s), want checkOK", r.state, r.path)
	}
}

func TestOnlineStaleIsAFailure(t *testing.T) {
	s := redirectServer(t, "v1.2.3")
	t.Setenv("DOTS_RELEASE_BASE", s.URL)
	old := version
	version = "v1.2.2"
	t.Cleanup(func() { version = old })

	r := onlineCheck()
	if r.state != checkBad {
		t.Errorf("stale version: state = %v, want checkBad", r.state)
	}
	if !strings.Contains(r.path, "v1.2.3") || !strings.Contains(r.path, "v1.2.2") {
		t.Errorf("stale version: detail = %q, want both versions named", r.path)
	}
}

// A redirect with no version in it is unverifiable, not stale. Guessing here
// would report drift against a version that was never read.
func TestOnlineRedirectWithoutAVersion(t *testing.T) {
	s := redirectServer(t, "")
	t.Setenv("DOTS_RELEASE_BASE", s.URL)
	old := version
	version = "v1.2.3"
	t.Cleanup(func() { version = old })

	if r := onlineCheck(); r.state != checkWarn {
		t.Errorf("no version in redirect: state = %v (%s), want checkWarn", r.state, r.path)
	}
}

// Being unable to reach GitHub is not an unhealthy machine.
func TestOnlineUnreachableIsAWarning(t *testing.T) {
	s := redirectServer(t, "v1.2.3")
	base := s.URL
	s.Close() // nothing is listening now
	t.Setenv("DOTS_RELEASE_BASE", base)

	r := onlineCheck()
	if r.state != checkWarn {
		t.Errorf("unreachable: state = %v, want checkWarn", r.state)
	}
	if !strings.Contains(r.path, "could not reach") {
		t.Errorf("unreachable: detail = %q, want it to say so", r.path)
	}
}

func TestVersionFromRedirect(t *testing.T) {
	cases := map[string]string{
		"https://objects.githubusercontent.com/releases/download/v0.1.9/dots_linux_arm64": "v0.1.9",
		"https://example.invalid/releases/download/v1.0.0/dots_darwin_arm64":              "v1.0.0",
		"https://example.invalid/nothing/useful":                                          "",
		"":                                                                                "",
	}
	for in, want := range cases {
		if got := versionFromRedirect(in); got != want {
			t.Errorf("versionFromRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── grouping, exit codes, and the two repair actions ──────────

func TestConfigChecksAreGroupedApartFromTools(t *testing.T) {
	for n := range configCheckNames {
		if g := checkGroup(n); g != "Config" {
			t.Errorf("checkGroup(%q) = %q, want Config", n, g)
		}
	}
	// A tool must not fall into Config.
	if g := checkGroup("nvim"); g == "Config" {
		t.Error("nvim was grouped under Config")
	}
}

// The "i" key builds a package-install command. A config failure is not a
// package, and letting one through produced `brew install "managed block"`.
func TestInstallKeyIgnoresConfigFailures(t *testing.T) {
	m := doctorModel{
		repo: t.TempDir(),
		checks: []checkResult{
			{name: "codex mode", state: checkBad},
			{name: "managed block", state: checkBad},
		},
	}
	if _, _, ok := m.buildInstall(); ok {
		t.Error("buildInstall offered to install config failures as packages")
	}
	if !configRepairable(m.checks) {
		t.Error("configRepairable said there was nothing to repair")
	}
}

func TestConfigRepairIsASeparateAction(t *testing.T) {
	repo := t.TempDir()
	m := doctorModel{
		repo:   repo,
		checks: []checkResult{{name: "managed block", state: checkBad}},
	}
	req, _, ok := m.buildConfigRepair()
	if !ok {
		t.Fatal("buildConfigRepair declined a real config failure")
	}
	plan, err := buildOperation(req)
	if err != nil {
		t.Fatal(err)
	}
	process := planProcess(t, plan, 0)
	if len(process.Argv) == 0 || !strings.HasSuffix(process.Argv[0], "install.sh") {
		t.Errorf("repair runs %v, want install.sh", process.Argv)
	}
}

// Warnings must not trigger the repair action — relinking cannot fix being
// offline or having no checkout.
func TestWarningsAreNotRepairable(t *testing.T) {
	m := doctorModel{
		repo:   t.TempDir(),
		checks: []checkResult{{name: "managed block", state: checkWarn}},
	}
	if configRepairable(m.checks) {
		t.Error("a warning was treated as repairable")
	}
	if _, _, ok := m.buildConfigRepair(); ok {
		t.Error("buildConfigRepair acted on a warning")
	}
}

func TestDoctorKeysOfferRepairOnlyWhenConfigIsBroken(t *testing.T) {
	healthy := doctorModel{checks: []checkResult{{name: "managed block", state: checkOK}}}
	if hasKey(healthy.keys(), "c") {
		t.Error("the repair key was offered with nothing to repair")
	}
	broken := doctorModel{checks: []checkResult{{name: "managed block", state: checkBad}}}
	if !hasKey(broken.keys(), "c") {
		t.Error("the repair key was missing with a broken config")
	}
	// And "i" is not offered for a config-only failure.
	if hasKey(broken.keys(), "i") {
		t.Error("the install key was offered for a config failure")
	}
}

func hasKey(ks []key.Binding, want string) bool {
	for _, k := range ks {
		if k.Help().Key == want {
			return true
		}
	}
	return false
}

// ── CLI exit status and rendering ─────────────────────────────

// The whole point of checkWarn: a machine that is offline, or has no checkout
// to compare against, is not a machine that fails its health check.
func TestWarningsDoNotFailTheRun(t *testing.T) {
	failed, warned := classify([]checkResult{
		{name: "codex config", state: checkOK},
		{name: "managed block", state: checkWarn},
		{name: "release", state: checkWarn},
	})
	if len(failed) != 0 {
		t.Errorf("warnings produced failures: %v", failed)
	}
	if warned != 2 {
		t.Errorf("warned = %d, want 2", warned)
	}
}

func TestFailuresAreReportedRegardlessOfWarnings(t *testing.T) {
	failed, warned := classify([]checkResult{
		{name: "codex mode", state: checkBad},
		{name: "release", state: checkWarn},
		{name: "nvim", state: checkBad},
	})
	if len(failed) != 2 || warned != 1 {
		t.Fatalf("failed = %v, warned = %d; want 2 failures and 1 warning", failed, warned)
	}
	// Both kinds of advice must be offered, because both kinds broke.
	if !anyConfigFailed(failed) {
		t.Error("a config failure was not recognised as one")
	}
	if !anyToolFailed(failed) {
		t.Error("a tool failure was not recognised as one")
	}
}

// A pending row is not a pass. If it ever fell through to the OK branch, a
// check that never ran would read as a check that succeeded.
func TestPendingCountsAsFailure(t *testing.T) {
	failed, _ := classify([]checkResult{{name: "zsh", state: checkPending}})
	if len(failed) != 1 {
		t.Errorf("a pending check did not fail the run: %v", failed)
	}
}

func TestDoctorRejectsUnknownFlag(t *testing.T) {
	if code := runDoctorCLI("", []string{"--nope"}); code != 2 {
		t.Errorf("unknown flag: exit = %d, want 2", code)
	}
}

func TestDoctorHelpExitsZero(t *testing.T) {
	if code := runDoctorCLI("", []string{"--help"}); code != 0 {
		t.Errorf("--help: exit = %d, want 0", code)
	}
}

// The pane must render warnings distinctly from failures, and put the Config
// rows under their own heading.
func TestDoctorViewShowsConfigGroupAndWarnings(t *testing.T) {
	m := doctorModel{
		w: 120, h: 34,
		checks: []checkResult{
			{name: "zsh", state: checkOK, path: "/bin/zsh"},
			{name: "codex mode", state: checkOK, path: "0600"},
			{name: "managed block", state: checkWarn, path: "no checkout to compare against"},
		},
	}
	out := m.view("")
	if !strings.Contains(out, "CONFIG") {
		t.Error("the Config group heading is missing from the pane")
	}
	if !strings.Contains(out, "no checkout to compare against") {
		t.Error("the warning's explanation is missing from the pane")
	}
	// A warning must not be painted with the failure colour.
	//
	// v2 owns terminal color negotiation in Bubble Tea rather than exposing a
	// global Lip Gloss profile switch. Compare the semantic style tokens instead
	// of rendered output, which is intentionally plain under `go test`.
	if fmt.Sprint(styPending.GetForeground()) == fmt.Sprint(styBad.GetForeground()) {
		t.Error("warnings and failures render identically")
	}
	if fmt.Sprint(styPending.GetForeground()) == fmt.Sprint(styOK.GetForeground()) {
		t.Error("warnings and passes render identically")
	}
}

// ── release signing key ───────────────────────────────────────

// A throwaway key, generated for this test and never used to sign anything.
// The expected fingerprint below was computed independently by openssl:
//
//	openssl pkey -pubin -in k.pub -outform DER | openssl dgst -sha256 -binary | base64
//
// so this pins doctor's output to the shell command the docs tell you to
// cross-check it with, rather than to itself.
const testPubKey = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAVg8eD4vTJqJk9U9Ld+rGpbIvHVsRZTqVVVGEXEDn9AQ=
-----END PUBLIC KEY-----
`

const testPubKeyFP = "SHA256:91D+dAZMScvq41HuImX/2M1KZNjA/rJdTwsD+IL3KK8"

func TestKeyFingerprintMatchesOpenSSL(t *testing.T) {
	got, err := keyFingerprint([]byte(testPubKey))
	if err != nil {
		t.Fatalf("keyFingerprint: %v", err)
	}
	if got != testPubKeyFP {
		t.Errorf("fingerprint = %q, want %q (openssl disagrees with doctor)", got, testPubKeyFP)
	}
}

func TestKeyFingerprintRejectsNonPEM(t *testing.T) {
	for _, in := range []string{"", "not a key", "-----BEGIN PUBLIC KEY-----\n"} {
		if _, err := keyFingerprint([]byte(in)); err == nil {
			t.Errorf("keyFingerprint(%q) succeeded; a malformed key must not be reported as trusted", in)
		}
	}
}

// A trailing newline is not a key rotation. Without this, saving the file in an
// editor would make doctor claim the checkout and the binary disagree.
func TestNormalizeKeyIgnoresWhitespace(t *testing.T) {
	a := normalizeKey([]byte(testPubKey))
	b := normalizeKey([]byte(strings.TrimSpace(testPubKey) + "\n\n"))
	if !bytes.Equal(a, b) {
		t.Error("whitespace changed the normalized key")
	}
}

// Until the offline key exists every release is unsigned, and the resolver
// accepts that by design in warn mode. Doctor must say so without going red on
// a machine that is working exactly as intended.
func TestSigningKeyCheckWarnsWhenNoKeyCommitted(t *testing.T) {
	got := signingKeyCheck("")
	if got.name != "signing key" {
		t.Fatalf("name = %q", got.name)
	}
	if _, err := dotfiles.KeysFS.ReadFile("keys/release.pub"); err != nil {
		if got.state != checkWarn {
			t.Errorf("state = %v, want checkWarn while no key is committed", got.state)
		}
		if !strings.Contains(got.path, "unsigned") {
			t.Errorf("path = %q, want it to say releases are unsigned", got.path)
		}
		return
	}
	// The key has landed: it must now parse and report a fingerprint.
	if got.state != checkOK || !strings.HasPrefix(got.path, "SHA256:") {
		t.Errorf("with a key committed, got state=%v path=%q", got.state, got.path)
	}
}

// The signing key belongs to the Config group and must never reach the "i"
// install key — brew install "signing key" is not a thing.
func TestSigningKeyIsAConfigCheck(t *testing.T) {
	if !isConfigCheck("signing key") {
		t.Error(`"signing key" is not grouped as a config check`)
	}
	if g := checkGroup("signing key"); g != "Config" {
		t.Errorf("group = %q, want Config", g)
	}
}
