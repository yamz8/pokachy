#!/usr/bin/env python3
"""Local contract tests for scripts/verify-live.py."""
from __future__ import annotations

import http.server
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/verify-live.py"


class Handler(http.server.BaseHTTPRequestHandler):
    mode = "valid"
    health_requests = 0

    def log_message(self, format: str, *args: object) -> None:
        pass

    def send_data(self, status: int, content_type: str, body: bytes) -> None:
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:
        if self.mode == "redirect" and self.path == "/health":
            self.send_response(302)
            self.send_header("Location", "/other")
            self.end_headers()
            return
        if self.path == "/health":
            type(self).health_requests += 1
            if self.mode == "transient" and type(self).health_requests == 1:
                self.send_data(503, "application/json", b"{}")
                return
            payload: object = {"status": "ok", "version": "0.1.0", "revision": "abc123"}
            if self.mode == "malformed":
                self.send_data(200, "application/json", b"{")
                return
            if self.mode == "wrong-revision":
                payload = {"status": "ok", "version": "0.1.0", "revision": "old"}
            self.send_data(200, "application/json", json.dumps(payload).encode())
        elif self.path == "/api/config":
            self.send_data(200, "application/json", b'{"local":false,"github":true,"turnstileSiteKey":"public-key"}')
        elif self.path.startswith("/api/dev/mail"):
            self.send_data(404, "application/json", b"{}")
        elif self.path == "/api/state":
            self.send_data(401, "application/json", b"{}")
        elif self.path == "/":
            self.send_data(200, "text/html", b'<title>Pokachy</title><script src="/app.js"></script><link href="/style.css">')
        elif self.path == "/app.js":
            self.send_data(200, "text/javascript", b"// app")
        elif self.path == "/style.css":
            self.send_data(200, "text/css", b"/* css */")
        else:
            self.send_data(404, "text/plain", b"")


class VerifyLiveTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.base_url = f"http://127.0.0.1:{cls.server.server_port}"

    @classmethod
    def tearDownClass(cls) -> None:
        cls.server.shutdown()
        cls.thread.join()
        cls.server.server_close()

    def run_verify(self, mode: str, *extra: str) -> tuple[subprocess.CompletedProcess[str], dict[str, object]]:
        Handler.mode = mode
        Handler.health_requests = 0
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory) / "report.json"
            result = subprocess.run([sys.executable, str(SCRIPT), "--base-url", self.base_url,
                                     "--report", str(report), *extra], text=True, capture_output=True, timeout=15)
            return result, json.loads(report.read_text())

    def test_valid_server_writes_passing_report(self) -> None:
        result, report = self.run_verify("valid", "--expected-revision", "abc123")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(all(check["status"] == "pass" for check in report["checks"]))

    def test_malformed_health_fails_and_reports(self) -> None:
        result, report = self.run_verify("malformed")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(report["checks"][0]["status"], "fail")

    def test_wrong_revision_fails_and_reports(self) -> None:
        result, report = self.run_verify("wrong-revision", "--expected-revision", "abc123", "--deadline-seconds", "0.01")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(report["checks"][0]["observed"], "revision mismatch")

    def test_redirect_is_not_followed(self) -> None:
        result, report = self.run_verify("redirect")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(report["checks"][0]["observed"], "HTTP 302")

    def test_transient_health_failure_retries_to_success(self) -> None:
        result, report = self.run_verify("transient")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertGreaterEqual(Handler.health_requests, 2)
        self.assertTrue(report["passed"])
        self.assertIn("timestamp", report)

    def test_response_uses_remaining_global_deadline(self) -> None:
        class Result:
            status = 200
            headers = type("Headers", (), {"get_content_type": lambda self: "text/plain"})()
            def read(self, _size: int) -> bytes: return b""
            def __enter__(self): return self
            def __exit__(self, *_args: object) -> None: pass
        opener = mock.Mock()
        opener.open.return_value = Result()
        with mock.patch.object(__import__("time"), "monotonic", return_value=100.0), \
             mock.patch("urllib.request.build_opener", return_value=opener):
            import importlib.util
            spec = importlib.util.spec_from_file_location("verify_live", SCRIPT)
            assert spec and spec.loader
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            module.response("http://127.0.0.1", "/health", 102.5)
        self.assertEqual(opener.open.call_args.kwargs["timeout"], 2.5)


if __name__ == "__main__":
    unittest.main()
