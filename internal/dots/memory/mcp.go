package memory

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"
)

// dots memory mcp is the cross-tool interface: one stdio JSON-RPC server that
// Codex, Grok, Cursor and any other MCP-capable tool can register once and
// query directly, instead of each reimplementing "read the index".
//
// This is hand-rolled over encoding/json rather than built on
// github.com/modelcontextprotocol/go-sdk. That was the plan's stated fallback,
// not the starting point — the SDK was the first choice, but the Go module
// proxy was unreachable from this environment (two independent timeouts
// fetching it), and the surface actually needed — initialize, an
// initialized notification, tools/list, tools/call — is small enough that
// waiting on network access wasn't worth it. Swapping in the SDK later is a
// contained change: everything outside this file talks to the memory package
// directly, not to the wire format.
//
// The framing is newline-delimited JSON-RPC 2.0 over stdio — one compact
// object per line, in both directions — which is what MCP's stdio transport
// specifies. This is NOT the Content-Length-header framing LSP uses; a server
// that assumes that would never see its first request.

const (
	mcpProtocolVersion = "2024-11-05"
	mcpServerName      = "dots-memory"
)

// RunMCPServer reads one JSON-RPC request or notification per line from in,
// and writes one JSON-RPC response per line to out for every request (never
// for a notification — replying to a notification is itself a protocol
// violation some clients reject). It returns nil on a clean EOF, which is how
// every MCP client ends a session: it just closes stdin.
//
// diag receives one line per unexpected condition (bad JSON, unknown method,
// a tool call that errored) and nothing else. It exists because stdout is the
// transport here: a single stray fmt.Println anywhere on this path corrupts
// the stream for the rest of the session, so every diagnostic has to have
// somewhere else to go.
func RunMCPServer(in io.Reader, out io.Writer, diag io.Writer, serverVersion string) error {
	logger := log.New(diag, "dots-memory-mcp: ", log.LstdFlags)
	w := bufio.NewWriter(out)
	scanner := bufio.NewScanner(in)
	// A rollout-length transcript pasted into memory_remember, or a client
	// that batches several tool results, can exceed the default 64 KiB line
	// buffer. 8 MiB comfortably covers anything this package would ever
	// redact and summarize in one call.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	srv := &mcpSession{logger: logger, version: serverVersion}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		resp := srv.handleLine(line)
		if resp == nil {
			// A notification: no id, no response, by protocol.
			continue
		}
		b, err := json.Marshal(resp)
		if err != nil {
			logger.Printf("marshal response: %v", err)
			continue
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return err
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// mcpSession holds the small amount of state a stdio session needs: nothing
// about the memory itself (that is always read fresh from the index), just
// what initialize negotiated.
type mcpSession struct {
	logger      *log.Logger
	version     string
	initialized bool
}

// JSON-RPC 2.0 envelope. id is carried as raw JSON rather than decoded, so a
// string id and a numeric id both round-trip unchanged — the spec allows
// either and a server that silently coerces one to the other breaks a client
// that sent the other kind.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// handleLine parses and dispatches one line. It returns nil for a
// notification (no id) and for a line so malformed that no id could even be
// recovered — the JSON-RPC spec permits replying to an unparseable request
// with id: null, but in practice a line that fails to parse as an object at
// all is far more often transport corruption than a client waiting on a
// response, and echoing noise back down a corrupted stream compounds the
// problem rather than fixing it.
func (s *mcpSession) handleLine(line string) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		s.logger.Printf("parse error: %v", err)
		return &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &rpcError{Code: codeParseError, Message: "parse error"}}
	}

	isNotification := len(req.ID) == 0
	result, rerr := s.dispatch(req)

	if isNotification {
		if rerr != nil {
			s.logger.Printf("notification %q: %v", req.Method, rerr)
		}
		return nil
	}
	if rerr != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rerr}
	}
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *mcpSession) dispatch(req rpcRequest) (any, *rpcError) {
	if req.Method == "" {
		return nil, &rpcError{Code: codeInvalidRequest, Message: "missing method"}
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.Params)
	case "notifications/initialized":
		// Already true from handleInitialize; the notification just confirms
		// the client agrees the handshake is done.
		s.initialized = true
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	}

	// Everything past this point is a real capability, and a client that has
	// not completed initialize has no business calling one yet — the
	// serverInfo and protocolVersion it needs to interpret the results
	// haven't been sent. handleInitialize sets s.initialized on the request
	// itself rather than waiting for notifications/initialized, since not
	// every client sends that notification promptly.
	if !s.initialized {
		return nil, &rpcError{Code: codeInvalidRequest, Message: "call initialize before " + req.Method}
	}

	switch req.Method {
	case "tools/list":
		return map[string]any{"tools": mcpToolDefs}, nil
	case "tools/call":
		return s.handleToolCall(req.Params)
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}
}

