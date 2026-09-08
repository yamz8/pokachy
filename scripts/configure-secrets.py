#!/usr/bin/env python3
"""Collect local production secrets without exposing them to the shell."""

from __future__ import annotations

import getpass
import json
import os
import secrets
import stat
import sys
import tempfile
import warnings
from pathlib import Path
from typing import Any, Callable


ROOT = Path(__file__).resolve().parent.parent
DEFAULT_PATH = ROOT / ".secrets" / "production.json"


def _ensure_directory(path: Path) -> None:
    """Create or validate a private, non-symlink directory."""
    try:
        info = path.lstat()
    except FileNotFoundError:
        path.mkdir(mode=0o700)
        info = path.lstat()
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
        raise RuntimeError(f"Refusing unsafe secrets directory: {path}")
    path.chmod(0o700)


def _read_existing(path: Path) -> dict[str, Any]:
    try:
        info = path.lstat()
    except FileNotFoundError:
        return {}
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise RuntimeError(f"Refusing unsafe secrets file: {path}")
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"Cannot read existing secrets file: {path}") from exc
    if not isinstance(data, dict) or any(
        not isinstance(key, str) or not isinstance(value, str) for key, value in data.items()
    ):
        raise RuntimeError("Existing secrets file must contain only string key/value pairs")
    return data


def _atomic_write(path: Path, data: dict[str, Any]) -> None:
    payload = (json.dumps(data, indent=2, sort_keys=True) + "\n").encode("utf-8")
    fd, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary_path = Path(temporary)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "wb") as output:
            output.write(payload)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary_path, path)
        path.chmod(0o600)
    except Exception:
        try:
            temporary_path.unlink()
        except FileNotFoundError:
            pass
        raise


def _secure_secret(prompt: str) -> str:
    if not sys.stdin.isatty():
        raise RuntimeError("The client secret must be entered from an interactive terminal")
    with warnings.catch_warnings():
        warnings.simplefilter("error", getpass.GetPassWarning)
        return getpass.getpass(prompt)


def configure(
    path: Path = DEFAULT_PATH,
    input_fn: Callable[[str], str] = input,
    secret_fn: Callable[[str], str] = _secure_secret,
) -> None:
    directory = path.parent
    _ensure_directory(directory)
    values = _read_existing(path)
    if "BETTER_AUTH_SECRET" in values and not values["BETTER_AUTH_SECRET"]:
        raise RuntimeError("Existing BETTER_AUTH_SECRET is empty")

    client_id = input_fn("GitHub OAuth Client ID (blank keeps the current value): ").strip()
    if client_id:
        values["GITHUB_CLIENT_ID"] = client_id
    elif not values.get("GITHUB_CLIENT_ID"):
        raise RuntimeError("GitHub OAuth Client ID is required on first setup")

    client_secret = secret_fn("GitHub OAuth Client Secret (blank keeps the current value): ").strip()
    if client_secret:
        values["GITHUB_CLIENT_SECRET"] = client_secret
    elif not values.get("GITHUB_CLIENT_SECRET"):
        raise RuntimeError("GitHub OAuth Client Secret is required on first setup")

    if "BETTER_AUTH_SECRET" not in values:
        values["BETTER_AUTH_SECRET"] = secrets.token_urlsafe(48)

    _atomic_write(path, values)
    print(f"Credentials saved locally to {path}. Nothing has been uploaded.")


if __name__ == "__main__":
    try:
        configure()
    except (EOFError, KeyboardInterrupt, OSError, RuntimeError, getpass.GetPassWarning) as error:
        if isinstance(error, (EOFError, KeyboardInterrupt)):
            raise SystemExit("Input cancelled.")
        else:
            raise SystemExit(f"Error: {error}")
