package main

// The application shell owns navigation, geometry, overlays, and mouse hit
// testing. Route bodies deliberately remain small adapters until each screen
// is migrated; they do not get to create their own global navigation system.

import (
	"fmt"
	"strings"

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
)

type shellHit struct {
	x, y, w, h int
	kind       shellHitKind
	route      routeID
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

	docs docsModel
	doc  doctorModel
	man  manageModel
	sp   spinner.Model

	act     *actionModel
	palette *shellPalette
	help    bool
	err     string
	sync    repoState
	hits    []shellHit
}

func newShellModel() *shellModel {
	repo := findRepo()
	docs, source := loadDocs(repo)
	return &shellModel{
		repo:   repo,
		source: source,
		route:  routeOverview,
		focus:  shellFocusSidebar,
		docs:   newDocsModel(docs),
		doc:    newDoctorModel(repo),
		man:    newManageModel(repo),
		sp: spinner.New(
			spinner.WithSpinner(spinner.Dot),
			spinner.WithStyle(styPending),
		),
		sync: readRepoState(repo),
	}
}

func (m *shellModel) Init() tea.Cmd {
	return tea.Batch(m.doc.Init(), m.man.Init(), m.sp.Tick)
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
			return m, tea.Quit
		}
		return m, tea.Quit
	case "ctrl+p", ":", "/":
		m.openPalette(false)
		return m, nil
	case "?", "f1":
		m.help = true
		return m, nil
	case "tab":
		if m.focus == shellFocusSidebar {
			m.focus = shellFocusContent
		} else {
			m.focus = shellFocusSidebar
		}
		return m, nil
	case "shift+tab":
		if m.focus == shellFocusSidebar {
			m.focus = shellFocusContent
		} else {
			m.focus = shellFocusSidebar
		}
		return m, nil
	case "esc":
		if m.focus == shellFocusContent {
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
		return m, nil
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

func (m *shellModel) dispatchRouteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.route {
	case routeDocs:
		m.docs, cmd = m.docs.update(msg)
	case routeHealth:
		m.doc, cmd = m.doc.update(msg)
	case routeOverview:
		m.man.section = secOverview
		m.man, cmd = m.man.updateOverviewKey(msg)
	case routeChanges:
		m.man.section = secDotfiles
		m.man, cmd = m.man.updateDotfilesKey(msg)
	case routeFleet:
		m.man.section = secMachines
		m.man, cmd = m.man.updateMachinesKey(msg)
	case routeServices:
		m.man.section = secServices
		m.man, cmd = m.man.updateServicesKey(msg)
	case routePackages:
		m.man.section = secPackages
		m.man, cmd = m.man.updatePackagesKey(msg)
	case routeProjects:
		m.man.section = secProjects
		m.man, cmd = m.man.updateProjectsKey(msg)
	}
	return m, cmd
}

func (m *shellModel) routeCapturesInput() bool {
	switch m.route {
	case routeDocs:
		return m.docs.filtering
	case routeServices:
		return m.man.svcFiltering
	case routePackages:
		return m.man.pkgFiltering
	}
	return false
}

func (m *shellModel) updateChildren(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.docs, cmd = m.docs.update(msg)
	cmds = append(cmds, cmd)
	m.doc, cmd = m.doc.update(msg)
	cmds = append(cmds, cmd)
	m.man, cmd = m.man.update(msg)
	cmds = append(cmds, cmd)
	return tea.Batch(cmds...)
}

func (m *shellModel) resizeChildren() {
	w, h := m.contentSize()
	m.docs = m.docs.resize(w, h)
	m.doc = m.doc.resize(w, h)
	m.man = m.man.resize(w, h)
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
	switch m.route {
	case routeOverview:
		m.man.section = secOverview
	case routeChanges:
		m.man.section = secDotfiles
	case routeFleet:
		m.man.section = secMachines
	case routeServices:
		m.man.section = secServices
	case routePackages:
		m.man.section = secPackages
	case routeProjects:
		m.man.section = secProjects
	}
}

