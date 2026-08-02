package main

// Dynamic service discovery. Replaces the old hardcoded pair (a TCP dial to
// open-webui, `ollama ps`) with a set of backends — launchd, systemd, docker
// — that each contribute what they can see, skipping themselves silently
// when their binary/path is absent. Data layer only: no rendering, no Bubble
// Tea views. That lives elsewhere; this file only produces services and the
// commands to act on them.

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// ── data shapes ───────────────────────────────────────────────

type svcSource int

const (
	srcLaunchd svcSource = iota
	srcSystemd
	srcDocker
)

type service struct {
	ID       string // launchd label, systemd unit, or container name
	Name     string // friendly display name
	Source   svcSource
	Running  bool
	Healthy  bool   // only meaningful when Port/URL known AND probed
	Probed   bool   // false = health unknown, do not claim healthy or not
	Port     int    // 0 = unknown
	Detail   string // short, e.g. "pid 4213" or "exited (0)"
	UserUnit bool   // systemd --user, or a user LaunchAgent
}

// ── messages ──────────────────────────────────────────────────

type servicesFoundMsg struct {
	services []service
	sources  []string // backends that actually ran, e.g. "launchd", "docker"
	err      string
}

type servicesProbedMsg struct {
	ports map[int]bool
}

// ── discovery ─────────────────────────────────────────────────

// discoverServices runs every applicable backend concurrently, bounded by a
// single timeout for the whole pass. Each backend is responsible for its own
// errors: a backend that fails or is unavailable simply contributes nothing,
// it never poisons the rest of the pass.
func discoverServices() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		var (
			mu       sync.Mutex
			services []service
			sources  []string
			wg       sync.WaitGroup
		)

		contribute := func(svcs []service, source string) {
			mu.Lock()
			defer mu.Unlock()
			services = append(services, svcs...)
			sources = append(sources, source)
		}

		if runtime.GOOS == "darwin" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if svcs, ran := discoverLaunchd(ctx); ran {
					contribute(svcs, "launchd")
				}
			}()
		}

		if runtime.GOOS == "linux" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if svcs, ran := discoverSystemd(ctx); ran {
					contribute(svcs, "systemd")
				}
			}()
		}

		if p, ok := have("docker"); ok {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if svcs, ran := discoverDocker(ctx, p); ran {
					contribute(svcs, "docker")
				}
			}()
		}

		wg.Wait()

		applyServiceOverrides(&services)

		sort.SliceStable(services, func(i, j int) bool {
			if services[i].Running != services[j].Running {
				return services[i].Running // running first
			}
			return services[i].Name < services[j].Name
		})
		sort.Strings(sources)

		errStr := ""
		if ctx.Err() != nil {
			errStr = ctx.Err().Error()
		}

		return servicesFoundMsg{services: services, sources: sources, err: errStr}
	}
}

// probeServices dials every distinct known port concurrently, bounded by a
// single timeout for the whole pass. It only reports which ports answered;
// deciding what that means for a given service's Probed/Healthy fields is
// the caller's job, since more than one service can share a port entry.
func probeServices(svcs []service) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		var (
			mu    sync.Mutex
			wg    sync.WaitGroup
			ports = map[int]bool{}
			seen  = map[int]bool{}
		)

		var d net.Dialer
		d.Timeout = 700 * time.Millisecond

		for _, s := range svcs {
			if s.Port == 0 || seen[s.Port] {
				continue
			}
			seen[s.Port] = true
			port := s.Port
			wg.Add(1)
			go func() {
				defer wg.Done()
				conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
				ok := err == nil
				if ok {
					conn.Close()
				}
				mu.Lock()
				ports[port] = ok
				mu.Unlock()
			}()
		}
		wg.Wait()

		return servicesProbedMsg{ports: ports}
	}
}

// ── backend: launchd (macOS user agents) ─────────────────────

type launchdEntry struct {
	pid    string
	status string
}

