package memory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mcpRequest(id, method, params string) string {
	return `{"jsonrpc":"2.0","id":` + id + `,"method":` + jsonString(method) + `,"params":` + params + "}\n"
}

func mcpNotification(method string) string {
	return `{"jsonrpc":"2.0","method":` + jsonString(method) + "}\n"
}

func mcpToolCall(id, name, args string) string {
	return mcpRequest(id, "tools/call", `{"name":`+jsonString(name)+`,"arguments":`+args+`}`)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func runMCPTest(t *testing.T, input string) ([]map[string]any, string) {
	t.Helper()
	var out, diag bytes.Buffer
	if err := RunMCPServer(strings.NewReader(input), &out, &diag, "test-version"); err != nil {
		t.Fatalf("RunMCPServer: %v", err)
	}

	raw := strings.TrimSpace(out.String())
	if raw == "" {
		t.Fatal("RunMCPServer produced no responses")
	}
	lines := strings.Split(raw, "\n")
	responses := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("invalid response line %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	return responses, diag.String()
}

func saveMCPIndex(t *testing.T, sessions []Session) {
	t.Helper()
	if err := SaveIndex(Index{Schema: indexSchema, Sessions: sessions}); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}
}

func responseResult(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result: %#v", response)
	}
	return result
}

func responseErrorCode(t *testing.T, response map[string]any) int {
	t.Helper()
	err, ok := response["error"].(map[string]any)
	if !ok {
		t.Fatalf("response has no JSON-RPC error: %#v", response)
	}
	code, ok := err["code"].(float64)
	if !ok {
		t.Fatalf("JSON-RPC error has no numeric code: %#v", err)
	}
	return int(code)
}

func toolText(t *testing.T, response map[string]any) (string, bool) {
	t.Helper()
	result := responseResult(t, response)
	content, ok := result["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("unexpected tool content: %#v", result["content"])
	}
	item, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected tool content item: %#v", content[0])
	}
	text, ok := item["text"].(string)
	if !ok {
		t.Fatalf("tool content has no text: %#v", item)
	}
	isError, _ := result["isError"].(bool)
	return text, isError
}

func decodeToolText(t *testing.T, response map[string]any, dst any) bool {
	t.Helper()
	text, isError := toolText(t, response)
	if err := json.Unmarshal([]byte(text), dst); err != nil {
		t.Fatalf("tool text is not JSON: %v\n%s", err, text)
	}
	return isError
}

func TestMCPInitialize(t *testing.T) {
	t.Setenv("DOTS_MEMORY_VAULT", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	responses, _ := runMCPTest(t,
		mcpRequest("1", "initialize", `{}`)+
			mcpNotification("notifications/initialized"))
	if len(responses) != 1 {
		t.Fatalf("got %d stdout lines, want exactly 1", len(responses))
	}
	result := responseResult(t, responses[0])
	if got, _ := result["protocolVersion"].(string); got != mcpProtocolVersion {
		t.Errorf("protocolVersion = %q, want %q", got, mcpProtocolVersion)
	}
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("initialize result has no serverInfo: %#v", result)
	}
	if got, _ := serverInfo["name"].(string); got != mcpServerName {
		t.Errorf("serverInfo.name = %q, want %q", got, mcpServerName)
	}
	if got, _ := serverInfo["version"].(string); got != "test-version" {
		t.Errorf("serverInfo.version = %q, want test-version", got)
	}
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("initialize result has no capabilities: %#v", result)
	}
	if _, ok := capabilities["tools"].(map[string]any); !ok {
		t.Errorf("capabilities.tools is missing or not an object: %#v", capabilities["tools"])
	}
}

func TestMCPProtocolGating(t *testing.T) {
	t.Setenv("DOTS_MEMORY_VAULT", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	responses, _ := runMCPTest(t,
		mcpRequest("1", "tools/list", `{}`)+
			mcpRequest("2", "initialize", `{}`)+
			mcpRequest("3", "tools/list", `{}`))
	if len(responses) != 3 {
		t.Fatalf("got %d responses, want 3", len(responses))
	}
	if got := responseErrorCode(t, responses[0]); got != codeInvalidRequest {
		t.Errorf("pre-initialize tools/list error code = %d, want %d", got, codeInvalidRequest)
	}
	if _, ok := responses[0]["result"]; ok {
		t.Error("pre-initialize request unexpectedly returned a result")
	}
	if _, ok := responses[2]["result"]; !ok {
		t.Fatalf("tools/list after initialize has no result: %#v", responses[2])
	}
}

func TestMCPToolsList(t *testing.T) {
	t.Setenv("DOTS_MEMORY_VAULT", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	responses, _ := runMCPTest(t,
		mcpRequest("1", "initialize", `{}`)+mcpRequest("2", "tools/list", `{}`))
	tools, ok := responseResult(t, responses[1])["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list result has no tools array: %#v", responses[1])
	}
	want := map[string]bool{
		"memory_search":   true,
		"memory_timeline": true,
		"memory_get":      true,
		"memory_remember": true,
	}
	if len(tools) != len(want) {
		t.Fatalf("tools/list returned %d tools, want %d: %#v", len(tools), len(want), tools)
	}
	seen := map[string]bool{}
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("tool is not an object: %#v", raw)
		}
		name, _ := tool["name"].(string)
		if !want[name] {
			t.Errorf("unexpected tool %q", name)
		}
		seen[name] = true
		if description, _ := tool["description"].(string); strings.TrimSpace(description) == "" {
			t.Errorf("tool %q has empty description", name)
		}
		if schema, ok := tool["inputSchema"].(map[string]any); !ok || len(schema) == 0 {
			t.Errorf("tool %q has no inputSchema object: %#v", name, tool["inputSchema"])
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("tools/list omitted %q", name)
		}
	}
}

