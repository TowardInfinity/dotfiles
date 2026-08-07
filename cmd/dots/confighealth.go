package main

// Configuration health: the half of "is this machine healthy?" that has
// nothing to do with which tools are installed.
//
// Doctor's original checks answer "can the configs run at all" — is nvim here,
// is tpm cloned. These answer a different question: the tools are all present,
// but has the configuration itself drifted? That failure mode is quieter and
// strictly worse, because nothing errors. ~/.codex/config.toml going 0664 sat
// unnoticed on three servers until it was found by reading the merge script,
// and a managed block that silently stopped matching the repo would look
// identical to one that matched.
//
// These live in Doctor rather than a separate `dots audit` for the same reason
// the tool checks are shared between the CLI and the pane: two commands that
// both answer "is this machine healthy?" eventually disagree, and then you have
// to know which one to believe.

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// The markers install.sh splices around the managed block. These are duplicated
// from install.sh (MERGE_BEGIN/MERGE_END) rather than parsed out of it: a
// mismatch here reports drift that is not real, which is worse than not
// checking, so they must be changed together and the test below pins them.
const (
	mergeBegin = "# >>> dots: managed block — edit the source in the dotfiles repo >>>"
	mergeEnd   = "# <<< dots: end managed block <<<"
)

// codexMode is the mode ~/.codex/config.toml must have on every machine. It can
// hold MCP server credentials, so anything wider than owner-only is a finding,
// not a preference.
const codexMode os.FileMode = 0o600

// policyRel is the source of truth for the managed block, inside the checkout.
const policyRel = "common/codex/config.policy.toml"

// releaseTimeout bounds the one request --online makes. Short on purpose: this
// is a health check, and a doctor that hangs is worse than one that says it
// could not tell. Matches bin/dots-resolve.sh's default.
const releaseTimeout = 10 * time.Second

func codexConfigPath() string {
	return filepath.Join(os.Getenv("HOME"), ".codex", "config.toml")
}

// configChecks returns the Config group of doctor rows. repo may be empty —
// that is a --copy install, which has no checkout to compare against, and the
// policy comparison degrades to a warning rather than a failure.
func configChecks(repo string) []checkResult {
	out := []checkResult{}
	path := codexConfigPath()

	data, err := os.ReadFile(path)
	if err != nil {
		// Everything downstream needs the file's contents. Reporting three
		// cascading failures for one missing file trains you to skim them, so
		// this is the only row when the file is absent.
		out = append(out, checkResult{
			name:  "codex config",
			state: checkBad,
			path:  "missing: " + short(path),
		})
		return append(out, binaryCheck())
	}

	// Parse with a real TOML library. The alternative — shelling out to
	// python3 or to codex itself — makes doctor's answer depend on which
	// interpreter the machine happens to have, and the whole point of this
	// check is that it means the same thing everywhere.
	var doc map[string]any
	if _, err := toml.Decode(string(data), &doc); err != nil {
		out = append(out, checkResult{
			name:  "codex config",
			state: checkBad,
			path:  "does not parse: " + firstErrLine(err.Error()),
		})
	} else {
		out = append(out, checkResult{
			name:  "codex config",
			state: checkOK,
			path:  short(path),
		})
	}

	out = append(out, codexModeCheck(path))
	out = append(out, managedBlockCheck(string(data), repo))
	return append(out, binaryCheck())
}

func codexModeCheck(path string) checkResult {
	fi, err := os.Stat(path)
	if err != nil {
		return checkResult{name: "codex mode", state: checkBad, path: "cannot stat"}
	}
	got := fi.Mode().Perm()
	if got != codexMode {
		return checkResult{
			name:  "codex mode",
			state: checkBad,
			path:  fmt.Sprintf("%04o — must be %04o (it can hold credentials)", got, codexMode),
		}
	}
	return checkResult{name: "codex mode", state: checkOK, path: fmt.Sprintf("%04o", got)}
}

