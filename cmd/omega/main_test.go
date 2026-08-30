package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/extensions/http_channel"
	"github.com/EndoTheDev/omega/extensions/logging"
	"github.com/EndoTheDev/omega/extensions/memory"
	"github.com/EndoTheDev/omega/extensions/provider"
	"github.com/EndoTheDev/omega/extensions/skills"
	"github.com/EndoTheDev/omega/extensions/store"
	"github.com/EndoTheDev/omega/extensions/trust"
	"github.com/EndoTheDev/omega/extensions/web"
)

// TestRunChdirError verifies a non-subcommand argument that is not a
// directory surfaces a clean chdir error (rather than launching the TUI).
func TestRunChdirError(t *testing.T) {
	err := run([]string{"/nonexistent/path/xyz"})
	if err == nil {
		t.Fatal("expected chdir error for nonexistent path")
	}
}

// TestRunHelp verifies --help and -h print help and exit cleanly, even
// when combined with a subcommand.
func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"-h"},
		{"serve", "--help"},
		{"run", "-h"},
	} {
		if err := run(args); err != nil {
			t.Errorf("run(%v) = %v, want nil", args, err)
		}
	}
}

// TestRunVersion verifies --version and -v print the version and exit
// cleanly.
func TestRunVersion(t *testing.T) {
	for _, args := range [][]string{
		{"--version"},
		{"-v"},
	} {
		if err := run(args); err != nil {
			t.Errorf("run(%v) = %v, want nil", args, err)
		}
	}
}

func TestParseConfigFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"space form", []string{"--config", "my.yaml"}, "my.yaml"},
		{"equals form", []string{"--config=my.yaml"}, "my.yaml"},
		{"absent", []string{"chat"}, ""},
		{"at end without value", []string{"chat", "--config"}, ""},
		{"among other args", []string{"chat", "--config", "x.yaml", "hello"}, "x.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseConfigFlag(tt.args); got != tt.want {
				t.Errorf("parseConfigFlag(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestStripConfigFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"space form removed", []string{"--config", "x.yaml", "chat"}, []string{"chat"}},
		{"equals form removed", []string{"--config=x.yaml", "chat"}, []string{"chat"}},
		{"no config unchanged", []string{"chat", "hello"}, []string{"chat", "hello"}},
		{"config in middle", []string{"run", "--config", "x.yaml", "do", "stuff"}, []string{"run", "do", "stuff"}},
		{"config equals in middle", []string{"run", "--config=x.yaml", "do"}, []string{"run", "do"}},
		{"at end without value", []string{"chat", "--config"}, []string{"chat"}},
		{"empty args", []string{}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripConfigFlag(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("stripConfigFlag(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseAppendPrompts(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"single space form", []string{"--append-system-prompt", "be brief"}, []string{"be brief"}},
		{"single equals form", []string{"--append-system-prompt=be brief"}, []string{"be brief"}},
		{"multiple flags", []string{
			"--append-system-prompt", "one",
			"--append-system-prompt=two",
		}, []string{"one", "two"}},
		{"mixed with other args", []string{
			"run", "--append-system-prompt", "hi", "prompt",
		}, []string{"hi"}},
		{"no flags", []string{"chat", "hello"}, nil},
		{"at end without value", []string{"chat", "--append-system-prompt"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAppendPrompts(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseAppendPrompts(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestStripAppendPrompts(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"space form removed", []string{"--append-system-prompt", "hi", "chat"}, []string{"chat"}},
		{"equals form removed", []string{"--append-system-prompt=hi", "chat"}, []string{"chat"}},
		{"multiple removed", []string{
			"--append-system-prompt", "one",
			"--append-system-prompt=two",
			"chat",
		}, []string{"chat"}},
		{"mixed with regular args", []string{
			"run", "--append-system-prompt", "x", "do", "--append-system-prompt=y", "stuff",
		}, []string{"run", "do", "stuff"}},
		{"no flags unchanged", []string{"chat", "hello"}, []string{"chat", "hello"}},
		{"at end without value", []string{"chat", "--append-system-prompt"}, []string{"chat"}},
		{"empty args", []string{}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripAppendPrompts(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("stripAppendPrompts(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// TestResolveConfigPath verifies --config wins outright, the home
// config.yaml is picked up when present, and empty means "no YAML".
func TestResolveConfigPath(t *testing.T) {
	t.Run("explicit flag path wins", func(t *testing.T) {
		t.Setenv("OMEGA_HOME", t.TempDir())
		if got := resolveConfigPath("my.yaml"); got != "my.yaml" {
			t.Errorf("resolveConfigPath(\"my.yaml\") = %q, want %q", got, "my.yaml")
		}
	})

	t.Run("home config.yaml exists", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("OMEGA_HOME", home)
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("x: 1"), 0o644); err != nil {
			t.Fatal(err)
		}
		want := home + "/config.yaml"
		if got := resolveConfigPath(""); got != want {
			t.Errorf("resolveConfigPath(\"\") = %q, want %q", got, want)
		}
	})

	t.Run("no home config.yaml", func(t *testing.T) {
		t.Setenv("OMEGA_HOME", t.TempDir())
		if got := resolveConfigPath(""); got != "" {
			t.Errorf("resolveConfigPath(\"\") = %q, want \"\"", got)
		}
	})
}

// TestResolveHomePaths verifies only relative defaults are rewritten to
// home-relative paths, custom values are left alone, and the home
// directory is created.
func TestResolveHomePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)

	cfg := DefaultConfig()
	resolveHomePaths(&cfg)

	if cfg.Store.DBPath != home+"/omega.db" {
		t.Errorf("DBPath = %q, want %q", cfg.Store.DBPath, home+"/omega.db")
	}
	if cfg.Skills.Dir != home+"/skills" {
		t.Errorf("Skills.Dir = %q, want %q", cfg.Skills.Dir, home+"/skills")
	}
	if fi, err := os.Stat(home); err != nil || !fi.IsDir() {
		t.Errorf("home dir not created: err=%v", err)
	}

	custom := DefaultConfig()
	custom.Store.DBPath = "custom.db"
	custom.Skills.Dir = "custom-skills"
	resolveHomePaths(&custom)
	if custom.Store.DBPath != "custom.db" {
		t.Errorf("custom DBPath = %q, want unchanged", custom.Store.DBPath)
	}
	if custom.Skills.Dir != "custom-skills" {
		t.Errorf("custom Skills.Dir = %q, want unchanged", custom.Skills.Dir)
	}
}


// TestBuildConfigs verifies buildConfigs routes every Config sub-section
// into the correct per-extension typed config struct, that all 9 keys are
// present, and that the trust Home matches omegaHome().
func TestBuildConfigs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)

	cfg := DefaultConfig()
	// Customize the fields buildConfigs is expected to propagate so a wrong
	// mapping (e.g. store<->provider swap) fails the test.
	cfg.Store.DBPath = "/tmp/store-test.db"
	cfg.Provider.Type = "ollama"
	cfg.Provider.ModelName = "unit-model"
	cfg.Provider.Host = "http://unit-host:1234"
	cfg.Provider.APIKey = "secret-key"
	cfg.Server.Port = 7000
	cfg.Skills.Dir = "/tmp/skills-test"
	cfg.Memory.Enabled = false
	cfg.Memory.CharLimit = 999
	cfg.Memory.File = "mem-test.md"
	cfg.Memory.UserProfileFile = "user-test.md"
	cfg.Logging.Enabled = false
	cfg.Logging.File = "log-test.log"
	cfg.Compaction = agent.CompactionConfig{
		Enabled:       true,
		Threshold:      0.42,
		ContextWindow: 4096,
		KeepFirst:     3,
		KeepLast:      8,
		ReserveTokens: 2048,
		MaxToolOutput: 8192,
	}

	configs := buildConfigs(cfg)

	// All 9 expected keys must be present.
	wantKeys := []string{
		"store", "provider", "http_channel", "skills",
		"memory", "logging", "compactor", "web", "trust",
	}
	for _, k := range wantKeys {
		if _, ok := configs[k]; !ok {
			t.Fatalf("buildConfigs: missing key %q", k)
		}
	}
	if len(configs) != len(wantKeys) {
		t.Errorf("buildConfigs returned %d keys, want %d", len(configs), len(wantKeys))
	}

	// store: store.Config{DBPath}
	sc, ok := configs["store"].(store.Config)
	if !ok {
		t.Fatalf("store config is %T, want store.Config", configs["store"])
	}
	if sc.DBPath != cfg.Store.DBPath {
		t.Errorf("store.DBPath = %q, want %q", sc.DBPath, cfg.Store.DBPath)
	}

	// provider: provider.Config{Type, ModelName, Host, APIKey}
	pc, ok := configs["provider"].(provider.Config)
	if !ok {
		t.Fatalf("provider config is %T, want provider.Config", configs["provider"])
	}
	if pc.Type != cfg.Provider.Type {
		t.Errorf("provider.Type = %q, want %q", pc.Type, cfg.Provider.Type)
	}
	if pc.ModelName != cfg.Provider.ModelName {
		t.Errorf("provider.ModelName = %q, want %q", pc.ModelName, cfg.Provider.ModelName)
	}
	if pc.Host != cfg.Provider.Host {
		t.Errorf("provider.Host = %q, want %q", pc.Host, cfg.Provider.Host)
	}
	if pc.APIKey != cfg.Provider.APIKey {
		t.Errorf("provider.APIKey = %q, want %q", pc.APIKey, cfg.Provider.APIKey)
	}

	// http_channel: http_channel.Config{Port}
	hc, ok := configs["http_channel"].(http_channel.Config)
	if !ok {
		t.Fatalf("http_channel config is %T, want http_channel.Config", configs["http_channel"])
	}
	if hc.Port != cfg.Server.Port {
		t.Errorf("http_channel.Port = %d, want %d", hc.Port, cfg.Server.Port)
	}

	// skills: skills.Config{Dir}
	sk, ok := configs["skills"].(skills.Config)
	if !ok {
		t.Fatalf("skills config is %T, want skills.Config", configs["skills"])
	}
	if sk.Dir != cfg.Skills.Dir {
		t.Errorf("skills.Dir = %q, want %q", sk.Dir, cfg.Skills.Dir)
	}

	// memory: memory.Config{Enabled, UserProfileEnabled, CharLimit,
	// UserProfileCharLimit, File, UserProfileFile}
	mc, ok := configs["memory"].(memory.Config)
	if !ok {
		t.Fatalf("memory config is %T, want memory.Config", configs["memory"])
	}
	if mc.Enabled != cfg.Memory.Enabled {
		t.Errorf("memory.Enabled = %v, want %v", mc.Enabled, cfg.Memory.Enabled)
	}
	if mc.CharLimit != cfg.Memory.CharLimit {
		t.Errorf("memory.CharLimit = %d, want %d", mc.CharLimit, cfg.Memory.CharLimit)
	}
	if mc.File != cfg.Memory.File {
		t.Errorf("memory.File = %q, want %q", mc.File, cfg.Memory.File)
	}
	if mc.UserProfileFile != cfg.Memory.UserProfileFile {
		t.Errorf("memory.UserProfileFile = %q, want %q", mc.UserProfileFile, cfg.Memory.UserProfileFile)
	}

	// logging: logging.Config{Enabled, File}
	lc, ok := configs["logging"].(logging.Config)
	if !ok {
		t.Fatalf("logging config is %T, want logging.Config", configs["logging"])
	}
	if lc.Enabled != cfg.Logging.Enabled {
		t.Errorf("logging.Enabled = %v, want %v", lc.Enabled, cfg.Logging.Enabled)
	}
	if lc.File != cfg.Logging.File {
		t.Errorf("logging.File = %q, want %q", lc.File, cfg.Logging.File)
	}

	// compactor: agent.CompactionConfig (value equality via reflect.DeepEqual)
	cc, ok := configs["compactor"].(agent.CompactionConfig)
	if !ok {
		t.Fatalf("compactor config is %T, want agent.CompactionConfig", configs["compactor"])
	}
	if !reflect.DeepEqual(cc, cfg.Compaction) {
		t.Errorf("compactor = %+v, want %+v", cc, cfg.Compaction)
	}

	// web: web.Config{APIKey}
	wc, ok := configs["web"].(web.Config)
	if !ok {
		t.Fatalf("web config is %T, want web.Config", configs["web"])
	}
	if wc.APIKey != cfg.Provider.APIKey {
		t.Errorf("web.APIKey = %q, want %q", wc.APIKey, cfg.Provider.APIKey)
	}

	// trust: trust.Config{Home} must equal omegaHome() (i.e. OMEGA_HOME).
	tc, ok := configs["trust"].(trust.Config)
	if !ok {
		t.Fatalf("trust config is %T, want trust.Config", configs["trust"])
	}
	if tc.Home != home {
		t.Errorf("trust.Home = %q, want %q (omegaHome)", tc.Home, home)
	}
}
