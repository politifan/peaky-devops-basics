"""Selective install of the exact reviewed software artifact; never extract reports/env.
No network or executable invocation. Intended only after student-wrapper peer approval.
"""
import argparse
import io
import json
from pathlib import Path
import stat
import tarfile
import zipfile
from student_control import load_catalog,sha,no_symlinks

def install(artifact,target):
    catalog=load_catalog(); entry=catalog['kits'][0]
    artifact,target=Path(artifact),Path(target)
    no_symlinks(target)
    if target.exists() or not target.is_absolute(): raise ValueError('fresh absolute kit target required')
    if sha(artifact)!=entry['artifact_sha256']: raise ValueError('GitHub artifact checksum mismatch')
    with zipfile.ZipFile(artifact) as outer:
        match=[x for x in outer.infolist() if x.filename=='ticketlab-linux-amd64.tar.gz']
        if len(match)!=1 or match[0].file_size>120000000 or stat.S_ISLNK(match[0].external_attr>>16): raise ValueError('known archive entry mismatch')
        raw=outer.read(match[0])
    import hashlib
    if hashlib.sha256(raw).hexdigest()!=entry['kit_archive_sha256']: raise ValueError('known kit archive checksum mismatch')
    selected={}
    with tarfile.open(fileobj=io.BytesIO(raw),mode='r:gz') as inner:
        for name,value in entry['files'].items():
            match=[x for x in inner.getmembers() if x.name.removeprefix('./')==name]
            if len(match)!=1 or not match[0].isfile() or match[0].size>40000000: raise ValueError('known binary entry mismatch')
            data=inner.extractfile(match[0]).read()
            if hashlib.sha256(data).hexdigest()!=value: raise ValueError('known binary checksum mismatch')
            selected[name]=data
    target.mkdir(mode=0o700)
    for name,data in selected.items():
        with (target/name).open('xb') as f:f.write(data)
        (target/name).chmod(0o500)
    (target/'reviewed-kit-source.json').write_text(json.dumps({'kit_id':entry['id'],'source_commit':entry['source_commit'],'artifact_id':entry['artifact_id'],'artifact_sha256':entry['artifact_sha256'],'kit_archive_sha256':entry['kit_archive_sha256']})+'\n')
    print('Only six checksum-verified software binaries installed; no reports/env/dump extraction')

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('artifact');p.add_argument('target');a=p.parse_args();install(a.artifact,a.target)
