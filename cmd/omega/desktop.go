package main

// omega desktop: one process serving the same plugin set `omega serve` mounts, with a
// Wails window pointed at it. The whole shell is a client of the loopback origin -
// docs/adr/0010 records why.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNeedsSetup is the desktop's first-run signal: with no config.yaml there is
// nothing to serve, so the wizard runs in its own console and this launch exits.
var ErrNeedsSetup = errors.New("desktop: no config.yaml - run `omega setup` first")

// desktopBindHost is the one address the desktop serves on. Loopback only: the HTTP
// channel is unauthenticated and /chat reaches the shell tools.
const desktopBindHost = "127.0.0.1"

// desktopConfig loads the serve config with the desktop's own bind folded over it.
func desktopConfig(configPath string) (Config, string, error) {
	cfgPath := resolveConfigPath(configPath)
	if cfgPath == "" {
		return Config{}, "", ErrNeedsSetup
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return Config{}, "", err
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return Config{}, "", err
	}
	cfg.Server.Port = cfg.Desktop.Port
	cfg.Server.Bind = []string{desktopBindHost}
	return cfg, cfgPath, nil
}

// cmdDesktop dispatches the three faces: the window, the headless health probe, and
// the setup gate. The window is Windows-only in 0.19.0; the probe stays portable so
// the release-gate smoke can run anywhere.
func cmdDesktop(args []string) error {
	headless := hasFlag(args, "--headless-health")
	if !headless {
		if err := desktopWindowSupported(); err != nil {
			return err
		}
	}
	configPath := parseConfigFlag(args)
	cfg, _, err := desktopConfig(configPath)
	if errors.Is(err, ErrNeedsSetup) && !headless {
		// The gate: a first-run home opens the wizard in its own console and this
		// process exits 0. The window opens on the NEXT launch, once the wizard has
		// written config.yaml.
		if serr := spawnSetupConsole(); serr != nil {
			return fmt.Errorf("%w (and the setup wizard did not start: %v)", ErrNeedsSetup, serr)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if headless {
		return desktopHeadless(cfg, configPath)
	}
	return desktopLaunch(cfg, configPath)
}

// desktopHeadless is the release-gate surface: boot the desktop runtime, print the
// /health body, shut down, exit 0. A boot that never turns healthy exits 1.
func desktopHeadless(cfg Config, configPath string) error {
	rt, err := startRuntimeWithin(cfg, configPath, nil, trustFlags{}, 15*time.Second)
	if err != nil {
		return err
	}
	defer rt.shutdown()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(rt.URL + "/health")
	if err != nil {
		return fmt.Errorf("desktop: read %s/health: %w", rt.URL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("desktop: read /health body: %w", err)
	}
	fmt.Print(string(body))
	return nil
}

// hasFlag reports whether args carries the exact flag.
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}