func (m *shellModel) updateWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseWheelUp {
		return m.dispatchRouteKey(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	if msg.Button == tea.MouseWheelDown {
		return m.dispatchRouteKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	return m, nil
}

func (m *shellModel) updateClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	for _, hit := range m.hits {
		if msg.X < hit.x || msg.X >= hit.x+hit.w || msg.Y < hit.y || msg.Y >= hit.y+hit.h {
			continue
		}
		switch hit.kind {
		case shellHitRoute:
			m.route = hit.route
			m.focus = shellFocusContent
			m.syncLegacySection()
		case shellHitFocus:
			m.focus = shellFocusContent
		case shellHitPalette:
			m.openPalette(false)
		case shellHitHelp:
			m.help = true
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
	items := []paletteItem{
		{label: "Go to Overview", detail: "route", route: routeOverview},
		{label: "Go to Changes", detail: "route", route: routeChanges},
		{label: "Go to Fleet", detail: "route", route: routeFleet},
		{label: "Go to Health", detail: "route", route: routeHealth},
		{label: "Go to Services", detail: "route", route: routeServices},
		{label: "Go to Packages", detail: "route", route: routePackages},
		{label: "Go to Projects", detail: "route", route: routeProjects},
		{label: "Go to Docs", detail: "route", route: routeDocs},
	}
	if actionsOnly {
		items = nil
	}
	items = append(items,
		paletteItem{label: "Sync this machine from origin", detail: "LOCAL · safe inbound", action: "sync"},
		paletteItem{label: "Apply current configuration", detail: "LOCAL · no network", action: "apply"},
		paletteItem{label: "Open publish workflow", detail: "REPO · Changes", route: routeChanges},
		paletteItem{label: "Open rollout workflow", detail: "FLEET · Machines", route: routeFleet},
	)
	_ = current
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
		item := items[p.cursor]
		m.palette = nil
		if item.route != "" {
			m.route = item.route
			m.focus = shellFocusContent
			m.syncLegacySection()
		}
		switch item.action {
		case "sync":
			return m, requestAction(syncInboundRequest{Repo: m.repo})
		case "apply":
			return m, requestAction(applyRequest{Repo: m.repo})
		}
		return m, nil
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
	if m.w < 44 || m.h < 14 {
		v := tea.NewView(fmt.Sprintf("dots · %s\n\nTerminal too small — need at least 44×14 (current %d×%d).", routeLabel(m.route), m.w, m.h))
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	bodyW, bodyH := m.contentSize()
	rows := shellNavRows()
	m.hits = m.hits[:0]
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
		view = lipgloss.Place(m.w, m.h, lipgloss.Left, lipgloss.Top, view)
		view = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.renderPalette(view))
	}
	if m.help {
		view = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.renderHelp(view))
	}
	if m.act != nil {
		view = m.act.view(m.sp.View())
	}
	v := tea.NewView(view)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
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
		m.hits = append(m.hits, shellHit{x: 0, y: line, w: shellSidebarWidth, h: 1, kind: shellHitRoute, route: row.route})
		line++
	}
	for line <= h {
		b.WriteByte('\n')
		line++
	}
	return stySidebar.Width(shellSidebarWidth).Height(h).Render(b.String())
}

func (m *shellModel) renderRoute(w, h int) string {
	m.syncLegacySection()
	spin := m.sp.View()
	switch m.route {
	case routeDocs:
		return m.docs.view()
	case routeHealth:
		return m.doc.view(spin)
	case routeOverview:
		return contentColumn(w, h, paneHeader("Overview", "Triage", "what needs attention now", measureFor(w)), m.man.viewOverview(spin))
	case routeChanges:
		return contentColumn(w, h, paneHeader("Workspace", "Changes", m.man.dotfilesSummary(), measureFor(w)), m.man.viewDotfiles())
	case routeFleet:
		return contentColumn(w, h, paneHeader("Fleet", "Machines", fmt.Sprintf("%d configured hosts", len(m.man.machines)), measureFor(w)), m.man.viewMachines())
	case routeServices:
		return contentColumn(w, h, paneHeader("This machine", "Services", m.man.servicesSummary(), measureFor(w)), m.man.viewServices(spin))
	case routePackages:
		return contentColumn(w, h, paneHeader("This machine", "Packages", m.man.packagesSummary(), measureFor(w)), m.man.viewPackages(spin))
	case routeProjects:
		return contentColumn(w, h, paneHeader("This machine", "Projects", fmt.Sprintf("%d repositories under ~/Codes", len(m.man.projects)), measureFor(w)), m.man.viewProjects())
	default:
		return ""
	}
}

func (m *shellModel) renderFooter() string {
	left := "↑↓ move  tab focus  enter inspect  a actions  / palette  ? help"
	if m.sync.needsSync() {
		left += "  " + styPending.Render("! changes")
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
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cMac).Padding(1, 2).Width(min(78, m.w-8)).Render(b.String())
}

func (m *shellModel) renderHelp(base string) string {
	_ = base
	body := styTitle.Render("Keyboard and mouse") + "\n\n" +
		"↑↓ / j k   move in the focused region\n" +
		"Tab        move focus between sidebar and content\n" +
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
			cmds = append(cmds, fetchMachinesInfo())
		}
	}
	return tea.Batch(cmds...)
}
