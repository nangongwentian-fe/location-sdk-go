package locationsdk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	p "github.com/nangongwentian-fe/location-sdk-go/protocol"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", path, "./internal/testengine").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	assertOrphanCleanup(t, path, hex.EncodeToString(sum[:]))
	if runtime.GOOS != "windows" {
		frozen := filepath.Join(t.TempDir(), "frozen-engine")
		if err := os.WriteFile(frozen, data, 0755); err != nil {
			t.Fatal(err)
		}
		began := time.Now()
		c, err := Open(context.Background(), Options{Path: frozen, SHA256: hex.EncodeToString(sum[:]), Version: "test", StartTimeout: time.Second, CallTimeout: 200 * time.Millisecond})
		if err == nil {
			c.Close()
			t.Fatal("frozen engine unexpectedly opened")
		}
		if time.Since(began) > 2*time.Second {
			t.Fatal("frozen handshake cleanup exceeded startup bound")
		}
	}
	client, err := Open(context.Background(), Options{Path: path, SHA256: hex.EncodeToString(sum[:]), Version: "test", CallTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.ValidateConfig(context.Background(), &p.ConfigRequest{Kind: "wait"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("timeout: %v", err)
	}
	_, err = client.ValidateConfig(context.Background(), &p.ConfigRequest{Kind: "crash"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("crash: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err = client.Info(context.Background()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	began := time.Now()
	_, err = client.ValidateConfig(context.Background(), &p.ConfigRequest{Kind: "crash"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		if _, err = client.Info(context.Background()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second restart did not complete")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if time.Since(began) < 2*time.Second {
		t.Fatal("short-lived successful handshake reset crash backoff")
	}
	client.mu.RLock()
	process := client.process
	client.mu.RUnlock()
	if err = client.Close(); err != nil {
		t.Fatal(err)
	}
	if !process.Exited() {
		t.Fatal("child still running after Close")
	}
	if _, err = client.Info(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
}

// This subprocess deliberately omits Close so the test can kill the host.
func TestHostHelper(t *testing.T) {
	if os.Getenv("LOCATION_TEST_HOST") != "1" {
		return
	}
	client, err := Open(context.Background(), Options{Path: os.Getenv("LOCATION_TEST_BINARY"), SHA256: os.Getenv("LOCATION_TEST_SHA"), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.ValidateConfig(context.Background(), &p.ConfigRequest{Kind: "pid"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("LOCATION_TEST_PID_FILE"), result.ParametersJson, 0600); err != nil {
		t.Fatal(err)
	}
	select {}
}
func assertOrphanCleanup(t *testing.T, path, checksum string) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "pid")
	host := exec.Command(os.Args[0], "-test.run=^TestHostHelper$")
	host.Env = append(os.Environ(), "LOCATION_TEST_HOST=1", "LOCATION_TEST_BINARY="+path, "LOCATION_TEST_SHA="+checksum, "LOCATION_TEST_PID_FILE="+pidFile)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { host.Process.Kill(); host.Wait() }()
	deadline := time.Now().Add(15 * time.Second)
	var pid string
	for {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid = string(data)
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("host did not start engine")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := host.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	host.Wait()
	deadline = time.Now().Add(10 * time.Second)
	for {
		var running bool
		if runtime.GOOS == "windows" {
			out, _ := exec.Command("tasklist", "/FI", "PID eq "+pid, "/NH").Output()
			running = strings.Contains(string(out), pid)
		} else {
			out, _ := exec.Command("ps", "-o", "stat=", "-p", pid).Output()
			value := strings.TrimSpace(string(out))
			running = value != "" && !strings.HasPrefix(value, "Z")
		}
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("engine survived abrupt host exit")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRestartDelayCapsAndGrows(t *testing.T) {
	delay := time.Second
	for _, want := range []time.Duration{2, 4, 8, 16, 30, 30} {
		delay = nextRestartDelay(delay)
		if delay != want*time.Second {
			t.Fatal(delay)
		}
	}
}
