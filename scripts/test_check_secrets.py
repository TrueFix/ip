import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("guard", Path(__file__).with_name("check-secrets.py"))
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)


class SecretGuardTest(unittest.TestCase):
    def test_local_files_blocked(self):
        for name in (".env", ".env.production", "secrets/token", "data/current.json", "cert.key", "dump.mmdb", "state.sqlite", "state.sqlite-journal"):
            self.assertTrue(guard.findings(name, b"value"))

    def test_templates_and_fixtures_allowed(self):
        self.assertFalse(guard.findings(".env.example", b"IPINFO_TOKEN=YOUR_TOKEN_HERE\n"))
        self.assertFalse(guard.findings("internal/geo/testdata/Test.mmdb", b"binary\0"))

    def test_values_redacted(self):
        value = "example-secret-value-123"
        result = guard.findings("settings.txt", ("IPINFO_TOKEN=" + value).encode())
        self.assertTrue(result)
        self.assertNotIn(value, " ".join(result))


if __name__ == "__main__":
    unittest.main()
