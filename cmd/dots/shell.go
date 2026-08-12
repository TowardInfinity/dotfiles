package main

// The application shell owns navigation, geometry, overlays, and mouse hit
// testing. Route bodies deliberately remain small adapters until each screen
// is migrated; they do not get to create their own global navigation system.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
)

type routeID string

const (
	routeOverview routeID = "overview"
	routeChanges  routeID = "changes"
	routeFleet    routeID = "fleet"
	routeHealth   routeID = "health"
	routeServices routeID = "services"
	routePackages routeID = "packages"
	routeProjects routeID = "projects"
	routeDocs     routeID = "docs"
)

type shellFocus uint8

const (
	shellFocusSidebar shellFocus = iota
	shellFocusContent
)

const shellSidebarWidth = 20
const shellBodyTop = 2 // one header row plus its bottom rule

type shellNavRow struct {
	group string
	label string
	route routeID
}

func shellNavRows() []shellNavRow {
	return []shellNavRow{
		{group: "Overview", label: "Overview", route: routeOverview},
		{group: "Workspace", label: "Changes", route: routeChanges},
		{group: "Fleet", label: "Machines", route: routeFleet},
		{group: "This machine", label: "Health", route: routeHealth},
		{group: "This machine", label: "Services", route: routeServices},
		{group: "This machine", label: "Packages", route: routePackages},
		{group: "This machine", label: "Projects", route: routeProjects},
		{group: "Reference", label: "Docs", route: routeDocs},
	}
}

func routeLabel(r routeID) string {
	for _, row := range shellNavRows() {
		if row.route == r {
			return row.label
		}
	}
	return string(r)
}

func routeGroup(r routeID) string {
	for _, row := range shellNavRows() {
		if row.route == r {
			return row.group
		}
	}
	return "dots"
}

type shellHitKind uint8

const (
	shellHitRoute shellHitKind = iota
	shellHitFocus
	shellHitPalette
	shellHitHelp
	shellHitRow
	shellHitAction
)

type shellHit struct {
	x, y, w, h int
	kind       shellHitKind
	route      routeID
	index      int
}

type paletteItem struct {
	label  string
	detail string
	route  routeID
	action string
}

type shellPalette struct {
	query  string
	cursor int
	items  []paletteItem
}

type shellModel struct {
	repo   string
	source string
	w, h   int

	route routeID
	focus shellFocus

	docs            docsModel
	changes         changesModel
	doc             doctorModel
	servicesView    servicesRouteModel
	packagesView    packagesRouteModel
	sp              spinner.Model
	ovInfo          overviewInfo
	ovLoading       bool
	projects        []projectInfo
	projectsLoading bool
	projectHint     string

	act              *actionModel
	palette          *shellPalette
	help             bool
	err              string
	sync             repoState
	fleet            fleetSnapshot
	healthProblems   bool
	healthCursor     int
	healthDetail     string
	fleetCursor      int
	fleetSelected    map[string]bool
	fleetDetail      string
	projectFilter    string
	projectFiltering bool
	projectCursor    int
	projectDetail    string
	loaded           map[routeID]bool
	hits             []shellHit
}

func newShellModel() *shellModel {
	repo := findRepo()
	docs, source := loadDocs(repo)
	return &shellModel{
		repo:            repo,
		source:          source,
		route:           routeOverview,
		focus:           shellFocusSidebar,
		docs:            newDocsModel(docs),
		changes:         newChangesModel(repo),
		doc:             newDoctorModel(repo),
		servicesView:    newServicesRouteModel(),
		packagesView:    newPackagesRouteModel(),
		ovLoading:       true,
		projectsLoading: true,
		sp: spinner.New(
			spinner.WithSpinner(spinner.Dot),
			spinner.WithStyle(styPending),
		),
		sync:          readRepoState(repo),
		fleet:         loadFleetSnapshot(),
		fleetSelected: map[string]bool{},
		loaded:        map[routeID]bool{routeOverview: true, routeChanges: true},
	}
}

func (m *shellModel) Init() tea.Cmd {
	cmds := []tea.Cmd{fetchOverviewInfo(), m.changes.Init(), m.sp.Tick}
	if len(m.fleet.Hosts) == 0 {
		m.loaded[routeFleet] = true
		cmds = append(cmds, fetchFleetSnapshot())
	}
	return tea.Batch(cmds...)
}

func (m *shellModel) ensureRouteLoaded() tea.Cmd {
	if m.loaded == nil {
		m.loaded = map[routeID]bool{}
	}
	if m.loaded[m.route] {
		if m.route == routeFleet && !m.fleet.fresh(time.Now()) {
			return fetchFleetSnapshot()
		}
		return nil
	}
	m.loaded[m.route] = true
	switch m.route {
	case routeHealth:
		return m.doc.Init()
	case routeServices:
		return discoverServices()
	case routePackages:
		return discoverPackages()
	case routeProjects:
		return fetchProjectsInfo()
	case routeFleet:
		return fetchFleetSnapshot()
	}
	return nil
}

func (m *shellModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.resizeChildren()
		return m, nil
	case runActionMsg:
		if m.act != nil {
			return m, nil
		}
		a := newAction(msg.plan, m.w, m.h)
		if a.confirm {
			m.act = &a
			return m, nil
		}
		started, cmd := a.start()
		m.act = &started
		return m, cmd
	case actionPlanErrorMsg:
		m.err = msg.err.Error()
		return m, nil
	case repoStateMsg:
		m.sync = msg.state
		return m, nil
	case fleetSnapshotMsg:
		m.fleet = msg.snapshot
		if m.fleetCursor >= len(m.fleet.Hosts) {
			m.fleetCursor = max(0, len(m.fleet.Hosts)-1)
		}
		return m, nil
	case doctorMsg:
		m.doc, _ = m.doc.update(msg)
		m.healthProblems = false
		for _, check := range m.doc.checks {
			if check.state == checkBad || check.state == checkWarn {
				m.healthProblems = true
				break
			}
		}
		m.clampHealthCursor()
		return m, nil
	case overviewInfoMsg:
		m.ovInfo = msg.info
		m.ovLoading = false
		return m, nil
	case projectsInfoMsg:
		m.projects = msg.projects
		m.projectsLoading = false
		if m.projectCursor >= len(m.projects) {
			m.projectCursor = len(m.projects) - 1
		}
		if m.projectCursor < 0 {
			m.projectCursor = 0
		}
		return m, nil
	case projTmuxMsg:
		m.projectHint = msg.hint
		return m, nil
	case servicesFoundMsg:
		var cmd tea.Cmd
		m.servicesView, cmd = m.servicesView.update(msg)
		return m, cmd
	case servicesProbedMsg:
		m.servicesView, _ = m.servicesView.update(msg)
		return m, nil
	case packagesFoundMsg:
		m.packagesView, _ = m.packagesView.update(msg)
		return m, nil
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	case tea.MouseWheelMsg:
		return m.updateWheel(msg)
	case tea.MouseClickMsg:
		return m.updateClick(msg)
	case spinner.TickMsg:
		m.sp, _ = m.sp.Update(msg)
		return m, nil
	}

	// Async data is broadcast to the child that owns it even when its route is
	// not visible. This keeps first paint useful and prevents hidden checks from
	// getting stranded in a loading state.
	return m, m.updateChildren(msg)
}

