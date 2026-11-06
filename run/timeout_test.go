//go:build unix

package run_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/karosia/driftsync/config"
	"github.com/karosia/driftsync/run"
)

// A code.command that hangs — and leaves a background child holding our
// stdout pipe, like a build daemon would — must fail fast with a clear
// message, and must not leave that child running.
func TestCheck_CodeCommandTimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	body := "version: 1\npublished: " + fixture(t, "published.yaml") + "\n" +
		"code:\n" +
		"  command: \"sleep 300 & echo $! > " + pidFile + "; wait\"\n" +
		"  file: openapi.gen.yaml\n" +
		"  timeout: 1s\n"
	p := filepath.Join(dir, "driftsync.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = run.Check(context.Background(), run.Options{Config: cfg, Stderr: io_discard{}})
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "timed out after 1s") {
		t.Fatalf("want a timeout error naming the limit, got %v", err)
	}
	if !strings.Contains(err.Error(), "code.timeout") {
		t.Errorf("error should tell the user which knob to turn: %v", err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("Check took %s; the timeout (1s) plus WaitDelay should bound it", elapsed)
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("child pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	// Give the kernel a moment to reap, then the grandchild must be gone.
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL) // don't leak it from the test either
			t.Fatalf("background child %d of code.command survived the timeout", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
