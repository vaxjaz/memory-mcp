import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

from package import bundle_tar_mode, packaged_documentation, verified_source_commit


class OfflineBundleTests(unittest.TestCase):
    def test_archived_source_requires_matching_provenance_and_rejects_conflicting_head(self):
        sha = 'a' * 40
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with patch('package.subprocess.check_output', side_effect=OSError):
                with self.assertRaises(ValueError):
                    verified_source_commit(root, sha)
                (root / '.source-commit').write_text(sha)
                self.assertEqual(verified_source_commit(root, sha), sha)
                with self.assertRaises(ValueError):
                    verified_source_commit(root, 'b' * 40)
            with patch('package.subprocess.check_output', return_value='b' * 40):
                with self.assertRaises(ValueError):
                    verified_source_commit(root, sha)

    def test_agent_documentation_uses_the_embedded_source(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            install = root / "docs/AGENT_INSTALL.md"
            install.parent.mkdir()
            install.write_text("Installation instructions")
            guide = root / "internal/server/guide.md"
            guide.parent.mkdir(parents=True)
            guide.write_text("Embedded agent instructions")
            docs = packaged_documentation(root)
            self.assertEqual(docs[Path("docs/AGENT_INSTALL.md")], install)
            self.assertEqual(docs[Path("docs/AGENT_GUIDE.md")], guide)

    def test_unix_archive_is_executable_even_from_nonexecutable_builder_files(self):
        with tempfile.TemporaryDirectory() as root:
            bundle = Path(root) / 'bundle'
            bundle.mkdir()
            binary = bundle / 'memory-mcp'
            binary.write_bytes(b'synthetic executable fixture')
            binary.chmod(0o644)
            (bundle / 'README.md').write_text('fixture', encoding='utf-8')
            output = io.BytesIO()
            with tarfile.open(fileobj=output, mode='w:gz') as archive:
                archive.add(bundle, arcname='bundle', filter=bundle_tar_mode)
            output.seek(0)
            with tarfile.open(fileobj=output, mode='r:gz') as archive:
                self.assertEqual(archive.getmember('bundle/memory-mcp').mode, 0o755)
                self.assertEqual(archive.getmember('bundle/README.md').mode, 0o644)
                self.assertEqual(archive.getmember('bundle').mode, 0o755)
