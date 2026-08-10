package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
	"github.com/TowardInfinity/dotfiles/internal/dots/providers"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	actionApply        ops.ActionID = "config.apply"
	actionDeps         ops.ActionID = "dependencies.install"
	actionNvimRestore  ops.ActionID = "nvim.restore"
	actionTPMRepair    ops.ActionID = "tmux.tpm.repair"
	actionDoctorRepair ops.ActionID = "doctor.dependencies.repair"
	actionService      ops.ActionID = "service.change"
	actionPackage      ops.ActionID = "package.upgrade"
	actionRemoteDoctor ops.ActionID = "machine.doctor"
)

const (
	resourceRepo     ops.ResourceID = "repo"
	resourceConfig   ops.ResourceID = "config"
	resourceDoctor   ops.ResourceID = "doctor"
	resourceServices ops.ResourceID = "services"
	resourcePackages ops.ResourceID = "packages"
	resourceMachines ops.ResourceID = "machines"
)

type applyRequest struct{ Repo string }
type depsRequest struct{ Repo string }
type nvimRestoreRequest struct{}
type tpmRepairRequest struct{ Repo string }
type doctorRepairRequest struct {
	Repo    string
	Missing []string
	GOOS    string
}
type serviceRequest struct {
	Service service
	Verb    string
}
type packageRequest struct{ Package pkg }
type remoteDoctorRequest struct{ Alias string }

func (applyRequest) ActionID() ops.ActionID        { return actionApply }
func (depsRequest) ActionID() ops.ActionID         { return actionDeps }
func (nvimRestoreRequest) ActionID() ops.ActionID  { return actionNvimRestore }
func (tpmRepairRequest) ActionID() ops.ActionID    { return actionTPMRepair }
func (doctorRepairRequest) ActionID() ops.ActionID { return actionDoctorRepair }
func (serviceRequest) ActionID() ops.ActionID      { return actionService }
func (packageRequest) ActionID() ops.ActionID      { return actionPackage }
func (remoteDoctorRequest) ActionID() ops.ActionID { return actionRemoteDoctor }

var operationRegistry = buildOperationRegistry()

func buildOperationRegistry() *ops.Registry {
	r := ops.NewRegistry()
	must := func(def ops.Definition) {
		if err := r.Register(def); err != nil {
			panic(err)
		}
	}
	must(ops.Definition{ID: actionApply, Summary: "Relink and merge local configuration", Scope: ops.ScopeLocal, Risk: ops.RiskReversible, Plan: planApply})
	must(ops.Definition{ID: actionDeps, Summary: "Install configured dependencies", Scope: ops.ScopeLocal, Risk: ops.RiskDisruptive, Plan: planDeps})
	must(ops.Definition{ID: actionNvimRestore, Summary: "Restore Neovim plugins", Scope: ops.ScopeLocal, Risk: ops.RiskReversible, Plan: planNvimRestore})
	must(ops.Definition{ID: actionTPMRepair, Summary: "Install or repair TPM", Scope: ops.ScopeLocal, Risk: ops.RiskReversible, Plan: planTPMRepair})
	must(ops.Definition{ID: actionDoctorRepair, Summary: "Repair missing dependencies", Scope: ops.ScopeLocal, Risk: ops.RiskDisruptive, Plan: planDoctorRepair})
	must(ops.Definition{ID: actionService, Summary: "Change service state", Scope: ops.ScopeLocal, Risk: ops.RiskDisruptive, Plan: planService})
	must(ops.Definition{ID: actionPackage, Summary: "Upgrade one package", Scope: ops.ScopeLocal, Risk: ops.RiskDisruptive, Plan: planPackage})
	must(ops.Definition{ID: actionRemoteDoctor, Summary: "Inspect a remote machine", Scope: ops.ScopeFleet, Risk: ops.RiskReadOnly, Plan: planRemoteDoctor})
	registerLifecycleOperations(r)
	return r
}

func buildOperation(req ops.Request) (ops.Plan, error) { return operationRegistry.Build(req) }

func requestAction(req ops.Request) tea.Cmd {
	return func() tea.Msg {
		plan, err := buildOperation(req)
		if err != nil {
			return actionPlanErrorMsg{err: err}
		}
		return runActionMsg{plan: plan}
	}
}

