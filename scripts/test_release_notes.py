import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("release-notes.py")
spec = importlib.util.spec_from_file_location("release_notes", SCRIPT)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ReleaseNotesTest(unittest.TestCase):
    def test_only_matching_version_body(self):
        text = "# Changelog\n\n## Unreleased\n\n- Future\n\n## v1.0.1 — 2026-11-04\n\n- New\n\n### Fixed\n\n- Detail\n\n## v1.0.0 — 2026-11-04\n\n- Old\n"
        self.assertEqual(module.release_notes(text, "v1.0.1"), "- New\n\n### Fixed\n\n- Detail\n")
        self.assertEqual(module.release_notes(text, "v1.0.0"), "- Old\n")

    def test_exact_version_and_prerelease(self):
        text = "## v1.0.01\n- Not matching\n## v1.0.0-rc.1\n- Candidate\n## v1.0.0\n- Stable\n"
        self.assertEqual(module.release_notes(text, "v1.0.0"), "- Stable\n")
        self.assertEqual(module.release_notes(text, "v1.0.0-rc.1"), "- Candidate\n")

    def test_headings_inside_code_are_preserved(self):
        for fence in ("```", "~~~~"):
            text = f"## v1.0.0\n\n{fence}md\n## example\n{fence}\n\n## v0.1.0\n- Old\n"
            self.assertEqual(module.release_notes(text, "v1.0.0"), f"{fence}md\n## example\n{fence}\n")

    def test_missing_empty_duplicate_and_invalid_fail(self):
        for text, version in (("## v1.0.1\n- Other", "v1.0.0"),
                              ("## v1.0.0\n\n## v0.1.0\n- Old", "v1.0.0"),
                              ("## v1.0.0\n- One\n## v1.0.0\n- Two", "v1.0.0"),
                              ("## v1.0.0\n- One", "../bad")):
            with self.subTest(text=text, version=version), self.assertRaises(ValueError):
                module.release_notes(text, version)

    def test_cli_fails_without_partial_notes(self):
        with tempfile.TemporaryDirectory() as directory:
            changelog = Path(directory) / "CHANGELOG.md"
            changelog.write_text("## v1.0.0\n- Stable\n", encoding="utf-8")
            result = subprocess.run([sys.executable, str(SCRIPT), "v1.0.1", "--changelog", str(changelog)],
                                    capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(result.stdout, "")

    def test_current_release_sections(self):
        text = (SCRIPT.parent.parent / "CHANGELOG.md").read_text()
        for version in ("v1.0.0", "v0.1.4", "v0.1.0"):
            self.assertTrue(module.release_notes(text, version).strip())
