package main

// Overview: a single-glance summary of this machine, for the FIRST route of
// the shell. Deliberately a thin layer over data other sections already gather
// — repo state (dotfilesInfo), tool presence (checkNames/evalCheck), service
// counts (the shell's discovered []service) — plus a handful of
// small OS facts (hostname, OS/version, memory, disk, uptime) that nothing
// else in this program collects yet. Those go here, gathered by one bounded,
// partly-concurrent pass so a slow or absent command degrades a single field
// rather than the whole pane.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ── data shapes ───────────────────────────────────────────────

type overviewInfo struct {
	host string
	arch string // runtime.GOARCH

	// osName is e.g. "macOS 14.5" or a Linux distro's PRETTY_NAME, falling
	// back to a bare "macOS"/"Linux" when the cheap lookup fails.
	osName string

	profileLight bool

	// version mirrors main.go's version var; distSource says where THIS
	// binary came from, inferred from its own path rather than tracked
	// anywhere at build time.
	version    string
	distSource string // "release cache", "source build", or "" unknown

	toolsHave, toolsTotal int

	memKnown                      bool
	memFreeBytes, memTotalBytes   uint64
	diskKnown                     bool
	diskFreeBytes, diskTotalBytes uint64

	uptimeKnown bool
	uptime      time.Duration
}

// ── messages ──────────────────────────────────────────────────

type overviewInfoMsg struct{ info overviewInfo }

// ── commands ──────────────────────────────────────────────────

// fetchOverviewInfo gathers everything overviewInfo needs. Repo state,
// tool/service counts and the like are deliberately NOT duplicated here —
// they already live on manageModel and are read directly in the view. The
// whole pass is bounded by one timeout; the handful of external commands
// (sw_vers, sysctl, vm_stat) run concurrently against it so one slow call
// cannot serialize behind another.
func fetchOverviewInfo() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		info := overviewInfo{arch: runtime.GOARCH}

		if h, err := os.Hostname(); err == nil {
			info.host = h
		}
		info.profileLight = profileIsLight()
		info.version = version
		info.distSource = distSource()

		// Stat/PATH lookups only — no external processes, so no need to bound
		// or parallelize this against ctx.
		names := checkNames()
		info.toolsTotal = len(names)
		for _, n := range names {
			if evalCheck(n).state == checkOK {
				info.toolsHave++
			}
		}

		// Each goroutine below writes only its own, disjoint fields of info;
		// wg.Wait() is the only synchronization needed before the read in the
		// return statement, the same pattern fetchMachinesInfo uses.
		var wg sync.WaitGroup
		wg.Add(4)
		go func() {
			defer wg.Done()
			info.osName = osVersionString(ctx)
		}()
		go func() {
			defer wg.Done()
			if d, ok := uptimeInfo(ctx); ok {
				info.uptimeKnown, info.uptime = true, d
			}
		}()
		go func() {
			defer wg.Done()
			if free, total, ok := memInfo(ctx); ok {
				info.memKnown, info.memFreeBytes, info.memTotalBytes = true, free, total
			}
		}()
		go func() {
			defer wg.Done()
			if free, total, ok := diskInfo(); ok {
				info.diskKnown, info.diskFreeBytes, info.diskTotalBytes = true, free, total
			}
		}()
		wg.Wait()

		return overviewInfoMsg{info: info}
	}
}

// distSource infers where the running binary came from: bin/dots-resolve.sh
// either caches a downloaded release under $XDG_CACHE_HOME/dots (default
// ~/.cache/dots) or builds one from source into <repo>/bin/dots-bin. Neither
// path is recorded anywhere at runtime, so the executable's own path is the
// only signal available.
func distSource() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	switch {
	case strings.Contains(exe, "/.cache/dots/"):
		return "release cache"
	case strings.Contains(exe, filepath.Join("bin", "dots-bin")):
		return "source build"
	default:
		return ""
	}
}

// osVersionString adds a version to the OS name when that is cheap to get: a
// single `sw_vers` call on darwin, a plain file read on linux. Anything that
// fails just falls back to the bare OS name — this line is decoration, not
// load-bearing.
func osVersionString(ctx context.Context) string {
	switch runtime.GOOS {
	case "darwin":
		base := "macOS"
		p, ok := have("sw_vers")
		if !ok {
			return base
		}
		out, err := exec.CommandContext(ctx, p, "-productVersion").Output()
		if err != nil {
			return base
		}
		if v := strings.TrimSpace(string(out)); v != "" {
			return base + " " + v
		}
		return base
	case "linux":
		b, err := os.ReadFile("/etc/os-release")
		if err != nil {
			return "Linux"
		}
		for _, line := range strings.Split(string(b), "\n") {
			name, val, ok := strings.Cut(line, "=")
			if !ok || name != "PRETTY_NAME" {
				continue
			}
			if v := strings.Trim(strings.TrimSpace(val), `"`); v != "" {
				return v
			}
		}
		return "Linux"
	default:
		return runtime.GOOS
	}
}

