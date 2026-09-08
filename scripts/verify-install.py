#!/usr/bin/env python3
"""Build and verify the contents and isolated installation of a release."""
from __future__ import annotations

import hashlib
import json
import os
import platform
from pathlib import Path, PurePosixPath
import signal
import shlex
import subprocess
import sys
import tarfile
import tempfile


ROOT = Path(__file__).resolve().parents[1]
TIMEOUT = 300


class CheckFailed(RuntimeError):
    pass


def check(condition: bool, name: str, detail: str = "") -> None:
    if not condition:
        raise CheckFailed(name + (": " + detail if detail else ""))
    print("PASS: " + name, flush=True)


def run(command: list[str], name: str, *, cwd: Path, env: dict[str, str], timeout: int = TIMEOUT) -> subprocess.CompletedProcess[str]:
    proc: subprocess.Popen[str] | None = None

    def stop() -> None:
        if proc is None:
            return
        try:
            os.killpg(proc.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            pass
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            pass

    try:
        proc = subprocess.Popen(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                                start_new_session=True)
        stdout, stderr = proc.communicate(timeout=timeout)
        result = subprocess.CompletedProcess(command, proc.returncode, stdout, stderr)
    except (subprocess.TimeoutExpired, KeyboardInterrupt):
        stop()
        raise CheckFailed(name + " timed out or was interrupted")
    except OSError as error:
        raise CheckFailed(name + ": " + str(error)) from error
    check(result.returncode == 0, name, result.stderr.strip()[-500:])
    return result


def safe_extract(archive: Path, destination: Path) -> None:
    with tarfile.open(archive, "r:gz") as tar:
        for member in tar.getmembers():
            path = PurePosixPath(member.name)
            if path.is_absolute() or ".." in path.parts:
                raise CheckFailed("unsafe archive path: " + member.name)
            if member.issym() or member.islnk() or member.isdev():
                raise CheckFailed("unsafe archive entry: " + member.name)
            target = (destination / Path(*path.parts)).resolve()
            if target != destination and destination not in target.parents:
                raise CheckFailed("archive path escapes destination: " + member.name)
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            elif member.isfile():
                target.parent.mkdir(parents=True, exist_ok=True)
                source = tar.extractfile(member)
                if source is None:
                    raise CheckFailed("unreadable archive entry: " + member.name)
                target.write_bytes(source.read())
                target.chmod(member.mode & 0o777)
            else:
                raise CheckFailed("unsupported archive entry: " + member.name)
    print("PASS: safely extracted " + archive.name, flush=True)


def mock_command(directory: Path, name: str, log: Path) -> None:
    script = directory / name
    script.write_text("#!/bin/sh\nprintf '%s\\n' " + name + " >> " + shlex.quote(str(log)) + "\nexit 97\n")
    script.chmod(0o755)


def verify_install(extracted: Path, prefix: Path, config: Path, package_version: str,
                   mock_bin: Path, attempts: Path) -> None:
    env = os.environ.copy()
    env.update(XDG_CONFIG_HOME=str(config), POKACHY_INSTALL_PREFIX=str(prefix),
               PATH=str(mock_bin) + os.pathsep + env.get("PATH", ""))
    run(["bash", str(extracted / "scripts/install.sh")], "installer", cwd=extracted, env=env, timeout=30)
    check(not attempts.exists() or not attempts.read_text().strip(), "installer did not invoke desktop commands")
    binary = prefix / "bin/pokachy"
    service = config / "systemd/user/pokachy.service"
    check(binary.is_file() and os.access(binary, os.X_OK), "installed CLI is executable")
    result = run([str(binary), "version"], "installed CLI version", cwd=extracted, env=env, timeout=10)
    check(result.stdout.strip() == package_version, "installed CLI matches package version")
    lines = service.read_text().splitlines()
    expected = str(prefix / "bin/pokachy").replace("\\", "\\\\").replace('"', '\\"').replace("%", "%%")
    check(f'ExecStart="{expected}" daemon' in lines, "service ExecStart targets installed CLI")


def main() -> int:
    def interrupted(_signum: int, _frame: object) -> None:
        raise KeyboardInterrupt

    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    package = json.loads((ROOT / "package.json").read_text())
    version = str(package["version"])
    machine = platform.machine().lower()
    arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(machine)
    if arch is None:
        print("FAIL: unsupported architecture " + machine, file=sys.stderr)
        return 1
    try:
        with tempfile.TemporaryDirectory(prefix="pokachy-install-") as work:
            dist = Path(work) / "dist"
            env = os.environ.copy()
            env["POKACHY_DIST_DIR"] = str(dist)
            run(["bash", str(ROOT / "scripts/package-release.sh")], "release packaging", cwd=ROOT, env=env)
            archives = [dist / f"pokachy_{version}_linux_{item}.tar.gz" for item in ("amd64", "arm64")]
            sums = {}
            for line in (dist / "SHA256SUMS").read_text().splitlines():
                digest, name = line.split(maxsplit=1)
                sums[name] = digest
            for archive in archives:
                check(archive.is_file(), "release archive " + archive.name)
                actual = hashlib.sha256(archive.read_bytes()).hexdigest()
                check(sums.get(archive.name) == actual, "SHA256SUMS " + archive.name)
            required = ["bin/pokachy", "scripts/install.sh", "packaging/systemd/pokachy.service",
                        "manifest.json", "README.md", "LICENSE", "plugins/omarchy/BarWidget.qml",
                        "plugins/omarchy/Panel.qml"]
            roots: dict[str, Path] = {}
            for archive in archives:
                extracted = Path(work) / archive.stem.removesuffix(".tar")
                extracted.mkdir()
                safe_extract(archive, extracted)
                root = extracted / archive.name.removesuffix(".tar.gz")
                for item in required:
                    check((root / item).is_file(), archive.name + " contains " + item)
                roots[archive.name] = root
            archive = archives[0 if arch == "amd64" else 1]
            root = roots[archive.name]
            mock = Path(work) / "mock-bin"
            mock.mkdir()
            attempts = Path(work) / "attempts.log"
            mock_command(mock, "systemctl", attempts)
            mock_command(mock, "notify-send", attempts)
            verify_install(root, Path(work) / "prefix", Path(work) / "config", version, mock, attempts)
            verify_install(root, Path(work) / "prefix spaces %", Path(work) / "config spaces %", version, mock, attempts)
    except (CheckFailed, OSError, json.JSONDecodeError, tarfile.TarError, ValueError, KeyboardInterrupt) as error:
        print("FAIL: " + str(error), file=sys.stderr)
        return 1
    print("PASS: install verification complete")
    return 0


if __name__ == "__main__":
    sys.exit(main())
