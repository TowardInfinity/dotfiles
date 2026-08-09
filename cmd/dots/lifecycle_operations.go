package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
	"github.com/TowardInfinity/dotfiles/internal/dots/providers"
)

const (
	actionSyncInbound ops.ActionID = "repo.sync.inbound"
	actionPublish     ops.ActionID = "repo.publish"
	actionRollout     ops.ActionID = "fleet.rollout"
)

type syncInboundRequest struct {
	Repo  string
	Check bool
}

type publishRequest struct {
	Repo     string
	Paths    []string
	All      bool
	Message  string
	NoVerify bool
}

type rolloutRequest struct {
	Repo     string
	Hosts    []string
	Revision string
	Version  string
}

func (syncInboundRequest) ActionID() ops.ActionID { return actionSyncInbound }
func (publishRequest) ActionID() ops.ActionID     { return actionPublish }
func (rolloutRequest) ActionID() ops.ActionID     { return actionRollout }

func registerLifecycleOperations(r *ops.Registry) {
	for _, def := range []ops.Definition{
		{ID: actionSyncInbound, Summary: "Fetch and safely fast-forward this checkout", Scope: ops.ScopeLocal, Risk: ops.RiskReversible, Plan: planInboundSync},
		{ID: actionPublish, Summary: "Validate, commit selected changes, and push", Scope: ops.ScopeRepo, Risk: ops.RiskOutbound, Plan: planPublish},
		{ID: actionRollout, Summary: "Apply one published revision to selected machines", Scope: ops.ScopeFleet, Risk: ops.RiskDisruptive, Plan: planRollout},
	} {
		if err := r.Register(def); err != nil {
			panic(err)
		}
	}
}

func planInboundSync(req ops.Request) (ops.Plan, error) {
	in, err := requestAs[syncInboundRequest](req, actionSyncInbound)
	if err != nil {
		return ops.Plan{}, err
	}
	if in.Repo == "" {
		return ops.Plan{}, fmt.Errorf("no checkout found — nothing to sync")
	}
	branch, ok := currentBranch(in.Repo)
	if !ok {
		return ops.Plan{}, fmt.Errorf("detached HEAD — check out a branch before syncing")
	}
	state := &providers.InboundState{}
	plan := ops.Plan{
		Title:   "Sync inbound",
		Summary: "Fetch origin, refuse local drift, and fast-forward only",
		Target:  in.Repo,
		Steps: []ops.Step{{
			ID: "sync", Title: "Fetch and inspect origin/" + branch,
			Exec: providers.InboundGit{Repo: in.Repo, Branch: branch, Check: in.Check, State: state},
		}},
		Affects: []ops.ResourceID{resourceRepo},
		Timeout: 10 * time.Minute,
	}
	if !in.Check {
		installer := filepath.Join(in.Repo, "install.sh")
		plan.Steps = append(plan.Steps,
			ops.Step{ID: "apply", Title: "Apply the checked-out configuration", Exec: providers.Func{
				Label: "install.sh --apply (only when the checkout advances)",
				Do: func(ctx context.Context, streams ops.IO) error {
					if !state.Changed {
						return ops.Skip("checkout already current; no configuration mutation needed")
					}
					return providers.Command(in.Repo, installer, "--apply").Run(ctx, streams)
				},
			}},
			processStep("verify", "Verify applied configuration", in.Repo, installer, "--apply", "--dry"),
		)
		plan.Affects = append(plan.Affects, resourceConfig, resourceDoctor)
	}
	return plan, nil
}