// discoverLaunchd lists ~/Library/LaunchAgents/*.plist, deriving the label
// from each filename stem, and cross-references `launchctl list` for
// running state and pid. Apple's own agents (com.apple.*) are excluded —
// a typical Mac registers dozens of them and none are ours to manage; the
// UI's search box handles narrowing everything else.
func discoverLaunchd(ctx context.Context) ([]service, bool) {
	home := os.Getenv("HOME")
	dir := filepath.Join(home, "Library", "LaunchAgents")
	matches, err := filepath.Glob(filepath.Join(dir, "*.plist"))
	if err != nil {
		return nil, false
	}

	running := map[string]launchdEntry{}
	if p, ok := have("launchctl"); ok {
		if out, err := exec.CommandContext(ctx, p, "list").Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 3 || fields[0] == "PID" {
					continue // header row, or a malformed line
				}
				label := strings.Join(fields[2:], " ")
				running[label] = launchdEntry{pid: fields[0], status: fields[1]}
			}
		}
	}

	var svcs []service
	for _, m := range matches {
		label := strings.TrimSuffix(filepath.Base(m), ".plist")
		if strings.HasPrefix(label, "com.apple.") {
			continue
		}
		s := service{
			ID:       label,
			Name:     launchdName(label),
			Source:   srcLaunchd,
			UserUnit: true,
		}
		if e, ok := running[label]; ok {
			switch {
			case e.pid != "-":
				s.Running = true
				s.Detail = "pid " + e.pid
			case e.status != "" && e.status != "0":
				s.Detail = "exited (" + e.status + ")"
			default:
				s.Detail = "not running"
			}
		} else {
			s.Detail = "not loaded"
		}
		svcs = append(svcs, s)
	}
	return svcs, true
}

// ── backend: systemd (Linux user + system units) ─────────────

// discoverSystemd asks both the user and system managers for their service
// units. Either query failing (or systemctl being absent) just means that
// half of the picture is missing, not that the backend didn't run.
func discoverSystemd(ctx context.Context) ([]service, bool) {
	p, ok := have("systemctl")
	if !ok {
		return nil, false
	}

	ran := false
	var svcs []service

	if out, err := exec.CommandContext(ctx, p, "--user", "list-units",
		"--type=service", "--all", "--no-legend", "--plain").Output(); err == nil {
		ran = true
		svcs = append(svcs, parseSystemdUnits(string(out), true)...)
	}
	if out, err := exec.CommandContext(ctx, p, "list-units",
		"--type=service", "--all", "--no-legend", "--plain").Output(); err == nil {
		ran = true
		svcs = append(svcs, parseSystemdUnits(string(out), false)...)
	}
	return svcs, ran
}

// parseSystemdUnits reads `--no-legend --plain` output: whitespace-separated
// UNIT LOAD ACTIVE SUB, then a free-text description for the rest of the
// line. Only the first four columns are used here.
func parseSystemdUnits(out string, userUnit bool) []service {
	var svcs []service
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		unit, active, sub := fields[0], fields[2], fields[3]
		svcs = append(svcs, service{
			ID:       unit,
			Name:     prettifyIdentifier(strings.TrimSuffix(unit, ".service")),
			Source:   srcSystemd,
			UserUnit: userUnit,
			Running:  active == "active",
			Detail:   sub,
		})
	}
	return svcs
}

// ── backend: docker ────────────────────────────────────────────

// discoverDocker lists every container, running or not (`-a`), so a stopped
// service still shows up with an offer to start it.
func discoverDocker(ctx context.Context, path string) ([]service, bool) {
	out, err := exec.CommandContext(ctx, path, "ps", "-a",
		"--format", "{{.Names}}\t{{.State}}\t{{.Ports}}").Output()
	if err != nil {
		return nil, false
	}

	var svcs []service
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		// A container can have multiple comma-separated aliases; use the first.
		name := fields[0]
		if i := strings.Index(name, ","); i >= 0 {
			name = name[:i]
		}
		state := fields[1]
		var portsCol string
		if len(fields) >= 3 {
			portsCol = fields[2]
		}
		svcs = append(svcs, service{
			ID:      name,
			Name:    name,
			Source:  srcDocker,
			Running: state == "running",
			Detail:  state,
			Port:    parseDockerPort(portsCol),
		})
	}
	return svcs, true
}

