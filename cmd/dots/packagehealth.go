package main

// Package health: whatever a package manager installed *after* the manager
// itself was set up — `pnpm add -g`, `uv tool install` — as opposed to
// checkNames()'s "is the manager on PATH at all" checks and configChecks'
// "has a config file drifted" checks.
//
// This is a narrower question than either: not "is pnpm here" (checkNames
// already answers that) and not "does a file match the repo" (nothing here
// is a file comparison) but "did something pnpm/uv installed actually turn
// out usable". A live audit of this Mac while designing this file found the
// answer was no: pnpm's own `pnpm bin -g` refuses to print its global bin
// directory because that directory is not on PATH, which means every
// `pnpm add -g` up to that point installed into a folder nothing can run.
// Doctor's tool checks would never have caught it — pnpm itself was present
// and on PATH the whole time.
//
// Kept to what pnpm/uv already verify about themselves rather than
// reimplementing PATH-membership checks by hand: `pnpm bin -g`'s own exit
// code IS the check, and asking it is less likely to drift from pnpm's own
// definition of "reachable" than a parallel implementation would be.

import (
	"os/exec"
	"strings"
)

// packageCheckNames excludes these rows from the "i" (install) key's repair
// set, the same way isConfigCheck does for the Config group. None of these is
// a package doctor could brew/apt install its way out of — "pnpm global bin"
// is a PATH problem, not a missing formula.
var packageCheckNames = map[string]bool{
	"pnpm global bin":     true,
	"uv tool: jupyterlab": true,
}

func isPackageCheck(name string) bool { return packageCheckNames[name] }

// packageChecks returns the Packages group of doctor rows. Unlike
// configChecks, a row is only produced when the manager it's about is
// actually present — reporting "pnpm global bin: bad" on a machine that
// never installed pnpm is not a finding, it's noise from a check that does
// not apply.
func packageChecks() []checkResult {
	out := []checkResult{}
	if r, ok := pnpmGlobalBinCheck(); ok {
		out = append(out, r)
	}
	if r, ok := uvJupyterlabCheck(); ok {
		out = append(out, r)
	}
	return out
}

// pnpmGlobalBinCheck shells out to pnpm itself rather than resolving pnpm's
// config and comparing it against $PATH by hand: `pnpm bin -g` already
// refuses (non-zero exit, no stdout) when its configured global bin
// directory is not reachable, which is exactly the question being asked, in
// pnpm's own terms rather than a reimplementation of them.
func pnpmGlobalBinCheck() (checkResult, bool) {
	pnpm, ok := have("pnpm")
	if !ok {
		return checkResult{}, false
	}
	out, err := exec.Command(pnpm, "bin", "-g").Output()
	return pnpmGlobalBinResult(err == nil, strings.TrimSpace(string(out))), true
}

// pnpmGlobalBinResult is the pure half of the check above, split out so a
// test can drive both branches without a working pnpm on the machine running
// `go test`.
func pnpmGlobalBinResult(reachable bool, dir string) checkResult {
	const name = "pnpm global bin"
	if !reachable {
		return checkResult{
			name:  name,
			state: checkBad,
			path:  "not on PATH — run `pnpm setup`, then restart the shell",
		}
	}
	return checkResult{name: name, state: checkOK, path: dir}
}

// uvJupyterlabCheck exists because install_uv_tools in bootstrap.sh declares
// jupyterlab as something every full install gets — doctor should notice if
// it's silently gone the same way it notices a missing symlink.
func uvJupyterlabCheck() (checkResult, bool) {
	uv, ok := have("uv")
	if !ok {
		return checkResult{}, false
	}
	out, err := exec.Command(uv, "tool", "list").Output()
	if err != nil {
		// `uv tool list` failing outright (not "empty list", an actual error)
		// is not the same claim as "jupyterlab is missing" — say so as a warn
		// rather than asserting something the command did not actually say.
		return checkResult{
			name:  "uv tool: jupyterlab",
			state: checkWarn,
			path:  "uv tool list failed: " + firstErrLine(err.Error()),
		}, true
	}
	return uvJupyterlabResult(string(out)), true
}

func uvJupyterlabResult(uvToolListOutput string) checkResult {
	const name = "uv tool: jupyterlab"
	if uvToolListHas(uvToolListOutput, "jupyterlab") {
		return checkResult{name: name, state: checkOK, path: "installed"}
	}
	return checkResult{
		name:  name,
		state: checkBad,
		path:  "not installed — run: uv tool install jupyterlab",
	}
}

// uvToolListHas parses `uv tool list` output:
//
//	kaggle v2.2.4
//	- kaggle
//	jupyterlab v4.5.0
//	- jupyter
//	- jupyter-lab
//
// One package line per tool (name, a space, "v" + version), followed by one
// "- <exposed command>" line per binary it puts on PATH. Only the package
// lines are matched — skipping "-" lines matters because a tool can expose a
// command that happens to share a name with another package.
func uvToolListHas(output, name string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == name {
			return true
		}
	}
	return false
}