func requestAs[T any](req ops.Request, id ops.ActionID) (T, error) {
	value, ok := req.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%s received %T", id, req)
	}
	return value, nil
}

func processStep(id, title, dir string, argv ...string) ops.Step {
	return ops.Step{ID: id, Title: title, Exec: providers.Command(dir, argv...)}
}

func planApply(req ops.Request) (ops.Plan, error) {
	in, err := requestAs[applyRequest](req, actionApply)
	if err != nil {
		return ops.Plan{}, err
	}
	if in.Repo == "" {
		return ops.Plan{}, fmt.Errorf("no checkout found — nothing to apply")
	}
	installer := filepath.Join(in.Repo, "install.sh")
	confirm := ""
	previewCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	preview := exec.CommandContext(previewCtx, installer, "--apply", "--dry")
	preview.Dir = in.Repo
	if output, err := preview.CombinedOutput(); err != nil ||
		strings.Contains(string(output), "would back up") || strings.Contains(string(output), "would merge") {
		confirm = "Applying will back up or rewrite at least one managed destination. Continue?"
	}
	return ops.Plan{
		Title:   "Apply dotfiles",
		Summary: "Relink configs and re-merge managed policy without network access",
		Target:  in.Repo,
		Confirm: confirm,
		Steps: []ops.Step{
			processStep("apply", "Relink and merge configuration", in.Repo, installer, "--apply"),
			processStep("verify", "Verify applied configuration", in.Repo, installer, "--apply", "--dry"),
		},
		Verification: []string{"all managed links are current", "Codex managed block parses and matches policy"},
		Recovery:     "Fix the reported destination and retry `dots apply`.",
		Affects:      []ops.ResourceID{resourceConfig, resourceDoctor, resourceRepo},
		Timeout:      10 * time.Minute,
	}, nil
}

func planDeps(req ops.Request) (ops.Plan, error) {
	in, err := requestAs[depsRequest](req, actionDeps)
	if err != nil {
		return ops.Plan{}, err
	}
	if in.Repo == "" {
		return ops.Plan{}, fmt.Errorf("no checkout found — dependencies require bootstrap.sh")
	}
	argv := []string{"sh", filepath.Join(in.Repo, "bootstrap.sh"), "--deps"}
	return ops.Plan{
		Title:   "Install dependencies",
		Summary: "Use OS package repositories plus the unpinned HTTPS installers and tarballs declared in bootstrap.sh",
		Target:  in.Repo,
		Confirm: "Install dependencies from package repositories and documented third-party sources?",
		Steps:   []ops.Step{processStep("deps", "Install configured dependencies", in.Repo, argv...)},
		Affects: []ops.ResourceID{resourceDoctor, resourcePackages, resourceConfig},
		Timeout: 30 * time.Minute,
	}, nil
}

func planNvimRestore(req ops.Request) (ops.Plan, error) {
	if _, err := requestAs[nvimRestoreRequest](req, actionNvimRestore); err != nil {
		return ops.Plan{}, err
	}
	return ops.Plan{
		Title: "Restore Neovim plugins", Confirm: "Restore Neovim plugins to match lazy-lock.json?",
		Steps:   []ops.Step{processStep("restore", "Restore plugin commits", "", "nvim", "--headless", "+Lazy! restore", "+qa")},
		Affects: []ops.ResourceID{resourcePackages}, Timeout: 20 * time.Minute,
	}, nil
}

func planTPMRepair(req ops.Request) (ops.Plan, error) {
	in, err := requestAs[tpmRepairRequest](req, actionTPMRepair)
	if err != nil {
		return ops.Plan{}, err
	}
	if in.Repo == "" {
		return ops.Plan{}, fmt.Errorf("no checkout found — TPM repair requires install.sh")
	}
	return ops.Plan{
		Title: "Install or repair TPM", Confirm: "Run install.sh to install or repair TPM?",
		Steps:   []ops.Step{processStep("tpm", "Install or repair TPM", in.Repo, filepath.Join(in.Repo, "install.sh"))},
		Affects: []ops.ResourceID{resourceDoctor, resourceConfig}, Timeout: 10 * time.Minute,
	}, nil
}