// managedBlockCheck verifies the block's structure, then its contents.
//
// Structure first and separately: a file with two opening markers, or with the
// end above the begin, is damage that a content diff would report as a
// mismatch — technically true and useless for working out what happened.
func managedBlockCheck(data, repo string) checkResult {
	lines := strings.Split(data, "\n")
	var begins, ends []int
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case mergeBegin:
			begins = append(begins, i)
		case mergeEnd:
			ends = append(ends, i)
		}
	}

	switch {
	case len(begins) == 0 && len(ends) == 0:
		return checkResult{
			name: "managed block", state: checkBad,
			path: "absent — run install.sh to merge the policy in",
		}
	case len(begins) != 1 || len(ends) != 1:
		return checkResult{
			name: "managed block", state: checkBad,
			path: fmt.Sprintf("%d begin / %d end markers — expected exactly one pair",
				len(begins), len(ends)),
		}
	case ends[0] < begins[0]:
		return checkResult{
			name: "managed block", state: checkBad,
			path: "end marker precedes the begin marker",
		}
	}

	// A --copy install keeps no checkout, so there is nothing to compare
	// against. That is not drift — it is an unanswerable question, and calling
	// it a failure would make doctor exit non-zero on a machine that is
	// working exactly as intended.
	if repo == "" {
		return checkResult{
			name: "managed block", state: checkWarn,
			path: "structure ok; no checkout to compare against (--copy install)",
		}
	}

	want, err := os.ReadFile(filepath.Join(repo, policyRel))
	if err != nil {
		return checkResult{
			name: "managed block", state: checkWarn,
			path: "structure ok; " + policyRel + " unreadable",
		}
	}

	// merge-toml-block.py writes `[begin] + src + [end]`, so the block is the
	// policy file's lines verbatim — an exact comparison is correct here, not
	// an approximation.
	got := lines[begins[0]+1 : ends[0]]
	wantLines := strings.Split(strings.TrimSuffix(string(want), "\n"), "\n")
	if !sameLines(got, wantLines) {
		return checkResult{
			name: "managed block", state: checkBad,
			path: fmt.Sprintf("drifted from %s (%d lines vs %d) — relink to repair",
				policyRel, len(got), len(wantLines)),
		}
	}
	return checkResult{name: "managed block", state: checkOK, path: "matches " + policyRel}
}

// binaryCheck reports what `dots` this is and where it came from. Informational
// by design: every source below is legitimate, and the offline pass has no way
// to know whether the version is current. `dots doctor --online` is what turns
// a stale version into a finding.
func binaryCheck() checkResult {
	src := "unknown source"
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		switch {
		case strings.Contains(exe, filepath.Join(".cache", "dots")):
			src = "release binary"
		case strings.HasSuffix(exe, "dots-bin"):
			src = "built from source"
		default:
			src = exe
		}
	}
	return checkResult{
		name:  "dots binary",
		state: checkOK,
		path:  fmt.Sprintf("%s · %s", version, src),
	}
}

// releaseBase is the "latest" download base, overridable so the online check
// can be tested against a local fixture server instead of GitHub.
func releaseBase() string {
	if v := os.Getenv("DOTS_RELEASE_BASE"); v != "" {
		return v
	}
	return "https://github.com/TowardInfinity/dotfiles/releases/latest/download"
}

// onlineCheck resolves the latest released version without downloading a
// binary, by reading the redirect that /releases/latest/download/<asset>
// issues — the tag is in the redirect target. Same trick bin/dots-resolve.sh
// uses, and for the same reason: it costs one request and no API token.
//
// Being unable to reach GitHub is explicitly NOT a failure. A laptop offline on
// a train is not an unhealthy machine, and making doctor exit non-zero for it
// would make the exit status useless in exactly the situation where you most
// want to trust it.
func onlineCheck() checkResult {
	asset := fmt.Sprintf("dots_%s_%s", runtime.GOOS, runtime.GOARCH)
	client := &http.Client{
		Timeout: releaseTimeout,
		// Do not follow: the redirect target is the answer.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(releaseBase() + "/" + asset)
	if err != nil {
		return checkResult{
			name: "release", state: checkWarn,
			path: "could not reach GitHub — version drift not checked",
		}
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	latest := versionFromRedirect(loc)
	if latest == "" {
		return checkResult{
			name: "release", state: checkWarn,
			path: "no version in the release redirect — drift not checked",
		}
	}

	// A dev build has no release to be behind. Say so rather than comparing a
	// tag against the literal string "dev" and calling it drift.
	if version == "dev" {
		return checkResult{
			name: "release", state: checkWarn,
			path: fmt.Sprintf("latest is %s; this is a dev build", latest),
		}
	}
	if version != latest {
		return checkResult{
			name: "release", state: checkBad,
			path: fmt.Sprintf("%s installed, %s is Latest — run `dots install` to relink",
				version, latest),
		}
	}
	return checkResult{name: "release", state: checkOK, path: latest + " (current)"}
}

// versionFromRedirect pulls the tag out of
// .../releases/download/<tag>/<asset>. Returns "" if the URL is not that shape,
// which the caller reports as unverifiable rather than guessing.
func versionFromRedirect(loc string) string {
	if loc == "" {
		return ""
	}
	parts := strings.Split(strings.TrimSuffix(loc, "/"), "/")
	for i := len(parts) - 2; i > 0; i-- {
		if parts[i-1] == "download" {
			return parts[i]
		}
	}
	return ""
}

func sameLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func firstErrLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// short rewrites $HOME back to ~ so a row fits and reads the way you'd type it.
func short(p string) string {
	if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}