func (m *shellModel) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.act != nil {
		a, cmd, closed := m.act.update(msg)
		if closed {
			result := a.result
			m.act = nil
			return m, m.refreshAffected(result.Affects)
		}
		m.act = &a
		return m, cmd
	}
	// Errors are transient route state. Keep them visible until the next user
	// input, then clear them so a stale plan failure cannot obscure a later
	// action's feedback.
	m.err = ""

	if m.palette != nil {
		return m.updatePalette(msg)
	}
	if m.help {
		switch msg.String() {
		case "esc", "?", "f1", "enter":
			m.help = false
		}
		return m, nil
	}

	// Text fields own every key until they explicitly close. Esc therefore
	// clears a filter before it ever becomes a shell-level Back action.
	if m.routeCapturesInput() {
		return m.dispatchRouteKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		if m.focus == shellFocusContent {
			if m.hasRouteDetail() {
				return m.dispatchRouteKey(tea.KeyPressMsg{Code: tea.KeyEsc})
			}
			m.focus = shellFocusSidebar
			return m, nil
		}
		return m, tea.Quit
	case "ctrl+p", ":":
		m.openPalette(false)
		return m, nil
	case "/":
		switch m.route {
		case routeDocs, routeChanges, routeServices, routePackages, routeProjects:
			return m.dispatchRouteKey(msg)
		default:
			m.openPalette(false)
			return m, nil
		}
	case "?", "f1":
		m.help = true
		return m, nil
	case "tab":
		m.cycleFocus(false)
		return m, nil
	case "shift+tab":
		m.cycleFocus(true)
		return m, nil
	case "esc":
		if m.focus == shellFocusContent {
			switch m.route {
			case routeChanges:
				if m.changes.detail != "" {
					return m.dispatchRouteKey(msg)
				}
			case routeFleet:
				if m.fleetDetail != "" {
					return m.dispatchRouteKey(msg)
				}
			case routeHealth:
				if m.healthDetail != "" {
					return m.dispatchRouteKey(msg)
				}
			case routeProjects:
				if m.projectDetail != "" {
					return m.dispatchRouteKey(msg)
				}
			case routeServices:
				if m.servicesView.detail != "" {
					return m.dispatchRouteKey(msg)
				}
			case routePackages:
				if m.packagesView.detail != "" {
					return m.dispatchRouteKey(msg)
				}
			}
			m.focus = shellFocusSidebar
			return m, nil
		}
		return m, nil
	}

	if m.focus == shellFocusSidebar {
		switch msg.String() {
		case "j", "down":
			m.moveRoute(1)
		case "k", "up":
			m.moveRoute(-1)
		case "enter", "l", "right":
			m.focus = shellFocusContent
		}
		return m, m.ensureRouteLoaded()
	}

	if msg.String() == "h" || msg.String() == "left" {
		m.focus = shellFocusSidebar
		return m, nil
	}
	if msg.String() == "a" {
		m.openPalette(true)
		return m, nil
	}
	return m.dispatchRouteKey(msg)
}

// cycleFocus keeps forward and reverse traversal explicit even though the
// current shell has only two focus regions. With two regions both directions
// necessarily toggle; making that a named cycle prevents Shift-Tab from
// looking accidentally duplicated and leaves one place to extend when a
// footer/inspector becomes a third region.
func (m *shellModel) cycleFocus(reverse bool) {
	regions := [...]shellFocus{shellFocusSidebar, shellFocusContent}
	index := 0
	if m.focus == shellFocusContent {
		index = 1
	}
	if reverse {
		index = (index + len(regions) - 1) % len(regions)
	} else {
		index = (index + 1) % len(regions)
	}
	m.focus = regions[index]
}

func (m *shellModel) dispatchRouteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.route {
	case routeDocs:
		m.docs, cmd = m.docs.update(msg)
	case routeChanges:
		m.changes, cmd = m.changes.update(msg)
	case routeHealth:
		return m.updateHealthKey(msg)
	case routeOverview:
		if msg.String() == "r" {
			return m, tea.Batch(fetchOverviewInfo(), fetchRepoState(m.repo), fetchFleetSnapshot())
		}
	case routeFleet:
		return m.updateFleetKey(msg)
	case routeServices:
		m.servicesView, cmd = m.servicesView.updateKey(msg)
	case routePackages:
		m.packagesView, cmd = m.packagesView.updateKey(msg)
	case routeProjects:
		return m.updateProjectsKey(msg)
	}
	return m, cmd
}

func (m *shellModel) routeCapturesInput() bool {
	switch m.route {
	case routeDocs:
		return m.docs.filtering
	case routeChanges:
		return m.changes.filtering
	case routeProjects:
		return m.projectFiltering
	case routeServices:
		return m.servicesView.filtering
	case routePackages:
		return m.packagesView.filtering
	}
	return false
}

func (m *shellModel) updateHealthKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "f" {
		m.healthProblems = !m.healthProblems
		m.healthCursor = 0
		return m, nil
	}
	checks := m.visibleHealthChecks()
	if m.healthCursor >= len(checks) {
		m.healthCursor = max(0, len(checks)-1)
	}
	switch msg.String() {
	case "j", "down":
		if m.healthCursor < len(checks)-1 {
			m.healthCursor++
		}
		return m, nil
	case "k", "up":
		if m.healthCursor > 0 {
			m.healthCursor--
		}
		return m, nil
	case "enter":
		if len(checks) > 0 {
			m.healthDetail = checks[m.healthCursor].name + " · " + checks[m.healthCursor].path
		}
		return m, nil
	case "esc":
		m.healthDetail = ""
		return m, nil
	}
	var cmd tea.Cmd
	m.doc, cmd = m.doc.update(msg)
	return m, cmd
}

