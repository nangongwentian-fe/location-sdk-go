package locationsdk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestStartupCompatibilityAndConcurrentClose(t *testing.T) {
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(dir, "engine"+suffix)
	if out, err := exec.Command("go", "build", "-o", binary, "./internal/testengine").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	options := Options{Path: binary, SHA256: hex.EncodeToString(sum[:]), Version: "test"}
	for _, name := range []string{"missing", "wrong-protocol", "wrong-state"} {
		t.Run(name, func(t *testing.T) {
			o := options
			o.Path = filepath.Join(dir, name+suffix)
			if name != "missing" {
				if err := os.WriteFile(o.Path, data, 0755); err != nil {
					t.Fatal(err)
				}
			}
			c, err := Open(context.Background(), o)
			if err == nil {
				c.Close()
				t.Fatal("accepted incompatible or absent engine")
			}
		})
	}
	c, err := Open(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = c.Info(context.Background()) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); _ = c.Close() }()
	wg.Wait()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}
