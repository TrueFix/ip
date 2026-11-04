#!/usr/bin/env python3
"""Exercise the non-root, read-only API image using public synthetic fixtures."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
IMAGE = os.environ.get("SMOKE_IMAGE", "myip-api:ci")
NAME = "myip-smoke-" + str(os.getpid())


def docker(*args, **kwargs):
    return subprocess.run(["docker", *args], check=True, **kwargs)


with tempfile.TemporaryDirectory(prefix="myip-smoke-") as temporary:
    data = Path(temporary)
    data.chmod(0o755)
    snapshot = data / "ci-fixture"
    snapshot.mkdir(mode=0o755)
    for source, target in (("GeoIP2-City-Test.mmdb", "City.mmdb"), ("GeoLite2-ASN-Test.mmdb", "ASN.mmdb")):
        shutil.copyfile(ROOT / "internal/geo/testdata" / source, snapshot / target)
        (snapshot / target).chmod(0o644)
    (data / "current.json").write_text(json.dumps({"provider": "dbip", "version": "ci-fixture"}))
    (data / "current.json").chmod(0o644)
    started = False
    try:
        docker("run", "-d", "--name", NAME, "--read-only", "--cap-drop=ALL",
               "--security-opt=no-new-privileges:true", "--memory=512m", "--pids-limit=128",
               "-e", "GEOIP_PROVIDER=dbip", "-p", "127.0.0.1::8080",
               "-v", f"{data}:/data:ro", IMAGE, stdout=subprocess.DEVNULL)
        started = True
        port = docker("inspect", "--format", '{{(index (index .NetworkSettings.Ports "8080/tcp") 0).HostPort}}', NAME,
                      capture_output=True, text=True).stdout.strip()
        origin = "http://127.0.0.1:" + port
        for _ in range(60):
            try:
                with urllib.request.urlopen(origin + "/healthz", timeout=2) as response:
                    assert response.status == 200
                break
            except (OSError, urllib.error.HTTPError):
                time.sleep(0.5)
        else:
            docker("logs", NAME)
            raise RuntimeError("API image did not become ready")
        with urllib.request.urlopen(origin + "/81.2.69.142", timeout=5) as response:
            result = json.load(response)
            assert result["success"] and result["city"] == "London"
            assert result["readme"] == origin + "/docs"
            assert response.headers["Cache-Control"] == "no-store"
            assert "dbip;dur=" in response.headers["Server-Timing"]
        metrics = docker("exec", NAME, "wget", "-qO-", "http://127.0.0.1:9090/metrics",
                         capture_output=True, text=True).stdout
        assert "myip_build_info" in metrics
        assert "myip_geoip_lookup_duration_seconds" in metrics
        docker("stop", "--time", "30", NAME, stdout=subprocess.DEVNULL)
        code = docker("inspect", "--format", "{{.State.ExitCode}}", NAME, capture_output=True, text=True).stdout.strip()
        assert code == "0", "API did not shut down cleanly"
        print("Non-root, read-only API image: lookup, health, metrics and graceful shutdown passed.")
    finally:
        if started:
            subprocess.run(["docker", "rm", "-f", NAME], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
