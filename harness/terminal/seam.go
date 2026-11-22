// The consumer-side seam resolver shared by this capsule's consults.
// Absence is the floor (nil rides, the nil-safe degradation applies); a wrong-shaped
// offer dies loud at mount. Each consumer package owns this file; suppliers never see
// the type.
package terminal

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/core"
)

func seam[T any](ctx *core.Context, key string) T {
	svc := ctx.Services().Resolve(key)
	if svc == nil {
		var zero T
		return zero
	}
	need, ok := svc.(T)
	if !ok {
		panic(fmt.Sprintf("seam %q: offered service (%T) does not satisfy the %q need (wrong shape)", key, svc, key))
	}
	return need
}

// sessionStores is the workspace-pin face the claim's root chain reads. The capsule's
// own local interface per the seam grammar; nil-safe absence = the chain falls through
// to the default_workspace knob and the process cwd.
type sessionStores interface {
	GetSession(ctx context.Context, id string) (core.Session, error)
}
