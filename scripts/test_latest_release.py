from pathlib import Path
import importlib.util
import subprocess
import sys
import unittest

SCRIPT = Path(__file__).with_name("is-latest-release.py")
spec = importlib.util.spec_from_file_location("latest_release", SCRIPT)
latest = importlib.util.module_from_spec(spec)
spec.loader.exec_module(latest)


def tags(*names):
    return ["a" * 40 + "\trefs/tags/" + name for name in names]


class LatestReleaseTest(unittest.TestCase):
    def test_numeric_semver_order_not_lexical(self):
        remote = tags("v0.9.9", "v0.10.0", "v0.10.1", "v0.10.1^{}")
        self.assertTrue(latest.is_latest("v0.10.1", remote))
        self.assertFalse(latest.is_latest("v0.9.9", remote))
        self.assertFalse(latest.is_latest("v0.10.0", remote))
        self.assertTrue(latest.is_latest("v2.0.0", tags("v1.99.99", "v2.0.0")))

    def test_prereleases_and_noncanonical_tags_do_not_promote(self):
        remote = tags("v1.0.0", "v2.0.0-rc.1", "v01.0.0", "other", "v3.0.0+build")
        self.assertTrue(latest.is_latest("v1.0.0", remote))
        for candidate in ("v2.0.0-rc.1", "v01.0.0", "other", "v3.0.0+build"):
            self.assertFalse(latest.is_latest(candidate, remote))

    def test_missing_candidate_or_empty_remote_fails_closed(self):
        self.assertFalse(latest.is_latest("v1.0.0", []))
        self.assertFalse(latest.is_latest("v2.0.0", tags("v1.0.0")))
        self.assertFalse(latest.is_latest("v1.0.0", ["bad input", "refs/tags/v1.0.0"]))

    def test_command_output(self):
        result = subprocess.run([sys.executable, str(SCRIPT), "v1.0.0"],
                                input="\n".join(tags("v1.0.0")), text=True,
                                capture_output=True, check=True)
        self.assertEqual(result.stdout, "true\n")
