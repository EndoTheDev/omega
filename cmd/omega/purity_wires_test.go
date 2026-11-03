package main

// The nm-purity wire-shape pins (card t_8abdbb15, the regression set).
// Two past reds where a host function carried a module's logic INLINE
// and the scrubber's line-only deletion stranded dead identifiers or a
// missing return (bea6abc, "loop/setup: purity-scrub-safe shapes" -
// the verdict and provider purity rotations went red together). A
// journey recipe cannot see this class: it is compile-time shape, not
// browser state. So the pins run in the go gate - the nightly and
// every train pass - and a deliberately reverted shape must stay red.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmdGoFiles collects the non-test .go files of cmd/ (the binary's
// host layer - the only layer the scrubber walks). Runs from the
// package dir cmd/omega, so cmd/ is the parent - walk one level over
// the siblings, this package included.
func cmdGoFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir("..", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		out = append(out, p)
		return nil
	})
	if err != nil {
		t.Fatalf("walk cmd/: %v", err)
	}
	return out
}

// TestVerdictExchangeStaysInCapsule pins the inline-verdict rehoming.
// The loop's busy-time verdict answering once lived in cmd/omega -
// ParseCardAnswer called inline, hint/echo helpers in the host - and
// the scrubber's line-only deletion of the verdict wires left dead
// identifiers and a stranded return, so the verdict rotation built
// red. The exchange lives in harness/verdict (AnswerInline) behind ONE
// wire line whose shape the line deletion cannot break: a named
// return on the host function. An inline re-copy in cmd/ is the exact
// past red.
func TestVerdictExchangeStaysInCapsule(t *testing.T) {
	for _, f := range cmdGoFiles(t) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(b), "ParseCardAnswer") {
			t.Errorf("%s names ParseCardAnswer: the verdict answer exchange must live in harness/verdict - an inline host copy re-strands dead identifiers under the verdict rotation (bea6abc, card t_8abdbb15)", f)
		}
	}
	b, err := os.ReadFile("loop.go")
	if err != nil {
		t.Fatalf("read cmd/omega/loop.go: %v", err)
	}
	body := string(b)
	if !strings.Contains(body, "func (lo *cliLoop) answerPendingVerdict(line string) (handled bool)") {
		t.Errorf("loop.go answerPendingVerdict lost the named return (handled bool) - the scrubber deletes the verdict wire line alone and a plain return strands a missing return (bea6abc)")
	}
	if !strings.Contains(body, "verdict.AnswerInline(") {
		t.Errorf("loop.go no longer wires verdict.AnswerInline - the busy-time verdict answering went inline or missing")
	}
}

// TestSetupProviderHookNamedReturn pins the wizard provider hook shape.
// The setup wizard touches ONE provider surface - the live model list -
// through the newSetupProvider hook. When it returned the module value
// directly (`return provider.NewProvider(...)`), the scrubber's
// deletion of the provider wire line left the hook returning nothing:
// the provider rotation built red. The named return is load-bearing -
// the hook must still return with the module gone.
func TestSetupProviderHookNamedReturn(t *testing.T) {
	b, err := os.ReadFile("setup.go")
	if err != nil {
		t.Fatalf("read cmd/omega/setup.go: %v", err)
	}
	body := string(b)
	if !strings.Contains(body, "func(typ, modelName, baseURL, apiKey string) (prov modelsLister)") {
		t.Errorf("setup.go newSetupProvider lost the named return (prov modelsLister) - a direct-return hook strands a missing return when the scrubber deletes the provider wire (bea6abc)")
	}
	if !strings.Contains(body, "return prov") {
		t.Errorf("setup.go newSetupProvider no longer returns through the named variable - the scrub-safe shape reverted")
	}
}