func (m *shellModel) clampHealthCursor() {
	checks := m.visibleHealthChecks()
	if m.healthCursor >= len(checks) {
		m.healthCursor = max(0, len(checks)-1)
	}
	if m.healthCursor < 0 {
		m.healthCursor = 0
	}
}

func (m *shellModel) visibleHealthChecks() []checkResult {
	if !m.healthProblems {
		return m.doc.checks
	}
	checks := make([]checkResult, 0, len(m.doc.checks))
	for _, check := range m.doc.checks {
		if check.state == checkBad || check.state == checkWarn {
			checks = append(checks, check)
		}
	}
	return checks
}

func (m *shellModel) updateFleetKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		return m, fetchFleetSnapshot()
	case "j", "down":
		if m.fleetCursor < len(m.fleet.Hosts)-1 {
			m.fleetCursor++
		}
	case "k", "up":
		if m.fleetCursor > 0 {
			m.fleetCursor--
		}
	case "space":
		if len(m.fleet.Hosts) > 0 {
			alias := m.fleet.Hosts[m.fleetCursor].Alias
			m.fleetSelected[alias] = !m.fleetSelected[alias]
		}
	case "enter":
		if len(m.fleet.Hosts) > 0 {
			host := m.fleet.Hosts[m.fleetCursor]
			m.fleetDetail = fmt.Sprintf("%s · %s · %s · %s", host.Alias, host.Outcome, host.Version, host.Revision)
		}
	case "esc":
		m.fleetDetail = ""
	}
	return m, nil
}

func (m *shellModel) visibleProjects() []projectInfo {
	q := strings.ToLower(strings.TrimSpace(m.projectFilter))
	if q == "" {
		return m.projects
	}
	projects := make([]projectInfo, 0, len(m.projects))
	for _, project := range m.projects {
		if strings.Contains(strings.ToLower(project.name+" "+project.branch+" "+project.path), q) {
			projects = append(projects, project)
		}
	}
	return projects
}

func (m *shellModel) updateProjectsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.projectFiltering {
		switch msg.String() {
		case "esc":
			m.projectFiltering, m.projectFilter = false, ""
		case "enter":
			m.projectFiltering = false
		case "backspace":
			if m.projectFilter != "" {
				m.projectFilter = m.projectFilter[:len(m.projectFilter)-1]
			}
		default:
			if msg.Text != "" && msg.Mod == 0 {
				m.projectFilter += msg.Text
			}
		}
		m.projectCursor = 0
		return m, nil
	}
	projects := m.visibleProjects()
	switch msg.String() {
	case "/":
		m.projectFiltering = true
	case "j", "down":
		if m.projectCursor < len(projects)-1 {
			m.projectCursor++
		}
	case "k", "up":
		if m.projectCursor > 0 {
			m.projectCursor--
		}
	case "enter":
		if len(projects) > 0 {
			project := projects[m.projectCursor]
			m.projectDetail = project.path + " · " + project.branch
			return m, openProjectTmux(project)
		}
	case "esc":
		m.projectDetail = ""
	}
	return m, nil
}

func indexOfProject(projects []projectInfo, name string) int {
	for i, project := range projects {
		if project.name == name {
			return i
		}
	}
	return 0
}

// openProjectTmux is a handoff rather than an action-runner operation: tmux
// must own the controlling terminal. The shell still reports the outcome as a
// message, so opening a project has the same async/error semantics as every
// other route action.
func openProjectTmux(p projectInfo) tea.Cmd {
	shown := p.path
	if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(shown, home) {
		shown = "~" + strings.TrimPrefix(shown, home)
	}
	cmdLine := fmt.Sprintf("tmux new-session -A -s %s -c %s", p.name, shown)
	if os.Getenv("TMUX") == "" {
		return func() tea.Msg { return projTmuxMsg{hint: "run in a terminal: " + cmdLine} }
	}
	name := p.name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "tmux", "switch-client", "-t", name).Run(); err != nil {
			return projTmuxMsg{hint: "no session named " + name + " yet — run: " + cmdLine}
		}
		return projTmuxMsg{hint: "switched to " + name}
	}
}

func (m *shellModel) updateChildren(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.docs, cmd = m.docs.update(msg)
	cmds = append(cmds, cmd)
	m.changes, cmd = m.changes.update(msg)
	cmds = append(cmds, cmd)
	m.doc, cmd = m.doc.update(msg)
	cmds = append(cmds, cmd)
	return tea.Batch(cmds...)
}

func (m *shellModel) resizeChildren() {
	w, h := m.contentSize()
	m.docs = m.docs.resize(w, h)
	m.changes = m.changes.resize(w, h)
	m.doc = m.doc.resize(w, h)
	if m.act != nil {
		a := m.act.resize(m.w, m.h)
		m.act = &a
	}
}

func (m *shellModel) contentSize() (int, int) {
	w := m.w
	if m.hasSidebar() {
		w -= shellSidebarWidth + 1
	}
	return max(1, w), max(3, m.h-4)
}

func (m *shellModel) hasSidebar() bool {
	return m.w >= 76
}

func (m *shellModel) moveRoute(delta int) {
	rows := shellNavRows()
	current := 0
	for i, row := range rows {
		if row.route == m.route {
			current = i
			break
		}
	}
	current = (current + delta + len(rows)) % len(rows)
	m.route = rows[current].route
	m.focus = shellFocusSidebar
	m.syncLegacySection()
}

func (m *shellModel) syncLegacySection() {
	// Production navigation is owned entirely by the shell. This method keeps
	// the route transition call sites readable while legacy Manage remains
	// available only to compatibility tests and the old model package.
}