func planPublish(req ops.Request) (ops.Plan, error) {
	in, err := requestAs[publishRequest](req, actionPublish)
	if err != nil {
		return ops.Plan{}, err
	}
	if in.Repo == "" {
		return ops.Plan{}, fmt.Errorf("no checkout found — nothing to publish")
	}
	if in.All == (len(in.Paths) > 0) {
		return ops.Plan{}, fmt.Errorf("choose explicit paths or --all, but not both")
	}
	if strings.TrimSpace(in.Message) == "" {
		return ops.Plan{}, fmt.Errorf("a commit message is required")
	}
	branch, ok := currentBranch(in.Repo)
	if !ok {
		return ops.Plan{}, fmt.Errorf("detached HEAD — check out a branch before publishing")
	}
	paths, err := normalizePublishPaths(in.Paths)
	if err != nil {
		return ops.Plan{}, err
	}
	if !in.All && len(paths) == 0 {
		return ops.Plan{}, fmt.Errorf("no paths selected")
	}
	stageArgv := []string{"git", "-C", in.Repo, "add"}
	if in.All {
		stageArgv = append(stageArgv, "-A")
	} else {
		stageArgv = append(stageArgv, "--")
		stageArgv = append(stageArgv, paths...)
	}
	target := strings.Join(paths, ", ")
	if in.All {
		target = "all changed paths"
	}
	preflight := providers.Func{Label: "verify branch, selection, and changed files", Do: func(ctx context.Context, streams ops.IO) error {
		return publishPreflight(ctx, in.Repo, branch, paths, in.All, in.NoVerify, streams)
	}}
	summary := "Validate and push selected repository changes; the fleet is not changed"
	if in.NoVerify {
		summary = "WARNING: validations are disabled; commit and push the selection without changing the fleet"
	}
	return ops.Plan{
		Title:   "Publish dotfiles",
		Summary: summary,
		Target:  target,
		Confirm: "Commit and push only the reviewed selection?",
		Steps: []ops.Step{
			processStep("fetch", "Fetch the current origin branch", in.Repo, "git", "-C", in.Repo, "fetch", "origin", branch),
			{ID: "preflight", Title: "Validate branch and selected files", Exec: preflight},
			processStep("stage", "Stage the selected paths", in.Repo, stageArgv...),
			{ID: "selection-guard", Title: "Verify the staged selection", Exec: providers.Func{
				Label: "verify the index contains only reviewed paths",
				Do: func(ctx context.Context, _ ops.IO) error {
					return verifyStagedSelection(ctx, in.Repo, paths, in.All)
				},
			}},
			processStep("commit", "Commit the selected paths", in.Repo, "git", "-C", in.Repo, "commit", "-m", in.Message),
			{ID: "branch-guard", Title: "Re-check the branch before pushing", Exec: providers.Func{Label: "verify HEAD is still on " + branch, Do: func(context.Context, ops.IO) error {
				return verifyCurrentBranch(in.Repo, branch)
			}}},
			processStep("push", "Push the current branch", in.Repo, "git", "-C", in.Repo, "push", "origin", branch),
		},
		Verification: []string{"origin contains the resulting commit", "no SSH or fleet apply step exists in this plan"},
		Recovery:     "Fix the failed validation or push, then rerun `dots publish` with the same explicit selection.",
		Affects:      []ops.ResourceID{resourceRepo},
		Timeout:      30 * time.Minute,
	}, nil
}

func verifyStagedSelection(ctx context.Context, repo string, paths []string, all bool) error {
	staged, err := gitNameList(ctx, repo, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return fmt.Errorf("read staged paths after staging: %w", err)
	}
	if len(staged) == 0 {
		return fmt.Errorf("staging produced no commit content")
	}
	if all {
		return nil
	}
	for _, path := range staged {
		if !pathSelected(path, paths) {
			return fmt.Errorf("%s became staged outside the reviewed selection; refusing to commit", path)
		}
	}
	return nil
}

