package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestMemoryMCPCLIWiring runs the real built binary, not runMemoryMCP
// in-process. mcp_test.go in internal/dots/memory exercises RunMCPServer
// directly and never touches runMemoryMCP's os.Stdin/os.Stdout/os.Stderr
// wiring at all — swap two of those three arguments and every one of those
// tests still passes while `dots memory mcp` is silently dead on a real
// stdio transport. This is the test that would catch that.
func TestMemoryMCPCLIWiring(t *testing.T) {
	bin := buildDots(t)

	cmd := exec.Command(bin, "memory", "mcp")
	cmd.Env = append(noRepoEnv(),
		"DOTS_MEMORY_VAULT="+t.TempDir(),
		"XDG_CACHE_HOME="+t.TempDir(),
	)
	cmd.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("dots memory mcp: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 1 || strings.TrimSpace(lines[0]) == "" {
		t.Fatalf("expected exactly one response line on stdout, got %d:\n%s", len(lines), stdout.String())
	}

	var resp struct {
		ID     int `json:"id"`
		Result struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("stdout line is not valid JSON: %v\nline: %s", err, lines[0])
	}
	if resp.Error != nil {
		t.Fatalf("initialize returned a JSON-RPC error: %s", resp.Error.Message)
	}
	if resp.ID != 1 {
		t.Errorf("id: got %d, want 1", resp.ID)
	}
	if resp.Result.ServerInfo.Name != "dots-memory" {
		t.Errorf("serverInfo.name: got %q, want %q", resp.Result.ServerInfo.Name, "dots-memory")
	}

	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr, got: %s", stderr.String())
	}
}
