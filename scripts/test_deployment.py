import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent


@unittest.skipUnless(shutil.which("docker"), "Docker Compose CLI required")
class DeploymentTest(unittest.TestCase):
    def config(self, template, settings=None):
        environment = {
            key: value for key, value in os.environ.items()
            if not key.startswith(("MAXMIND_", "IPINFO_"))
            and key not in ("IMAGE_TAG", "PORT", "BIND_ADDRESS", "COMPOSE_FILE")
        }
        environment.update(settings or {})
        # Explicit empty env file prevents tests from loading local credentials.
        with tempfile.NamedTemporaryFile() as empty:
            return subprocess.run(
                ["docker", "compose", "--env-file", empty.name, "-f",
                 str(ROOT / "deploy" / template), "config", "--format", "json"],
                env=environment, capture_output=True, text=True,
            )

    def test_dbip_without_credentials(self):
        result = self.config("compose.dbip.yml")
        self.assertEqual(result.returncode, 0, result.stderr)
        services = json.loads(result.stdout)["services"]
        for service in services.values():
            self.assertEqual(service["image"], "mayankfawkes/ip:latest")
            self.assertEqual(service["environment"]["GEOIP_PROVIDER"], "dbip")
            self.assertNotIn("MAXMIND_LICENSE_KEY", service["environment"])

    def test_maxmind_requires_both_values(self):
        for settings, missing in (
            ({}, "MAXMIND_ACCOUNT_ID"),
            ({"MAXMIND_LICENSE_KEY": "test-license-value"}, "MAXMIND_ACCOUNT_ID"),
            ({"MAXMIND_ACCOUNT_ID": "123"}, "MAXMIND_LICENSE_KEY"),
            ({"MAXMIND_ACCOUNT_ID": "123", "MAXMIND_LICENSE_KEY": ""}, "MAXMIND_LICENSE_KEY"),
        ):
            with self.subTest(missing=missing, settings=list(settings)):
                result = self.config("compose.maxmind.yml", settings)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(missing, result.stderr)

    def test_maxmind_keeps_credentials_out_of_api(self):
        result = self.config("compose.maxmind.yml", {
            "MAXMIND_ACCOUNT_ID": "123", "MAXMIND_LICENSE_KEY": "test-license-value",
        })
        self.assertEqual(result.returncode, 0, result.stderr)
        services = json.loads(result.stdout)["services"]
        self.assertNotIn("MAXMIND_LICENSE_KEY", services["api"]["environment"])
        self.assertNotIn("MAXMIND_ACCOUNT_ID", services["api"]["environment"])
        self.assertEqual(services["updater"]["environment"]["MAXMIND_ACCOUNT_ID"], "123")
        self.assertTrue(services["updater"]["healthcheck"]["disable"])
        self.assertTrue(all(port["target"] != 9090 for port in services["api"].get("ports", [])))
