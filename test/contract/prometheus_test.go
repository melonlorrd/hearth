// Package contract checks assumptions Hearth makes about upstream Prometheus.
//
// These tests run a real Prometheus binary (from the dev shell / flake test app).
// They fail when a Prometheus or Go upgrade changes behaviour Hearth depends on,
// which is the signal to revisit the matching Hearth setting.
package contract

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// maxFormBody is the POST form body limit Prometheus inherits from Go's net/http
// (parsePostForm in $GOROOT/src/net/http/request.go). Prometheus sets no limit of
// its own. Hearth's read-body limit must stay >= this value.
const maxFormBody = 10 << 20 // 10 MiB

func TestPrometheusQueryBodyLimit(t *testing.T) {
	addr := startPrometheus(t)

	cases := []struct {
		name     string
		size     int
		wantCode int
	}{
		{"at limit is accepted", maxFormBody, http.StatusOK},
		// Over the limit Go stops parsing the form, Prometheus sees no `query`
		// parameter, and answers 400 bad_data (not 413).
		{"one byte over is rejected", maxFormBody + 1, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := formBody(tc.size)
			resp, err := http.Post("http://"+addr+"/api/v1/query",
				"application/x-www-form-urlencoded", strings.NewReader(body))
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer resp.Body.Close()
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("body of %d bytes: got %d, want %d (%s)\n"+
					"Prometheus/Go body limit changed: update maxFormBody here and Hearth's limit.",
					tc.size, resp.StatusCode, tc.wantCode, msg)
			}
		})
	}
}

// formBody returns a valid form body of exactly n bytes: a cheap query plus padding
// in a parameter Prometheus ignores.
func formBody(n int) string {
	const prefix = "query=vector(1)&pad="
	return prefix + strings.Repeat("a", n-len(prefix))
}

// startPrometheus runs a throwaway Prometheus and returns its host:port once ready.
func startPrometheus(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("contract test: needs a real Prometheus; skipped with -short")
	}
	bin, err := exec.LookPath("prometheus")
	if err != nil {
		t.Skip("prometheus not on PATH; run inside `nix develop` or `nix run .#test`")
	}

	dir := t.TempDir()
	cfg := filepath.Join(dir, "prometheus.yml")
	if err := os.WriteFile(cfg, []byte("global: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	addr := freeAddr(t)

	cmd := exec.Command(bin,
		"--config.file="+cfg,
		"--storage.tsdb.path="+filepath.Join(dir, "data"),
		"--web.listen-address="+addr,
	)
	logPath := filepath.Join(dir, "prometheus.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start prometheus: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		logFile.Close()
	})

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/-/ready")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return addr
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	out, _ := os.ReadFile(logPath)
	t.Fatalf("prometheus not ready on %s after 15s; log:\n%s", addr, out)
	return ""
}

// freeAddr picks a free localhost port. Small race between Close and Prometheus
// binding it; acceptable for a test.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return fmt.Sprintf("127.0.0.1:%d", l.Addr().(*net.TCPAddr).Port)
}
