#!/usr/bin/env python3
"""Check a packaged executable outside its checkout, without opening a browser.

Usage: python3 scripts/smoke-release.py /absolute/path/to/pr-triage
Requires Python 3 and git. Run on macOS or Linux.
"""
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import tempfile
import time
import urllib.request

binary = str(Path(sys.argv[1]).resolve())
with tempfile.TemporaryDirectory(prefix="pr-triage-smoke-") as tmp:
    root = Path(tmp)
    helpers = root / "bin"
    helpers.mkdir()
    capture = root / "browser-url"
    for name in ("open", "xdg-open"):
        helper = helpers / name
        helper.write_text('#!/bin/sh\nprintf "%s" "$1" > "$BROWSER_CAPTURE"\n')
        helper.chmod(0o755)
    env = dict(os.environ, PATH=str(helpers) + os.pathsep + os.environ["PATH"],
               BROWSER_CAPTURE=str(capture))
    print(subprocess.check_output([binary, "version"], cwd=root, env=env, text=True).strip())
    repo = root / "sample"
    repo.mkdir()
    subprocess.run(["git", "init", "-q", str(repo)], check=True)
    (repo / "sample.py").write_text("def greet(name):\n    return 'Hello ' + name\n")
    subprocess.run(["git", "-C", str(repo), "add", "."], check=True)
    subprocess.run(["git", "-C", str(repo), "-c", "user.name=Smoke",
                    "-c", "user.email=smoke@example.invalid", "commit", "-qm", "fixture"], check=True)
    map_dir = root / "map"
    subprocess.run([binary, "codemap", "build", "-C", str(repo), "-output", str(map_dir),
                    "-cache", str(root / "graphs")], cwd=root, env=env, check=True)
    subprocess.run([binary, "codemap", "lookup", "-map", str(map_dir), "sample/sample.py"],
                   cwd=root, env=env, check=True, stdout=subprocess.DEVNULL)
    with (root / "server.log").open("w") as log:
        proc = subprocess.Popen([binary, "serve", "-cache", str(root / "cache"),
                                 "-codemap", str(map_dir)], cwd=root, env=env, stdout=log, stderr=log)
        try:
            for _ in range(100):
                if capture.exists():
                    break
                if proc.poll() is not None:
                    raise RuntimeError((root / "server.log").read_text())
                time.sleep(0.1)
            url = capture.read_text()
            assert re.fullmatch(r"http://127\.0\.0\.1:[1-9][0-9]*", url), url
            with urllib.request.urlopen(url, timeout=5) as response:
                html = response.read().decode()
                assert response.status == 200 and "<html" in html.lower()
            # Verify the HTML's local CSS/JS references are embedded as well.
            for asset in re.findall(r'(?:src|href)="([^"?#]+\.(?:js|css))', html):
                if not asset.startswith(("http:", "https:", "//")):
                    with urllib.request.urlopen(url + "/" + asset.lstrip("/"), timeout=5) as response:
                        assert response.status == 200
            assert url in (root / "server.log").read_text()
        finally:
            proc.send_signal(signal.SIGINT)
            proc.wait(timeout=10)
        assert proc.returncode == 0
    print("PASS: standalone indexing, embedded UI/assets, browser URL and clean shutdown")
