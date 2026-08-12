package main

// Native collection routes for Services and Packages. Discovery remains in
// services.go/packages.go and mutations remain typed operation requests; this
// file owns only route state, filtering, selection, inspection, and rendering.

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type servicesRouteModel struct {
	items       []service
	sources     []string
	loading     bool
	filter      string
	filtering   bool
	input       textinput.Model
	runningOnly bool
	cursor      int
	detail      string
	message     string
}

func newServicesRouteModel() servicesRouteModel {
	return servicesRouteModel{
		input:   newRouteFilterInput("filter services"),
		loading: true,
	}
}

func (m servicesRouteModel) update(msg tea.Msg) (servicesRouteModel, tea.Cmd) {
	switch msg := msg.(type) {
	case servicesFoundMsg:
		m.items = msg.services
		m.sources = msg.sources
		m.loading = false
		m.message = msg.err
		m.clampCursor()
		return m, probeServices(m.items)
	case servicesProbedMsg:
		for i := range m.items {
			if m.items[i].Port == 0 {
				continue
			}
			m.items[i].Probed = true
			m.items[i].Healthy = msg.ports[m.items[i].Port]
		}
		return m, nil
	}
	return m, nil
}

func (m *servicesRouteModel) clampCursor() {
	visible := m.visible()
	if m.cursor >= len(visible) {
		m.cursor = len(visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m servicesRouteModel) visible() []service {
	q := strings.ToLower(strings.TrimSpace(m.filter))
	items := make([]service, 0, len(m.items))
	for _, item := range m.items {
		if m.runningOnly && !item.Running {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(item.Name+" "+item.ID+" "+svcSourceName(item.Source)), q) {
			continue
		}
		items = append(items, item)
	}
	return items
}

func (m servicesRouteModel) summary() string {
	if m.loading {
		return "discovering services…"
	}
	if len(m.items) == 0 {
		return "no services discovered on this machine"
	}
	running := 0
	for _, item := range m.items {
		if item.Running {
			running++
		}
	}
	sources := strings.Join(m.sources, ", ")
	if sources == "" {
		sources = "none"
	}
	scope := "all"
	if m.runningOnly {
		scope = "running only"
	}
	return fmt.Sprintf("%d of %d running · %s · via %s", running, len(m.items), scope, sources)
}

func (m servicesRouteModel) updateKey(msg tea.KeyPressMsg) (servicesRouteModel, tea.Cmd) {
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.input.Blur()
			m.input.SetValue("")
			m.filter = ""
			m.cursor = 0
			return m, nil
		case "enter":
			m.filtering = false
			m.input.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if value := m.input.Value(); value != m.filter {
			m.filter = value
			m.cursor = 0
		}
		return m, cmd
	}

	items := m.visible()
	switch msg.String() {
	case "j", "down":
		if m.cursor < len(items)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "/":
		m.filtering = true
		m.input.Focus()
		return m, textinput.Blink
	case "f":
		m.runningOnly = !m.runningOnly
		m.cursor = 0
	case "r":
		m.loading = true
		m.message = ""
		return m, discoverServices()
	case "enter":
		if m.cursor < len(items) {
			item := items[m.cursor]
			m.detail = fmt.Sprintf("%s · %s · %s · %s", item.Name, svcSourceName(item.Source), serviceState(item), item.Detail)
		}
	case "esc":
		m.detail = ""
	case "s", "x", "R":
		if m.cursor >= len(items) {
			return m, nil
		}
		verb := map[string]string{"s": "start", "x": "stop", "R": "restart"}[msg.String()]
		item := items[m.cursor]
		if _, ok := serviceAction(item, verb); !ok {
			m.message = verb + " is not available for " + svcSourceName(item.Source) + " units of this kind"
			return m, nil
		}
		m.message = ""
		return m, requestAction(serviceRequest{Service: item, Verb: verb})
	}
	return m, nil
}

func serviceState(item service) string {
	if !item.Running {
		return "stopped"
	}
	if item.Probed && !item.Healthy {
		return "not answering"
	}
	return "running"
}

func (m servicesRouteModel) rowOffset() int {
	if m.filtering || m.filter != "" {
		return 2
	}
	return 1
}

func (m servicesRouteModel) view(w, h int, spin string) string {
	if m.loading {
		return "\n  " + spin + styPending.Render(" discovering services")
	}
	if len(m.items) == 0 {
		return "\n  " + styMuted.Render("Nothing found. Looked for launchd agents, systemd units, and Docker containers.")
	}

	measure := measureFor(w)
	lines := []string{truncate("↑↓ move · / filter · f running only · Enter inspect", measure)}
	if m.filtering || m.filter != "" {
		m.input.SetWidth(max(10, measure-2))
		lines = append(lines, "  "+truncate(m.input.View(), measure))
	}
	items := m.visible()
	rows := h - len(lines) - 4
	if rows < 3 {
		rows = 3
	}
	start := boundedWindowStart(m.cursor, len(items), rows)
	end := min(len(items), start+rows)
	for i := start; i < end; i++ {
		item := items[i]
		mark := stateDot(item.Running, true)
		state := serviceState(item)
		if item.Running && item.Probed && !item.Healthy {
			mark = styPending.Render("●")
		}
		port := ""
		if item.Port != 0 {
			port = ":" + strconv.Itoa(item.Port)
		}
		line := fmt.Sprintf("%s %-24s %-12s %-9s %-7s %s", mark, item.Name, svcSourceName(item.Source), state, port, item.Detail)
		lines = append(lines, selectableLine(line, measure, i == m.cursor))
	}
	if len(items) > rows {
		lines = append(lines, styMuted.Render(fmt.Sprintf("… %d more · j/k to move", len(items)-rows)))
	}
	if m.detail != "" {
		lines = append(lines, "", styTitle.Render("SERVICE DETAIL"), styMuted.Render("  "+truncate(m.detail, measure)))
	}
	if m.message != "" {
		lines = append(lines, "", styMuted.Render(truncate(m.message, measure)))
	}
	return strings.Join(lines, "\n")
}

type packagesRouteModel struct {
	items      []pkg
	advisories []advisory
	sources    []string
	loading    bool
	filter     string
	filtering  bool
	input      textinput.Model
	cursor     int
	sortMode   pkgSortMode
	manager    pkgManager
	detail     string
	message    string
}

func newPackagesRouteModel() packagesRouteModel {
	return packagesRouteModel{input: newRouteFilterInput("filter packages"), loading: true}
}

func (m packagesRouteModel) update(msg tea.Msg) (packagesRouteModel, tea.Cmd) {
	if found, ok := msg.(packagesFoundMsg); ok {
		m.items = found.packages
		m.advisories = found.advisories
		m.sources = found.sources
		m.loading = false
		m.clampCursor()
	}
	return m, nil
}

func (m *packagesRouteModel) clampCursor() {
	items := m.visible()
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m packagesRouteModel) visible() []pkg {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	items := make([]pkg, 0, len(m.items))
	for _, item := range m.items {
		if m.manager != pkgManagerAll && item.Manager != m.manager {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item.Name+" "+item.Manager.String()), query) {
			continue
		}
		items = append(items, item)
	}
	sortPackagesFor(items, m.sortMode)
	return items
}

