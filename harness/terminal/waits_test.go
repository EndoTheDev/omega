package terminal

import "time"

// testWaitTimeout bounds every poll-until-true wait in this package's tests. It exists
// to catch a HANG, not to time correctness: a healthy condition can take seconds when
// the full suite runs packages in parallel and starves the goroutine under test.
const testWaitTimeout = 30 * time.Second
