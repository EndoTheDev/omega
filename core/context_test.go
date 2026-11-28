package core

import (
	"sync"
	"testing"
)

// TestServicesConcurrentOfferResolve is the unit receipt for the sharing contract the
// multi-bind mount relies on: one Context takes offers, drops and resolves from several
// goroutines at once. A goroutine only ever sees its own key, so an assertion that fails
// here is a publication failure, never cross-tenant interference.
func TestServicesConcurrentOfferResolve(t *testing.T) {
	// Closed-namespace keys only: the seams test scans every offer literal in the tree.
	keys := []string{"provider", "store", "loop", "logger", "compactor", "prompt", "approvals", "skills"}
	const rounds = 50
	ctx := &Context{}
	ctx.InitServices()
	var wg sync.WaitGroup
	for w := range keys {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			key := keys[w]
			for i := 0; i < rounds; i++ {
				ctx.ApplyOffer(key, "test", w)
				if got := ctx.Services().Resolve(key); got != w {
					t.Errorf("worker %d round %d: resolve = %v, want %d", w, i, got, w)
					return
				}
				ctx.DropOffer(key, "test")
			}
		}(w)
	}
	wg.Wait()
}

// TestScopedServicesIsolateProxy pins the remount law around the registry pointer: the
// scoped proxy starts from the live registry and its own offers never reach the live one
// until the journal commits them forward.
func TestScopedServicesIsolateProxy(t *testing.T) {
	live := &Context{}
	live.InitServices()
	live.ApplyOffer("provider", "owner", "live-service")

	proxy := live.Scoped()
	proxy.ApplyOffer("store", "owner", "proxy-service")

	if live.Services().Has("store") {
		t.Fatal("a proxy offer reached the live registry before commit")
	}
	if !live.Services().Has("provider") {
		t.Fatal("the live registry lost an offer while scoping")
	}
	if !proxy.Services().Has("provider") {
		t.Fatal("the proxy lost the live registry seed")
	}
	if !proxy.Services().Has("store") {
		t.Fatal("the proxy offer did not land on the proxy")
	}
}
