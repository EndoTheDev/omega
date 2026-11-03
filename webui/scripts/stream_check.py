#!/usr/bin/env python3
"""stream_check: assert the omega /chat SSE stream arrives incrementally.

Hits POST /chat with a reasoning-forcing prompt and timestamps every
data frame. Streaming is healthy when chunks arrive with small gaps and
no single burst delivers a large share of the content. Exits 0 healthy,
1 buffering, 2 no stream at all.

Usage:
  python3 stream_check.py [base_url]     # default http://127.0.0.1:8099
"""

import json
import statistics
import sys
import time
import urllib.request

PROMPT = "think step by step about how a kite flies, then answer in 3 sentences"

# contract thresholds
MAX_SMOOTH_GAP = 0.3      # s: a healthy inter-chunk gap should be under this
MIN_SMOOTH_SHARE = 0.9     # fraction of gaps that must be smooth
# healthy fast content phases pack ~30-45% of frames in a 1s window; true
# buffering delivers ~everything at once (80%+). 0.6 splits the two.
MAX_BURST_SHARE = 0.6      # max fraction of total chunks in any 1s window


def probe(base_url: str):
    req = urllib.request.Request(
        base_url.rstrip("/") + "/chat",
        data=json.dumps({"messages": [{"role": "user", "content": PROMPT}]}).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    arrivals = []
    start = time.time()
    with urllib.request.urlopen(req, timeout=240) as resp:
        for raw in resp:
            if raw.startswith(b"data:"):
                arrivals.append(time.time() - start)
    return arrivals


def check(arrivals) -> int:
    n = len(arrivals)
    if n < 10:
        print(f"FAIL: only {n} frames - no meaningful stream")
        return 2

    gaps = [arrivals[i] - arrivals[i - 1] for i in range(1, n)]
    smooth = [g for g in gaps if g < MAX_SMOOTH_GAP]
    smooth_share = len(smooth) / len(gaps)
    span = arrivals[-1] - arrivals[0]

    # burst share: most chunks inside any 1s window
    burst = 0
    lo = 0
    for hi in range(n):
        while arrivals[hi] - arrivals[lo] > 1.0:
            lo += 1
        burst = max(burst, hi - lo + 1)
    burst_share = burst / n

    print(f"frames: {n}, span: {span:.1f}s, first frame: {arrivals[0]:.2f}s")
    print(f"median gap: {statistics.median(gaps)*1000:.0f}ms, max gap: {max(gaps)*1000:.0f}ms")
    print(f"smooth gap share (<{MAX_SMOOTH_GAP*1000:.0f}ms): {smooth_share*100:.0f}% (need >= {MIN_SMOOTH_SHARE*100:.0f}%)")
    print(f"largest 1s burst: {burst_share*100:.0f}% of frames (need <= {MAX_BURST_SHARE*100:.0f}%)")

    ok = smooth_share >= MIN_SMOOTH_SHARE and burst_share <= MAX_BURST_SHARE
    print("STREAMING: HEALTHY" if ok else "STREAMING: BUFFERED/BURSTY")
    return 0 if ok else 1


def main():
    base = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8099"
    print(f"probing {base}/chat ...")
    arrivals = probe(base)
    sys.exit(check(arrivals))


if __name__ == "__main__":
    main()