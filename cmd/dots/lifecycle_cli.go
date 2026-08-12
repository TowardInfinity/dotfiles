package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

type statusReport struct {
	Host           string              `json:"host"`
	OS             string              `json:"os"`
	Arch           string              `json:"arch"`
	Version        string              `json:"version"`
	BinarySource   string              `json:"binary_source,omitempty"`
	KeyMarker      string              `json:"key_marker,omitempty"`
	Repo           string              `json:"repo,omitempty"`
	Revision       string              `json:"revision,omitempty"`
	Branch         string              `json:"branch,omitempty"`
	Detached       bool                `json:"detached"`
	Dirty          int                 `json:"dirty"`
	Ahead          int                 `json:"ahead"`
	ConfigOK       bool                `json:"config_ok"`
	ConfigProblems []string            `json:"config_problems,omitempty"`
	Warnings       []string            `json:"warnings,omitempty"`
	Latest         string              `json:"latest,omitempty"`
	ReleaseCurrent *bool               `json:"release_current,omitempty"`
	Fleet          []fleetStatusReport `json:"fleet,omitempty"`
}

type fleetStatusReport struct {
	Host         string `json:"host"`
	Revision     string `json:"revision,omitempty"`
	Version      string `json:"version,omitempty"`
	BinarySource string `json:"binary_source,omitempty"`
	KeyMarker    string `json:"key_marker,omitempty"`
	ConfigOK     bool   `json:"config_ok"`
	Error        string `json:"error,omitempty"`
}

func runStatusCLI(args []string) int {
	online, fleet, jsonOut := false, false, false
	for _, arg := range args {
		switch arg {
		case "--online":
			online = true
		case "--fleet":
			fleet = true
		case "--json":
			jsonOut = true
		case "-h", "--help":
			fmt.Print(`dots status — concise read-only state

  dots status            local checkout, config, and binary state
  dots status --online   also compare with the current signed release
  dots status --fleet    query configured SSH hosts (read-only)
  dots status --json     stable machine-readable output
`)
			return 0
		default:
			fmt.Fprintf(os.Stderr, "dots status: unknown option: %s\n", arg)
			return 2
		}
	}
	report := collectStatus(findRepo(), online)
	if fleet {
		report.Fleet = collectFleetStatus(sshHosts())
	}
	if jsonOut {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "dots status: encode JSON: %v\n", err)
			return 1
		}
	} else {
		printStatus(report)
	}
	if !report.ConfigOK {
		return 1
	}
	for _, remote := range report.Fleet {
		if remote.Error != "" || !remote.ConfigOK {
			return 1
		}
	}
	return 0
}

func collectStatus(repo string, online bool) statusReport {
	host, _ := os.Hostname()
	report := statusReport{Host: host, OS: runtime.GOOS, Arch: runtime.GOARCH, Version: version, BinarySource: distSource(), Repo: repo, ConfigOK: true}
	if repo != "" {
		if out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output(); err == nil {
			report.Revision = strings.TrimSpace(string(out))
		}
		if branch, ok := currentBranch(repo); ok {
			report.Branch = branch
		} else {
			report.Detached = true
		}
		state := readRepoState(repo)
		report.Dirty, report.Ahead = state.dirty, state.unpushed
		cache := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "dots")
		if os.Getenv("XDG_CACHE_HOME") == "" {
			if home, err := os.UserHomeDir(); err == nil {
				cache = filepath.Join(home, ".cache", "dots")
			}
		}
		if marker, err := os.ReadFile(filepath.Join(cache, "dots-"+version+".sig-ok")); err == nil {
			report.KeyMarker = strings.TrimSpace(string(marker))
		}
	}
	for _, check := range configChecks(repo) {
		detail := check.name + ": " + check.path
		switch check.state {
		case checkBad:
			report.ConfigOK = false
			report.ConfigProblems = append(report.ConfigProblems, detail)
		case checkWarn:
			report.Warnings = append(report.Warnings, detail)
		}
	}
	if online {
		if latest, err := latestReleaseTag(); err == nil {
			report.Latest = latest
			current := report.Version == latest
			report.ReleaseCurrent = &current
		} else {
			report.Warnings = append(report.Warnings, "release: "+err.Error())
		}
	}
	return report
}

func collectFleetStatus(hosts []string) []fleetStatusReport {
	results := make([]fleetStatusReport, len(hosts))
	group, ctx := errgroup.WithContext(context.Background())
	group.SetLimit(4)
	for i := range hosts {
		i := i
		group.Go(func() error {
			results[i] = collectFleetHost(ctx, hosts[i])
			return nil
		})
	}
	_ = group.Wait()
	return results
}

