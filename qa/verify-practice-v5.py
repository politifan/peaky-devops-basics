"""Verify published v5 bytes on a fresh Linux runner; no Docker or ELF kit."""
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request

URL = 'https://github.com/politifan/peaky-devops-basics/releases/download/course-v5-final/'
ARCHIVE = 'practice-v5.tar.gz'
SUMS = 'SHA256SUMS-practice-v5'
EXPECTED = {
    ARCHIVE: 'dc6d40153e4d7100a922d01897e30707920cb3ba4a6148a248c0005a7e6f8dcb',
    SUMS: '536ec7269e81b44a3eed7ba44e3815a0b8ccb3a8c0d9b61f31659fac251b31e1',
}


def run(command, cwd, env, report):
    result = subprocess.run(command, cwd=cwd, env=env, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, timeout=120, check=False)
    report.write_bytes(result.stdout)
    return result.returncode


def main():
    report = Path('reports/practice-v5').resolve()
    report.mkdir(parents=True, exist_ok=True)
    evidence = {'platform': platform.platform(), 'network_scope': 'published asset download only',
                'docker_or_elf_executed': False, 'existing_user_data_removed': False}
    try:
        if sys.platform != 'linux' or os.environ.get('GITHUB_ACTIONS') != 'true':
            raise RuntimeError('Run only on a dedicated GitHub-hosted Linux runner')
        root = Path(tempfile.mkdtemp(prefix='practice-v5-verify-')).resolve()
        evidence['temp_root'] = str(root)
        for name in ('home', 'tmp', 'attempts', 'empty-template'):
            (root / name).mkdir()
        env = {key: value for key, value in os.environ.items()
               if not key.upper().startswith('GIT_') and key not in {'BASH_ENV', 'ENV'}}
        env.update(HOME=str(root/'home'), V5_PRACTICE_ROOT=str(root/'attempts'),
                   TMPDIR=str(root/'tmp'), BASH_ENV='/dev/null', ENV='/dev/null',
                   GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL='/dev/null',
                   GIT_TEMPLATE_DIR=str(root/'empty-template'), GIT_CONFIG_COUNT='1',
                   GIT_CONFIG_KEY_0='core.hooksPath', GIT_CONFIG_VALUE_0='/dev/null',
                   LC_ALL='C')
        evidence['assets'] = {}
        for name, expected in EXPECTED.items():
            with urllib.request.urlopen(URL+name, timeout=30) as response:
                data = response.read(1_000_001)
            if len(data) > 1_000_000:
                raise RuntimeError('Unexpected asset size')
            digest = hashlib.sha256(data).hexdigest()
            evidence['assets'][name] = {'sha256': digest, 'bytes': len(data),
                                      'crlf_count': data.count(b'\r\n'), 'expected_match': digest == expected}
            if digest != expected:
                raise RuntimeError('Published bytes changed; review new asset before execution')
            (root/name).write_bytes(data)
        shutil.copyfile(root/SUMS, report/'published-SHA256SUMS.bin')
        # Test the exact published file first. Do not abort later independent cases.
        raw_status = run(['sha256sum', '-c', SUMS], root, env, report/'raw-checksum.txt')
        evidence['published_raw_checksum_exit'] = raw_status
        evidence['sha256sum_version_exit'] = run(['sha256sum', '--version'], root, env, report/'sha256sum-version.txt')
        normalized = (root/SUMS).read_bytes().replace(b'\r\n', b'\n')
        (root/'SHA256SUMS').write_bytes(normalized)
        normalized_status = run(['sha256sum', '-c', 'SHA256SUMS'], root, env, report/'normalized-checksum.txt')
        evidence['normalized_copy_checksum_exit'] = normalized_status
        evidence['published_checksum_unchanged'] = hashlib.sha256((root/SUMS).read_bytes()).hexdigest() == EXPECTED[SUMS]
        if normalized_status:
            raise RuntimeError('Independent LF-copy checksum failed; do not execute')
        members = []
        allowed = {'practice/v5', 'practice/v5/check-http.sh', 'practice/v5/practice.sh'}
        with tarfile.open(root/ARCHIVE, 'r:gz') as source:
            for member in source.getmembers():
                target = (root/member.name).resolve()
                if member.name not in allowed or not target.is_relative_to(root) or not (member.isdir() or member.isfile()):
                    raise RuntimeError('Unsafe archive member')
                members.append({'path': member.name, 'bytes': member.size})
                if member.isdir():
                    target.mkdir(parents=True, exist_ok=True)
                else:
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_bytes(source.extractfile(member).read())
        evidence['members'] = members
        # check-http.sh is read material only; no BASE, curl, or HTTP lab is run.
        evidence['http_check_executed'] = False
        harness = Path('qa/practice-v5-cases.sh').resolve()
        status = run(['/bin/bash', '--noprofile', '--norc', str(harness), str(root)], root, env, report/'practice-cases-console.txt')
        evidence['case_harness_exit'] = status
        rows = []
        if (root/'results.tsv').exists():
            shutil.copyfile(root/'results.tsv', report/'results.tsv')
            for line in (root/'results.tsv').read_text().splitlines()[1:]:
                name, expected, actual = line.split('\t')
                rows.append({'case': name, 'expected': int(expected), 'actual': int(actual)})
        evidence['cases'] = rows
        evidence['checks_passed'] = sum(row['expected'] == row['actual'] for row in rows)
        evidence['checks_total'] = len(rows)
        if (root/'help.txt').exists():
            shutil.copyfile(root/'help.txt', report/'help.txt')
            evidence['help_verified'] = 'Usage:' in (root/'help.txt').read_text()
        for path in root.glob('*-*.log'):
            shutil.copyfile(path, report/path.name)
        evidence['practice_cases_pass'] = status == 0 and len(rows) == 30 and evidence['checks_passed'] == 30
        evidence['published_step5_pass'] = raw_status == 0
        evidence['overall_pass'] = evidence['practice_cases_pass'] and evidence['published_step5_pass'] and evidence.get('help_verified', False)
        return 0 if evidence['overall_pass'] else 1
    except Exception as error:
        evidence['error_type'] = type(error).__name__
        evidence['error'] = str(error)
        evidence['overall_pass'] = False
        return 1
    finally:
        (report/'verification.json').write_text(json.dumps(evidence, indent=2), encoding='utf-8')
        print(json.dumps(evidence, indent=2))


if __name__ == '__main__':
    raise SystemExit(main())