func (s *mcpSession) handleInitialize(params json.RawMessage) (any, *rpcError) {
	// The client's requested protocolVersion is not echoed back: this server
	// implements exactly one version, and stating that plainly is more useful
	// to the client than agreeing to a version it does not actually speak.
	_ = params
	s.initialized = true
	return map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    mcpServerName,
			"version": s.version,
		},
	}, nil
}

// mcpToolDef is the tools/list shape: name, a description a model reads to
// decide when to call it, and a JSON Schema for its arguments.
type mcpToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

var mcpToolDefs = []mcpToolDef{
	{
		Name: "memory_search",
		Description: "Search this machine's cross-tool AI session memory by keyword. " +
			"Matches session titles and summaries, case-insensitively. Returns short " +
			"records — use memory_get with the tool and id from a result to read a " +
			"session's full distilled note.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":   map[string]any{"type": "string", "description": "term to search for"},
				"project": map[string]any{"type": "string", "description": "limit to one project key, e.g. github.com/owner/repo"},
				"limit":   map[string]any{"type": "integer", "description": "max results (default 20)"},
			},
			"required": []string{"query"},
		},
	},
	{
		Name: "memory_timeline",
		Description: "List recent AI sessions, most recent first. Give a project key to " +
			"scope it; omit to see recent activity across every project on this machine.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project": map[string]any{"type": "string", "description": "project key; omit for all projects"},
				"limit":   map[string]any{"type": "integer", "description": "max sessions (default 10)"},
			},
		},
	},
	{
		Name:        "memory_get",
		Description: "Fetch one session's full record, including its complete distilled note if one was written to the vault.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool": map[string]any{"type": "string", "description": "tool that produced the session, e.g. claude-code, codex, grok, manual"},
				"id":   map[string]any{"type": "string", "description": "session id, as returned by memory_search or memory_timeline"},
			},
			"required": []string{"tool", "id"},
		},
	},
	{
		Name: "memory_remember",
		Description: "Save something worth remembering outside of any tool's own session — a " +
			"decision, a fact, a piece of context — so it surfaces later in recall and search " +
			"for this project. Secrets are redacted before anything is written.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text":    map[string]any{"type": "string", "description": "what to remember"},
				"project": map[string]any{"type": "string", "description": "project key; omit to resolve from dir"},
				"dir":     map[string]any{"type": "string", "description": "directory to resolve the project from, if project is not given"},
				"title":   map[string]any{"type": "string", "description": "short title; derived from text if omitted"},
			},
			"required": []string{"text"},
		},
	},
}