func (m *shellModel) updateWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.hasSidebar() && msg.X < shellSidebarWidth {
		if msg.Button == tea.MouseWheelDown {
			m.moveRoute(1)
		} else if msg.Button == tea.MouseWheelUp {
			m.moveRoute(-1)
		}
		// Scrolling the navigation rail is browsing, not an explicit request
		// to start a route provider. In particular, do not turn a wheel pass
		// over the sidebar into an SSH fleet probe; Enter/right-clicking into a
		// route remains the deliberate load boundary.
		return m, nil
	}
	if m.route == routeDocs {
		var cmd tea.Cmd
		m.docs, cmd = m.docs.update(msg)
		return m, cmd
	}
	if m.route == routeChanges {
		key := tea.KeyPressMsg{Code: tea.KeyDown}
		if msg.Button == tea.MouseWheelUp {
			key.Code = tea.KeyUp
		}
		var cmd tea.Cmd
		m.changes, cmd = m.changes.update(key)
		return m, cmd
	}
	if msg.Button == tea.MouseWheelUp {
		return m.dispatchRouteKey(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	if msg.Button == tea.MouseWheelDown {
		return m.dispatchRouteKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	return m, nil
}

func (m *shellModel) updateClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	// Hit rectangles are derived from the current frame, not a mutable side
	// effect of View. Bubble Tea may deliver a mouse event after state changes
	// but before the next paint; rebuilding here keeps the input map tied to
	// the same state the click is about to act on.
	_, hits := m.renderFrame()
	for _, hit := range hits {
		if msg.X < hit.x || msg.X >= hit.x+hit.w || msg.Y < hit.y || msg.Y >= hit.y+hit.h {
			continue
		}
		switch hit.kind {
		case shellHitRoute:
			m.route = hit.route
			m.focus = shellFocusContent
			m.syncLegacySection()
			return m, m.ensureRouteLoaded()
		case shellHitFocus:
			m.focus = shellFocusContent
		case shellHitPalette:
			if m.palette != nil && hit.index >= 0 {
				return m.choosePalette(hit.index)
			}
			m.openPalette(false)
		case shellHitAction:
			if m.act == nil {
				return m, nil
			}
			if m.act.confirm {
				if hit.index == 0 {
					return m.updateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
				}
				return m.updateKey(tea.KeyPressMsg{Code: tea.KeyEsc})
			}
			return m.updateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		case shellHitHelp:
			m.help = true
		case shellHitRow:
			m.focus = shellFocusContent
			switch hit.route {
			case routeChanges:
				m.changes.cursor = hit.index
			case routeFleet:
				m.fleetCursor = hit.index
			case routeHealth:
				m.healthCursor = hit.index
			case routeProjects:
				m.projectCursor = hit.index
			case routeServices:
				m.servicesView.cursor = hit.index
			case routePackages:
				m.packagesView.cursor = hit.index
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *shellModel) openPalette(actionsOnly bool) {
	items := shellPaletteItems(m.route, actionsOnly)
	m.palette = &shellPalette{items: items}
}

func shellPaletteItems(current routeID, actionsOnly bool) []paletteItem {
	var items []paletteItem
	if !actionsOnly {
		items = []paletteItem{
			{label: "Go to Overview", detail: "route", route: routeOverview},
			{label: "Go to Changes", detail: "route", route: routeChanges},
			{label: "Go to Fleet", detail: "route", route: routeFleet},
			{label: "Go to Health", detail: "route", route: routeHealth},
			{label: "Go to Services", detail: "route", route: routeServices},
			{label: "Go to Packages", detail: "route", route: routePackages},
			{label: "Go to Projects", detail: "route", route: routeProjects},
			{label: "Go to Docs", detail: "route", route: routeDocs},
		}
	}
	switch current {
	case routeChanges:
		items = append(items,
			paletteItem{label: "Sync this machine from origin", detail: "LOCAL · safe inbound", action: "sync"},
			paletteItem{label: "Apply current configuration", detail: "LOCAL · no network", action: "apply"},
			paletteItem{label: "Publish selected changes", detail: "REPOSITORY · reviewed commit and push", action: "publish"},
		)
	case routeHealth:
		items = append(items,
			paletteItem{label: "Recheck health", detail: "LOCAL · read-only", action: "health-refresh"},
			paletteItem{label: "Install missing tools", detail: "LOCAL · package managers", action: "health-install"},
			paletteItem{label: "Repair configuration links", detail: "LOCAL · no network", action: "health-config"},
		)
	case routeServices:
		items = append(items, paletteItem{label: "Rescan services", detail: "LOCAL · read-only", action: "services-refresh"})
	case routePackages:
		items = append(items, paletteItem{label: "Rescan packages", detail: "LOCAL · read-only", action: "packages-refresh"})
	case routeProjects:
		items = append(items, paletteItem{label: "Rescan projects", detail: "LOCAL · read-only", action: "projects-refresh"})
	case routeFleet:
		items = append(items,
			paletteItem{label: "Refresh fleet", detail: "FLEET · SSH read-only", action: "fleet-refresh"},
			paletteItem{label: "Roll out selected machines", detail: "FLEET · exact published revision", action: "rollout"},
		)
	}
	return items
}

func (m *shellModel) updatePalette(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.palette
	if p == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.palette = nil
		return m, nil
	case "up", "ctrl+k":
		if p.cursor > 0 {
			p.cursor--
		}
		return m, nil
	case "down", "ctrl+j":
		if p.cursor+1 < len(p.filtered()) {
			p.cursor++
		}
		return m, nil
	case "enter":
		items := p.filtered()
		if len(items) == 0 {
			return m, nil
		}
		return m.choosePalette(p.cursor)
	case "backspace":
		if p.query != "" {
			p.query = p.query[:len(p.query)-1]
			p.cursor = 0
		}
		return m, nil
	}
	if msg.Text != "" && msg.Mod == 0 {
		p.query += msg.Text
		p.cursor = 0
	}
	return m, nil
}

func (m *shellModel) choosePalette(index int) (tea.Model, tea.Cmd) {
	if m.palette == nil {
		return m, nil
	}
	items := m.palette.filtered()
	if index < 0 || index >= len(items) {
		return m, nil
	}
	item := items[index]
	m.palette = nil
	if item.route != "" {
		m.route = item.route
		m.focus = shellFocusContent
		m.syncLegacySection()
		if cmd := m.ensureRouteLoaded(); cmd != nil {
			return m, cmd
		}
	}
	switch item.action {
	case "sync":
		return m, requestAction(syncInboundRequest{Repo: m.repo})
	case "apply":
		return m, requestAction(applyRequest{Repo: m.repo})
	case "publish":
		paths := m.changes.selectedPaths()
		if len(paths) == 0 {
			m.route, m.focus = routeChanges, shellFocusContent
			m.err = "select at least one changed path before publishing"
			return m, nil
		}
		message := strings.TrimSpace(m.changes.commitMessage)
		if message == "" {
			message = defaultCommitMessage
		}
		return m, requestAction(publishRequest{Repo: m.repo, Paths: paths, Message: message})
	case "health-refresh":
		return m, runDoctorChecks
	case "health-install":
		req, note, ok := m.doc.buildInstall()
		m.doc.note = note
		if ok {
			return m, requestAction(req)
		}
	case "health-config":
		req, note, ok := m.doc.buildConfigRepair()
		m.doc.note = note
		if ok {
			return m, requestAction(req)
		}
	case "services-refresh":
		m.route = routeServices
		return m, discoverServices()
	case "packages-refresh":
		m.route = routePackages
		return m, discoverPackages()
	case "projects-refresh":
		m.route = routeProjects
		return m, fetchProjectsInfo()
	case "fleet-refresh":
		m.route = routeFleet
		return m, fetchFleetSnapshot()
	case "rollout":
		m.route = routeFleet
		return m, m.prepareFleetRollout()
	}
	return m, nil
}

func (p *shellPalette) filtered() []paletteItem {
	q := strings.ToLower(strings.TrimSpace(p.query))
	if q == "" {
		return p.items
	}
	out := make([]paletteItem, 0, len(p.items))
	for _, item := range p.items {
		if strings.Contains(strings.ToLower(item.label+" "+item.detail), q) {
			out = append(out, item)
		}
	}
	return out
}

func (m *shellModel) View() tea.View {
	view, _ := m.renderFrame()
	return view
}

// renderFrame builds a complete frame and its hit map on a value copy. View is
// intentionally observational: rendering must not mutate the live model or
// leave input geometry cached from a previous state. updateClick consumes the
// returned hit map directly, while tests can inspect it through renderedHits.
func (m shellModel) renderFrame() (tea.View, []shellHit) {
	// The copy may share the backing array of a previously cached hit slice;
	// detach it before the render helpers append so the caller remains pure.
	m.hits = nil
	if m.w < 44 || m.h < 14 {
		v := tea.NewView(fmt.Sprintf("dots · %s\n\nTerminal too small — need at least 44×14 (current %d×%d).", routeLabel(m.route), m.w, m.h))
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v, nil
	}

	bodyW, bodyH := m.contentSize()
	rows := shellNavRows()
	sidebar := ""
	if m.hasSidebar() {
		sidebar = m.renderSidebar(rows, bodyH)
	}
	body := m.renderRoute(bodyW, bodyH)
	body = lipgloss.NewStyle().Width(bodyW).Height(bodyH).MaxWidth(bodyW).MaxHeight(bodyH).Render(body)

	headerText := "dots  ·  " + routeLabel(m.route)
	if m.repo != "" {
		headerText += "  ·  " + filepathBase(m.repo)
	}
	header := styTabBar.Width(m.w).Render(truncate(headerText, m.w-2))
	footer := m.renderFooter()
	content := body
	if sidebar != "" {
		content = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, body)
	}
	view := lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
	if m.palette != nil {
		m.hits = m.hits[:0] // modal input blocks routes and footer behind it
		view = lipgloss.Place(m.w, m.h, lipgloss.Left, lipgloss.Top, view)
		view = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.renderPalette(view))
	}
	if m.help {
		m.hits = m.hits[:0]
		view = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.renderHelp(view))
	}
	if m.act != nil {
		m.hits = m.hits[:0] // modal input blocks every route behind it
		overlay := m.act.view(m.sp.View())
		boxW, boxH := lipgloss.Width(overlay), lipgloss.Height(overlay)
		left := max(0, (m.w-boxW)/2)
		top := max(0, (m.h-boxH)/2)
		if m.act.confirm {
			half := max(1, boxW/2)
			m.hits = append(m.hits,
				shellHit{x: left, y: top + boxH - 2, w: half, h: 2, kind: shellHitAction, index: 0},
				shellHit{x: left + half, y: top + boxH - 2, w: boxW - half, h: 2, kind: shellHitAction, index: 1})
		} else {
			m.hits = append(m.hits, shellHit{x: left, y: top + boxH - 2, w: boxW, h: 2, kind: shellHitAction, index: 0})
		}
		view = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, overlay)
	}
	if os.Getenv("NO_COLOR") != "" {
		view = stripANSI(view)
	}
	if os.Getenv("DOTS_ASCII") != "" || os.Getenv("TERM") == "dumb" {
		view = asciiize(view)
	}
	v := tea.NewView(view)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v, m.hits
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			continue
		}
		switch s[i+1] {
		case '[':
			// CSI sequences end in the first byte in the final range @–~.
			i += 2
			for i < len(s) && (s[i] < '@' || s[i] > '~') {
				i++
			}
		case ']':
			// OSC sequences (notably OSC 8 hyperlinks from Glamour) end at
			// BEL or the ST two-byte terminator. They are control metadata;
			// the visible link text after them must remain.
			i += 2
			for i < len(s) {
				if s[i] == '\a' {
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
				i++
			}
		}
	}
	return b.String()
}

