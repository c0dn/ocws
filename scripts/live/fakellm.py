#!/usr/bin/env python3
"""Minimal OpenAI-compatible chat endpoint for model-free harness tests.

Usage: fakellm.py PORT LOGFILE [TOOL_NAME TOOL_ARGS_JSON]

Every request body is appended to LOGFILE (one JSON per line). If TOOL_NAME is
given and the request offers that tool but has no tool result yet, the reply
is a call to it with TOOL_ARGS_JSON; otherwise the reply is the text "ok".
"""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT, LOG = int(sys.argv[1]), sys.argv[2]
TOOL = sys.argv[3] if len(sys.argv) > 3 else ""
TOOL_ARGS = sys.argv[4] if len(sys.argv) > 4 else "{}"


def offered(req):
    return [(t.get("function") or t).get("name") for t in req.get("tools") or []]


def has_result(req):
    return any(m.get("role") == "tool" for m in req.get("messages") or [])


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def send_json(self, obj):
        body = json.dumps(obj).encode()
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.send_json({"object": "list", "data": [{"id": "fake", "object": "model"}]})

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("content-length", 0)))
        with open(LOG, "ab") as f:
            f.write(raw.replace(b"\n", b" ") + b"\n")
        try:
            req = json.loads(raw)
        except ValueError:
            req = {}
        call = TOOL and TOOL in offered(req) and not has_result(req)
        if call:
            tc = {"index": 0, "id": "call_live", "type": "function", "function": {"name": TOOL, "arguments": TOOL_ARGS}}
            deltas = [({"role": "assistant", "content": None, "tool_calls": [tc]}, None), ({}, "tool_calls")]
            message = {"role": "assistant", "content": None, "tool_calls": [dict(tc)]}
            message["tool_calls"][0].pop("index")
            finish = "tool_calls"
        else:
            deltas = [({"role": "assistant", "content": "ok"}, None), ({}, "stop")]
            message, finish = {"role": "assistant", "content": "ok"}, "stop"
        now = int(time.time())
        if req.get("stream"):
            self.send_response(200)
            self.send_header("content-type", "text/event-stream")
            self.end_headers()
            for delta, reason in deltas:
                chunk = {"id": "x", "object": "chat.completion.chunk", "created": now, "model": "fake",
                         "choices": [{"index": 0, "delta": delta, "finish_reason": reason}]}
                self.wfile.write(b"data: " + json.dumps(chunk).encode() + b"\n\n")
            self.wfile.write(b"data: [DONE]\n\n")
            return
        self.send_json({"id": "x", "object": "chat.completion", "created": now, "model": "fake",
                        "choices": [{"index": 0, "message": message, "finish_reason": finish}],
                        "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})


ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
