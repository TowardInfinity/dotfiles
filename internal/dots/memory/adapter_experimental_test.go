package memory

import (
	"os"
	"path/filepath"
	"testing"
)

// TestChatGPTAdapterNeverFabricates locks in the deliberate non-implementation:
// Available() reports the real directory honestly (so `dots memory status`
// shows the tool as present rather than silently absent), but Scan() must
// always return nothing — there is no parse to write, because the
// conversation files are encrypted at rest. A future edit that starts
// returning fabricated sessions here would put invented summaries in the
// vault, which is worse than an absent note.
func TestChatGPTAdapterNeverFabricates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := ChatGPTAdapter{}
	if a.Available() {
		t.Fatal("Available() true before the app-support directory exists")
	}

	dir := filepath.Join(home, "Library", "Application Support", "com.openai.chat")
	if err := os.MkdirAll(filepath.Join(dir, "conversations-v3-fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "conversations-v3-fixture", "x.data"), []byte("whatever"), 0o600); err != nil {
		t.Fatal(err)
	}

	if !a.Available() {
		t.Fatal("Available() false after the app-support directory exists")
	}
	sessions, err := a.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if sessions != nil {
		t.Fatalf("Scan() = %v, want nil — there is no readable content to parse", sessions)
	}
}