func planDoctorRepair(req ops.Request) (ops.Plan, error) {
	in, err := requestAs[doctorRepairRequest](req, actionDoctorRepair)
	if err != nil {
		return ops.Plan{}, err
	}
	if len(in.Missing) == 0 {
		return ops.Plan{}, fmt.Errorf("doctor found nothing repairable")
	}
	var dirs, pkgs []string
	for _, name := range in.Missing {
		if name == "oh-my-zsh" || name == "tpm" {
			dirs = append(dirs, name)
		} else {
			pkgs = append(pkgs, name)
		}
	}
	if in.GOOS == "" {
		in.GOOS = runtime.GOOS
	}
	if in.GOOS != "darwin" || len(dirs) > 0 {
		if in.Repo == "" {
			return ops.Plan{}, fmt.Errorf("no checkout found — repair requires the repository installer")
		}
		if in.GOOS == "darwin" && len(dirs) == 1 && dirs[0] == "tpm" && len(pkgs) == 0 {
			return planTPMRepair(tpmRepairRequest{Repo: in.Repo})
		}
		return planDeps(depsRequest{Repo: in.Repo})
	}

	formulas, seen := make([]string, 0, len(pkgs)), map[string]bool{}
	for _, name := range pkgs {
		formula := brewFormula(name)
		if !seen[formula] {
			seen[formula] = true
			formulas = append(formulas, formula)
		}
	}
	confirm := fmt.Sprintf("Install %d package(s) with Homebrew: %s?", len(formulas), strings.Join(formulas, " "))
	return ops.Plan{
		Title: "Install missing tools", Confirm: confirm,
		Steps:   []ops.Step{processStep("brew", "Install missing Homebrew formulae", "", append([]string{"brew", "install"}, formulas...)...)},
		Affects: []ops.ResourceID{resourceDoctor, resourcePackages}, Timeout: 15 * time.Minute,
	}, nil
}

func planService(req ops.Request) (ops.Plan, error) {
	in, err := requestAs[serviceRequest](req, actionService)
	if err != nil {
		return ops.Plan{}, err
	}
	if in.Verb != "start" && in.Verb != "stop" && in.Verb != "restart" {
		return ops.Plan{}, fmt.Errorf("unsupported service action %q", in.Verb)
	}
	s := in.Service
	title := verbTitle(in.Verb) + " " + s.Name
	plan := ops.Plan{Title: title, Target: s.ID, Affects: []ops.ResourceID{resourceServices}, Timeout: 30 * time.Second}
	switch s.Source {
	case srcLaunchd:
		plist := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", s.ID+".plist")
		gui, target := fmt.Sprintf("gui/%d", os.Getuid()), fmt.Sprintf("gui/%d/%s", os.Getuid(), s.ID)
		plan.Confirm = confirmSentence(s, in.Verb, "launchd")
		switch in.Verb {
		case "start":
			plan.Steps = []ops.Step{processStep("start", "Bootstrap launchd agent", "", "launchctl", "bootstrap", gui, plist)}
		case "stop":
			plan.Steps = []ops.Step{processStep("stop", "Boot out launchd agent", "", "launchctl", "bootout", target)}
		case "restart":
			plan.Steps = []ops.Step{
				{ID: "bootout", Title: "Boot out launchd agent", Exec: providers.Func{Label: "launchctl bootout " + target, Do: func(ctx context.Context, streams ops.IO) error {
					// An unloaded label is already in the desired intermediate
					// state. Preserve the diagnostic, but do not gate bootstrap.
					if err := providers.Command("", "launchctl", "bootout", target).Run(ctx, streams); err != nil {
						if streams.Stdout != nil {
							fmt.Fprintln(streams.Stdout, "launchd label was not loaded; continuing to bootstrap")
						}
					}
					return nil
				}}},
				{ID: "wait", Title: "Wait for launchd teardown", Exec: providers.Func{Label: "wait for launchd label to disappear", Do: func(ctx context.Context, streams ops.IO) error {
					return waitForLaunchdTeardown(ctx, target, streams)
				}}},
				processStep("bootstrap", "Bootstrap launchd agent", "", "launchctl", "bootstrap", gui, plist),
			}
		}
	case srcSystemd:
		if !s.UserUnit {
			return ops.Plan{}, fmt.Errorf("system-level systemd units are not changed without an explicit sudo flow")
		}
		plan.Confirm = confirmSentence(s, in.Verb, "systemctl --user")
		plan.Steps = []ops.Step{processStep(in.Verb, title, "", "systemctl", "--user", in.Verb, s.ID)}
	case srcDocker:
		plan.Confirm = confirmSentence(s, in.Verb, "docker")
		plan.Steps = []ops.Step{processStep(in.Verb, title, "", "docker", in.Verb, s.ID)}
	default:
		return ops.Plan{}, fmt.Errorf("unsupported service source")
	}
	return plan, nil
}

