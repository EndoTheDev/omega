package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/EndoTheDev/omega/gateway"
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

	cfg := gateway.DefaultConfig()
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

	custom := gateway.DefaultConfig()
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
