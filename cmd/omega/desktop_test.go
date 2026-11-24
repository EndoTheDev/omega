package main

// desktop_test.go: the desktop boot contract - one runtime serving the embedded SPA on
// the stable loopback port, a loud failure when the port is taken, the setup gate, and
// the headless health probe the release gate calls.

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freeDesktopPort reserves a port and releases it for the test's own bind.
func freeDesktopPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// desktopTestConfig writes the minimal network-free config: the mock provider answers
// without a host, so a boot needs nothing but the temp home.
func desktopTestConfig(t *testing.T, port int) string {
	t.Helper()
	return writeTempConfig(t, fmt.Sprintf(`provider:
  model_name: mock-model
  type: mock
desktop:
  port: %d
`, port))
}

func TestDesktopRuntimeServesSPA(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	port := freeDesktopPort(t)

	cfgPath := desktopTestConfig(t, port)
	cfg, _, err := desktopConfig(cfgPath)
	if err != nil {
		t.Fatalf("desktopConfig: %v", err)
	}
	if cfg.Server.Port != port || len(cfg.Server.Bind) != 1 || cfg.Server.Bind[0] != desktopBindHost {
		t.Fatalf("desktop bind = %v:%d, want %s:%d", cfg.Server.Bind, cfg.Server.Port, desktopBindHost, port)
	}

	rt, err := startRuntime(cfg, cfgPath, nil, trustFlags{})
	if err != nil {
		t.Fatalf("startRuntime: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}

	health, err := healthBody(client, rt.URL)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	if !strings.Contains(health, `"status":"ok"`) {
		t.Fatalf("/health body = %q, want status ok", health)
	}

	resp, err := client.Get(rt.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		t.Fatalf("read /: %v", readErr)
	}
	if !strings.Contains(string(body), "<title>omega</title>") {
		t.Fatalf("served index has no <title>omega</title>: %.200q", string(body))
	}

	rt.shutdown()

	if _, err := net.Listen("tcp", net.JoinHostPort(desktopBindHost, strconv.Itoa(port))); err != nil {
		t.Fatalf("port %d is still bound after shutdown: %v", port, err)
	}
}

func TestDesktopPortOccupiedFailsLoud(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())

	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}
	defer held.Close()
	port := held.Addr().(*net.TCPAddr).Port

	cfgPath := desktopTestConfig(t, port)
	cfg, _, err := desktopConfig(cfgPath)
	if err != nil {
		t.Fatalf("desktopConfig: %v", err)
	}

	rt, err := startRuntime(cfg, cfgPath, nil, trustFlags{})
	if err == nil {
		rt.shutdown()
		t.Fatalf("startRuntime bound a port that is already held")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Fatalf("error %q does not name the held port %d", err, port)
	}
}

func TestDesktopNeedsSetup(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	if _, _, err := desktopConfig(""); !errors.Is(err, ErrNeedsSetup) {
		t.Fatalf("desktopConfig on an empty home = %v, want ErrNeedsSetup", err)
	}
}

func TestDesktopHeadlessHealth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	port := freeDesktopPort(t)
	cfgPath := desktopTestConfig(t, port)

	out, err := captureStdout(t, func() error {
		return run([]string{"desktop", "--headless-health", "--config", cfgPath})
	})
	if err != nil {
		t.Fatalf("omega desktop --headless-health: %v", err)
	}
	if !strings.Contains(out, `"status":"ok"`) {
		t.Fatalf("headless output carries no healthy body: %q", out)
	}
	if conn, err := net.DialTimeout("tcp", net.JoinHostPort(desktopBindHost, strconv.Itoa(port)), 250*time.Millisecond); err == nil {
		conn.Close()
		t.Fatalf("headless run left the listener up on %d", port)
	}
}