func waitForLaunchdTeardown(ctx context.Context, target string, streams ops.IO) error {
	for i := 0; i < 50; i++ {
		if err := exec.CommandContext(ctx, "launchctl", "print", target).Run(); err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if streams.Stderr != nil {
		fmt.Fprintf(streams.Stderr, "launchd label %s did not disappear\n", target)
	}
	return fmt.Errorf("launchd teardown timed out")
}

func planPackage(req ops.Request) (ops.Plan, error) {
	in, err := requestAs[packageRequest](req, actionPackage)
	if err != nil {
		return ops.Plan{}, err
	}
	p := in.Package
	target := p.Latest
	if target == "" {
		target = "latest"
	}
	var argv []string
	var confirm string
	switch p.Manager {
	case pmBrew:
		argv, confirm = []string{"brew", "upgrade", p.Name}, fmt.Sprintf("Upgrade %s (%s → %s) via Homebrew?", p.Name, p.Version, target)
	case pmPnpm:
		argv, confirm = []string{"pnpm", "add", "-g", p.Name + "@latest"}, fmt.Sprintf("Upgrade %s (%s → %s) via pnpm?", p.Name, p.Version, target)
	case pmNpm:
		argv, confirm = []string{"npm", "update", "-g", p.Name}, fmt.Sprintf("Upgrade %s (%s → %s) via npm?", p.Name, p.Version, target)
	case pmUvTool:
		argv, confirm = []string{"uv", "tool", "upgrade", p.Name}, fmt.Sprintf("Upgrade %s via uv tool?", p.Name)
	case pmPip:
		argv, confirm = []string{"pip3", "install", "--user", "--upgrade", p.Name}, fmt.Sprintf("Upgrade %s (%s → %s) via pip?", p.Name, p.Version, target)
	case pmInstaller:
		command, ok := installerCmdFor(p.Name)
		if !ok {
			return ops.Plan{}, fmt.Errorf("no known installer for %s", p.Name)
		}
		argv, confirm = []string{"sh", "-c", command}, fmt.Sprintf("Upgrade %s by re-running its upstream installer?", p.Name)
	default:
		return ops.Plan{}, fmt.Errorf("no safe single-package upgrade for %s packages", p.Manager.String())
	}
	return ops.Plan{
		Title: "Upgrade " + p.Name, Target: p.Name, Confirm: confirm,
		Steps:   []ops.Step{processStep("upgrade", "Upgrade "+p.Name, "", argv...)},
		Affects: []ops.ResourceID{resourcePackages, resourceDoctor}, Timeout: 5 * time.Minute,
	}, nil
}

func planRemoteDoctor(req ops.Request) (ops.Plan, error) {
	in, err := requestAs[remoteDoctorRequest](req, actionRemoteDoctor)
	if err != nil {
		return ops.Plan{}, err
	}
	if err := validateSSHHost(in.Alias); err != nil {
		return ops.Plan{}, fmt.Errorf("remote doctor: %w", err)
	}
	return ops.Plan{
		Title: "Remote doctor: " + in.Alias, Target: in.Alias,
		Confirm: "Run doctor on " + in.Alias + " over SSH?",
		Steps: []ops.Step{{ID: "doctor", Title: "Run remote doctor", Exec: providers.SSHScript{
			Host: in.Alias, Timeout: 4, Label: "ssh " + in.Alias + " <dots doctor>",
			Script: remoteDoctorScript,
		}}},
		Affects: []ops.ResourceID{resourceMachines}, Timeout: 20 * time.Second,
	}, nil
}

const remoteDoctorScript = `set -eu
export PATH="$HOME/.local/bin:$PATH"
exec dots doctor
`

func validateSSHHost(host string) error {
	if host == "" {
		return fmt.Errorf("a machine alias is required")
	}
	if strings.HasPrefix(host, "-") || strings.ContainsAny(host, " \t\r\n") {
		return fmt.Errorf("unsafe SSH host %q", host)
	}
	return nil
}

func nonemptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(value), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
