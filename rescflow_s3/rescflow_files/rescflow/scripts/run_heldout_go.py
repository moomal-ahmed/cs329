#!/usr/bin/env python3
"""Run the held-out Go programs on every labelled input (including strace fault injection)
and compare with the expected traces in heldout/labels.json.

Usage: python3 scripts/run_heldout_go.py [GOROOT]
Writes results/heldout_go_runs.json. Exits non-zero if any input disagrees with its label.
"""
import json, os, shutil, subprocess, sys, tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def run_input(binary, inp, spec, cwd, attempts=25):
    """Run once; with an inject spec 'syscall:N:ERRNO', fail the Nth syscall on the input file.

    strace counts `when=N` per thread and Go may make the file syscalls from more than one OS
    thread, so an injected run is accepted only if strace reports the injection and all
    syscalls on the input file came from one thread; otherwise it is retried."""
    cmd = [str(binary), str(inp), spec['mode']]
    log = Path(cwd) / 'inject.log'
    if spec.get('inject'):
        call, nth, errno = spec['inject'].split(':')
        cmd = ['strace', '-f', '-qq', '-o', str(log), '-P', str(inp), '-e', 'trace=openat,close',
               '-e', f'inject={call}:error={errno}:when={nth}'] + cmd
    for attempt in range(attempts if spec.get('inject') else 1):
        p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=60)
        if not spec.get('inject'):
            break
        text = log.read_text()
        if 'INJECTED' in text and len({l.split()[0] for l in text.splitlines() if l.strip()}) <= 1:
            break
    return dict(returncode=p.returncode, stdout=p.stdout, stderr=p.stderr, attempts=attempt + 1)


def main():
    env = dict(os.environ, GOTOOLCHAIN='local')
    if len(sys.argv) > 1:
        env['PATH'] = f"{sys.argv[1]}/bin:{env['PATH']}"
    labels = json.loads((ROOT / 'heldout/labels.json').read_text())
    tmp = Path(tempfile.mkdtemp(prefix='rescflow-heldout-'))
    inp = tmp / 'input.txt'
    inp.write_text('resource fixture\n')
    rows, bad = [], 0
    for c in labels:
        w = tmp / c['id']
        w.mkdir()
        shutil.copy(ROOT / c['file'], w / 'main.go')
        (w / 'go.mod').write_text('module x\n\ngo 1.26\n')
        b = subprocess.run(['go', 'build', '-o', 'prog', '.'], cwd=w, env=env, capture_output=True, text=True)
        row = dict(id=c['id'], name=c['name'], build_stderr=b.stderr, runs=[])
        if b.returncode != 0:
            row['status'] = 'go_build_failed'; bad += 1; rows.append(row); print(c['id'], 'BUILD FAILED', b.stderr); continue
        for spec in c['inputs']:
            r = run_input(w / 'prog', inp, spec, tmp)
            r.update(mode=spec['mode'], inject=spec['inject'])
            r['label_match'] = (r['stdout'].splitlines() == spec['expected_stdout'] and
                                r['returncode'] == spec['expected_exit'] and not r['stderr'])
            bad += not r['label_match']
            row['runs'].append(r)
            if not r['label_match']:
                print(c['id'], c['name'], repr(spec['mode']), spec['inject'], 'MISMATCH\n  expected', spec['expected_stdout'],
                      '\n  got     ', r['stdout'].splitlines(), r['returncode'], r['stderr'][:300])
        row['status'] = 'pass' if all(x['label_match'] for x in row['runs']) else 'mismatch'
        rows.append(row)
    go = subprocess.run(['go', 'version'], env=env, capture_output=True, text=True).stdout.strip()
    (ROOT / 'results/heldout_go_runs.json').write_text(json.dumps(dict(go=go, programs=rows), indent=2) + '\n')
    n = sum(len(r['runs']) for r in rows)
    print(f"{go}: {n - bad}/{n} inputs match labels")
    shutil.rmtree(tmp, ignore_errors=True)
    return 1 if bad else 0


if __name__ == '__main__':
    sys.exit(main())
