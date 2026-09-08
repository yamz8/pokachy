#!/usr/bin/env python3
"""Repeatable loopback end-to-end check for the Pokachy Worker and CLI.

This intentionally starts its own Wrangler instance and never reads the user's
Pokachy configuration, Wrangler persistence, secrets, or network services.
"""
from __future__ import annotations

import json
import os
import queue
import re
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SERVER = ROOT / "server"
WRANGLER = ROOT / "node_modules" / ".bin" / "wrangler"
TIMEOUT = 25
children: list[subprocess.Popen] = []
stopping = False


class CheckFailed(RuntimeError):
    pass


def fail(name: str) -> None:
    cleanup()
    raise CheckFailed(name)


def check(ok: bool, name: str) -> None:
    if not ok:
        fail(name)
    print("ok: " + name, flush=True)


def cleanup() -> None:
    global stopping
    if stopping:
        return
    stopping = True
    for proc in reversed(children):
        if proc.poll() is None:
            try:
                os.killpg(proc.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
    deadline = time.monotonic() + 5
    for proc in reversed(children):
        if proc.poll() is None:
            try:
                proc.wait(max(0.1, deadline - time.monotonic()))
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(proc.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                try:
                    proc.wait(timeout=1)
                except subprocess.TimeoutExpired:
                    pass


def on_signal(_signum: int, _frame: object) -> None:
    cleanup()
    raise KeyboardInterrupt


def run(command: list[str], name: str, *, cwd: Path, env: dict[str, str] | None = None,
        timeout: int = TIMEOUT) -> subprocess.CompletedProcess:
    proc = None
    try:
        proc = subprocess.Popen(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                                start_new_session=True)
        children.append(proc)
        stdout, stderr = proc.communicate(timeout=timeout)
        result = subprocess.CompletedProcess(command, proc.returncode, stdout, stderr)
        check(result.returncode == 0, name)
        return result
    except subprocess.TimeoutExpired:
        if proc is not None and proc.poll() is None:
            os.killpg(proc.pid, signal.SIGTERM)
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL)
                proc.wait()
        fail(name)
    except (OSError, subprocess.SubprocessError):
        fail(name)


def run_fails(command: list[str], name: str, *, cwd: Path, env: dict[str, str], expected: str) -> None:
    proc = None
    try:
        proc = subprocess.Popen(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                                start_new_session=True)
        children.append(proc)
        stdout, stderr = proc.communicate(timeout=TIMEOUT)
        result = subprocess.CompletedProcess(command, proc.returncode, stdout, stderr)
    except subprocess.TimeoutExpired:
        if proc is not None and proc.poll() is None:
            os.killpg(proc.pid, signal.SIGTERM)
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL)
                proc.wait()
        fail(name)
    except (OSError, subprocess.SubprocessError):
        fail(name)
    check(result.returncode != 0, name)
    check(expected in (result.stdout + result.stderr), name + " error")


def port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):  # type: ignore[no-untyped-def]
        return None


def request(base: str, path: str, *, method: str = "GET", body: object | None = None,
            token: str | None = None, extra_headers: dict[str, str] | None = None) -> tuple[int, object]:
    headers = {"X-Pokachy-Client": "cli"}
    data = None
    if body is not None:
        headers["Content-Type"] = "application/json"
        data = json.dumps(body).encode()
    if token:
        headers["Authorization"] = "Bearer " + token
    if extra_headers:
        headers.update(extra_headers)
    req = urllib.request.Request(base + path, data=data, method=method, headers=headers)
    try:
        with urllib.request.build_opener(NoRedirect).open(req, timeout=5) as response:
            raw = response.read(1 << 20)
            status = response.status
    except urllib.error.HTTPError as response:
        raw, status = response.read(1 << 20), response.code
    except OSError:
        return 0, {}
    try:
        return status, json.loads(raw)
    except (json.JSONDecodeError, UnicodeDecodeError):
        return status, {}


def api(base: str, path: str, *, method: str = "GET", body: object | None = None,
        token: str | None = None, status: int = 200, headers: dict[str, str] | None = None) -> object:
    actual, data = request(base, path, method=method, body=body, token=token, extra_headers=headers)
    safe_path = path.split("?", 1)[0]
    check(actual == status, "API " + safe_path + " expected " + str(status) + " got " + str(actual))
    return data


