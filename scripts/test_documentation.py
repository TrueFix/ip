from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parent.parent


class DocumentationTest(unittest.TestCase):
    def test_local_links_and_heading_anchors(self):
        documents = list(ROOT.glob("*.md")) + list((ROOT / "docs").glob("*.md"))
        documents += list((ROOT / "deploy").glob("*.md")) + [ROOT / "llms.txt"]
        for document in documents:
            for target in re.findall(r"\]\(([^)]+)\)", document.read_text()):
                if target.startswith(("https://", "http://", "mailto:")):
                    continue
                with self.subTest(document=str(document.relative_to(ROOT)), target=target):
                    filename, _, anchor = target.partition("#")
                    destination = document.parent / filename if filename else document
                    self.assertTrue(destination.exists(), "Broken local document link")
                    if anchor and destination.suffix == ".md":
                        headings = re.findall(r"^#+\s+(.+)$", destination.read_text(), re.M)
                        anchors = [re.sub(r"[^\w\- ]", "", heading.lower()).replace(" ", "-")
                                   for heading in headings]
                        self.assertIn(anchor, anchors, "Broken heading anchor")

    def test_llm_guide_exists(self):
        guide = (ROOT / "llms.txt").read_text()
        for required in ("docs/ENVIRONMENT.md", "RATELIMIT_ENABLE=false",
                         "RATE_LIMIT_RPM=0", "github.com/TrueFix/myip"):
            self.assertIn(required, guide)
