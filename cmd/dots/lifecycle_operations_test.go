package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
	"github.com/TowardInfinity/dotfiles/internal/dots/providers"
)

func TestMutationActionMatrixIsRegistered(t *testing.T) {
	want := map[ops.ActionID]bool{
		actionApply: true, actionDeps: true,
		actionNvimRestore: true, actionTPMRepair: true, actionDoctorRepair: true,
		actionService: true, actionPackage: true, actionRemoteDoctor: true,
		actionSyncInbound: true, actionPublish: true, actionRollout: true,
	}
	for _, def := range operationRegistry.Definitions() {
		delete(want, def.ID)
		if def.Scope == "" || def.Risk == "" || def.Summary == "" {
			t.Errorf("incomplete registry metadata for %s: %#v", def.ID, def)
		}
	}
	if len(want) != 0 {
		t.Fatalf("unregistered mutation actions: %v", reflect.ValueOf(want).MapKeys())
	}
}

func TestApplyPlanHasNoNetworkOrPackageManager(t *testing.T) {
	repo := t.TempDir()
	plan, err := buildOperation(applyRequest{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	for i := range plan.Steps {
		process := planProcess(t, plan, i)
		joined := strings.Join(process.Argv, " ")
		for _, forbidden := range []string{"curl", "git clone", "brew", "apt", "pnpm", "npm", "uv ", "go build", "dots-resolve"} {
			if strings.Contains(joined, forbidden) {
				t.Errorf("apply step %d reaches %q: %s", i, forbidden, joined)
			}
		}
		if !strings.Contains(joined, "--apply") {
			t.Errorf("apply step %d omitted install.sh's network-free guard: %s", i, joined)
		}
	}
}

func TestInboundPlanCannotPublishOrReachFleet(t *testing.T) {
	repo := newTestRepo(t)
	plan, err := buildOperation(syncInboundRequest{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plan.Steps[0].Exec.(providers.InboundGit); !ok {
		t.Fatalf("sync provider = %T, want providers.InboundGit", plan.Steps[0].Exec)
	}
	text := plan.CommandSummary()
	for _, forbidden := range []string{"git add", "git commit", "git push", "ssh "} {
		if strings.Contains(text, forbidden) {
			t.Errorf("inbound plan contains %q: %s", forbidden, text)
		}
	}
}

func TestPublishPlanCannotRollOut(t *testing.T) {
	repo := newTestRepo(t)
	plan, err := buildOperation(publishRequest{Repo: repo, Paths: []string{"tracked.txt"}, Message: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Scope != ops.ScopeRepo || plan.Risk != ops.RiskOutbound {
		t.Fatalf("scope/risk = %s/%s", plan.Scope, plan.Risk)
	}
	for _, step := range plan.Steps {
		if process, ok := step.Exec.(providers.Process); ok && len(process.Argv) > 0 && process.Argv[0] == "ssh" {
			t.Fatalf("publish contains SSH: %v", process.Argv)
		}
	}
}

func TestRolloutPlanCannotPublish(t *testing.T) {
	plan, err := buildOperation(rolloutRequest{
		Repo: t.TempDir(), Hosts: []string{"a1"},
		Revision: strings.Repeat("a", 40), Version: "v1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := plan.CommandSummary() + rolloutScript
	for _, forbidden := range []string{"git add", "git commit", "git push"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("rollout contains %q", forbidden)
		}
	}
	if _, ok := plan.Steps[1].Exec.(providers.SSHScript); !ok {
		t.Fatalf("remote rollout = %T, want typed SSHScript", plan.Steps[1].Exec)
	}
	if !strings.Contains(rolloutScript, `DOTS_RESOLVE_VERSION="$expected_version"`) {
		t.Fatal("rollout does not pin install.sh's release resolution")
	}
}

func TestTUIDispatchBuildsTheRegistryPlan(t *testing.T) {
	repo := t.TempDir()
	req := applyRequest{Repo: repo}
	want, err := buildOperation(req)
	if err != nil {
		t.Fatal(err)
	}
	msg := requestAction(req)()
	got, ok := msg.(runActionMsg)
	if !ok {
		t.Fatalf("dispatch returned %T", msg)
	}
	if got.plan.Action != want.Action || got.plan.CommandSummary() != want.CommandSummary() || !reflect.DeepEqual(got.plan.Affects, want.Affects) {
		t.Fatalf("TUI plan diverged:\n got %#v\nwant %#v", got.plan, want)
	}
}

func TestRolloutLatestGuardRejectsStalePublishedTag(t *testing.T) {
	if err := validateRolloutLatest("v0.1.14", "v0.1.15"); err == nil || !strings.Contains(err.Error(), "Latest is v0.1.15") {
		t.Fatalf("stale tag guard error = %v", err)
	}
	if err := validateRolloutLatest("v0.1.15", "v0.1.15"); err != nil {
		t.Fatalf("current Latest rejected: %v", err)
	}
}

func TestInboundProviderRefusesDirtyRepoWithoutChangingIt(t *testing.T) {
	repo := newTestRepo(t)
	file := filepath.Join(repo, "local.txt")
	if err := os.WriteFile(file, []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	plan, err := buildOperation(syncInboundRequest{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	result := ops.NewRunner().Run(context.Background(), plan, ops.IO{})
	if result.OK() || !strings.Contains(result.Error, "dirty") {
		t.Fatalf("dirty inbound result = %#v", result)
	}
	after, _ := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if string(after) != string(before) {
		t.Fatalf("dirty refusal moved HEAD from %s to %s", before, after)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("dirty file was not preserved: %v", err)
	}
}

func TestCurrentInboundSyncVerifiesWithoutApplying(t *testing.T) {
	repo := newTestRepo(t)
	sentinel := filepath.Join(repo, "applied")
	installer := "#!/bin/sh\nif [ \"${2:-}\" != --dry ]; then : > applied; fi\n"
	if err := os.WriteFile(filepath.Join(repo, "install.sh"), []byte(installer), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "install.sh"}, {"commit", "-m", "add installer"}, {"push", "-q", "origin", "main"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	plan, err := buildOperation(syncInboundRequest{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	result := ops.NewRunner().Run(context.Background(), plan, ops.IO{})
	if !result.OK() || result.Steps[1].Status != ops.StatusSkipped || result.Steps[2].Status != ops.StatusCompleted {
		t.Fatalf("current inbound result = %#v", result)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("current sync invoked mutating Apply: %v", err)
	}
}

func TestInboundProviderRefusesBranchChangeAfterPlanning(t *testing.T) {
	repo := newTestRepo(t)
	plan, err := buildOperation(syncInboundRequest{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "switch", "-c", "other").CombinedOutput(); err != nil {
		t.Fatalf("switch branch: %v\n%s", err, out)
	}
	result := ops.NewRunner().Run(context.Background(), plan, ops.IO{})
	if result.OK() || !strings.Contains(result.Error, "branch changed from main to other") {
		t.Fatalf("branch-change result = %#v", result)
	}
	branch, ok := currentBranch(repo)
	if !ok || branch != "other" {
		t.Fatalf("refusal moved branch: %q, %t", branch, ok)
	}
}

func TestInboundSyncNeverStagesOrPushes(t *testing.T) {
	repo := newTestRepo(t)
	file := filepath.Join(repo, "local.txt")
	if err := os.WriteFile(file, []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := buildOperation(syncInboundRequest{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	result := ops.NewRunner().Run(context.Background(), plan, ops.IO{})
	if result.OK() || !strings.Contains(result.Error, "dirty") {
		t.Fatalf("dirty inbound result = %#v", result)
	}
	staged, err := exec.Command("git", "-C", repo, "diff", "--cached", "--name-only").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(staged)) != "" {
		t.Fatalf("inbound sync staged files: %q", staged)
	}
}

func TestPublishRejectsAnyInvalidPathInsteadOfDroppingIt(t *testing.T) {
	_, err := buildOperation(publishRequest{
		Repo: newTestRepo(t), Paths: []string{"tracked.txt", "../outside"}, Message: "test",
	})
	if err == nil || !strings.Contains(err.Error(), "outside the checkout") {
		t.Fatalf("error = %v", err)
	}
}

func TestPublishDirectorySelectionValidatesChangedFilesInsideIt(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "config", "bad.json"), []byte("{\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := publishPreflight(context.Background(), repo, "main", []string{"config"}, false, false, ops.IO{})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("directory preflight error = %v", err)
	}
}

func TestPublishGoValidationRequiresEveryChangedGoPathSelected(t *testing.T) {
	repo := newTestRepo(t)
	for _, name := range []string{"selected.go", "unreviewed.go"} {
		body := "package example\n"
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := publishPreflight(context.Background(), repo, "main", []string{"selected.go"}, false, false, ops.IO{})
	if err == nil || !strings.Contains(err.Error(), "select every Go/module file") || !strings.Contains(err.Error(), "unreviewed.go") {
		t.Fatalf("partial Go selection error = %v", err)
	}
}

func TestPublishStagesOnlyTheExplicitSelection(t *testing.T) {
	repo := newTestRepo(t)
	for _, name := range []string{"selected.txt", "left-local.txt"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := buildOperation(publishRequest{
		Repo: repo, Paths: []string{"selected.txt"}, Message: "test: selected only", NoVerify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := ops.NewRunner().Run(context.Background(), plan, ops.IO{})
	if !result.OK() {
		t.Fatalf("publish result = %#v", result)
	}
	listed, err := exec.Command("git", "--git-dir", filepath.Join(filepath.Dir(repo), "remote.git"), "ls-tree", "-r", "--name-only", "main").Output()
	if err != nil {
		t.Fatal(err)
	}
	files := string(listed)
	if !strings.Contains(files, "selected.txt\n") || strings.Contains(files, "left-local.txt") {
		t.Fatalf("origin/main files:\n%s", files)
	}
	if _, err := os.Stat(filepath.Join(repo, "left-local.txt")); err != nil {
		t.Fatalf("unselected local file was changed: %v", err)
	}
}

func TestPublishPostStageGuardRefusesAConcurrentUnselectedPath(t *testing.T) {
	repo := newTestRepo(t)
	for _, name := range []string{"selected.txt", "appeared-late.txt"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("git", "-C", repo, "add", "--", "selected.txt", "appeared-late.txt").CombinedOutput(); err != nil {
		t.Fatalf("stage fixture: %v\n%s", err, out)
	}
	err := verifyStagedSelection(context.Background(), repo, []string{"selected.txt"}, false)
	if err == nil || !strings.Contains(err.Error(), "appeared-late.txt") || !strings.Contains(err.Error(), "outside the reviewed selection") {
		t.Fatalf("selection guard error = %v", err)
	}
}

func TestRolloutDeduplicatesHosts(t *testing.T) {
	plan, err := buildOperation(rolloutRequest{
		Repo: t.TempDir(), Hosts: []string{"a1", "a1"},
		Revision: strings.Repeat("a", 40), Version: "v1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %d, want local fetch plus one host", len(plan.Steps))
	}
}

func TestRemoteOperationsRejectOptionLikeSSHHosts(t *testing.T) {
	host := "-oProxyCommand=touch-pwned"
	requests := []ops.Request{
		remoteDoctorRequest{Alias: host},
		rolloutRequest{
			Repo: t.TempDir(), Hosts: []string{host},
			Revision: strings.Repeat("a", 40), Version: "v1.2.3",
		},
	}
	for _, req := range requests {
		if _, err := buildOperation(req); err == nil || !strings.Contains(err.Error(), "unsafe SSH host") {
			t.Errorf("%s error = %v", req.ActionID(), err)
		}
	}
}

func TestRemoteDoctorUsesTypedSSHProvider(t *testing.T) {
	plan, err := buildOperation(remoteDoctorRequest{Alias: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := plan.Steps[0].Exec.(providers.SSHScript)
	if !ok {
		t.Fatalf("remote doctor = %T, want providers.SSHScript", plan.Steps[0].Exec)
	}
	if provider.Host != "a1" || !strings.Contains(provider.Script, "exec dots doctor") {
		t.Fatalf("remote doctor provider = %#v", provider)
	}
}

func TestStatusTreatsUnanswerableConfigCheckAsWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := mergeBegin + "\n" + mergeEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	report := collectStatus("", false)
	if !report.ConfigOK || len(report.ConfigProblems) != 0 || len(report.Warnings) == 0 {
		t.Fatalf("status = %#v", report)
	}
}

func TestFallbackLifecycleVerbsRefuseExplicitly(t *testing.T) {
	for _, verb := range []string{"publish", "rollout", "deps", "apply", "install"} {
		cmd := exec.Command("bash", filepath.Join("..", "..", "bin", "dots"), verb)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("fallback %s succeeded", verb)
			continue
		}
		text := string(out)
		if !strings.Contains(text, "signed Go binary") || !strings.Contains(text, "dots update") {
			t.Errorf("fallback %s gave an ambiguous refusal:\n%s", verb, text)
		}
	}
}

func TestFallbackSyncRefusesRetiredOutboundFlags(t *testing.T) {
	for _, args := range [][]string{{"--push-only"}, {"-m", "message"}, {"--remotes-only"}, {"--yes"}} {
		cmd := exec.Command("bash", append([]string{filepath.Join("..", "..", "bin", "dots"), "sync"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("fallback sync %v succeeded", args)
			continue
		}
		text := string(out)
		if !strings.Contains(text, "retired") || (!strings.Contains(text, "dots publish") && !strings.Contains(text, "dots rollout")) {
			t.Errorf("fallback sync %v gave an ambiguous refusal:\n%s", args, text)
		}
	}
}
