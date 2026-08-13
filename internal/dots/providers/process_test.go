package providers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
)

func TestProcessStreamsOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Command("", "sh", "-c", "printf out; printf err >&2").Run(context.Background(), ops.IO{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != "out" {
		t.Errorf("stdout = %q, want out", got)
	}
	if got := stderr.String(); got != "err" {
		t.Errorf("stderr = %q, want err", got)
	}
}

func TestProcessCancellationStopsChildProcesses(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-survived")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The background child inherits the command's process group. Without group
	// termination, cancelling the shell kills only the parent and this child
	// writes the marker after the operation has supposedly stopped.
	done := make(chan error, 1)
	go func() {
		done <- Command("", "sh", "-c", "sleep 1; : > \"$1\" & wait", "sh", marker).Run(ctx, ops.IO{})
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled process did not exit")
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("cancelled child still ran: %v", err)
	}
}