// uptimeInfo is cheap on both platforms: a single file read on linux, one
// `sysctl` call on darwin. Anything with heavier machinery (parsing `uptime`
// output, watching for reboots) is exactly the kind of "skip if awkward" this
// field allows for.
func uptimeInfo(ctx context.Context) (time.Duration, bool) {
	switch runtime.GOOS {
	case "linux":
		b, err := os.ReadFile("/proc/uptime")
		if err != nil {
			return 0, false
		}
		fields := strings.Fields(string(b))
		if len(fields) == 0 {
			return 0, false
		}
		secs, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return 0, false
		}
		return time.Duration(secs * float64(time.Second)), true

	case "darwin":
		p, ok := have("sysctl")
		if !ok {
			return 0, false
		}
		out, err := exec.CommandContext(ctx, p, "-n", "kern.boottime").Output()
		if err != nil {
			return 0, false
		}
		// Format: "{ sec = 1690000000, usec = 0 } Wed ..."
		s := string(out)
		i := strings.Index(s, "sec = ")
		if i < 0 {
			return 0, false
		}
		rest := s[i+len("sec = "):]
		j := strings.IndexAny(rest, ",}")
		if j < 0 {
			return 0, false
		}
		sec, err := strconv.ParseInt(strings.TrimSpace(rest[:j]), 10, 64)
		if err != nil {
			return 0, false
		}
		return time.Since(time.Unix(sec, 0)), true

	default:
		return 0, false
	}
}

// memInfo reports free/total system memory in bytes. darwin has no single
// syscall for this — hw.memsize gives the total, and vm_stat's free page
// count (deliberately not the inactive/speculative pages some tools fold in)
// gives a conservative "free"; linux exposes both directly in /proc/meminfo.
func memInfo(ctx context.Context) (free, total uint64, ok bool) {
	switch runtime.GOOS {
	case "darwin":
		return memInfoDarwin(ctx)
	case "linux":
		return memInfoLinux()
	default:
		return 0, 0, false
	}
}

func memInfoDarwin(ctx context.Context) (free, total uint64, ok bool) {
	sysctlPath, hasSysctl := have("sysctl")
	vmStatPath, hasVMStat := have("vm_stat")
	if !hasSysctl || !hasVMStat {
		return 0, 0, false
	}

	out, err := exec.CommandContext(ctx, sysctlPath, "-n", "hw.memsize").Output()
	if err != nil {
		return 0, 0, false
	}
	total, err = strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, 0, false
	}

	vmOut, err := exec.CommandContext(ctx, vmStatPath).Output()
	if err != nil {
		return 0, 0, false
	}
	s := string(vmOut)
	pageSize, ok := vmStatPageSize(s)
	if !ok {
		pageSize = 4096 // vm_stat's long-standing default; better than nothing.
	}
	pagesFree, ok := vmStatField(s, "Pages free")
	if !ok {
		return 0, 0, false
	}
	return pagesFree * pageSize, total, true
}

// vmStatPageSize pulls the page size out of vm_stat's banner line: "Mach
// Virtual Memory Statistics: (page size of 4096 bytes)".
func vmStatPageSize(s string) (uint64, bool) {
	i := strings.Index(s, "page size of ")
	if i < 0 {
		return 0, false
	}
	rest := s[i+len("page size of "):]
	j := strings.Index(rest, " bytes")
	if j < 0 {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSpace(rest[:j]), 10, 64)
	return n, err == nil
}

// vmStatField reads a "Label:  N." line, e.g. "Pages free:  12345.".
func vmStatField(s, label string) (uint64, bool) {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, label)
		if !ok {
			continue
		}
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), ":"))
		rest = strings.TrimSuffix(rest, ".")
		if n, err := strconv.ParseUint(strings.TrimSpace(rest), 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

func memInfoLinux() (free, total uint64, ok bool) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	var gotTotal, gotAvail bool
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			if n, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				total, gotTotal = n*1024, true
			}
		case "MemAvailable":
			if n, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				free, gotAvail = n*1024, true
			}
		}
	}
	return free, total, gotTotal && gotAvail
}

// diskInfo statfs's the filesystem holding $HOME. Bavail (blocks available
// to an unprivileged user), not Bfree, is the honest "free" figure — Bfree
// includes root-reserved blocks this process could not actually use.
func diskInfo() (free, total uint64, ok bool) {
	home := os.Getenv("HOME")
	if home == "" {
		return 0, 0, false
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(home, &st); err != nil {
		return 0, 0, false
	}
	bs := uint64(st.Bsize)
	return bs * uint64(st.Bavail), bs * uint64(st.Blocks), true
}

// ── formatting ────────────────────────────────────────────────

// formatBytes renders a byte count as a short binary-unit string. Precision
// only matters for orientation here ("about half full"), not accounting.
func formatBytes(n uint64) string {
	const (
		kb = 1 << 10
		mb = 1 << 20
		gb = 1 << 30
		tb = 1 << 40
	)
	switch {
	case n >= tb:
		return fmt.Sprintf("%.1f TB", float64(n)/tb)
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%.0f MB", float64(n)/mb)
	case n >= kb:
		return fmt.Sprintf("%.0f KB", float64(n)/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// formatDuration renders an uptime as e.g. "3d 4h", "4h 12m" or "12m" —
// coarser as it gets longer, since nobody needs uptime to the minute after a
// week.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	hours := d / time.Hour
	d -= hours * time.Hour
	mins := d / time.Minute

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