// toolCallParams is tools/call's params shape.
type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// toolResult is what a tool/call returns on the wire. isError is how MCP
// signals a tool-level failure (bad arguments, nothing found) without going
// through the JSON-RPC error path, which is reserved for protocol failures —
// unknown method, malformed request — rather than "the tool ran and had bad
// news."
type toolResult struct {
	Content []map[string]any `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

func textResult(s string) toolResult {
	return toolResult{Content: []map[string]any{{"type": "text", "text": s}}}
}

func errResult(format string, a ...any) toolResult {
	return toolResult{Content: []map[string]any{{"type": "text", "text": fmt.Sprintf(format, a...)}}, IsError: true}
}

func (s *mcpSession) handleToolCall(params json.RawMessage) (any, *rpcError) {
	var p toolCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid tools/call params: " + err.Error()}
	}

	switch p.Name {
	case "memory_search":
		return mcpMemorySearch(p.Arguments), nil
	case "memory_timeline":
		return mcpMemoryTimeline(p.Arguments), nil
	case "memory_get":
		return mcpMemoryGet(p.Arguments), nil
	case "memory_remember":
		return mcpMemoryRemember(p.Arguments), nil
	default:
		// Unknown tool name is a protocol-level problem — the client asked to
		// call something tools/list never advertised — not a tool-level one.
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
	}
}

// mcpSearchHit is deliberately smaller than Session. memory_search's default
// limit is 20, and returning full records — full Summary bodies included —
// would put up to 20 distilled notes in one tool result, which is exactly the
// uncapped-injection problem the whole recall/search split exists to avoid.
// A snippet is enough to judge relevance; memory_get is the second hop for
// the full body, paid only for the session actually worth reading.
type mcpSearchHit struct {
	Tool      string   `json:"tool"`
	ID        string   `json:"id"`
	Project   string   `json:"project"`
	Title     string   `json:"title"`
	Date      string   `json:"date"`
	VaultNote string   `json:"vault_note,omitempty"`
	Snippet   string   `json:"snippet,omitempty"`
	Hits      []string `json:"hits"`
}

func mcpMemorySearch(args json.RawMessage) toolResult {
	var in struct {
		Query   string `json:"query"`
		Project string `json:"project"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errResult("invalid arguments: %v", err)
	}
	if strings.TrimSpace(in.Query) == "" {
		return errResult("query is required")
	}

	results := Search(LoadIndex(), SearchOptions{
		Query:   in.Query,
		Project: ProjectKey(in.Project),
		Limit:   in.Limit,
	})
	if len(results) == 0 {
		return textResult("no matches")
	}

	hits := make([]mcpSearchHit, 0, len(results))
	for _, r := range results {
		s := r.Session
		hits = append(hits, mcpSearchHit{
			Tool: s.Tool, ID: s.ID, Project: string(s.Project),
			Title: s.Title, Date: s.Updated.Format("2006-01-02"),
			VaultNote: s.VaultNote, Snippet: snippet(s.Summary, 200), Hits: r.Hits,
		})
	}
	return textResult(mustJSON(hits))
}

func mcpMemoryTimeline(args json.RawMessage) toolResult {
	var in struct {
		Project string `json:"project"`
		Limit   int    `json:"limit"`
	}
	// args is empty ("{}" or absent) for a call with no arguments at all;
	// json.Unmarshal on a zero-length RawMessage would error, so treat that
	// as "no filters" rather than a malformed call.
	if len(strings.TrimSpace(string(args))) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return errResult("invalid arguments: %v", err)
		}
	}
	if in.Limit <= 0 {
		in.Limit = 10
	}

	idx := LoadIndex()
	var sessions []Session
	if in.Project != "" {
		sessions = Recent(idx.Sessions, ProjectKey(in.Project), in.Limit)
	} else {
		sessions = recentAcrossProjects(idx.Sessions, in.Limit)
	}
	if len(sessions) == 0 {
		return textResult("no sessions indexed" + projectSuffix(in.Project))
	}

	type row struct {
		Tool    string `json:"tool"`
		ID      string `json:"id"`
		Project string `json:"project"`
		Title   string `json:"title"`
		Date    string `json:"date"`
	}
	rows := make([]row, 0, len(sessions))
	for _, s := range sessions {
		rows = append(rows, row{Tool: s.Tool, ID: s.ID, Project: string(s.Project),
			Title: s.Title, Date: s.Updated.Format("2006-01-02 15:04")})
	}
	return textResult(mustJSON(rows))
}

func projectSuffix(project string) string {
	if project == "" {
		return ""
	}
	return " for project " + project
}