func asciiize(s string) string {
	return strings.NewReplacer(
		"▌", ">", "●", "*", "○", "o", "✓", "OK", "×", "x",
		"↑", "^", "↓", "v", "─", "-", "│", "|", "╭", "+", "╮", "+",
		"╰", "+", "╯", "+", "⣾", "*", "…", "...",
	).Replace(s)
}

func (m *shellModel) hasRouteDetail() bool {
	switch m.route {
	case routeChanges:
		return m.changes.detail != ""
	case routeFleet:
		return m.fleetDetail != ""
	case routeHealth:
		return m.healthDetail != ""
	case routeServices:
		return m.servicesView.detail != ""
	case routePackages:
		return m.packagesView.detail != ""
	case routeProjects:
		return m.projectDetail != ""
	}
	return false
}

func (m *shellModel) renderSidebar(rows []shellNavRow, h int) string {
	var b strings.Builder
	line := 1
	lastGroup := ""
	for _, row := range rows {
		if row.group != "" && row.group != lastGroup {
			b.WriteString(styGroup.Render(strings.ToUpper(row.group)))
			b.WriteByte('\n')
			line++
			lastGroup = row.group
		}
		selected := row.route == m.route
		label := "  " + row.label
		if selected {
			label = "▌ " + row.label
		}
		if selected && m.focus == shellFocusSidebar {
			b.WriteString(styItemOn.Render(padRight(label, shellSidebarWidth-1)))
		} else if selected {
			b.WriteString(styItemCursor.Render("▌") + styItem.Render(" "+row.label))
		} else {
			b.WriteString(styItem.Render(label))
		}
		b.WriteByte('\n')
		m.hits = append(m.hits, shellHit{x: 0, y: shellBodyTop + line, w: shellSidebarWidth, h: 1, kind: shellHitRoute, route: row.route})
		line++
	}
	for line <= h {
		b.WriteByte('\n')
		line++
	}
	return stySidebar.Width(shellSidebarWidth).Height(h).Render(b.String())
}