func TestMCPSearch(t *testing.T) {
	t.Setenv("DOTS_MEMORY_VAULT", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	longSummary := "summaryneedle " + strings.Repeat("distilled body ", 30)
	saveMCPIndex(t, []Session{
		{ID: "title-hit", Tool: "claude-code", Project: "repo/one", Title: "AuthFix title", Summary: "ordinary summary", Updated: base.Add(time.Hour)},
		{ID: "summary-hit", Tool: "codex", Project: "repo/one", Title: "A different title", Summary: longSummary, Updated: base.Add(2 * time.Hour)},
		{ID: "scope-one", Tool: "grok", Project: "repo/one", Title: "shared project work", Updated: base.Add(3 * time.Hour)},
		{ID: "scope-two", Tool: "grok", Project: "repo/two", Title: "shared project work", Updated: base.Add(4 * time.Hour)},
	})

	responses, _ := runMCPTest(t,
		mcpRequest("1", "initialize", `{}`)+
			mcpToolCall("2", "memory_search", `{"query":"AuthFix"}`)+
			mcpToolCall("3", "memory_search", `{"query":"summaryneedle"}`)+
			mcpToolCall("4", "memory_search", `{"query":"shared","project":"repo/one"}`)+
			mcpToolCall("5", "memory_search", `{"query":"   "}`))
	if len(responses) != 5 {
		t.Fatalf("got %d responses, want 5", len(responses))
	}

	var titleHits []map[string]any
	if isError := decodeToolText(t, responses[1], &titleHits); isError || len(titleHits) != 1 {
		t.Fatalf("title search got isError=%v, rows=%d", isError, len(titleHits))
	}
	if titleHits[0]["id"] != "title-hit" || titleHits[0]["title"] != "AuthFix title" {
		t.Errorf("title search returned %#v", titleHits[0])
	}
	if hits, _ := titleHits[0]["hits"].([]any); len(hits) != 1 || hits[0] != "title" {
		t.Errorf("title search hits = %#v, want [title]", titleHits[0]["hits"])
	}

	var summaryHits []map[string]any
	if isError := decodeToolText(t, responses[2], &summaryHits); isError || len(summaryHits) != 1 {
		t.Fatalf("summary search got isError=%v, rows=%d", isError, len(summaryHits))
	}
	snippet, _ := summaryHits[0]["snippet"].(string)
	if summaryHits[0]["id"] != "summary-hit" || !strings.Contains(snippet, "summaryneedle") {
		t.Errorf("summary search returned %#v", summaryHits[0])
	}
	if snippet == longSummary || len([]rune(snippet)) > 201 || strings.Contains(snippet, strings.TrimSpace(longSummary)) {
		t.Errorf("search returned full summary instead of a short snippet: rune length=%d", len([]rune(snippet)))
	}

	var scoped []map[string]any
	if isError := decodeToolText(t, responses[3], &scoped); isError || len(scoped) != 1 {
		t.Fatalf("scoped search got isError=%v, rows=%d", isError, len(scoped))
	}
	if scoped[0]["id"] != "scope-one" || scoped[0]["project"] != "repo/one" {
		t.Errorf("scoped search returned %#v", scoped[0])
	}

	if _, isError := toolText(t, responses[4]); !isError {
		t.Errorf("blank query should be a tool-level error: %#v", responses[4])
	}
	if _, ok := responses[4]["error"]; ok {
		t.Error("blank query should not be a JSON-RPC error")
	}
}

func TestMCPGet(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("DOTS_MEMORY_VAULT", vault)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	notePath := filepath.Join(vault, "full-note.md")
	noteBody := "# Distilled note\n\nThe complete note body."
	if err := os.WriteFile(notePath, []byte(noteBody), 0o600); err != nil {
		t.Fatal(err)
	}
	saveMCPIndex(t, []Session{{
		ID: "get-me", Tool: "codex", Project: "repo/get", Title: "Get this", Summary: "indexed summary",
		VaultNote: notePath, Updated: time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC),
	}})

	responses, _ := runMCPTest(t,
		mcpRequest("1", "initialize", `{}`)+
			mcpToolCall("2", "memory_get", `{"tool":"codex","id":"get-me"}`)+
			mcpToolCall("3", "memory_get", `{"tool":"codex","id":"missing"}`))
	if len(responses) != 3 {
		t.Fatalf("got %d responses, want 3", len(responses))
	}
	var got map[string]any
	if isError := decodeToolText(t, responses[1], &got); isError {
		t.Fatalf("found memory_get returned a tool error: %#v", responses[1])
	}
	for field, want := range map[string]string{
		"id": "get-me", "tool": "codex", "summary": "indexed summary", "note_body": noteBody,
	} {
		if got[field] != want {
			t.Errorf("memory_get %s = %#v, want %q", field, got[field], want)
		}
	}
	if _, isError := toolText(t, responses[2]); !isError {
		t.Error("missing memory_get should be a tool-level error")
	}
	if _, ok := responses[2]["error"]; ok {
		t.Error("missing memory_get should not be a JSON-RPC error")
	}
}

