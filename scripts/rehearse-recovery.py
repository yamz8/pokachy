#!/usr/bin/env python3
"""Exercise SQL/application recovery mechanics in disposable SQLite databases.

This is not a Cloudflare Worker rollback or D1 Time Travel drill. It uses only
the checked-in initial schema and temporary local SQLite files to demonstrate
that restoring a pre-deletion backup can resurrect deleted fixture data and a
stale fixture session, then verifies the application's synthetic deletion
sequence removes both again.
"""
from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path
import sqlite3
import tempfile


ROOT = Path(__file__).resolve().parents[1]
MIGRATION = ROOT / "server" / "migrations" / "0001_initial.sql"
REPORT = ROOT / "artifacts" / "recovery-rehearsal.json"
FIXTURE_USER_ID = "recovery-fixture-user"
FIXTURE_SESSION_ID = "recovery-fixture-session"


def guarded_repository_path(path: Path) -> Path:
    """Resolve only a fixed path that remains inside this repository."""
    resolved_root = ROOT.resolve()
    resolved_path = path.resolve()
    if not resolved_path.is_relative_to(resolved_root):
        raise RuntimeError("refusing a path outside the repository")
    return resolved_path


def count(connection: sqlite3.Connection, table: str, where: str = "", values: tuple[object, ...] = ()) -> int:
    return int(connection.execute(f"SELECT COUNT(*) FROM {table}{where}", values).fetchone()[0])


def main() -> int:
    migration = guarded_repository_path(MIGRATION)
    report = guarded_repository_path(REPORT)
    if not migration.is_file():
        raise RuntimeError("initial migration is required for the rehearsal")
    # The script has no command-line database argument by design. Every opened
    # database below lives under this new temporary directory.
    schema = migration.read_text(encoding="utf-8")
    report.parent.mkdir(parents=True, exist_ok=True)
    started_at = datetime.now(timezone.utc).isoformat()
    report.write_text(json.dumps({"startedAt": started_at, "passed": False, "scope": "temporary local sqlite only"}, indent=2) + "\n", encoding="utf-8")

    with tempfile.TemporaryDirectory(prefix="pokachy-recovery-rehearsal-") as temporary:
        tempdir = Path(temporary).resolve()
        source_path = tempdir / "source.sqlite"
        backup_path = tempdir / "backup.sqlite"
        restored_path = tempdir / "restored.sqlite"

        source = sqlite3.connect(source_path)
        try:
            source.execute("PRAGMA foreign_keys = ON")
            source.executescript(schema)
            source.execute(
                "INSERT INTO user(id,name,email,emailVerified,createdAt,updatedAt) VALUES (?,?,?,?,?,?)",
                (FIXTURE_USER_ID, "Recovery fixture", "recovery-fixture@example.invalid", 1, 1, 1),
            )
            source.execute(
                "INSERT INTO profiles(user_id,handle) VALUES (?,?)",
                (FIXTURE_USER_ID, "recovery_fixture"),
            )
            source.execute(
                "INSERT INTO session(id,token,expiresAt,createdAt,updatedAt,userId) VALUES (?,?,?,?,?,?)",
                (FIXTURE_SESSION_ID, "fixture-session-value", 9_999_999_999_999, 1, 1, FIXTURE_USER_ID),
            )
            source.commit()

            backup = sqlite3.connect(backup_path)
            try:
                source.backup(backup)
            finally:
                backup.close()

            # This models a later deletion in the active database. Cascades are
            # enabled explicitly so the rehearsal exercises the production DDL.
            source.execute("DELETE FROM user WHERE id=?", (FIXTURE_USER_ID,))
            source.commit()
            if count(source, "user", " WHERE id=?", (FIXTURE_USER_ID,)) or count(source, "session", " WHERE userId=?", (FIXTURE_USER_ID,)):
                raise RuntimeError("fixture deletion did not cascade in source database")
        finally:
            source.close()

        # Restore only into a separate temporary file. Restoring never
        # overwrites the active source database.
        backup = sqlite3.connect(backup_path)
        restored = sqlite3.connect(restored_path)
        try:
            backup.backup(restored)
        finally:
            backup.close()

        try:
            restored.execute("PRAGMA foreign_keys = ON")
            restored_user_count = count(restored, "user", " WHERE id=?", (FIXTURE_USER_ID,))
            restored_session_count = count(restored, "session", " WHERE userId=?", (FIXTURE_USER_ID,))
            if restored_user_count != 1 or restored_session_count != 1:
                raise RuntimeError("backup restore did not demonstrate restored fixture data and session")

            # Model the required post-restore application action: revoke every
            # restored session before removing the restored account record.
            revoked_sessions = restored.execute("DELETE FROM session").rowcount
            deleted_users = restored.execute("DELETE FROM user WHERE id=?", (FIXTURE_USER_ID,)).rowcount
            restored.commit()
            foreign_key_violations = restored.execute("PRAGMA foreign_key_check").fetchall()
            remaining_users = count(restored, "user", " WHERE id=?", (FIXTURE_USER_ID,))
            remaining_sessions = count(restored, "session", " WHERE userId=?", (FIXTURE_USER_ID,))
            if revoked_sessions != 1 or deleted_users != 1 or remaining_users or remaining_sessions or foreign_key_violations:
                raise RuntimeError("restored database did not reach the expected safe state")
        finally:
            restored.close()

    report.write_text(json.dumps({
        "startedAt": started_at,
        "finishedAt": datetime.now(timezone.utc).isoformat(),
        "schema": "server/migrations/0001_initial.sql",
        "passed": True,
        "scope": "temporary local sqlite only",
        "restored_deleted_fixture_data": restored_user_count == 1,
        "restored_stale_session_risk": restored_session_count == 1,
        "revoked_restored_sessions": revoked_sessions,
        "deleted_restored_users": deleted_users,
        "foreign_key_violations": len(foreign_key_violations),
        "remaining_fixture_users": remaining_users,
        "remaining_fixture_sessions": remaining_sessions,
    }, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print("PASS: disposable SQL recovery rehearsal completed; see artifacts/recovery-rehearsal.json")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