func (m *shellModel) registerRowHit(route routeID, index, y, width int) {
	x := 0
	if m.hasSidebar() {
		x = shellSidebarWidth + 1
	}
	if width < 1 {
		return
	}
	m.hits = append(m.hits, shellHit{x: x, y: y, w: width, h: 1, kind: shellHitRow, route: route, index: index})
}

func (m *shellModel) renderRoute(w, h int) string {
	m.syncLegacySection()
	spin := m.sp.View()
	switch m.route {
	case routeDocs:
		return m.docs.view()
	case routeHealth:
		return m.renderHealth(w, h, spin)
	case routeOverview:
		return contentColumn(w, h, paneHeader("Overview", "Triage", "what needs attention now", measureFor(w)), m.renderOverview(w, h))
	case routeChanges:
		body := m.changes.view(w-4, h-3)
		row := 0
		if m.changes.messageEditing {
			row += 2 // prompt label and the text-input row
		}
		if m.changes.filtering || m.changes.message != "" || m.changes.loading {
			row++
		}
		row++ // LOCAL CHANGES label
		for i := range m.changes.visibleFiles() {
			m.registerRowHit(routeChanges, i, shellBodyTop+3+row+i, w)
		}
		return contentColumn(w, h, paneHeader("Workspace", "Changes", m.changesSummary(), measureFor(w)), body)
	case routeFleet:
		body := m.renderFleet(w, h)
		for i := range m.fleet.Hosts {
			// Fleet has an instruction row, then dataTable's header row and the
			// rule beneath it, before its first selectable host. Counting the
			// header but not its rule shifted every row hit up by one, so a
			// click on one machine selected the machine below it.
			m.registerRowHit(routeFleet, i, shellBodyTop+3+3+i, w)
		}
		return contentColumn(w, h, paneHeader("Fleet", "Machines", m.fleetSummary(), measureFor(w)), body)
	case routeServices:
		body := m.servicesView.view(w, h, spin)
		for i := range m.servicesView.visible() {
			m.registerRowHit(routeServices, i, shellBodyTop+3+m.servicesView.rowOffset()+i, w)
		}
		return contentColumn(w, h, paneHeader("This machine", "Services", m.servicesView.summary(), measureFor(w)), body)
	case routePackages:
		body := m.packagesView.view(w, h, spin)
		for i := range m.packagesView.visible() {
			m.registerRowHit(routePackages, i, shellBodyTop+3+m.packagesView.rowOffset()+i, w)
		}
		return contentColumn(w, h, paneHeader("This machine", "Packages", m.packagesView.summary(), measureFor(w)), body)
	case routeProjects:
		body := m.renderProjects(w, h)
		row := 1
		if m.projectFiltering {
			row++
		}
		for i := range m.visibleProjects() {
			m.registerRowHit(routeProjects, i, shellBodyTop+3+row+i, w)
		}
		return contentColumn(w, h, paneHeader("This machine", "Projects", fmt.Sprintf("%d repositories under ~/Codes", len(m.projects)), measureFor(w)), body)
	default:
		return ""
	}
}

func (m *shellModel) healthSummary() string {
	if m.doc.loading {
		return "checking tools, frameworks, and config"
	}
	ok, bad, warn := 0, 0, 0
	for _, check := range m.doc.checks {
		switch check.state {
		case checkOK:
			ok++
		case checkBad:
			bad++
		case checkWarn:
			warn++
		}
	}
	if bad > 0 || warn > 0 {
		return fmt.Sprintf("%d healthy · %d failed · %d warnings", ok, bad, warn)
	}
	return fmt.Sprintf("%d checks healthy", ok)
}

func (m *shellModel) renderHealth(w, h int, spin string) string {
	if m.doc.loading {
		return "\n  " + spin + styPending.Render(" checking")
	}
	checks := m.visibleHealthChecks()
	if len(checks) == 0 {
		return styOK.Render("✓ Everything the configs call is installed and healthy.")
	}
	rows := []string{}
	filter := "PROBLEMS"
	if !m.healthProblems {
		filter = "ALL CHECKS"
	}
	rows = append(rows, styMuted.Render(filter+" · f toggles filter"))
	lastGroup := ""
	for i, check := range checks {
		group := checkGroup(check.name)
		if group != lastGroup {
			rows = append(rows, "", styGroup.Render(strings.ToUpper(group)))
			lastGroup = group
		}
		where := check.path
		if where == "" && check.state == checkBad {
			where = "not found"
		}
		line := checkDot(check.state) + " " + check.name
		if where != "" {
			line += "  " + styMuted.Render(where)
		}
		if i == m.healthCursor {
			line = styItemOn.Render(padRight(truncate(line, max(1, w-4)), max(1, w-4)))
		}
		rows = append(rows, line)
		localY := 3 + len(rows) - 1
		m.registerRowHit(routeHealth, i, shellBodyTop+localY, w)
	}
	if m.healthDetail != "" {
		rows = append(rows, "", styTitle.Render("DETAIL"), styMuted.Render("  "+truncate(m.healthDetail, max(1, w-4))))
	}
	rows = append(rows, "", styMuted.Render("r recheck · i install missing · c repair config · Enter inspect"))
	return strings.Join(rows, "\n")
}

func (m *shellModel) changesSummary() string {
	files := len(m.changes.visibleFiles())
	parts := []string{}
	if files == 0 {
		parts = append(parts, "clean")
	} else {
		parts = append(parts, plural(files, "local change", "local changes"))
	}
	if len(m.changes.incoming) > 0 {
		parts = append(parts, plural(len(m.changes.incoming), "incoming commit", "incoming commits"))
	}
	if m.changes.branch != "" {
		parts = append(parts, m.changes.branch)
	}
	return strings.Join(parts, " · ")
}

func (m *shellModel) fleetSummary() string {
	if len(m.fleet.Hosts) == 0 {
		return "unknown · press r to check"
	}
	return m.fleet.summary() + " · " + formatAgeOrUnknown(m.fleet)
}

func formatAgeOrUnknown(snapshot fleetSnapshot) string {
	age, ok := fleetSnapshotAge(snapshot)
	if !ok {
		return "never checked"
	}
	state := "fresh"
	if !snapshot.fresh(time.Now()) {
		state = "stale"
	}
	return state + " " + formatAge(age)
}

