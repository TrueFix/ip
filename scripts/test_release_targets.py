from pathlib import Path
import subprocess
import unittest


class ReleaseTargetsTest(unittest.TestCase):
    def test_supported_unique_targets(self):
        available = set(subprocess.check_output(["go", "tool", "dist", "list"], text=True).splitlines())
        names = set()
        operating_systems = set()
        manifest = Path(__file__).with_name("release-targets.txt")
        for line in manifest.read_text().splitlines():
            if not line.strip() or line.lstrip().startswith("#"):
                continue
            os_name, architecture, label, arm = line.split()
            self.assertIn(os_name + "/" + architecture, available)
            self.assertNotIn((os_name, label), names)
            self.assertNotIn("/", label)
            if architecture == "arm":
                self.assertIn(arm, ("5", "6", "7"))
            else:
                self.assertEqual(arm, "-")
            names.add((os_name, label))
            operating_systems.add(os_name)
        self.assertEqual(operating_systems, {"linux", "darwin", "windows"})
        self.assertEqual(len(names), 20)
