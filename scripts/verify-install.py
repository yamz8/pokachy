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


def run_fails(command: list[str], name: str, *, cwd: Path, env: dict[str, str], expected: str) -> None:
    result = subprocess.run(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=30)
    output = result.stdout + result.stderr
    check(result.returncode != 0 and expected in output, name, output.strip()[-500:])


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


def mock_curl(directory: Path, releases: Path) -> None:
    script = directory / "curl"
    script.write_text("""#!/bin/sh
set -eu
output=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output=$2; shift 2 ;;
    --proto|--tlsv1.2) if [ "$1" = --proto ]; then shift 2; else shift; fi ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
[ -n "$output" ] && [ -n "$url" ]
cp "$POKACHY_TEST_RELEASES/${url##*/}" "$output"
""")
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
    check((prefix / "share/icons/hicolor/scalable/apps/pokachy.svg").is_file(), "installed parrot notification icon")
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
            archives = [dist / f"pokachy_linux_{item}.tar.gz" for item in ("amd64", "arm64")]
            sums = {}
            for line in (dist / "SHA256SUMS").read_text().splitlines():
                digest, name = line.split(maxsplit=1)
                sums[name] = digest
            for archive in archives:
                check(archive.is_file(), "release archive " + archive.name)
                actual = hashlib.sha256(archive.read_bytes()).hexdigest()
                check(sums.get(archive.name) == actual, "SHA256SUMS " + archive.name)
            public_installer = dist / "install.sh"
            check(public_installer.is_file(), "release includes the public installer")
            check(sums.get(public_installer.name) == hashlib.sha256(public_installer.read_bytes()).hexdigest(),
                  "SHA256SUMS install.sh")
            required = ["bin/pokachy", "scripts/install.sh", "packaging/systemd/pokachy.service",
                        "manifest.json", "README.md", "LICENSE", "plugins/omarchy/BarWidget.qml",
                        "plugins/omarchy/manifest.json", "plugins/omarchy/README.md", "plugins/omarchy/LICENSE",
                        "plugins/omarchy/Panel.qml", "plugins/omarchy/BrandIcon.qml",
                        "plugins/omarchy/PokeButton.qml", "plugins/omarchy/PokeHand.qml",
                        "plugins/omarchy/SquareKeyboardPanel.qml",
                        "assets/brand/pokachy-parrot.svg", "assets/brand/pokachy-symbol.svg"]
            roots: dict[str, Path] = {}
            for archive in archives:
                extracted = Path(work) / archive.stem.removesuffix(".tar")
                extracted.mkdir()
                safe_extract(archive, extracted)
                directories = [item for item in extracted.iterdir() if item.is_dir()]
                check(len(directories) == 1, archive.name + " has one release root")
                root = directories[0]
                for item in required:
                    check((root / item).is_file(), archive.name + " contains " + item)
                roots[archive.name] = root

            plugin = dist / "pokachy-omarchy"
            plugin_required = {"manifest.json", "README.md", "LICENSE", "BarWidget.qml", "Panel.qml",
                               "BrandIcon.qml", "PokeButton.qml", "PokeHand.qml", "SquareKeyboardPanel.qml",
                               "pokachy-bar-icon.png"}
            check(plugin.is_dir(), "standalone Omarchy plugin export")
            check({item.name for item in plugin.iterdir()} == plugin_required,
                  "standalone Omarchy plugin contains only its runtime and documentation")
            plugin_manifest = json.loads((plugin / "manifest.json").read_text())
            root_manifest = json.loads((ROOT / "manifest.json").read_text())
            check(plugin_manifest.get("version") == version == root_manifest.get("version"),
                  "CLI and Omarchy manifest versions match")
            check(plugin_manifest.get("id") == root_manifest.get("id") == "com.pokachy.poke",
                  "Omarchy plugin id matches")
            check(plugin_manifest.get("entryPoints", {}).get("barWidget") == "BarWidget.qml",
                  "standalone Omarchy entry point is local")

            archive = archives[0 if arch == "amd64" else 1]
            root = roots[archive.name]
            mock = Path(work) / "mock-bin"
            mock.mkdir()
            attempts = Path(work) / "attempts.log"
            mock_command(mock, "systemctl", attempts)
            mock_command(mock, "notify-send", attempts)
            verify_install(root, Path(work) / "prefix", Path(work) / "config", version, mock, attempts)
            verify_install(root, Path(work) / "prefix spaces %", Path(work) / "config spaces %", version, mock, attempts)

            bootstrap_bin = Path(work) / "bootstrap-bin"
            bootstrap_bin.mkdir()
            mock_curl(bootstrap_bin, dist)
            bootstrap_home = Path(work) / "bootstrap-home"
            bootstrap_home.mkdir()
            bootstrap_temp = Path(work) / "bootstrap-temp"
            bootstrap_temp.mkdir()
            bootstrap_env = os.environ.copy()
            bootstrap_env.update(HOME=str(bootstrap_home), XDG_CONFIG_HOME=str(bootstrap_home / "config"),
                                 POKACHY_INSTALL_PREFIX=str(bootstrap_home / "local"),
                                 POKACHY_DOWNLOAD_BASE_URL="https://downloads.example.test/latest",
                                 POKACHY_UNAME_MACHINE=platform.machine(), POKACHY_TEST_RELEASES=str(dist),
                                 TMPDIR=str(bootstrap_temp),
                                 PATH=str(bootstrap_bin) + os.pathsep + bootstrap_env.get("PATH", ""))
            run(["sh", str(ROOT / "server/public/install.sh")], "bootstrap installer", cwd=ROOT,
                env=bootstrap_env, timeout=30)
            check((bootstrap_home / "local/bin/pokachy").is_file(), "bootstrap installed CLI")
            check((bootstrap_home / "config/systemd/user/pokachy.service").is_file(),
                  "bootstrap installed user service")
            check(not any(bootstrap_temp.iterdir()), "bootstrap cleaned temporary files")

            unsupported_env = bootstrap_env.copy()
            unsupported_env["POKACHY_UNAME_MACHINE"] = "mips64"
            run_fails(["sh", str(ROOT / "server/public/install.sh")], "bootstrap rejects unsupported architecture",
                      cwd=ROOT, env=unsupported_env, expected="unsupported architecture")

            bad_releases = Path(work) / "bad-releases"
            bad_releases.mkdir()
            for item in dist.iterdir():
                if item.is_file():
                    (bad_releases / item.name).write_bytes(item.read_bytes())
            target = bad_releases / archive.name
            target.write_bytes(target.read_bytes() + b"tampered")
            bad_env = bootstrap_env.copy()
            bad_env["POKACHY_TEST_RELEASES"] = str(bad_releases)
            run_fails(["sh", str(ROOT / "server/public/install.sh")], "bootstrap rejects checksum mismatch",
                      cwd=ROOT, env=bad_env, expected="FAILED")
    except (CheckFailed, OSError, json.JSONDecodeError, tarfile.TarError, ValueError, KeyboardInterrupt) as error:
        print("FAIL: " + str(error), file=sys.stderr)
        return 1
    print("PASS: install verification complete")
    return 0


if __name__ == "__main__":
    sys.exit(main())