func TestMCPTimeline(t *testing.T) {
	t.Setenv("DOTS_MEMORY_VAULT", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	saveMCPIndex(t, []Session{
		{ID: "old-one", Tool: "claude-code", Project: "repo/one", Title: "old one", Updated: base.Add(time.Hour)},
		{ID: "new-one", Tool: "claude-code", Project: "repo/one", Title: "new one", Updated: base.Add(3 * time.Hour)},
		{ID: "new-two", Tool: "codex", Project: "repo/two", Title: "new two", Updated: base.Add(4 * time.Hour)},
		{ID: "mid-two", Tool: "codex", Project: "repo/two", Title: "mid two", Updated: base.Add(2 * time.Hour)},
	})

	responses, _ := runMCPTest(t,
		mcpRequest("1", "initialize", `{}`)+
			mcpToolCall("2", "memory_timeline", `{"project":"repo/one","limit":2}`)+
			mcpToolCall("3", "memory_timeline", `{"limit":3}`))
	if len(responses) != 3 {
		t.Fatalf("got %d responses, want 3", len(responses))
	}
	var scoped []map[string]any
	if isError := decodeToolText(t, responses[1], &scoped); isError || len(scoped) != 2 {
		t.Fatalf("scoped timeline got isError=%v, rows=%d", isError, len(scoped))
	}
	if scoped[0]["id"] != "new-one" || scoped[1]["id"] != "old-one" {
		t.Errorf("scoped timeline order = %#v", scoped)
	}
	for _, row := range scoped {
		if row["project"] != "repo/one" {
			t.Errorf("scoped timeline leaked project: %#v", row)
		}
	}

	var all []map[string]any
	if isError := decodeToolText(t, responses[2], &all); isError || len(all) != 3 {
		t.Fatalf("all-project timeline got isError=%v, rows=%d", isError, len(all))
	}
	wantIDs := []string{"new-two", "new-one", "mid-two"}
	for i, want := range wantIDs {
		if all[i]["id"] != want {
			t.Errorf("all-project timeline row %d = %#v, want id %q", i, all[i], want)
		}
	}
}

func TestMCPRemember(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("DOTS_MEMORY_VAULT", vault)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	secret := "sk-ant-abcdefghijklmnopqrst"
	rememberText := "Remember release secret " + secret + " for launch"
	responses, _ := runMCPTest(t,
		mcpRequest("1", "initialize", `{}`)+
			mcpToolCall("2", "memory_remember", `{"project":"repo/remember","text":`+jsonString(rememberText)+`}`))
	if len(responses) != 2 {
		t.Fatalf("got %d responses, want 2", len(responses))
	}
	rememberResponse, isError := toolText(t, responses[1])
	if isError {
		t.Fatalf("memory_remember returned a tool error: %s", rememberResponse)
	}
	if strings.Contains(rememberResponse, secret) || !strings.Contains(rememberResponse, "[redacted]") {
		t.Errorf("remember response was not redacted: %q", rememberResponse)
	}

	idx := LoadIndex()
	if len(idx.Sessions) != 1 {
		t.Fatalf("index has %d sessions, want 1", len(idx.Sessions))
	}
	sess := idx.Sessions[0]
	if sess.Tool != "manual" {
		t.Errorf("remembered session tool = %q, want manual", sess.Tool)
	}
	if sess.Project != ProjectKey("repo/remember") {
		t.Errorf("remembered project = %q", sess.Project)
	}
	if strings.Contains(sess.Title, secret) || !strings.Contains(sess.Title, "[redacted]") {
		t.Errorf("remembered title was not redacted: %q", sess.Title)
	}
	if strings.Contains(sess.Summary, secret) || !strings.Contains(sess.Summary, "[redacted]") {
		t.Errorf("remembered summary was not redacted: %q", sess.Summary)
	}
	if sess.VaultNote == "" {
		t.Fatal("remembered session has no vault note")
	}
	if strings.Contains(filepath.Base(sess.VaultNote), secret) || !strings.Contains(filepath.Base(sess.VaultNote), "[redacted]") {
		t.Errorf("vault note filename was not redacted: %q", filepath.Base(sess.VaultNote))
	}
	note, err := os.ReadFile(sess.VaultNote)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(note), secret) || !strings.Contains(string(note), "[redacted]") {
		t.Errorf("vault note was not redacted: %s", note)
	}

	followup, _ := runMCPTest(t,
		mcpRequest("3", "initialize", `{}`)+
			mcpToolCall("4", "memory_get", `{"tool":"manual","id":`+jsonString(sess.ID)+`}`)+
			mcpToolCall("5", "memory_search", `{"query":"launch","project":"repo/remember"}`)+
			mcpToolCall("6", "memory_remember", `{"project":"repo/remember"}`))
	if len(followup) != 4 {
		t.Fatalf("got %d follow-up responses, want 4", len(followup))
	}
	var got map[string]any
	if isError := decodeToolText(t, followup[1], &got); isError || got["tool"] != "manual" {
		t.Errorf("follow-up memory_get tool = %#v, isError=%v", got["tool"], isError)
	}
	var found []map[string]any
	if isError := decodeToolText(t, followup[2], &found); isError || len(found) != 1 || found[0]["id"] != sess.ID {
		t.Errorf("follow-up memory_search = %#v, isError=%v", found, isError)
	}
	if _, isError := toolText(t, followup[3]); !isError {
		t.Error("memory_remember without text should be a tool-level error")
	}
	if _, ok := followup[3]["error"]; ok {
		t.Error("memory_remember without text should not be a JSON-RPC error")
	}
}