def create_user(base: str, handle: str) -> str:
    email = handle + "@example.test"
    api(base, "/api/auth/email-otp/send-verification-otp", method="POST",
        body={"email": email, "type": "sign-in"})
    mail = api(base, "/api/dev/mail?email=" + urllib.parse.quote(email))
    check(isinstance(mail, dict) and isinstance(mail.get("otp"), str), "local OTP")
    signed = api(base, "/api/auth/sign-in/email-otp", method="POST",
                 body={"email": email, "otp": mail["otp"], "name": handle})
    check(isinstance(signed, dict) and isinstance(signed.get("token"), str), "OTP sign-in")
    token = signed["token"]
    api(base, "/api/profile", method="PUT", token=token, body={"handle": handle})
    return token


def wait_for(predicate, name: str, seconds: float = 15) -> None:
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        if predicate():
            return
        time.sleep(0.1)
    fail(name)


def read_json(path: Path) -> object:
    try:
        return json.loads(path.read_text())
    except (OSError, json.JSONDecodeError):
        return {}


def launch(command: list[str], *, cwd: Path, env: dict[str, str], stdout=None, stderr=None) -> subprocess.Popen:
    try:
        proc = subprocess.Popen(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=stdout, stderr=stderr, text=True, start_new_session=True)
    except OSError:
        fail("process start")
    children.append(proc)
    return proc


def local_env() -> dict[str, str]:
    result = os.environ.copy()
    for key in list(result):
        if key.startswith("CLOUDFLARE_") or key in {"BETTER_AUTH_SECRET", "TURNSTILE_SECRET", "TURNSTILE_SITE_KEY", "GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET"}:
            result.pop(key, None)
    result.update({"WRANGLER_SEND_METRICS": "false", "WRANGLER_LOG": "none", "CI": "1"})
    return result


