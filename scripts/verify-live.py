#!/usr/bin/env python3
"""Read-only, bounded smoke verification for a deployed Pokachy Worker."""
from __future__ import annotations

import argparse
import ipaddress
import json
from pathlib import Path
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_BASE_URL = "https://pokachy.com"
DEFAULT_REPORT = ROOT / "artifacts/live-verification.json"
USER_AGENT = "Pokachy-verification/1"
DEADLINE_SECONDS = 60


class NoRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, newurl):
        return None


class ProbeError(RuntimeError):
    def __init__(self, detail: str, retryable: bool = False, revision: str | None = None):
        super().__init__(detail)
        self.retryable = retryable
        self.revision = revision


def validate_base_url(value: str) -> str:
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme not in ("https", "http") or not parsed.hostname:
        raise ValueError("--base-url must be an HTTPS origin or an HTTP loopback origin")
    if parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in ("", "/"):
        raise ValueError("--base-url must be an origin without credentials, path, query, or fragment")
    if parsed.scheme == "http" and not is_loopback(parsed.hostname):
        raise ValueError("HTTP --base-url is allowed only for loopback testing")
    host = parsed.hostname
    if ":" in host:
        host = f"[{host}]"
    return f"{parsed.scheme}://{host}" + (f":{parsed.port}" if parsed.port else "")


def is_loopback(host: str) -> bool:
    if host.lower() == "localhost":
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


def response(base_url: str, path: str, deadline: float) -> tuple[int, str, bytes]:
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise ProbeError("deadline exceeded", retryable=True)
    request = urllib.request.Request(base_url + path, headers={"User-Agent": USER_AGENT, "Accept": "application/json, text/html, text/css, text/javascript, */*"})
    opener = urllib.request.build_opener(NoRedirects())
    try:
        with opener.open(request, timeout=min(10, remaining)) as result:
            return result.status, result.headers.get_content_type(), result.read(256_000)
    except urllib.error.HTTPError as error:
        return error.code, error.headers.get_content_type() if error.headers else "", b""
    except (urllib.error.URLError, TimeoutError, OSError) as error:
        raise ProbeError("network error: " + type(error).__name__, retryable=True) from error


def require_status(status: int, expected: int) -> None:
    if status != expected:
        raise ProbeError(f"HTTP {status}", retryable=status >= 500)


def json_body(body: bytes) -> dict[str, object]:
    try:
        parsed = json.loads(body)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ProbeError("malformed JSON") from error
    if not isinstance(parsed, dict):
        raise ProbeError("JSON object required")
    return parsed


def check(name: str, expected: str, action) -> dict[str, object]:
    item: dict[str, object] = {"name": name, "expected": expected}
    try:
        observed, revision = action()
        item["status"] = "pass"
        item["observed"] = observed
        if revision is not None:
            item["revision"] = revision
    except ProbeError as error:
        item["status"] = "fail"
        item["observed"] = str(error)
        item["retryable"] = error.retryable
        if error.revision is not None:
            item["revision"] = error.revision
    return item


def run_once(base_url: str, expected_revision: str | None, deadline: float) -> tuple[list[dict[str, object]], bool]:
    revision: str | None = None

    def get(path: str) -> tuple[int, str, bytes]:
        return response(base_url, path, deadline)

    def health():
        nonlocal revision
        status, content_type, body = get("/health")
        require_status(status, 200)
        if content_type != "application/json":
            raise ProbeError("content type " + content_type)
        data = json_body(body)
        if data.get("status") != "ok" or not isinstance(data.get("version"), str) or not data["version"]:
            raise ProbeError("status and nonempty version required")
        candidate = data.get("revision")
        if candidate is not None and not isinstance(candidate, str):
            raise ProbeError("revision must be text")
        revision = candidate
        if expected_revision is not None and revision != expected_revision:
            raise ProbeError("revision mismatch", retryable=True, revision=revision)
        return "HTTP 200, JSON status ok", revision

    def config():
        status, content_type, body = get("/api/config")
        require_status(status, 200)
        if content_type != "application/json":
            raise ProbeError("content type " + content_type)
        data = json_body(body)
        if data.get("local") is not False or data.get("github") is not True or not isinstance(data.get("turnstileSiteKey"), str) or not data["turnstileSiteKey"]:
            raise ProbeError("public GitHub and Turnstile configuration required")
        return "HTTP 200, public configuration", None

    def exact_status(path: str, wanted: int):
        def action():
            status, _content_type, _body = get(path)
            require_status(status, wanted)
            return f"HTTP {status}", None
        return action

    def page():
        status, content_type, body = get("/")
        require_status(status, 200)
        if content_type != "text/html":
            raise ProbeError("content type " + content_type)
        text = body.decode("utf-8", "replace")
        if "Pokachy" not in text or 'src="/app.js"' not in text or 'href="/style.css"' not in text:
            raise ProbeError("expected Pokachy page markup")
        return "HTTP 200, HTML Pokachy", None

    def asset(path: str, content_type: str):
        def action():
            status, actual, _body = get(path)
            require_status(status, 200)
            if actual != content_type:
                raise ProbeError("content type " + actual)
            return f"HTTP 200, {actual}", None
        return action

    checks = [
        check("health", "HTTP 200 JSON status=ok with nonempty version", health),
        check("config", "HTTP 200 public GitHub and nonempty Turnstile site key", config),
        check("production dev-mail disabled", "HTTP 404", exact_status("/api/dev/mail?email=smoke@example.test", 404)),
        check("unauthenticated state", "HTTP 401", exact_status("/api/state", 401)),
        check("homepage", "HTTP 200 HTML with Pokachy and app assets", page),
        check("app asset", "HTTP 200 text/javascript", asset("/app.js", "text/javascript")),
        check("style asset", "HTTP 200 text/css", asset("/style.css", "text/css")),
    ]
    failed = [item for item in checks if item.get("status") == "fail"]
    retryable = bool(failed) and all(item.get("retryable") is True for item in failed)
    return checks, retryable


def write_report(path: Path, base_url: str, expected_revision: str | None, checks: list[dict[str, object]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                                "passed": all(item["status"] == "pass" for item in checks),
                                "base_url": base_url, "expected_revision": expected_revision,
                                "checks": checks}, indent=2, sort_keys=True) + "\n")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL)
    parser.add_argument("--expected-revision")
    parser.add_argument("--report", type=Path, default=DEFAULT_REPORT)
    parser.add_argument("--deadline-seconds", type=float, default=DEADLINE_SECONDS,
                        help=argparse.SUPPRESS)
    args = parser.parse_args()
    try:
        base_url = validate_base_url(args.base_url)
    except ValueError as error:
        parser.error(str(error))

    if args.deadline_seconds <= 0:
        parser.error("--deadline-seconds must be positive")
    deadline = time.monotonic() + args.deadline_seconds
    delay = 1.0
    while True:
        checks, retryable = run_once(base_url, args.expected_revision, deadline)
        failed = [item for item in checks if item["status"] == "fail"]
        if not failed or not retryable or time.monotonic() >= deadline:
            write_report(args.report, base_url, args.expected_revision, checks)
            for item in checks:
                print(f"{item['status'].upper()}: {item['name']} ({item['observed']})")
            return 0 if not failed else 1
        time.sleep(min(delay, max(0, deadline - time.monotonic())))
        delay = min(delay * 2, 8)


if __name__ == "__main__":
    sys.exit(main())