func collectFleetHost(parent context.Context, host string) fleetStatusReport {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=4", host, remoteDots("status", "--json"))
	out, err := cmd.CombinedOutput()
	item := fleetStatusReport{Host: host}
	if err != nil {
		item.Error = strings.TrimSpace(string(out))
		if item.Error == "" {
			item.Error = err.Error()
		}
	} else {
		var remote statusReport
		if err := json.Unmarshal(out, &remote); err != nil {
			item.Error = "invalid status response: " + err.Error()
		} else {
			item.Revision, item.Version, item.BinarySource, item.KeyMarker, item.ConfigOK = remote.Revision, remote.Version, remote.BinarySource, remote.KeyMarker, remote.ConfigOK
		}
	}
	return item
}

func printStatus(report statusReport) {
	fmt.Printf("%s  %s/%s  dots %s", report.Host, report.OS, report.Arch, report.Version)
	if report.BinarySource != "" {
		fmt.Printf(" (%s)", report.BinarySource)
	}
	fmt.Println()
	if report.Repo != "" {
		fmt.Printf("repo    %s  %s  %s", short(report.Repo), shortSHA(report.Revision), report.Branch)
		if report.Detached {
			fmt.Print("  detached")
		}
		if report.Dirty > 0 || report.Ahead > 0 {
			fmt.Printf("  dirty=%d ahead=%d", report.Dirty, report.Ahead)
		}
		fmt.Println()
	}
	if report.ConfigOK {
		fmt.Println("config  ok")
	} else {
		fmt.Printf("config  %s\n", strings.Join(report.ConfigProblems, "; "))
	}
	for _, warning := range report.Warnings {
		fmt.Printf("warning %s\n", warning)
	}
	if report.Latest != "" {
		fmt.Printf("release %s", report.Latest)
		if report.ReleaseCurrent != nil && *report.ReleaseCurrent {
			fmt.Print(" (current)")
		}
		fmt.Println()
	}
	for _, host := range report.Fleet {
		if host.Error != "" {
			fmt.Printf("fleet   %-12s error: %s\n", host.Host, host.Error)
		} else {
			fmt.Printf("fleet   %-12s %s  %s  config=%t\n", host.Host, shortSHA(host.Revision), host.Version, host.ConfigOK)
		}
	}
}

func runApplyCLI(args []string) int {
	dry, yes := false, false
	for _, arg := range args {
		switch arg {
		case "--dry-run", "--dry", "-n":
			dry = true
		case "-y", "--yes":
			yes = true
		case "-h", "--help":
			fmt.Print("dots apply [--dry-run] [-y] — relink and merge from this checkout; no network\n")
			return 0
		default:
			fmt.Fprintf(os.Stderr, "dots apply: unknown option: %s\n", arg)
			return 2
		}
	}
	repo, ok := needRepo("apply")
	if !ok {
		return 1
	}
	plan, err := buildOperation(applyRequest{Repo: repo})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots apply: %v\n", err)
		return 1
	}
	return runPlanCLI(plan, cliPlanOptions{DryRun: dry, AssumeYes: yes, AskConfirm: true})
}

func runDepsCLI(args []string) int {
	dry, yes := false, false
	for _, arg := range args {
		switch arg {
		case "--dry-run", "--dry", "-n":
			dry = true
		case "-y", "--yes":
			yes = true
		case "-h", "--help":
			fmt.Print("dots deps [--dry-run] [-y] — install declared dependencies and third-party tools\n")
			return 0
		default:
			fmt.Fprintf(os.Stderr, "dots deps: unknown option: %s\n", arg)
			return 2
		}
	}
	repo, ok := needRepo("install dependencies")
	if !ok {
		return 1
	}
	plan, err := buildOperation(depsRequest{Repo: repo})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots deps: %v\n", err)
		return 1
	}
	return runPlanCLI(plan, cliPlanOptions{DryRun: dry, AssumeYes: yes, AskConfirm: true})
}

