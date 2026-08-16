package main

import (
	"testing"

	"github.com/EndoTheDev/omega/internal/gateway"
)

// TestParseExtensionArgs verifies all extension flag forms parse correctly.
func TestParseExtensionArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want extFlags
	}{
		{
			name: "no flags",
			args: []string{"hello", "world"},
			want: extFlags{},
		},
		{
			name: "no-extensions",
			args: []string{"--no-extensions"},
			want: extFlags{noExt: true},
		},
		{
			name: "project-extensions",
			args: []string{"--project-extensions"},
			want: extFlags{project: true},
		},
		{
			name: "extension space form",
			args: []string{"--extension", "/path/to/ext"},
			want: extFlags{explicit: []string{"/path/to/ext"}},
		},
		{
			name: "extension equals form",
			args: []string{"--extension=/path/to/ext"},
			want: extFlags{explicit: []string{"/path/to/ext"}},
		},
		{
			name: "short -e space form",
			args: []string{"-e", "/path/to/ext"},
			want: extFlags{explicit: []string{"/path/to/ext"}},
		},
		{
			name: "short -e equals form",
			args: []string{"-e=/path/to/ext"},
			want: extFlags{explicit: []string{"/path/to/ext"}},
		},
		{
			name: "repeatable extension",
			args: []string{"-e", "/a", "--extension", "/b", "-e=/c"},
			want: extFlags{explicit: []string{"/a", "/b", "/c"}},
		},
		{
			name: "all flags together",
			args: []string{"--no-extensions", "--project-extensions", "-e", "/a"},
			want: extFlags{explicit: []string{"/a"}, noExt: true, project: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseExtensionArgs(tt.args)
			if got.noExt != tt.want.noExt || got.project != tt.want.project {
				t.Errorf("parseExtensionArgs(%v) = {noExt:%v project:%v}, want {noExt:%v project:%v}",
					tt.args, got.noExt, got.project, tt.want.noExt, tt.want.project)
			}
			if len(got.explicit) != len(tt.want.explicit) {
				t.Fatalf("explicit = %v, want %v", got.explicit, tt.want.explicit)
			}
			for i := range got.explicit {
				if got.explicit[i] != tt.want.explicit[i] {
					t.Errorf("explicit[%d] = %q, want %q", i, got.explicit[i], tt.want.explicit[i])
				}
			}
		})
	}
}

// TestStripExtensionArgs verifies extension flags are removed, leaving the
// prompt intact.
func TestStripExtensionArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "no flags",
			args: []string{"hello", "world"},
			want: []string{"hello", "world"},
		},
		{
			name: "strips no-extensions",
			args: []string{"--no-extensions", "hello"},
			want: []string{"hello"},
		},
		{
			name: "strips project-extensions",
			args: []string{"--project-extensions", "hello"},
			want: []string{"hello"},
		},
		{
			name: "strips extension with value",
			args: []string{"--extension", "/path", "hello"},
			want: []string{"hello"},
		},
		{
			name: "strips extension equals",
			args: []string{"--extension=/path", "hello"},
			want: []string{"hello"},
		},
		{
			name: "strips short -e with value",
			args: []string{"-e", "/path", "hello"},
			want: []string{"hello"},
		},
		{
			name: "strips short -e equals",
			args: []string{"-e=/path", "hello"},
			want: []string{"hello"},
		},
		{
			name: "keeps prompt words",
			args: []string{"-e", "/a", "what", "is", "up"},
			want: []string{"what", "is", "up"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripExtensionArgs(tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("stripExtensionArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

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

// TestApplyExtFlags verifies precedence: --no-extensions wins, otherwise
// --extension/-e and --project-extensions each force extensions on.
func TestApplyExtFlags(t *testing.T) {
	t.Run("no-extensions wins", func(t *testing.T) {
		cfg := gateway.Config{}
		cfg.Extensions.Enabled = true
		applyExtFlags(&cfg, extFlags{noExt: true, explicit: []string{"/a"}, project: true})
		if cfg.Extensions.Enabled {
			t.Error("Enabled = true, want false (--no-extensions wins)")
		}
	})

	t.Run("explicit forces on", func(t *testing.T) {
		cfg := gateway.Config{}
		applyExtFlags(&cfg, extFlags{explicit: []string{"/a", "/b"}})
		if !cfg.Extensions.Enabled {
			t.Error("Enabled = false, want true")
		}
		if len(cfg.Extensions.Explicit) != 2 {
			t.Errorf("Explicit = %v, want 2 paths", cfg.Extensions.Explicit)
		}
	})

	t.Run("project forces on", func(t *testing.T) {
		cfg := gateway.Config{}
		applyExtFlags(&cfg, extFlags{project: true})
		if !cfg.Extensions.Enabled {
			t.Error("Enabled = false, want true")
		}
		if !cfg.Extensions.Project {
			t.Error("Project = false, want true")
		}
	})

	t.Run("no flags leaves config alone", func(t *testing.T) {
		cfg := gateway.Config{}
		cfg.Extensions.Enabled = false
		applyExtFlags(&cfg, extFlags{})
		if cfg.Extensions.Enabled {
			t.Error("Enabled = true, want false (unchanged)")
		}
	})
}
