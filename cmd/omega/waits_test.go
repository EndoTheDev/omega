package main

import "time"

// testWaitTimeout bounds every poll-until-true wait: it catches a HANG, not timing.
const testWaitTimeout = 30 * time.Second