// parseDockerPort pulls the first host port out of a `docker ps` Ports
// column, e.g. "0.0.0.0:8080->80/tcp, :::8080->80/tcp" -> 8080. Entries with
// no "->" are container-only ports with nothing listening on the host.
func parseDockerPort(ports string) int {
	for _, part := range strings.Split(ports, ",") {
		part = strings.TrimSpace(part)
		i := strings.Index(part, "->")
		if i < 0 {
			continue
		}
		hostPart := part[:i]
		if j := strings.LastIndex(hostPart, ":"); j >= 0 {
			hostPart = hostPart[j+1:]
		}
		if n, err := strconv.Atoi(hostPart); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// ── actions ───────────────────────────────────────────────────

// serviceAction builds the actionSpec for start/stop/restart, per source.
// It always sets Confirm on success, and returns false for anything it
// cannot safely do — notably a system-level (non --user) systemd unit,
// which would need sudo, and offering an action that will just fail is
// worse than not offering it at all.
func serviceAction(s service, verb string) (actionSpec, bool) {
	switch verb {
	case "start", "stop", "restart":
	default:
		return actionSpec{}, false
	}

	switch s.Source {
	case srcLaunchd:
		return launchdActionSpec(s, verb)
	case srcSystemd:
		return systemdActionSpec(s, verb)
	case srcDocker:
		return dockerActionSpec(s, verb)
	default:
		return actionSpec{}, false
	}
}

func launchdActionSpec(s service, verb string) (actionSpec, bool) {
	home := os.Getenv("HOME")
	plist := filepath.Join(home, "Library", "LaunchAgents", s.ID+".plist")
	gui := fmt.Sprintf("gui/%d", os.Getuid())
	target := gui + "/" + s.ID

	var argv []string
	switch verb {
	case "start":
		argv = []string{"launchctl", "bootstrap", gui, plist}
	case "stop":
		argv = []string{"launchctl", "bootout", target}
	case "restart":
		// bootout then bootstrap, one shell so the second step always runs
		// even when the job wasn't loaded to begin with (`;`, not `&&`).
		argv = []string{"sh", "-c",
			"launchctl bootout " + shellQuote(target) + "; " +
				"launchctl bootstrap " + shellQuote(gui) + " " + shellQuote(plist)}
	}
	return actionSpec{
		Title:   verbTitle(verb) + " " + s.Name,
		Argv:    argv,
		Confirm: confirmSentence(s, verb, "launchd"),
		Timeout: 30 * time.Second,
	}, true
}

func systemdActionSpec(s service, verb string) (actionSpec, bool) {
	if !s.UserUnit {
		// A system unit needs sudo; offering it here would just fail loudly.
		return actionSpec{}, false
	}
	return actionSpec{
		Title:   verbTitle(verb) + " " + s.Name,
		Argv:    []string{"systemctl", "--user", verb, s.ID},
		Confirm: confirmSentence(s, verb, "systemctl --user"),
		Timeout: 30 * time.Second,
	}, true
}

func dockerActionSpec(s service, verb string) (actionSpec, bool) {
	return actionSpec{
		Title:   verbTitle(verb) + " " + s.Name,
		Argv:    []string{"docker", verb, s.ID},
		Confirm: confirmSentence(s, verb, "docker"),
		Timeout: 30 * time.Second,
	}, true
}

func verbTitle(verb string) string {
	if verb == "" {
		return verb
	}
	r := []rune(verb)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func confirmSentence(s service, verb, how string) string {
	return fmt.Sprintf("%s %s via %s?", verbTitle(verb), s.Name, how)
}

// ── overrides: services.toml ──────────────────────────────────

// serviceOverride is one [<id>] section of the optional overrides file.
type serviceOverride struct {
	Name string
	Port int
}

// applyServiceOverrides decorates discovered entries by ID (name/port) and
// appends entries discovery cannot see at all — a raw port to watch with no
// backing launchd/systemd/docker entity. Those synthetic entries are given
// Source srcDocker purely as a harmless default: if someone ever tries to
// act on one, `docker start/stop <id>` fails cleanly with "no such
// container" instead of doing anything surprising.
func applyServiceOverrides(services *[]service) {
	overrides := loadServiceOverrides()
	if len(overrides) == 0 {
		return
	}

	// Key by source AND id. Two backends can produce the same id — a launchd
	// label and a docker container both called "redis" — and a plain id map
	// keeps only whichever the concurrent discovery appended last, so an
	// override could silently land on the wrong unit, differently between runs.
	// A bare id is still accepted so simple files keep working.
	byID := map[string]int{}
	for i, s := range *services {
		byID[svcSourceName(s.Source)+":"+s.ID] = i
		if _, clash := byID[s.ID]; !clash {
			byID[s.ID] = i
		}
	}

	// Deterministic order for anything appended below, independent of map
	// iteration order (the final sort in discoverServices reorders anyway,
	// but this keeps behavior reproducible on its own).
	ids := make([]string, 0, len(overrides))
	for id := range overrides {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		ov := overrides[id]
		if idx, ok := byID[id]; ok {
			if ov.Name != "" {
				(*services)[idx].Name = ov.Name
			}
			if ov.Port != 0 {
				(*services)[idx].Port = ov.Port
			}
			continue
		}
		name := ov.Name
		if name == "" {
			name = id
		}
		*services = append(*services, service{
			ID:     id,
			Name:   name,
			Source: srcDocker,
			Port:   ov.Port,
		})
	}
}

// loadServiceOverrides parses <repo>/services.toml if present. Only this
// trivial shape is supported, deliberately, to avoid a TOML dependency:
//
//	[<id>]
//	name = "Friendly Name"
//	port = 11435
//
// Anything else in the file (other keys, comments, blank lines) is ignored.
// A missing file changes nothing — that's the common case and must stay
// silent.
func loadServiceOverrides() map[string]serviceOverride {
	repo := findRepo()
	if repo == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(repo, "services.toml"))
	if err != nil {
		return nil
	}

	overrides := map[string]serviceOverride{}
	cur := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = strings.TrimSpace(line[1 : len(line)-1])
			if _, ok := overrides[cur]; !ok {
				overrides[cur] = serviceOverride{}
			}
			continue
		}
		if cur == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.Trim(strings.TrimSpace(line[eq+1:]), `"`)
		ov := overrides[cur]
		switch key {
		case "name":
			ov.Name = val
		case "port":
			if n, err := strconv.Atoi(val); err == nil {
				ov.Port = n
			}
		}
		overrides[cur] = ov
	}
	return overrides
}

// ── small string helpers ──────────────────────────────────────

// lastSegment returns the part of s after the last occurrence of sep, or
// all of s if sep does not appear (or is empty).
func lastSegment(s, sep string) string {
	if sep == "" {
		return s
	}
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[i+len(sep):]
	}
	return s
}

// prettifyIdentifier turns a launchd label's last segment or a systemd unit
// name into a display-friendly title, e.g. "open-webui" -> "Open Webui".
func prettifyIdentifier(id string) string {
	name := strings.ReplaceAll(id, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	words := strings.Fields(name)
	for i, w := range words {
		r := []rune(w)
		if len(r) == 0 {
			continue
		}
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// launchdName keeps enough of a reverse-DNS label to tell entries apart.
//
// Taking only the last segment turned com.google.keystone.agent into "Agent"
// and com.jetbrains.toolbox into "Toolbox" — which loses the vendor, and on a
// machine with several agents produces names that collide and mean nothing.
// Dropping just the TLD segment keeps it short without losing identity.
func launchdName(label string) string {
	for _, tld := range []string{"com.", "org.", "io.", "net.", "dev.", "app."} {
		if rest, ok := strings.CutPrefix(label, tld); ok && rest != "" {
			return rest
		}
	}
	return label
}
