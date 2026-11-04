from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parent.parent


class PublishWorkflowTest(unittest.TestCase):
    def setUp(self):
        self.workflow = (ROOT / ".github/workflows/publish.yml").read_text()
        # Check the safety-critical structure without adding a YAML dependency;
        # actionlint validates the complete workflow separately.
        self.jobs = dict(re.findall(r"^  ([a-z-]+):\n(.*?)(?=^  [a-z-]+:|\Z)",
                                    self.workflow.split("jobs:\n", 1)[1], re.M | re.S))

    def test_main_and_manual_events_only_verify(self):
        self.assertIn("    branches: [main]", self.workflow)
        self.assertIn("  workflow_dispatch:", self.workflow)
        self.assertEqual(set(self.jobs), {"verification", "dockerhub", "github-release"})
        for job in ("dockerhub", "github-release"):
            self.assertIn("    needs: verification\n", self.jobs[job])
            self.assertIn("    if: github.event_name == 'push' && startsWith(github.ref, 'refs/tags/')\n",
                          self.jobs[job])

    def test_verification_checks_production_container(self):
        verification = self.jobs["verification"]
        for command in ("make check", "make audit",
                        "docker build --target production -t myip-api:ci .",
                        "SMOKE_IMAGE=myip-api:ci python3 scripts/smoke-container.py"):
            self.assertIn("run: " + command, verification)
        self.assertNotIn("continue-on-error", verification)

    def test_latest_is_serialized_and_checked_after_version_push(self):
        self.assertIn("  group: ${{ github.event_name == 'push' && startsWith(github.ref, 'refs/tags/') && 'publish-release' || format('verify-{0}', github.ref) }}\n", self.workflow)
        self.assertIn("  cancel-in-progress: false\n", self.workflow)
        docker = self.jobs["dockerhub"]
        self.assertGreater(docker.index("git ls-remote --tags --refs origin"),
                           docker.index("push: true"))
        self.assertIn('scripts/is-latest-release.py "$GITHUB_REF_NAME"', docker)
        self.assertEqual(docker.count("mayankfawkes/ip:latest"), 1)
        self.assertIn('imagetools create --tag mayankfawkes/ip:latest "mayankfawkes/ip:$GITHUB_REF_NAME"', docker)
