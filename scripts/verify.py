#!/usr/bin/env python3
"""Run Pokachy's repeatable verification suite and write a machine-readable report."""
import argparse
import datetime
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report', type=Path, default=ROOT / 'artifacts/verification.json')
    args = parser.parse_args()
    def interrupted(_signum, _frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupted)
    checks = [
        ('Worker types', ['npm', 'run', 'check'], ROOT, 180),
        ('Worker integration tests', ['npm', 'test'], ROOT, 180),
        ('Go tests', ['go', 'test', './...'], ROOT / 'cli', 180),
        ('Go static analysis', ['go', 'vet', './...'], ROOT / 'cli', 180),
        ('Deployment smoke checker tests', [sys.executable, 'scripts/test-verify-live.py'], ROOT, 60),
        ('Release installer', [sys.executable, 'scripts/verify-install.py'], ROOT, 360),
        ('Native CLI end-to-end', [sys.executable, 'scripts/verify-e2e.py'], ROOT, 360),
    ]
    report = {'startedAt': datetime.datetime.now(datetime.timezone.utc).isoformat(),
              'passed': False, 'checks': []}
    env = dict(os.environ, WRANGLER_SEND_METRICS='false', WRANGLER_WRITE_LOGS='false', CI='true')
    failed = False
    try:
        for name, command, cwd, timeout in checks:
            print(f'\n== {name} ==', flush=True)
            start = time.monotonic()
            proc = subprocess.Popen(command, cwd=cwd, env=env, start_new_session=True)
            try:
                code = proc.wait(timeout=timeout)
            except (subprocess.TimeoutExpired, KeyboardInterrupt):
                os.killpg(proc.pid, signal.SIGTERM)
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(proc.pid, signal.SIGKILL)
                    proc.wait()
                raise
            report['checks'].append({'name': name, 'passed': code == 0,
                                     'exitCode': code, 'seconds': round(time.monotonic() - start, 2)})
            if code:
                failed = True
                break
        report['passed'] = not failed and len(report['checks']) == len(checks)
    except (OSError, subprocess.TimeoutExpired, KeyboardInterrupt) as error:
        report['error'] = type(error).__name__
        print(f'Verification stopped: {type(error).__name__}', file=sys.stderr)
    finally:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, indent=2) + '\n')
        print(f"\n{'PASS' if report['passed'] else 'FAIL'} — report: {args.report}", flush=True)
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