func (m *shellModel) renderFleet(w, h int) string {
	if len(m.fleet.Hosts) == 0 {
		return styMuted.Render("unknown · press r to check configured SSH hosts")
	}
	rows := []string{styMuted.Render("SPACE select · ENTER inspect · r refresh · a actions")}
	measure := measureFor(w)
	// Preserve the high-value identity/state columns as the terminal narrows;
	// revision and then release version are details available in the inspector.
	showVersion := measure >= 58
	showRevision := measure >= 78
	headers := []string{"HOST", "STATE"}
	if showVersion {
		headers = append(headers, "VERSION")
	}
	if showRevision {
		headers = append(headers, "REVISION")
	}
	tableRows := make([][]string, 0, len(m.fleet.Hosts))
	for _, host := range m.fleet.Hosts {
		mark := "○"
		if m.fleetSelected[host.Alias] {
			mark = "●"
		}
		state := styPending.Render(host.Outcome)
		if host.Outcome == "ok" && host.ConfigOK {
			state = styOK.Render("healthy")
		} else if host.Outcome == "unreachable" {
			state = styBad.Render("unreachable")
		}
		row := []string{mark + " " + truncate(host.Alias, 18), stripANSI(state)}
		if showVersion {
			version := host.Version
			if version == "" {
				version = "—"
			}
			row = append(row, version)
		}
		if showRevision {
			revision := shortSHA(host.Revision)
			if revision == "" {
				revision = "—"
			}
			row = append(row, revision)
		}
		tableRows = append(tableRows, row)
	}
	rows = append(rows, dataTable(headers, tableRows, m.fleetCursor, measure))
	if m.fleetDetail != "" {
		rows = append(rows, "", styTitle.Render("MACHINE DETAIL"), styMuted.Render("  "+truncate(m.fleetDetail, max(1, w-4))))
	}
	return strings.Join(rows, "\n")
}

func (m *shellModel) renderProjects(w, h int) string {
	projects := m.visibleProjects()
	rows := []string{}
	if m.projectFiltering {
		rows = append(rows, styFilter.Render("/"+m.projectFilter))
	}
	if m.projectsLoading {
		return styPending.Render("scanning projects…")
	}
	if len(projects) == 0 {
		return styMuted.Render("no projects match · repositories are discovered under ~/Codes")
	}
	rows = append(rows, styMuted.Render("ENTER open/switch · / filter · projects never mutate git state"))
	for i, project := range projects {
		status := styOK.Render("clean")
		if !project.dirtyKnown {
			status = styMuted.Render("unknown")
		} else if project.dirty {
			status = styBad.Render("dirty")
		}
		tmux := styMuted.Render("-")
		if project.tmux {
			tmux = styOK.Render("tmux")
		}
		line := fmt.Sprintf("%-22s %-16s %s  %s", truncate(project.name, 22), truncate(project.branch, 16), status, tmux)
		line = truncate(line, max(1, w-4))
		if i == m.projectCursor {
			line = styItemOn.Render(padRight("▌ "+line, max(1, w-4)))
		} else {
			line = styItem.Render("  " + line)
		}
		rows = append(rows, line)
	}
	if m.projectHint != "" {
		rows = append(rows, "", styMuted.Render("  "+truncate(m.projectHint, max(1, w-4))))
	}
	if m.projectDetail != "" {
		rows = append(rows, "", styTitle.Render("PROJECT DETAIL"), styMuted.Render("  "+truncate(m.projectDetail, max(1, w-4))))
	}
	return strings.Join(rows, "\n")
}

func (m *shellModel) selectedFleetHosts() []string {
	hosts := make([]string, 0, len(m.fleetSelected))
	for _, host := range m.fleet.Hosts {
		if m.fleetSelected[host.Alias] {
			hosts = append(hosts, host.Alias)
		}
	}
	return hosts
}

func (m *shellModel) prepareFleetRollout() tea.Cmd {
	hosts := m.selectedFleetHosts()
	if len(hosts) == 0 {
		m.err = "select at least one machine before rollout"
		return nil
	}
	repo := m.repo
	return func() tea.Msg {
		revision, tag, err := resolveFleetRolloutTarget(repo)
		if err != nil {
			return actionPlanErrorMsg{err: err}
		}
		return requestAction(rolloutRequest{Repo: repo, Hosts: hosts, Revision: revision, Version: tag})()
	}
}

// resolveFleetRolloutTarget is shared by the TUI entry point and the CLI's
// rollout contract: a rollout may only pair a published semver tag with the
// exact commit that tag names, and that tag must still be Latest. Resolving
// origin/main independently would pair an untagged checkout with an older
// release binary and make the remote script appear successful while the
// convergence probe failed afterwards.
func resolveFleetRolloutTarget(repo string) (string, string, error) {
	revision, tag, err := resolvePublishedRevision(repo, "")
	if err != nil {
		return "", "", fmt.Errorf("resolve published revision: %w", err)
	}
	latest, err := latestReleaseTag()
	if err != nil {
		return "", "", fmt.Errorf("resolve Latest release: %w", err)
	}
	if err := validateRolloutLatest(tag, latest); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(revision), tag, nil
}

