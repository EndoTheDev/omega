// The module-route vocabulary (v0.12 phase F, grill Q4).
package core

import "net/http"

// ModuleRoute is one module-registered REST route: the capsule registers it through
// RegisterRoute, and nothing module-shaped rides on it, so the shell serves the surface
// without importing the module that declared it.
type ModuleRoute struct {
	Module string
	Pattern string
	Handler func(http.ResponseWriter, *http.Request)
}