def main() -> None:
    check(WRANGLER.is_file(), "local Wrangler available")
    check(shutil.which("go") is not None, "Go available")
    signal.signal(signal.SIGINT, on_signal)
    signal.signal(signal.SIGTERM, on_signal)
    with tempfile.TemporaryDirectory(prefix="pokachy-e2e-") as temp:
        tmp = Path(temp)
        data, config_dir, cli_dir = tmp / "persist", tmp / "config", tmp / "cli"
        config_dir.mkdir()
        chosen_port = port()
        base = "http://127.0.0.1:" + str(chosen_port)
        # This deliberately contains no account ID or production environment.
        # Source paths are absolute because the disposable config is elsewhere.
        config = json.dumps({
            "name": "pokachy-e2e-local",
            "main": str((SERVER / "src/index.ts").resolve()),
            "compatibility_date": "2026-09-07",
            "compatibility_flags": ["nodejs_compat"],
            "assets": {"directory": str((SERVER / "public").resolve()), "binding": "ASSETS", "run_worker_first": True},
            "vars": {"ENVIRONMENT": "local", "BASE_URL": base, "TURNSTILE_SITE_KEY": "",
                     "TURNSTILE_HOSTNAMES": "127.0.0.1,localhost", "MAIL_FROM": "Pokachy <local@example.test>", "ADMIN_USER_IDS": ""},
            "d1_databases": [{"binding": "DB", "database_name": "pokachy-local",
                              "database_id": "00000000-0000-0000-0000-000000000001",
                              "migrations_dir": str((SERVER / "migrations").resolve())}],
            "durable_objects": {"bindings": [{"name": "HUBS", "class_name": "UserHub"}]},
            "migrations": [{"tag": "v1", "new_sqlite_classes": ["UserHub"]}],
        }, indent=2)
        config_path = config_dir / "wrangler.jsonc"
        config_path.write_text(config)
        binary = tmp / "pokachy"
        run(["go", "build", "-o", str(binary), "./cmd/pokachy"], "native CLI build", cwd=ROOT / "cli", timeout=120)
        run([str(WRANGLER), "d1", "migrations", "apply", "pokachy-local", "--local",
             "--persist-to", str(data), "--config", str(config_path)], "local D1 migration", cwd=SERVER, env=local_env(), timeout=120)
        worker_log = (tmp / "worker.log").open("w")
        worker_env = local_env()
        worker = launch([str(WRANGLER), "dev", "--ip", "127.0.0.1", "--port", str(chosen_port),
                         "--local", "--persist-to", str(data), "--var", "BASE_URL:" + base,
                         "--config", str(config_path)], cwd=SERVER, env=worker_env,
                        stdout=worker_log, stderr=subprocess.STDOUT)
        wait_for(lambda: request(base, "/health")[0] == 200, "worker health")

        # bobby is a browser/API-only user; clialice proves the native device flow.
        bobby = create_user(base, "bobby")
        alice_browser = create_user(base, "clialice")
        cli_env = os.environ.copy()
        cli_env.update({"POKACHY_CONFIG_DIR": str(cli_dir), "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1"})
        init = launch([str(binary), "init", "--server", base, "--no-browser"], cwd=ROOT, env=cli_env,
                      stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        lines: queue.Queue[str] = queue.Queue()
        captured: list[str] = []
        def collect() -> None:
            assert init.stdout is not None
            for line in init.stdout:
                captured.append(line)
                lines.put(line)
        threading.Thread(target=collect, daemon=True).start()
        code = ""
        end = time.monotonic() + 10
        while time.monotonic() < end and not code:
            try:
                found = re.search(r"Your device code: ([A-Z0-9-]+)", lines.get(timeout=0.2))
                if found:
                    code = found.group(1)
            except queue.Empty:
                pass
        check(bool(code), "CLI device code")
        quoted = urllib.parse.quote(code)
        api(base, "/api/auth/device?user_code=" + quoted, token=alice_browser)
        api(base, "/api/auth/device/approve", method="POST", token=alice_browser, body={"userCode": code})
        try:
            init.wait(15)
        except subprocess.TimeoutExpired:
            fail("CLI device completion")
        check(init.returncode == 0, "CLI device approval exit " + str(init.returncode))
        initialized = read_json(cli_dir / "state.json")
        check(isinstance(initialized, dict) and initialized.get("me", {}).get("handle") == "clialice", "CLI authenticated account")
        check((cli_dir / "credentials.json").is_file(), "CLI credentials")

        run_fails([str(binary), "poke", "@bobby"], "CLI poke requires friendship", cwd=ROOT, env=cli_env,
                  expected="Poke unavailable")
        run([str(binary), "friends", "add", "@bobby"], "CLI friendship request", cwd=ROOT, env=cli_env)
        run_fails([str(binary), "poke", "@bobby"], "CLI poke requires consent", cwd=ROOT, env=cli_env,
                  expected="Poke unavailable")
        no_pokes = api(base, "/api/state", token=bobby)
        check(isinstance(no_pokes, dict) and no_pokes.get("inbox") == [], "denied CLI pokes create no inbox item")
        api(base, "/api/friends/clialice/accept", method="POST", token=bobby, body={})

        notify_log = tmp / "notifications.log"
        mock_bin = tmp / "bin"
        mock_bin.mkdir()
        mock = mock_bin / "notify-send"
        mock.write_text("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$POKACHY_E2E_NOTIFY_LOG\"\nprintf '\\n'\n")
        mock.chmod(0o700)
        daemon_env = cli_env.copy()
        daemon_env["PATH"] = str(mock_bin) + os.pathsep + daemon_env.get("PATH", "")
        daemon_env["POKACHY_E2E_NOTIFY_LOG"] = str(notify_log)
        daemon_log = (tmp / "daemon.log").open("w")
        daemon = launch([str(binary), "daemon"], cwd=ROOT, env=daemon_env, stdout=daemon_log, stderr=subprocess.STDOUT)
        wait_for(lambda: isinstance(read_json(cli_dir / "state.json"), dict) and bool(read_json(cli_dir / "state.json").get("online")), "daemon initial sync")
        run([str(binary), "poke", "@bobby"], "CLI poke after consent", cwd=ROOT, env=cli_env)
        bobby_state = api(base, "/api/state", token=bobby)
        check(isinstance(bobby_state, dict) and any(
            isinstance(poke, dict) and poke.get("handle") == "clialice"
            for poke in bobby_state.get("inbox", [])
        ), "Bobby inbox records CLI poke")
        # Bobby replies so the native Alice daemon receives an incoming poke.
        api(base, "/api/pokes/clialice", method="POST", token=bobby, body={}, status=201,
            headers={"Idempotency-Key": "first-poke-key-0001"})
        wait_for(lambda: notify_log.exists() and len(notify_log.read_text().splitlines()) == 1, "poke notification")
        # A restart must respect notified.json and avoid repeating the same poke.
        os.killpg(daemon.pid, signal.SIGTERM)
        daemon.wait(8)
        (cli_dir / "state.json").unlink(missing_ok=True)
        daemon = launch([str(binary), "daemon"], cwd=ROOT, env=daemon_env, stdout=daemon_log, stderr=subprocess.STDOUT)
        wait_for(lambda: isinstance(read_json(cli_dir / "state.json"), dict) and bool(read_json(cli_dir / "state.json").get("online")), "daemon restart")
        time.sleep(1)
        check(len(notify_log.read_text().splitlines()) == 1, "notification restart dedup")

        inbox = run([str(binary), "inbox", "--json"], "CLI inbox", cwd=ROOT, env=cli_env)
        pending = json.loads(inbox.stdout)
        check(isinstance(pending, list) and len(pending) == 1 and isinstance(pending[0].get("id"), str), "CLI inbox state")
        run([str(binary), "dismiss", pending[0]["id"]], "CLI dismiss", cwd=ROOT, env=cli_env)
        run([str(binary), "quiet", "on"], "CLI quiet", cwd=ROOT, env=cli_env)
        time.sleep(11)
        api(base, "/api/pokes/clialice", method="POST", token=bobby, body={}, status=201,
            headers={"Idempotency-Key": "quiet-poke-key-0002"})
        wait_for(lambda: any(p.get("handle") == "bobby" for p in read_json(cli_dir / "state.json").get("inbox", [])), "quiet poke delivery")
        time.sleep(1)
        check(len(notify_log.read_text().splitlines()) == 1, "quiet notification suppression")
        quiet_inbox = read_json(cli_dir / "state.json").get("inbox", [])
        check(isinstance(quiet_inbox, list) and len(quiet_inbox) == 1, "quiet inbox state")
        run([str(binary), "dismiss", quiet_inbox[0]["id"]], "CLI quiet dismiss", cwd=ROOT, env=cli_env)
        run([str(binary), "quiet", "off"], "CLI quiet off", cwd=ROOT, env=cli_env)
        time.sleep(11)
        api(base, "/api/pokes/clialice", method="POST", token=bobby, body={}, status=201,
            headers={"Idempotency-Key": "resumed-poke-key-0003"})
        wait_for(lambda: len(notify_log.read_text().splitlines()) == 2, "notifications resume")

        credentials = read_json(cli_dir / "credentials.json")
        check(isinstance(credentials, dict) and isinstance(credentials.get("token"), str), "device token")
        api(base, "/api/auth/sign-out", method="POST", token=credentials["token"], body={})
        # A hub update forces the daemon to observe the revoked bearer session.
        api(base, "/api/friends/clialice", method="DELETE", token=bobby, body={})
        wait_for(lambda: daemon.poll() is not None, "revoked daemon exit")
        state = read_json(cli_dir / "state.json")
        check(isinstance(state, dict) and state.get("needsLogin") is True, "revoked session state")
        run([str(binary), "logout"], "CLI logout", cwd=ROOT, env=cli_env)
        check(not (cli_dir / "credentials.json").exists(), "logout clears credentials")
        cleanup()
        print("verify-e2e: passed")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("verify-e2e: interrupted", file=sys.stderr)
        sys.exit(130)
    except CheckFailed as exc:
        print("verify-e2e: failed: " + str(exc), file=sys.stderr)
        sys.exit(1)
    finally:
        cleanup()