// recentAcrossProjects sorts by Updated descending with no project filter —
// Recent does the same job scoped to one project, and duplicating the sort
// here rather than adding a project-optional parameter to Recent keeps that
// function's contract (always scoped) simple for its other callers.
func recentAcrossProjects(all []Session, n int) []Session {
	out := make([]Session, len(all))
	copy(out, all)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Updated.After(out[j].Updated)
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

func mcpMemoryGet(args json.RawMessage) toolResult {
	var in struct {
		Tool string `json:"tool"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errResult("invalid arguments: %v", err)
	}
	if in.Tool == "" || in.ID == "" {
		return errResult("both tool and id are required")
	}

	idx := LoadIndex()
	sess, ok := idx.Find(in.Tool, in.ID)
	if !ok {
		return errResult("no session found for tool %q id %q", in.Tool, in.ID)
	}

	out := struct {
		Session
		NoteBody string `json:"note_body,omitempty"`
	}{Session: sess}

	if sess.VaultNote != "" {
		if b, err := os.ReadFile(sess.VaultNote); err == nil {
			out.NoteBody = string(b)
		}
		// A missing or unreadable note is not an error worth failing the call
		// over: the index record itself is still a complete answer.
	}
	return textResult(mustJSON(out))
}

func mcpMemoryRemember(args json.RawMessage) toolResult {
	var in struct {
		Text    string `json:"text"`
		Project string `json:"project"`
		Dir     string `json:"dir"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errResult("invalid arguments: %v", err)
	}
	if strings.TrimSpace(in.Text) == "" {
		return errResult("text is required")
	}

	project := ProjectKey(in.Project)
	if project == "" {
		if in.Dir != "" {
			project, _, _ = ResolveProject(in.Dir)
		} else {
			project = Unscoped
		}
	}

	// Redact before anything else touches this text, same as every other
	// entry path into the vault — Scrub is the only way to produce a Clean,
	// and the compiler is what enforces that ordering here, not a comment.
	red := NewRedactor(DefaultRedactionsPath())
	clean := red.Scrub(in.Text)

	title := in.Title
	if title == "" {
		title = TitleLine(string(clean))
	} else {
		title = TitleLine(string(red.Scrub(title)))
	}
	if title == "" {
		title = "Remembered note"
	}

	now := time.Now()
	sess := Session{
		ID: "remember-" + randomHex(6),
		// "manual" is a tool name no adapter owns. Index.Upsert keys on
		// (Tool, ID); if this reused e.g. "claude-code", the next ScanAll
		// could overwrite it the moment a real session happened to land on
		// the same id space.
		Tool:    "manual",
		Project: project,
		Title:   title,
		Summary: string(clean),
		Started: now,
		Updated: now,
		// A deliberate memory_remember call is not the low-signal "continue"
		// traffic worthListing filters out of the auto-injected digest — it
		// is explicit intent to be remembered, so it should be able to earn
		// a spot there like any other real session. This is a real coupling
		// to digest.go's worthListing, which currently requires Messages >=
		// 4: raise that threshold later and every remembered note silently
		// stops qualifying for the digest unless this constant moves with it.
		Messages: 4,
	}

	if vault := VaultDir(); vault != "" {
		if path, err := WriteNote(vault, sess, clean); err == nil {
			sess.VaultNote = path
		}
		// A write failure here still leaves the index entry, which is
		// searchable — degrading to index-only rather than failing the whole
		// call matches how capture treats a missing vault everywhere else.
	}

	idx := LoadIndex()
	idx.Upsert(sess)
	if err := SaveIndex(idx); err != nil {
		return errResult("remembered, but failed to save the index: %v", err)
	}

	msg := fmt.Sprintf("remembered under project %q: %s", project, title)
	if sess.VaultNote != "" {
		msg += "\nnote: " + sess.VaultNote
	}
	return textResult(msg)
}

func snippet(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		// v is always one of this file's own plain structs; a marshal
		// failure here would be a programming error, not a runtime one.
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(b)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is effectively unheard of; fall back to
		// something still unique enough for a single-process id, rather than
		// letting a manual-remember call fail over it.
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))[:n*2]
	}
	return hex.EncodeToString(b)
}
