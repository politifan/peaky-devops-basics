import contextlib
import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile
import install_tested_kit as installer

class Tests(unittest.TestCase):
    def fixture(self,path):
        files={name:b'reviewed fake binary' for name in ('ticketlab','labctl','labcheck','ticketlab-bad-body','ticketlab-bad-write','ticketlab-bad-retry')}
        data=io.BytesIO()
        with tarfile.open(fileobj=data,mode='w:gz') as tar:
            for name,value in {**files,'.env':b'FAKE_ENV_DO_NOT_EXTRACT'}.items():
                info=tarfile.TarInfo('./'+name);info.size=len(value);tar.addfile(info,io.BytesIO(value))
        raw=data.getvalue()
        with zipfile.ZipFile(path,'w') as archive:
            archive.writestr('ticketlab-linux-amd64.tar.gz',raw)
            archive.writestr('reports/fake.dump',b'FAKE_DO_NOT_EXTRACT')
        entry={'id':'test','source_commit':'test-source','artifact_id':1,'artifact_sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'kit_archive_sha256':hashlib.sha256(raw).hexdigest(),'files':{name:hashlib.sha256(value).hexdigest() for name,value in files.items()}}
        return {'kits':[entry]}
    def test_selective_install_never_extracts_other_members(self):
        with tempfile.TemporaryDirectory() as t:
            root=Path(t); artifact=root/'artifact.zip';catalog=self.fixture(artifact);target=root/'kit'
            with patch.object(installer,'load_catalog',return_value=catalog),contextlib.redirect_stdout(io.StringIO()):installer.install(artifact,target)
            self.assertEqual({x.name for x in target.iterdir()},set(catalog['kits'][0]['files'])|{'reviewed-kit-source.json'})
            self.assertFalse((target/'.env').exists());self.assertFalse((root/'reports').exists())
    def test_digest_failure_creates_no_target(self):
        with tempfile.TemporaryDirectory() as t:
            root=Path(t);artifact=root/'artifact.zip';catalog=self.fixture(artifact);target=root/'kit'
            catalog['kits'][0]['artifact_sha256']='wrong'
            with patch.object(installer,'load_catalog',return_value=catalog):
                with self.assertRaises(ValueError):installer.install(artifact,target)
            self.assertFalse(target.exists())
    def test_existing_target_refused_without_overwrite(self):
        with tempfile.TemporaryDirectory() as t:
            root=Path(t);artifact=root/'artifact.zip';catalog=self.fixture(artifact);target=root/'kit';target.mkdir();(target/'keep').write_text('keep')
            with patch.object(installer,'load_catalog',return_value=catalog):
                with self.assertRaises(ValueError):installer.install(artifact,target)
            self.assertEqual((target/'keep').read_text(),'keep')
if __name__=='__main__':unittest.main()
