package main

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Changes is intentionally a review surface, not a second git client. It
// shows exactly what can be selected and routes mutations through the shared
// operation registry. The screen never stages or commits while it renders.
type changeFile struct {
	Path   string
	Status string
	Staged bool
}

type changeCommit struct {
	Hash    string
	Subject string
}

type changesInfo struct {
	Branch   string
	Files    []changeFile
	Incoming []changeCommit
	Message  string
}

type changesInfoMsg struct{ info changesInfo }

type changesModel struct {
	repo      string
	branch    string
	files     []changeFile
	incoming  []changeCommit
	cursor    int
	selected  map[string]bool
	filter    string
	filtering bool
	detail    string
	loading   bool
	message   string
	width     int
	height    int
}

func newChangesModel(repo string) changesModel {
	return changesModel{repo: repo, selected: map[string]bool{}, loading: true}
}

func (m changesModel) Init() tea.Cmd { return fetchChangesInfo(m.repo) }

func fetchChangesInfo(repo string) tea.Cmd {
	return func() tea.Msg {
		info := changesInfo{}
		if repo == "" {
			info.Message = "no checkout found"
			return changesInfoMsg{info: info}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		branch, ok := currentBranch(repo)
		if !ok {
			info.Message = "detached HEAD — changes can be reviewed, but publish is unavailable"
		} else {
			info.Branch = branch
		}
		status, err := gitOutputContext(ctx, repo, "status", "--porcelain=v1")
		if err != nil {
			info.Message = "git status: " + err.Error()
		} else {
			info.Files = parseChangeFiles(status)
		}
		if branch != "" {
			if log, err := gitOutputContext(ctx, repo, "log", "--format=%h%x09%s", "HEAD..origin/"+branch); err == nil {
				for _, line := range nonemptyLines(log) {
					parts := strings.SplitN(line, "\t", 2)
					commit := changeCommit{Hash: parts[0]}
					if len(parts) == 2 {
						commit.Subject = parts[1]
					}
					info.Incoming = append(info.Incoming, commit)
				}
			}
		}
		return changesInfoMsg{info: info}
	}
}

func gitOutputContext(ctx context.Context, repo string, args ...string) (string, error) {
	argv := append([]string{"-C", repo}, args...)
	out, err := exec.CommandContext(ctx, "git", argv...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
	}
	return string(out), err
}

func parseChangeFiles(status string) []changeFile {
	var files []changeFile
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		code := line[:2]
		path := strings.TrimSpace(line[3:])
		if path == "" {
			continue
		}
		// Porcelain rename entries are "old -> new". Review the resulting
		// path while preserving the status code in the row.
		if i := strings.LastIndex(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		files = append(files, changeFile{Path: path, Status: code, Staged: code[0] != ' ' && code[0] != '?'})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func (m changesModel) resize(w, h int) changesModel {
	m.width, m.height = w, h
	if m.cursor >= len(m.visibleFiles()) {
		m.cursor = max(0, len(m.visibleFiles())-1)
	}
	return m
}

func (m changesModel) visibleFiles() []changeFile {
	q := strings.ToLower(strings.TrimSpace(m.filter))
	if q == "" {
		return m.files
	}
	visible := make([]changeFile, 0, len(m.files))
	for _, file := range m.files {
		if strings.Contains(strings.ToLower(file.Path+" "+file.Status), q) {
			visible = append(visible, file)
		}
	}
	return visible
}

func (m changesModel) update(msg tea.Msg) (changesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case changesInfoMsg:
		m.branch, m.files, m.incoming, m.message = msg.info.Branch, msg.info.Files, msg.info.Incoming, msg.info.Message
		m.loading = false
		if m.cursor >= len(m.visibleFiles()) {
			m.cursor = max(0, len(m.visibleFiles())-1)
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.filtering {
			switch msg.String() {
			case "esc":
				m.filtering, m.filter = false, ""
				m.cursor = 0
			case "enter":
				m.filtering = false
			default:
				if msg.String() == "backspace" && m.filter != "" {
					m.filter = m.filter[:len(m.filter)-1]
				} else if msg.Text != "" && msg.Mod == 0 {
					m.filter += msg.Text
				}
				m.cursor = 0
			}
			return m, nil
		}
		files := m.visibleFiles()
		switch msg.String() {
		case "r":
			m.loading, m.message = true, ""
			return m, m.Init()
		case "/":
			m.filtering = true
			return m, nil
		case "j", "down":
			if m.cursor < len(files)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			m.cursor = max(0, len(files)-1)
		case "space":
			if len(files) > 0 {
				path := files[m.cursor].Path
				m.selected[path] = !m.selected[path]
			}
		case "enter":
			if len(files) > 0 {
				m.detail = files[m.cursor].Path + "  " + files[m.cursor].Status
			}
		case "esc":
			m.detail = ""
		case "u":
			return m, requestAction(syncInboundRequest{Repo: m.repo})
		case "L":
			return m, requestAction(applyRequest{Repo: m.repo})
		case "p":
			paths := m.selectedPaths()
			if len(paths) == 0 && len(files) > 0 {
				paths = []string{files[m.cursor].Path}
			}
			if len(paths) == 0 {
				m.message = "select at least one changed path before publishing"
				return m, nil
			}
			return m, requestAction(publishRequest{Repo: m.repo, Paths: paths, Message: "changes: publish selected paths"})
		}
	}
	return m, nil
}

func (m changesModel) selectedPaths() []string {
	paths := make([]string, 0, len(m.selected))
	for path, selected := range m.selected {
		if selected {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func (m changesModel) view(w, h int) string {
	files := m.visibleFiles()
	rows := []string{}
	if m.filtering {
		rows = append(rows, styFilter.Render("/"+m.filter))
	}
	if m.loading {
		rows = append(rows, styPending.Render("⣾ checking repository changes…"))
	} else if m.message != "" {
		rows = append(rows, styPending.Render("! "+m.message))
	}
	rows = append(rows, styMuted.Render("LOCAL CHANGES"))
	if len(files) == 0 {
		rows = append(rows, styMuted.Render("  clean — no local files need review"))
	} else {
		for i, file := range files {
			mark := "○"
			if m.selected[file.Path] {
				mark = "●"
			}
			status := file.Status
			if file.Staged {
				status = "staged " + status
			}
			line := fmt.Sprintf("%s %-8s %s", mark, status, file.Path)
			if i == m.cursor {
				line = styItemOn.Render(padRight(truncate(line, max(1, w-4)), max(1, w-4)))
			} else {
				line = styItem.Render(truncate(line, max(1, w-4)))
			}
			rows = append(rows, line)
		}
	}
	rows = append(rows, "", styMuted.Render("INCOMING FROM ORIGIN"))
	if len(m.incoming) == 0 {
		rows = append(rows, styMuted.Render("  no incoming commits (fetch with r)"))
	} else {
		for _, commit := range m.incoming {
			rows = append(rows, styValue.Render("  "+commit.Hash+" ")+styMuted.Render(truncate(commit.Subject, max(1, w-12))))
		}
	}
	if m.detail != "" {
		rows = append(rows, "", styTitle.Render("INSPECTOR"), styValue.Render("  "+truncate(m.detail, max(1, w-4))))
	}
	return strings.Join(rows, "\n")
}