func publishPreflight(ctx context.Context, repo, branch string, paths []string, all, noVerify bool, streams ops.IO) error {
	if err := verifyCurrentBranch(repo, branch); err != nil {
		return err
	}
	counts, err := gitOutput(ctx, repo, "rev-list", "--left-right", "--count", "HEAD...origin/"+branch)
	if err != nil {
		return fmt.Errorf("compare with origin/%s: %w", branch, err)
	}
	var ahead, behind int
	if _, err := fmt.Sscan(strings.TrimSpace(counts), &ahead, &behind); err != nil {
		return fmt.Errorf("unexpected git divergence result %q", strings.TrimSpace(counts))
	}
	if behind > 0 {
		return fmt.Errorf("origin/%s is %d commit(s) ahead; sync inbound before publishing", branch, behind)
	}
	if !all {
		staged, err := gitNameList(ctx, repo, "diff", "--cached", "--name-only", "-z")
		if err != nil {
			return fmt.Errorf("read staged paths: %w", err)
		}
		for _, path := range staged {
			if !pathSelected(path, paths) {
				return fmt.Errorf("%s is already staged but not selected; include or unstage it", path)
			}
		}
	}
	changed, err := changedPaths(ctx, repo)
	if err != nil {
		return err
	}
	selected := changed
	if !all {
		selected = make([]string, 0, len(changed))
		for _, changedPath := range changed {
			if pathSelected(changedPath, paths) {
				selected = append(selected, changedPath)
			}
		}
		for _, requested := range paths {
			matched := false
			for _, changedPath := range selected {
				if pathSelected(changedPath, []string{requested}) {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("selected path %q has no changes", requested)
			}
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("nothing selected has changed")
	}
	if noVerify {
		opWrite(streams.Stdout, "WARNING: validations skipped by --no-verify\n")
		return nil
	}
	if !all && hasGoValidationPath(selected) {
		for _, changedPath := range changed {
			if isGoValidationPath(changedPath) && !pathSelected(changedPath, paths) {
				return fmt.Errorf("%s is also changed; select every Go/module file so repository-wide validation cannot use unreviewed code", changedPath)
			}
		}
	}
	return validatePublishPaths(ctx, repo, selected, streams)
}

func hasGoValidationPath(paths []string) bool {
	for _, path := range paths {
		if isGoValidationPath(path) {
			return true
		}
	}
	return false
}

func isGoValidationPath(path string) bool {
	base := filepath.Base(path)
	return strings.EqualFold(filepath.Ext(path), ".go") ||
		base == "go.mod" || base == "go.sum" || base == "go.work" || base == "go.work.sum"
}

func verifyCurrentBranch(repo, expected string) error {
	branch, ok := currentBranch(repo)
	if !ok {
		return fmt.Errorf("HEAD became detached; refusing")
	}
	if branch != expected {
		return fmt.Errorf("branch changed from %s to %s; refusing", expected, branch)
	}
	return nil
}

func validatePublishPaths(ctx context.Context, repo string, paths []string, streams ops.IO) error {
	var goFiles []string
	runSelftest := false
	for _, path := range paths {
		full := filepath.Join(repo, filepath.FromSlash(path))
		info, err := os.Lstat(full)
		if os.IsNotExist(err) {
			continue // deletion
		}
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue // validate the link as Git data, never read through its target
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".json":
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				return fmt.Errorf("%s is invalid JSON: %w", path, err)
			}
		case ".toml":
			var value map[string]any
			if _, err := toml.Decode(string(data), &value); err != nil {
				return fmt.Errorf("%s is invalid TOML: %w", path, err)
			}
		case ".go":
			goFiles = append(goFiles, path)
		case ".sh", ".zsh":
			if err := validateShell(ctx, repo, path, data, streams); err != nil {
				return err
			}
		default:
			if isShellConfig(path) || (strings.HasPrefix(string(data), "#!") && strings.Contains(strings.SplitN(string(data), "\n", 2)[0], "sh")) {
				if err := validateShell(ctx, repo, path, data, streams); err != nil {
					return err
				}
			}
		}
		if path == "bootstrap.sh" || path == "install.sh" || strings.HasPrefix(path, "bin/dots-resolve") || path == "bin/sign-release.sh" || path == "bin/selftest.sh" {
			runSelftest = true
		}
	}
	if len(goFiles) > 0 {
		argv := []string{"gofmt", "-l"}
		for _, path := range goFiles {
			argv = append(argv, filepath.Join(repo, filepath.FromSlash(path)))
		}
		out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
		if err != nil {
			return fmt.Errorf("gofmt: %w", err)
		}
		if strings.TrimSpace(string(out)) != "" {
			return fmt.Errorf("Go files need gofmt: %s", strings.Join(nonemptyLines(string(out)), ", "))
		}
		for _, argv := range [][]string{{"go", "build", "./..."}, {"go", "vet", "./..."}, {"go", "test", "./..."}} {
			if err := providers.Command(repo, argv...).Run(ctx, streams); err != nil {
				return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
			}
		}
	}
	if runSelftest {
		if err := providers.Command(repo, filepath.Join(repo, "bin", "selftest.sh")).Run(ctx, streams); err != nil {
			return fmt.Errorf("selftest: %w", err)
		}
	}
	return nil
}

