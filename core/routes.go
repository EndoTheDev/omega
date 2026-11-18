// The module-route vocabulary (v0.12 phase F, grill Q4).
package core

import "net/http"

// ModuleRoute is one module-registered REST route. The capsule registers it through the
// ExtensionAPI (RegisterRoute); nothing module-shaped rides on it - the pattern is an
// http.ServeMux pattern and the handler an opaque http func, so the shell can serve the
// surface while never importing the module that declared it.
type ModuleRoute struct {
	Module string
	Pattern string
	Handler func(http.ResponseWriter, *http.Request)
}
