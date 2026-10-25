#!/usr/bin/env python3
"""Mock ollama-compatible provider for the t_1c9793a8 journey lane.

GET /api/tags  -> one model list
POST /api/chat -> NDJSON stream: thinking chunks then content chunks,
                  then done. Mimics the real glm-5.3 lane profile the
                  flake lived under: identical one-word answers, a
                  pre-stream delay, ~30 streamed chunks, 250ms cadence
                  batching downstream.

Every response says READY (the real model answered the same word both
runs - the recipe's exact prompt is deterministic).
"""
import json
import random
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

_lock = threading.Lock()


def _thinking():
    return (
        "The user wants me to reply with exactly one word: READY. "
        "Reply with one word only. " * 8
    )


class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def _stream_line(self, obj):
        self.wfile.write((json.dumps(obj) + "\n").encode())
        self.wfile.flush()

    def _serve(self):
        length = int(self.headers.get("Content-Length", 0) or 0)

        msgs = []
        try:
            body = self.rfile.read(length).decode() if length else ""
            msgs = json.loads(body).get("messages", [])
        except Exception:
            msgs = []

        if self.path == "/api/chat" and self.command == "POST":
            self.send_response(200)
            self.send_header("Content-Type", "application/x-ndjson")
            self.send_header("Connection", "close")
            self.end_headers()
            # realistic latency: jittered pre-stream delay (the keypool
            # glm profile the flake lived under)
            time.sleep(random.uniform(0.8, 2.5))
            # thinking phase: a few chunks over ~0.5-1.5s
            think = _thinking()
            step = max(1, len(think) // 6)
            for i in range(0, len(think), step):
                self._stream_line({"message": {"role": "assistant", "thinking": think[i:i + step]}})
                time.sleep(random.uniform(0.04, 0.12))
            # content: 24 short chunks; exactly one carries READY (the
            # real model answers the same word deterministically)
            for i in range(24):
                c = "READY" if i == 12 else ""
                self._stream_line({"message": {"role": "assistant", "content": c}})
                time.sleep(random.uniform(0.02, 0.08))
            self._stream_line({
                "message": {"role": "assistant", "content": ""},
                "done": True,
                "prompt_eval_count": 120,
                "eval_count": 42,
                "done_reason": "stop",
            })
            return

        body = json.dumps({"models": [{"name": "glm-5.3", "model": "glm-5.3"}]}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self._serve()

    def do_POST(self):
        self._serve()


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", 18201), H).serve_forever()