func (m packagesRouteModel) summary() string {
	if m.loading {
		return "checking package managers…"
	}
	if len(m.items) == 0 {
		return "no package managers found on this machine"
	}
	outdated := 0
	for _, item := range m.items {
		if item.Outdated() {
			outdated++
		}
	}
	sources := strings.Join(m.sources, ", ")
	if sources == "" {
		sources = "none"
	}
	summary := fmt.Sprintf("%d packages, %d outdated · via %s · sorted by %s", len(m.items), outdated, sources, m.sortMode)
	if m.manager != pkgManagerAll {
		summary += " · showing " + m.manager.String() + " only"
	}
	return summary
}

func (m packagesRouteModel) updateKey(msg tea.KeyPressMsg) (packagesRouteModel, tea.Cmd) {
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.input.Blur()
			m.input.SetValue("")
			m.filter = ""
			m.cursor = 0
			return m, nil
		case "enter":
			m.filtering = false
			m.input.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if value := m.input.Value(); value != m.filter {
			m.filter = value
			m.cursor = 0
		}
		return m, cmd
	}

	items := m.visible()
	switch msg.String() {
	case "j", "down":
		if m.cursor < len(items)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "/":
		m.filtering = true
		m.input.Focus()
		return m, textinput.Blink
	case "r":
		m.loading = true
		m.message = ""
		return m, discoverPackages()
	case "s":
		if m.sortMode == pkgSortOutdated {
			m.sortMode = pkgSortName
		} else {
			m.sortMode = pkgSortOutdated
		}
		m.cursor = 0
	case "m":
		m.manager = (m.manager + 1) % numPkgManagers
		m.cursor = 0
	case "enter":
		if m.cursor < len(items) {
			item := items[m.cursor]
			latest := item.Latest
			if latest == "" {
				latest = "not checked"
			}
			m.detail = fmt.Sprintf("%s · %s · installed %s · latest %s", item.Name, item.Manager, item.Version, latest)
		}
	case "esc":
		m.detail = ""
	case "u":
		if m.cursor >= len(items) {
			return m, nil
		}
		item := items[m.cursor]
		if _, ok := packageAction(item); !ok {
			m.message = "no upgrade action for " + item.Manager.String() + " packages"
			return m, nil
		}
		m.message = ""
		return m, requestAction(packageRequest{Package: item})
	}
	return m, nil
}