func runPublishCLI(args []string) int {
	request := publishRequest{}
	dry, yes := false, false
	literalPaths := false
	for i := 0; i < len(args); i++ {
		if literalPaths {
			request.Paths = append(request.Paths, args[i])
			continue
		}
		switch args[i] {
		case "--":
			literalPaths = true
		case "-m", "--message":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "dots publish: -m needs a message")
				return 2
			}
			i++
			request.Message = args[i]
		case "--all":
			request.All = true
		case "--dry-run", "-n":
			dry = true
		case "-y", "--yes":
			yes = true
		case "--no-verify":
			request.NoVerify = true
		case "-h", "--help":
			fmt.Print(`dots publish [paths…] -m <message> [-y] [--dry-run]

Validate, stage only the explicit selection, commit, and push. Never SSH or
roll out. Use --all deliberately to select every changed path.
`)
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "dots publish: unknown option: %s\n", args[i])
				return 2
			}
			request.Paths = append(request.Paths, args[i])
		}
	}
	repo, ok := needRepo("publish")
	if !ok {
		return 1
	}
	request.Repo = repo
	if !request.All && len(request.Paths) == 0 {
		if !stdinIsTerminal() {
			fmt.Fprintln(os.Stderr, "dots publish: select paths explicitly or use --all")
			return 2
		}
		paths, err := chooseChangedPaths(repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dots publish: %v\n", err)
			return 1
		}
		request.Paths = paths
	}
	if strings.TrimSpace(request.Message) == "" {
		if !stdinIsTerminal() {
			fmt.Fprintln(os.Stderr, "dots publish: -m <message> is required outside a terminal")
			return 2
		}
		fmt.Fprint(os.Stderr, "Commit message: ")
		message, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			fmt.Fprintf(os.Stderr, "dots publish: read commit message: %v\n", err)
			return 1
		}
		request.Message = message
		request.Message = strings.TrimSpace(request.Message)
	}
	plan, err := buildOperation(request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots publish: %v\n", err)
		return 1
	}
	code := runPlanCLI(plan, cliPlanOptions{DryRun: dry, AssumeYes: yes, AskConfirm: true})
	if code == 0 && !dry {
		fmt.Println("fleet unchanged — run `dots rollout` separately when this revision is released")
	}
	return code
}

func chooseChangedPaths(repo string) ([]string, error) {
	paths, err := changedPaths(context.Background(), repo)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("nothing has changed")
	}
	staged, err := gitNameList(context.Background(), repo, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return nil, fmt.Errorf("read staged paths: %w", err)
	}
	selected := make(map[string]bool, len(staged))
	for _, path := range staged {
		selected[path] = true
	}
	fmt.Fprintln(os.Stderr, "Changed paths (already staged paths start selected):")
	for i, path := range paths {
		mark := " "
		if selected[path] {
			mark = "x"
		}
		fmt.Fprintf(os.Stderr, "  %d. [%s] %s\n", i+1, mark, path)
	}
	fmt.Fprint(os.Stderr, "Add numbers (comma-separated), 'all', or Enter to keep staged: ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.TrimSpace(answer)
	if strings.EqualFold(answer, "all") {
		return paths, nil
	}
	if answer != "" {
		seen := map[int]bool{}
		for _, field := range strings.Split(answer, ",") {
			var n int
			if _, err := fmt.Sscan(strings.TrimSpace(field), &n); err != nil || n < 1 || n > len(paths) {
				return nil, fmt.Errorf("invalid selection %q", field)
			}
			if !seen[n] {
				seen[n] = true
				selected[paths[n-1]] = true
			}
		}
	}
	chosen := make([]string, 0, len(selected))
	for _, path := range paths {
		if selected[path] {
			chosen = append(chosen, path)
		}
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("no paths selected")
	}
	return chosen, nil
}

func runRolloutCLI(args []string) int {
	var hosts []string
	all, dry, yes := false, false, false
	revision := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--all":
			all = true
		case "--revision":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "dots rollout: --revision needs a tag or commit")
				return 2
			}
			i++
			revision = args[i]
		case "--dry-run", "-n":
			dry = true
		case "-y", "--yes":
			yes = true
		case "-h", "--help":
			fmt.Print("dots rollout [hosts…|--all] [--revision tag|sha] [--dry-run] [-y]\n")
			return 0
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "dots rollout: unknown option: %s\n", args[i])
				return 2
			}
			hosts = append(hosts, args[i])
		}
	}
	if all && len(hosts) > 0 {
		fmt.Fprintln(os.Stderr, "dots rollout: name hosts or use --all, not both")
		return 2
	}
	if all {
		hosts = sshHosts()
	}
	if len(hosts) == 0 {
		if !stdinIsTerminal() {
			fmt.Fprintln(os.Stderr, "dots rollout: name at least one host or use --all")
			return 2
		}
		var err error
		hosts, err = chooseHosts(sshHosts())
		if err != nil {
			fmt.Fprintf(os.Stderr, "dots rollout: %v\n", err)
			return 1
		}
	}
	repo, ok := needRepo("roll out")
	if !ok {
		return 1
	}
	sha, tag, err := resolvePublishedRevision(repo, revision)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots rollout: %v\n", err)
		return 1
	}
	latest, err := latestReleaseTag()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots rollout: cannot resolve Latest: %v\n", err)
		return 1
	}
	if err := validateRolloutLatest(tag, latest); err != nil {
		fmt.Fprintf(os.Stderr, "dots rollout: %v\n", err)
		return 1
	}
	plan, err := buildOperation(rolloutRequest{Repo: repo, Hosts: hosts, Revision: sha, Version: tag})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots rollout: %v\n", err)
		return 1
	}
	return runPlanCLI(plan, cliPlanOptions{DryRun: dry, AssumeYes: yes, AskConfirm: true})
}