func (m *shellModel) renderOverview(w, h int) string {
	var rows []string
	rows = append(rows, styMuted.Render("NEEDS ATTENTION"))
	attention := 0
	if m.changes.loading {
		rows = append(rows, styPending.Render("  ⣾ checking repository changes"))
	} else if len(m.changes.files) > 0 {
		rows = append(rows, styPending.Render(fmt.Sprintf("  ! %s — review in Changes", plural(len(m.changes.files), "local change", "local changes"))))
		attention++
	}
	if len(m.changes.incoming) > 0 {
		rows = append(rows, styPending.Render(fmt.Sprintf("  ! %s from origin — sync when ready", plural(len(m.changes.incoming), "incoming commit", "incoming commits"))))
		attention++
	}
	if m.sync.detached {
		rows = append(rows, styBad.Render("  × detached HEAD — sync and publish are blocked"))
		attention++
	}
	if !m.ovLoading && m.ovInfo.toolsTotal > 0 && m.ovInfo.toolsHave < m.ovInfo.toolsTotal {
		rows = append(rows, styPending.Render(fmt.Sprintf("  ! tools %d/%d present — inspect Health", m.ovInfo.toolsHave, m.ovInfo.toolsTotal)))
		attention++
	}
	if attention == 0 && !m.ovLoading {
		rows = append(rows, styOK.Render("  ✓ nothing needs attention"))
	}

	rows = append(rows, "", styMuted.Render("WORKSPACE"))
	branch := m.changes.branch
	if branch == "" {
		branch = "unknown branch"
	}
	workspace := fmt.Sprintf("  %s · %s", branch, m.changesSummary())
	rows = append(rows, styValue.Render(truncate(workspace, max(1, w-4))))
	if m.repo != "" {
		rows = append(rows, styMuted.Render("  "+truncate(m.repo, max(1, w-4))))
	}

	rows = append(rows, "", styMuted.Render("FLEET"))
	if len(m.fleet.Hosts) == 0 {
		rows = append(rows, styMuted.Render("  unknown · press r to check"))
	} else {
		line := "  " + m.fleet.summary()
		if age, ok := fleetSnapshotAge(m.fleet); ok {
			state := "fresh"
			if !m.fleet.fresh(time.Now()) {
				state = "stale"
			}
			line += fmt.Sprintf(" · %s · %s", state, formatAge(age))
		}
		rows = append(rows, styValue.Render(line))
	}

	rows = append(rows, "", styMuted.Render("THIS MACHINE"))
	if m.ovLoading {
		rows = append(rows, styPending.Render("  ⣾ collecting local facts"))
	} else {
		info := m.ovInfo
		name := info.osName
		if name == "" {
			name = "OS unknown"
		}
		rows = append(rows, styValue.Render(fmt.Sprintf("  %s · %s · dots %s", name, info.arch, info.version)))
		rows = append(rows, styMuted.Render(fmt.Sprintf("  tools %d/%d · services %d · packages %d", info.toolsHave, info.toolsTotal, len(m.servicesView.items), len(m.packagesView.items))))
	}
	return strings.Join(rows, "\n")
}

func formatAge(age time.Duration) string {
	if age < time.Minute {
		return "just now"
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm ago", int(age/time.Minute))
	}
	return fmt.Sprintf("%dh ago", int(age/time.Hour))
}

func (m *shellModel) renderFooter() string {
	left := "↑↓ move  tab focus  enter inspect  a actions  / palette  ? help"
	switch m.route {
	case routeChanges:
		left = "↑↓ move  space select  enter inspect  m message  L apply  u sync  p publish  a actions  ? help"
	case routeFleet:
		left = "↑↓ move  space select  enter inspect  r refresh  a actions  ? help"
	case routeHealth:
		left = "↑↓ move  enter inspect  f filter  r recheck  a repair  ? help"
	case routeServices:
		left = "↑↓ move  / filter  s start  x stop  R restart  a actions"
	case routePackages:
		left = "↑↓ move  / filter  u upgrade  s sort  m manager  a actions"
	case routeProjects:
		left = "↑↓ move  enter open  a actions  ? help"
	case routeDocs:
		left = "↑↓ topic  / filter  d/u scroll  a actions  ? help"
	}
	if m.err != "" {
		left = "error: " + m.err + "  ·  press any key to dismiss"
	}
	if m.sync.needsSync() {
		left += "  " + styPending.Render("! changes")
	}
	// The footer is the visible affordance for the palette/action layer. A
	// click anywhere in it opens the same searchable action surface as `a`;
	// this keeps the mouse path discoverable without trying to duplicate the
	// variable-width key labels as separate coordinate math.
	if m.h >= 2 {
		m.hits = append(m.hits, shellHit{x: 0, y: m.h - 2, w: m.w, h: 2, kind: shellHitPalette})
	}
	return styStatus.Width(m.w).Render(truncate(left, m.w-2))
}

func (m *shellModel) renderPalette(base string) string {
	_ = base
	p := m.palette
	items := p.filtered()
	var b strings.Builder
	b.WriteString(styTitle.Render("Command palette") + "\n")
	b.WriteString(styMuted.Render("Type to search · Enter select · Esc close") + "\n\n")
	b.WriteString(styFilter.Render("> "+p.query) + "\n\n")
	for i, item := range items {
		marker := "  "
		if i == p.cursor {
			marker = "▌ "
		}
		b.WriteString(marker + styItem.Render(item.label) + "  " + styMuted.Render(item.detail) + "\n")
	}
	if len(items) == 0 {
		b.WriteString(styMuted.Render("No matching routes or actions."))
	}
	out := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cMac).Padding(1, 2).Width(min(78, m.w-8)).Render(b.String())
	boxW, boxH := lipgloss.Width(out), lipgloss.Height(out)
	left := max(0, (m.w-boxW)/2)
	top := max(0, (m.h-boxH)/2)
	itemTop := top + 7 // border, padding, title, hint, spacer, filter, spacer
	for i := range items {
		m.hits = append(m.hits, shellHit{x: left, y: itemTop + i, w: boxW, h: 1, kind: shellHitPalette, index: i})
	}
	return out
}

func (m *shellModel) renderHelp(base string) string {
	_ = base
	body := styTitle.Render("Keyboard and mouse") + "\n\n" +
		"↑↓ / j k   move in the focused region\n" +
		"Tab / Shift-Tab move focus between sidebar and content\n" +
		"Enter      inspect or choose\n" +
		"a          actions for this route\n" +
		"Ctrl-P :   command palette\n" + "Esc        back / close one layer\n" + "q          quit at the base layer\n\n" +
		styMuted.Render("Mouse: click routes or visible controls; wheel scrolls the focused region.")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cViolet).Padding(1, 2).Width(min(70, m.w-8)).Render(body)
}

func filepathBase(path string) string {
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}

func (m *shellModel) refreshAffected(resources []ops.ResourceID) tea.Cmd {
	if len(resources) == 0 {
		return nil
	}
	seen := map[ops.ResourceID]bool{}
	cmds := []tea.Cmd{fetchOverviewInfo()}
	for _, resource := range resources {
		if seen[resource] {
			continue
		}
		seen[resource] = true
		switch resource {
		case resourceDoctor, resourceConfig:
			cmds = append(cmds, m.doc.Init())
		case resourceRepo:
			cmds = append(cmds, fetchDotfilesInfo(m.repo), fetchRepoState(m.repo))
		case resourceServices:
			cmds = append(cmds, discoverServices())
		case resourcePackages:
			cmds = append(cmds, discoverPackages())
		case resourceMachines:
			cmds = append(cmds, fetchMachinesInfo(), fetchFleetSnapshot())
		}
	}
	return tea.Batch(cmds...)
}