func isShellConfig(path string) bool {
	base := filepath.Base(path)
	return base == ".zshrc" || base == ".zprofile" || base == ".bashrc" || base == ".bash_profile"
}

func validateShell(ctx context.Context, repo, path string, data []byte, streams ops.IO) error {
	interpreter := "sh"
	first := strings.SplitN(string(data), "\n", 2)[0]
	if filepath.Ext(path) == ".zsh" || strings.HasPrefix(filepath.Base(path), ".z") || strings.Contains(first, "zsh") {
		interpreter = "zsh"
	} else if strings.Contains(first, "bash") {
		interpreter = "bash"
	}
	if err := providers.Command(repo, interpreter, "-n", filepath.Join(repo, filepath.FromSlash(path))).Run(ctx, streams); err != nil {
		return fmt.Errorf("%s failed syntax validation: %w", path, err)
	}
	return nil
}

func planRollout(req ops.Request) (ops.Plan, error) {
	in, err := requestAs[rolloutRequest](req, actionRollout)
	if err != nil {
		return ops.Plan{}, err
	}
	if in.Repo == "" || len(in.Hosts) == 0 {
		return ops.Plan{}, fmt.Errorf("rollout requires a checkout and at least one host")
	}
	if !regexp.MustCompile(`^[0-9a-fA-F]{40,64}$`).MatchString(in.Revision) {
		return ops.Plan{}, fmt.Errorf("rollout revision must be a full commit ID, got %q", in.Revision)
	}
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(in.Version) {
		return ops.Plan{}, fmt.Errorf("rollout revision has no SemVer release identity")
	}
	hosts := make([]string, 0, len(in.Hosts))
	seenHosts := make(map[string]bool, len(in.Hosts))
	for _, host := range in.Hosts {
		if err := validateSSHHost(host); err != nil {
			return ops.Plan{}, fmt.Errorf("rollout: %w", err)
		}
		if !seenHosts[host] {
			seenHosts[host] = true
			hosts = append(hosts, host)
		}
	}
	sort.Strings(hosts)
	plan := ops.Plan{
		Title:   "Roll out " + in.Version,
		Summary: "Fast-forward selected machines to one published commit, apply, then verify revision and signed-binary provenance",
		Target:  strings.Join(hosts, ", "),
		Confirm: fmt.Sprintf("Roll out %s (%s) to %d selected machine(s)?", in.Version, shortSHA(in.Revision), len(hosts)),
		Steps: []ops.Step{
			processStep("fetch", "Fetch the pinned release tag locally", in.Repo, "git", "-C", in.Repo, "fetch", "origin", "tag", in.Version),
		},
		Verification: []string{"exact checkout revision", "managed configuration", "release binary version", "signing-key provenance marker"},
		Recovery:     "Fix the reported host and rerun rollout for only that host; clean hosts are not repeated implicitly.",
		Affects:      []ops.ResourceID{resourceMachines},
		Timeout:      time.Duration(len(hosts))*12*time.Minute + 2*time.Minute,
	}
	for _, host := range hosts {
		plan.Steps = append(plan.Steps, ops.Step{
			ID: "rollout-" + host, Title: "Roll out and verify " + host, ContinueOnError: true,
			Exec: providers.SSHScript{
				Host: host, Args: []string{in.Revision, in.Version}, Timeout: 8,
				Label:  "ssh " + host + " <rollout " + in.Version + ">",
				Script: rolloutScript,
			},
		})
	}
	return plan, nil
}

