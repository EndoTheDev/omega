package main

// omega setup: the mandatory first-run wizard (card t_0744eb8e). With
// no config.yaml, every command refuses and points here; bare `omega`
// in an empty home drops straight into this wizard. Stdlib stdin/fmt
// only - the loop law keeps the terminal one-dimensional, so nothing
// here draws a screen. The live connection test goes through the real
// provider transport (modules/provider), so "it works" is the same
// code path a chat run uses.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/modules/provider"
)

// modelsLister is the one provider surface setup touches: the live
// model list. A module-level hook so tests drive a stub transport.
// The named return is load-bearing for the purity law (scripts/
// nm-purity.sh): the scrubber deletes the wire line alone and never
// deletes a host function, so the hook must still return with the
// module gone.
var newSetupProvider = func(typ, modelName, baseURL, apiKey string) (prov modelsLister) {
	prov = provider.NewProvider(typ, modelName, baseURL, apiKey)
	return prov
}

type modelsLister interface {
	ListModels() ([]string, error)
}

// setupProvider is one pick on the wizard's provider menu.
type setupProvider struct {
	Type   string // the config type key
	Label  string // menu label
	Host   string // default base URL
	NoKey  bool   // an api key is optional (local hosts)
	Manual bool   // a missing model list still lets the user type a model name
}

// setupProviders is the menu order ollama/openai/anthropic, matching
// the card's provider list.
var setupProviders = []setupProvider{
	{Type: "ollama", Label: "ollama (local, default http://localhost:11434)", Host: "http://localhost:11434", NoKey: true, Manual: false},
	{Type: "openai", Label: "openai (api.openai.com)", Host: "https://api.openai.com/v1", Manual: true},
	{Type: "anthropic", Label: "anthropic (api.anthropic.com)", Host: "https://api.anthropic.com/v1", Manual: true},
}

// setupError is the printable "not interactive enough" guard.
func runnableSetup() error {
	fi, err := os.Stdin.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return fmt.Errorf("omega setup is interactive - run it at a terminal (a config.yaml can also be written by hand; see config.yaml.example)")
	}
	return nil
}

// setupSession carries the wizard's io so tests can drive it.
type setupSession struct {
	in  *bufio.Reader
	out io.Writer
}

// cmdSetup runs the wizard and writes <home>/config/config.yaml. An
// existing config is never silently overwritten: the operator confirms.
// On success it prints the webui URL and returns; the caller decides
// whether to continue into the loop.
func cmdSetup() error {
	if err := runnableSetup(); err != nil {
		return err
	}
	s := setupSession{in: bufio.NewReader(os.Stdin), out: os.Stdout}
	return s.Run()
}

// Run drives the whole wizard. Errors are phrased for one-shot reading:
// the user fixes one thing, not three.
func (s *setupSession) Run() error {
	pick, err := s.pickProvider()
	if err != nil {
		return err
	}
	host := s.ask("base url", pick.Host)
	key := ""
	if !pick.NoKey {
		// The key echoes as it is pasted (no terminal raw mode here -
		// the loop law owns this process): say so plainly.
		key = strings.TrimSpace(s.ask(fmt.Sprintf("api key for %s", pick.Type), ""))
		if key == "" {
			return fmt.Errorf("an api key is required for %s (get one at the provider's console)", pick.Type)
		}
	}

	model, finalHost, err := s.pickModel(pick, host, key)
	if err != nil {
		return err
	}

	path, err := writeSetupConfig(pick.Type, model, finalHost, key, s.in)
	if err != nil {
		return err
	}

	cfg, cerr := LoadConfig(path)
	port := 8099
	if cerr == nil {
		port = cfg.Server.Port
	}
	fmt.Fprintf(s.out, "\nconfig written: %s\n", path)
	fmt.Fprintf(s.out, "model: %s via %s\n", model, finalHost)
	fmt.Fprintf(s.out, "webui: http://localhost:%d/ (serve it with `omega serve`)\n", port)
	fmt.Fprintf(s.out, "loop:  `omega` starts a terminal session against the same model\n")
	return nil
}

func (s *setupSession) pickProvider() (setupProvider, error) {
	fmt.Fprintf(s.out, "\nwhich provider?\n")
	for i, p := range setupProviders {
		fmt.Fprintf(s.out, "  %d) %s\n", i+1, p.Label)
	}
	for {
		line := s.ask("pick [1]", "1")
		if line == "" || line == "?" {
			continue
		}
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(setupProviders) {
			fmt.Fprintf(s.out, "  a number between 1 and %d, please\n", len(setupProviders))
			continue
		}
		return setupProviders[n-1], nil
	}
}