func (m packagesRouteModel) rowOffset() int {
	offset := 1
	if m.filtering || m.filter != "" {
		offset++
	}
	if shown, more := outdatedOverview(m.items, pkgOutdatedOverviewCap); len(shown) > 0 {
		offset += 1 + len(shown)
		if more > 0 {
			offset++
		}
	}
	offset += len(m.advisories)
	return offset
}

func (m packagesRouteModel) view(w, h int, spin string) string {
	if m.loading {
		return "\n  " + spin + styPending.Render(" checking package managers")
	}
	if len(m.items) == 0 {
		return "\n  " + styMuted.Render("Nothing found. Looked for brew, pnpm, npm, uv tool, pip, Go, Claude, and OpenCode.")
	}
	measure := measureFor(w)
	lines := []string{truncate("↑↓ move · / filter · s sort · m manager · Enter inspect · u upgrade", measure)}
	if m.filtering || m.filter != "" {
		m.input.SetWidth(max(10, measure-2))
		lines = append(lines, "  "+truncate(m.input.View(), measure))
	}
	if shown, more := outdatedOverview(m.items, pkgOutdatedOverviewCap); len(shown) > 0 {
		lines = append(lines, styGroup.Render(fmt.Sprintf("OUTDATED (%d)", len(shown)+more)))
		for _, item := range shown {
			lines = append(lines, truncate(fmt.Sprintf("  %s %s %s → %s", strings.ToUpper(item.Manager.String()), item.Name, item.Version, item.Latest), measure))
		}
		if more > 0 {
			lines = append(lines, styMuted.Render(fmt.Sprintf("  … %d more outdated", more)))
		}
	}
	for _, advisory := range m.advisories {
		lines = append(lines, styPending.Render("! "+truncate(advisory.Text, measure)))
	}
	items := m.visible()
	rows := h - len(lines) - 4
	if rows < 3 {
		rows = 3
	}
	start := boundedWindowStart(m.cursor, len(items), rows)
	end := min(len(items), start+rows)
	lastManager := pkgManager(-1)
	for i := start; i < end; i++ {
		item := items[i]
		group := ""
		if item.Manager != lastManager {
			group = strings.ToUpper(item.Manager.String()) + "  "
			lastManager = item.Manager
		}
		latest := item.Latest
		if latest == "" {
			latest = "—"
		}
		mark := " "
		if item.Outdated() {
			mark = styPending.Render("↑")
		}
		line := fmt.Sprintf("%s %-12s %-24s %-12s %s", mark, group, item.Name, item.Version, latest)
		lines = append(lines, selectableLine(line, measure, i == m.cursor))
	}
	if len(items) > rows {
		lines = append(lines, styMuted.Render(fmt.Sprintf("… %d more · j/k to move", len(items)-rows)))
	}
	if m.detail != "" {
		lines = append(lines, "", styTitle.Render("PACKAGE DETAIL"), styMuted.Render("  "+truncate(m.detail, measure)))
	}
	if m.message != "" {
		lines = append(lines, "", styMuted.Render(truncate(m.message, measure)))
	}
	return strings.Join(lines, "\n")
}

func newRouteFilterInput(placeholder string) textinput.Model {
	input := textinput.New()
	input.Prompt = "/"
	input.Placeholder = placeholder
	input.CharLimit = 40
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Prompt = styFilter
	styles.Focused.Text = styValue
	styles.Focused.Placeholder = styMuted
	styles.Blurred.Prompt = styFilter
	styles.Blurred.Text = styValue
	styles.Blurred.Placeholder = styMuted
	input.SetStyles(styles)
	return input
}

func boundedWindowStart(cursor, length, rows int) int {
	if length <= rows || rows <= 0 {
		return 0
	}
	start := cursor - rows/2
	if start < 0 {
		start = 0
	}
	if start > length-rows {
		start = length - rows
	}
	return start
}
