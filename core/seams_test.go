package core

import (
	"strings"
	"testing"
)

// TestNormalizeInstancesAddressShorthand pins the address law the webui's add row and
// the file share: a bare host:port is the http shorthand (the spelling users type), a
// bare non-http scheme is refused, and both spellings of one address are one row.
func TestNormalizeInstancesAddressShorthand(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"bare scheme-less host and port", "127.0.0.1:8099", "http://127.0.0.1:8099"},
		{"named host shorthand", "laptop:8099", "http://laptop:8099"},
		{"bare host", "omega.example.test", "http://omega.example.test"},
		{"explicit scheme is kept", "HTTPS://Omega.Example.Test/", "https://Omega.Example.Test"},
		{"path prefix survives", "127.0.0.1:8099/omega", "http://127.0.0.1:8099/omega"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := NormalizeInstances([]SavedInstance{{Name: "row", URL: tc.raw}})
			if err != nil {
				t.Fatalf("%q refused: %v", tc.raw, err)
			}
			if len(out) != 1 || out[0].URL != tc.want {
				t.Fatalf("rows = %+v, want url %q", out, tc.want)
			}
		})
	}

	refused := []struct{ raw, want string }{
		{"javascript:alert(1)", "scheme must be http or https"},
		{"ftp://127.0.0.1:8099", "scheme must be http or https"},
		{"http://", "needs a host"},
	}
	for _, tc := range refused {
		if _, err := NormalizeInstances([]SavedInstance{{Name: "row", URL: tc.raw}}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%q: error = %v, want %q", tc.raw, err, tc.want)
		}
	}

	// the two spellings of one address are ONE row: the shorthand cannot slip past the
	// duplicate check by dropping its scheme.
	_, err := NormalizeInstances([]SavedInstance{
		{Name: "a", URL: "http://127.0.0.1:8099"},
		{Name: "b", URL: "127.0.0.1:8099"},
	})
	if err == nil || !strings.Contains(err.Error(), "listed twice") {
		t.Fatalf("duplicate error = %v, want the duplicate refusal", err)
	}
}

// TestNormalizeInstancesCarriesUnverified pins the hatch's marker through the ONE
// validator: a set flag survives the normalization, and an absent one stays false so a
// verified row and a hand edit without it are one row.
func TestNormalizeInstancesCarriesUnverified(t *testing.T) {
	out, err := NormalizeInstances([]SavedInstance{
		{Name: "down", URL: "pibtw:8099", Unverified: true},
		{Name: "up", URL: "winbtw:8099"},
	})
	if err != nil {
		t.Fatalf("rows refused: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("rows = %+v, want two", out)
	}
	if !out[0].Unverified || out[0].URL != "http://pibtw:8099" {
		t.Fatalf("row 0 = %+v, want the flag kept beside the normalized address", out[0])
	}
	if out[1].Unverified {
		t.Fatalf("row 1 = %+v, want an absent flag to stay false", out[1])
	}
}