const rolloutScript = `set -eu
revision=$1
expected_version=$2
export PATH="$HOME/.local/bin:$PATH"
repo=$(dots path)
branch=$(git -C "$repo" symbolic-ref --short HEAD) || {
  echo "detached HEAD — refusing rollout" >&2
  exit 1
}
test -z "$(git -C "$repo" status --porcelain)" || {
  echo "dirty checkout — refusing rollout" >&2
  exit 1
}
git -C "$repo" fetch origin
git -C "$repo" merge --ff-only "$revision"
DOTS_RESOLVE_VERSION="$expected_version" "$repo/install.sh"
test "$(git -C "$repo" rev-parse HEAD)" = "$revision"
"$repo/install.sh" --apply --dry
got_version=$(dots version)
test "$got_version" = "$expected_version" || {
  echo "binary is $got_version, expected $expected_version" >&2
  exit 1
}
cache=${XDG_CACHE_HOME:-$HOME/.cache}/dots
test "$(cat "$cache/current-version")" = "$expected_version"
target=$(readlink "$HOME/.local/bin/dots")
expected_target="$cache/dots-$expected_version"
test "$target" = "$expected_target" || {
  echo "installed symlink targets $target, expected $expected_target" >&2
  exit 1
}
if command -v sha256sum >/dev/null 2>&1; then
  key_id=$(sha256sum "$repo/keys/release.pub" | awk '{print $1}')
else
  key_id=$(shasum -a 256 "$repo/keys/release.pub" | awk '{print $1}')
fi
test "$(cat "$cache/dots-$expected_version.sig-ok")" = "$key_id"
dots status --json | grep -q '"config_ok":true'
printf 'verified %s %s on %s\n' "$expected_version" "$revision" "$branch"
`

func normalizePublishPaths(paths []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		path := filepath.ToSlash(filepath.Clean(raw))
		if raw == "" || path == "." || filepath.IsAbs(raw) || path == ".." || strings.HasPrefix(path, "../") {
			return nil, fmt.Errorf("publish path %q is outside the checkout or not a file selection", raw)
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out, nil
}

func pathSelected(path string, selected []string) bool {
	for _, candidate := range selected {
		if path == candidate || strings.HasPrefix(path, strings.TrimSuffix(candidate, "/")+"/") {
			return true
		}
	}
	return false
}

func changedPaths(ctx context.Context, repo string) ([]string, error) {
	var all []string
	for _, args := range [][]string{
		{"diff", "--name-only", "-z"},
		{"diff", "--cached", "--name-only", "-z"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	} {
		paths, err := gitNameList(ctx, repo, args...)
		if err != nil {
			return nil, fmt.Errorf("list changed paths: %w", err)
		}
		all = append(all, paths...)
	}
	seen, out := map[string]bool{}, []string{}
	for _, path := range all {
		if path != "" && !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}

func gitNameList(ctx context.Context, repo string, args ...string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		return nil, err
	}
	value := strings.TrimSuffix(string(out), "\x00")
	if value == "" {
		return nil, nil
	}
	return strings.Split(value, "\x00"), nil
}

func gitOutput(ctx context.Context, repo string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).Output()
	return string(out), err
}

func opWrite(w io.Writer, format string, args ...any) {
	if w != nil {
		_, _ = fmt.Fprintf(w, format, args...)
	}
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func latestReleaseTag() (string, error) {
	asset := fmt.Sprintf("dots_%s_%s", runtime.GOOS, runtime.GOARCH)
	client := &http.Client{Timeout: releaseTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(releaseBase() + "/" + asset)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if tag := versionFromRedirect(resp.Header.Get("Location")); tag != "" {
		return tag, nil
	}
	return "", fmt.Errorf("latest release redirect carried no tag")
}
