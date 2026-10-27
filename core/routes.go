// The module-route vocabulary (v0.12 phase F, grill Q4): a module REST
// surface the registry journals and the live Context carries, so the
// serving channels follow the module lifecycle without knowing the
// modules - a disabled module's routes die, a re-enabled one's return,
// through the same RewireSeams re-read every other served seam follows
// (E2 law text). Stdlib-only by the core law: net/http is vocabulary.
package core

import "net/http"

// ModuleRoute is one module-registered REST route. The capsule
// registers it through the ExtensionAPI (RegisterRoute); nothing
// module-shaped rides on it - the pattern is an http.ServeMux pattern
// and the handler an opaque http func, so the engine ring can serve
// the surface while never importing the module that declared it.
type ModuleRoute struct {
	// Module names the registering capsule (the registry tenant on
	// the "route:<pattern>" ledger).
	Module string
	// Pattern is an http.ServeMux pattern: "GET /tasks/{id}".
	Pattern string
	// Handler serves the route.
	Handler func(http.ResponseWriter, *http.Request)
}
