import json
import os
import re
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent


class EnvironmentDocsTest(unittest.TestCase):
    def test_every_runtime_and_compose_setting_is_documented(self):
        documented = (ROOT / "docs/ENVIRONMENT.md").read_text()
        variables = set()
        for name in ("internal/config/config.go", "internal/httpapi/config.go"):
            source = (ROOT / name).read_text()
            variables.update(re.findall(r'(?:Env|Getenv|Secret)\("([A-Z_]+)"', source))
            variables.update(key + "_FILE" for key in re.findall(r'Secret\("([A-Z_]+)"', source))
        for name in ("compose.yml", "deploy/compose.dbip.yml", "deploy/compose.maxmind.yml"):
            variables.update(re.findall(r'\$\{([A-Z_]+)', (ROOT / name).read_text()))
        for key in variables:
            with self.subTest(variable=key):
                self.assertIn("`" + key + "`", documented)

    def test_source_examples_are_separate(self):
        def settings(name):
            return dict(line.split("=", 1) for line in
                        (ROOT / "deploy/env" / name).read_text().splitlines()
                        if line.strip() and not line.lstrip().startswith("#"))

        self.assertEqual(settings("dbip.env.example"), {})
        self.assertEqual(set(settings("maxmind.env.example")),
                         {"MAXMIND_ACCOUNT_ID", "MAXMIND_LICENSE_KEY"})
        self.assertEqual(set(settings("ipinfo.env.example")),
                         {"IPINFO_ENABLED", "IPINFO_TOKEN"})


@unittest.skipUnless(shutil.which("docker"), "Docker Compose CLI required")
class DeploymentTest(unittest.TestCase):
    def config(self, template, settings=None):
        environment = {
            key: value for key, value in os.environ.items()
            if not key.startswith(("MAXMIND_", "IPINFO_"))
            and key not in ("COMPOSE_FILE",)
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

    def test_docker_environment_overrides_are_removed(self):
        for template in ("compose.dbip.yml", "compose.maxmind.yml", "../compose.yml"):
            with self.subTest(template=template):
                result = self.config(template, {
                    "GEOIP_PROVIDER": "dbip", "MAXMIND_ACCOUNT_ID": "123",
                    "MAXMIND_LICENSE_KEY": "test-license-value",
                    "IMAGE_TAG": "unused", "BIND_ADDRESS": "0.0.0.0",
                    "PORT": "9999", "DATA_VOLUME": "/unused",
                    "RATE_LIMIT_RPM": "0",
                })
                self.assertEqual(result.returncode, 0, result.stderr)
                services = json.loads(result.stdout)["services"]
                self.assertEqual(services["api"]["environment"]["RATE_LIMIT_RPM"], "0")
                self.assertEqual(services["api"]["ports"][0]["host_ip"], "127.0.0.1")
                self.assertEqual(str(services["api"]["ports"][0]["published"]), "8080")
                for service in services.values():
                    self.assertEqual(service["image"], "mayankfawkes/ip:latest")
                    data = next(v for v in service["volumes"] if v["target"] == "/data")
                    self.assertEqual(data["type"], "volume")