// pickModel runs the live connection test: ListModels through the real
// provider transport. A working host lists models to pick from; a
// failing host names the error and falls back to a typed model name
// (or a retried host) so a transient failure never strands the install.
// It returns the model AND the final base url - a retried host must
// land in the written config, not just this conversation.
func (s *setupSession) pickModel(pick setupProvider, host, key string) (string, string, error) {
	for attempt := 0; ; attempt++ {
		prov := newSetupProvider(pick.Type, "", host, key)
		models, lerr := prov.ListModels()
		if lerr == nil && len(models) > 0 {
			fmt.Fprintf(s.out, "\nconnected - %d models:\n", len(models))
			width := 0
			for _, m := range models {
				if len(m) > width {
					width = len(m)
				}
				if width > 60 {
					width = 60 // menu width stays tame no matter the registry
				}
			}
			if width > 60 {
				width = 60
			}
			for i, m := range models {
				if i >= 50 {
					fmt.Fprintf(s.out, "  ... (%d more; type the exact name)\n", len(models)-i)
					break
				}
				name := m
				if len(name) > 60 {
					name = name[:57] + "..."
				}
				fmt.Fprintf(s.out, "  %3d) %-*s\n", i+1, width, name)
			}
			for {
				line := strings.TrimSpace(s.ask("pick a model [1] (number or name)", "1"))
				if line == "" {
					continue
				}
				if n, err := strconv.Atoi(line); err == nil {
					if n < 1 || n > len(models) {
						fmt.Fprintf(s.out, "  a number between 1 and %d, or the exact model name\n", len(models))
						continue
					}
					return models[n-1], host, nil
				}
				for _, m := range models {
					if m == line {
						return line, host, nil
					}
				}
				fmt.Fprintf(s.out, "  %q is not in the list - use its number, or type the exact name\n", line)
			}
		}
		if lerr != nil {
			fmt.Fprintf(s.out, "\nconnection test failed: %v\n", lerr)
		} else {
			fmt.Fprintf(s.out, "\nconnected, but the host lists no models.\n")
		}
		if !pick.Manual {
			// ollama with nothing running is the common miss; a retry loop
			// around host+test is the useful hand.
			line := s.ask("enter a base url to retry, or a model name to force [model name]", "")
			if strings.HasPrefix(strings.ToLower(line), "http") {
				host = line
				continue
			}
			if line != "" {
				return line, host, nil
			}
			return "", host, fmt.Errorf("no model chosen - start ollama (or set the right base url) and run `omega setup` again")
		}
		line := s.ask("model name (or a base url to retry, empty to abort)", "")
		if line == "" {
			return "", host, fmt.Errorf("no model chosen - run `omega setup` again")
		}
		if strings.HasPrefix(strings.ToLower(line), "http") {
			host = line
			continue
		}
		return line, host, nil
	}
}

func (s *setupSession) ask(prompt, def string) string {
	if def != "" {
		fmt.Fprintf(s.out, "%s [%s]: ", prompt, def)
	} else {
		fmt.Fprintf(s.out, "%s: ", prompt)
	}
	line, _ := s.in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// writeSetupConfig writes the fresh config.yaml under the home's
// config/ layout. An existing file needs an explicit yes - the wizard
// never eats a hand-tuned config silently. The confirm reader is
// injected so tests drive the no-overwrite path without stdin.
func writeSetupConfig(typ, model, host, key string, confirm *bufio.Reader) (string, error) {
	path := omegaHome() + "/config/config.yaml"
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("config already exists: %s\noverwrite it? [y/N]: ", path)
		line := ""
		if confirm != nil {
			line, _ = confirm.ReadString('\n')
		}
		if !strings.EqualFold(strings.TrimSpace(line), "y") {
			return "", fmt.Errorf("setup cancelled - %s is untouched", path)
		}
	}
	if err := os.MkdirAll(omegaHome()+"/config", 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	var b strings.Builder
	b.WriteString("# omega config - written by `omega setup`.\n")
	b.WriteString("# Every knob lives in config.yaml.example next to this file.\n\n")
	b.WriteString("provider:\n")
	fmt.Fprintf(&b, "  type: %s\n", typ)
	fmt.Fprintf(&b, "  model_name: %s\n", model)
	fmt.Fprintf(&b, "  host: %s\n", host)
	fmt.Fprintf(&b, "  api_key: %q\n", key)
	b.WriteString("\nserver:\n")
	fmt.Fprintf(&b, "  port: %d\n", 8099)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return path, nil
}