func TestMCPUnknownMethod(t *testing.T) {
	t.Setenv("DOTS_MEMORY_VAULT", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	responses, _ := runMCPTest(t,
		mcpRequest("1", "initialize", `{}`)+
			mcpRequest("2", "not/a/real/method", `{}`))
	if len(responses) != 2 {
		t.Fatalf("got %d responses, want 2", len(responses))
	}
	if got := responseErrorCode(t, responses[1]); got != codeMethodNotFound {
		t.Errorf("unknown method error code = %d, want %d", got, codeMethodNotFound)
	}
}

func TestMCPUnknownTool(t *testing.T) {
	t.Setenv("DOTS_MEMORY_VAULT", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	responses, _ := runMCPTest(t,
		mcpRequest("1", "initialize", `{}`)+
			mcpToolCall("2", "tools/list", `{}`))
	if len(responses) != 2 {
		t.Fatalf("got %d responses, want 2", len(responses))
	}
	if got := responseErrorCode(t, responses[1]); got != codeInvalidParams {
		t.Errorf("unknown tool error code = %d, want %d", got, codeInvalidParams)
	}
	if _, ok := responses[1]["result"]; ok {
		t.Error("unknown tool returned a result instead of a JSON-RPC error")
	}
}

func TestMCPMalformedLineContinues(t *testing.T) {
	t.Setenv("DOTS_MEMORY_VAULT", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	responses, diag := runMCPTest(t,
		mcpRequest("1", "initialize", "{}")+
			"this is not JSON\n"+
			mcpRequest("2", "tools/list", `{}`))
	if len(responses) != 3 {
		t.Fatalf("got %d responses, want 3", len(responses))
	}
	if responses[1]["id"] != nil {
		t.Errorf("parse error id = %#v, want null", responses[1]["id"])
	}
	if got := responseErrorCode(t, responses[1]); got != codeParseError {
		t.Errorf("parse error code = %d, want %d", got, codeParseError)
	}
	if !strings.Contains(diag, "parse error") {
		t.Errorf("diagnostic output does not report parse error: %q", diag)
	}
	if _, ok := responses[2]["result"]; !ok {
		t.Errorf("valid request after malformed line did not run: %#v", responses[2])
	}
}

func TestMCPStringIDRoundTrips(t *testing.T) {
	t.Setenv("DOTS_MEMORY_VAULT", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	responses, _ := runMCPTest(t, mcpRequest(`"abc-123"`, "initialize", `{}`))
	if len(responses) != 1 {
		t.Fatalf("got %d responses, want 1", len(responses))
	}
	if got, ok := responses[0]["id"].(string); !ok || got != "abc-123" {
		t.Errorf("response id = %#v (%T), want JSON string abc-123", responses[0]["id"], responses[0]["id"])
	}
}
