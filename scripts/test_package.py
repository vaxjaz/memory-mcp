import io
from pathlib import Path
import tarfile
import tempfile
import unittest

from package import bundle_tar_mode


class OfflineBundleTests(unittest.TestCase):
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