func validateRolloutLatest(tag, latest string) error {
	if latest != tag {
		return fmt.Errorf("%s is signed/tagged but Latest is %s; refusing a resolver/version mismatch", tag, latest)
	}
	return nil
}

func chooseHosts(hosts []string) ([]string, error) {
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no concrete hosts found in ~/.ssh/config")
	}
	fmt.Fprintln(os.Stderr, "Machines:")
	for i, host := range hosts {
		fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, host)
	}
	fmt.Fprint(os.Stderr, "Select numbers (comma-separated), or 'all': ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.TrimSpace(answer)
	if strings.EqualFold(answer, "all") {
		return hosts, nil
	}
	seen := map[int]bool{}
	selected := make([]string, 0, len(hosts))
	for _, field := range strings.Split(answer, ",") {
		var n int
		if _, err := fmt.Sscan(strings.TrimSpace(field), &n); err != nil || n < 1 || n > len(hosts) {
			return nil, fmt.Errorf("invalid selection %q", field)
		}
		if !seen[n] {
			seen[n] = true
			selected = append(selected, hosts[n-1])
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no machines selected")
	}
	return selected, nil
}

func resolvePublishedRevision(repo, requested string) (string, string, error) {
	out, err := exec.Command("git", "-C", repo, "ls-remote", "--heads", "--tags", "origin").Output()
	if err != nil {
		return "", "", fmt.Errorf("query origin: %w", err)
	}
	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			refs[fields[1]] = fields[0]
		}
	}
	sha := ""
	switch {
	case requested == "":
		sha = refs["refs/heads/main"]
	case strings.HasPrefix(requested, "v"):
		sha = refs["refs/tags/"+requested+"^{}"]
		if sha == "" {
			sha = refs["refs/tags/"+requested]
		}
	default:
		for _, candidate := range refs {
			if candidate == requested {
				sha = requested
				break
			}
		}
	}
	if sha == "" {
		return "", "", fmt.Errorf("revision %q is not a commit advertised by origin", requested)
	}
	var tags []string
	for ref, candidate := range refs {
		if !strings.HasPrefix(ref, "refs/tags/v") {
			continue
		}
		name := strings.TrimPrefix(strings.TrimSuffix(ref, "^{}"), "refs/tags/")
		peeled := refs["refs/tags/"+name+"^{}"]
		if peeled != "" {
			candidate = peeled
		}
		if candidate == sha && semverTag(name) {
			tags = append(tags, name)
		}
	}
	if len(tags) == 0 {
		return "", "", fmt.Errorf("revision %s has no vMAJOR.MINOR.PATCH release tag", shortSHA(sha))
	}
	sort.Slice(tags, func(i, j int) bool { return versionIsOlder(tags[i], tags[j]) })
	return sha, tags[len(tags)-1], nil
}

func semverTag(tag string) bool {
	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(parts) != 3 || !strings.HasPrefix(tag, "v") {
		return false
	}
	for _, part := range parts {
		var n int
		if _, err := fmt.Sscan(part, &n); err != nil || fmt.Sprint(n) != part {
			return false
		}
	}
	return true
}

func versionIsOlder(a, b string) bool {
	var av, bv [3]int
	_, _ = fmt.Sscanf(strings.TrimPrefix(a, "v"), "%d.%d.%d", &av[0], &av[1], &av[2])
	_, _ = fmt.Sscanf(strings.TrimPrefix(b, "v"), "%d.%d.%d", &bv[0], &bv[1], &bv[2])
	for i := range av {
		if av[i] != bv[i] {
			return av[i] < bv[i]
		}
	}
	return false
}
