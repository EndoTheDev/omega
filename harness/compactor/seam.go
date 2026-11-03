// The consumer-side seam resolver shared by this capsule's consults
// (part 3, the seam constitution): one resolve by key + shape assert.
// Absence is the floor (nil rides, the nil-safe degradation applies);
// a wrong-shaped offer dies loud at mount - the panic names the need
// and the mount machinery recovers it into ModulePanicError naming the
// capsule. Each consumer package owns this file; suppliers never see
// the type.
package compactor

import (
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