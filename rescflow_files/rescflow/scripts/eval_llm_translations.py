#!/usr/bin/env python3
"""Evaluate the LLM-written Rust translations in ../Claude_translations against Go.

For each translation: compile with rustc, run every labels.json input, compare stdout,
exit code and stderr presence with observed Go (results/go_runs.json), then run under
strace and compare the interleaved OS open/close trace with Go's (results/os_traces.json).
Also runs two extra inputs per case (missing input path, no arguments) against Go.
Output: results/llm_translations.json
"""
import json, os, re, shutil, subprocess, sys, tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from os_trace import os_events, render, lifetimes  # noqa: E402

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT.parent / 'Claude_translations'


def run(cmd, cwd, timeout=60):
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=timeout)
    return dict(returncode=p.returncode, stdout=p.stdout, stderr=p.stderr)


def main():
    goroot = sys.argv[1] if len(sys.argv) > 1 else None
    labels = {c['id']: c for c in json.loads((ROOT / 'benchmarks/labels.json').read_text())}
    go_runs = {p['id']: p for p in json.loads((ROOT / 'results/go_runs.json').read_text())['programs']}
    go_os = {(r['id'], r['mode']): r['tools']['go'] for r in json.loads((ROOT / 'results/os_traces.json').read_text())['rows']}
    files = {}
    for f in sorted(SRC.glob('*.rs')):
        m = re.search(r'Case (\d{2}) `', f.read_text())
        if m:
            files[m.group(1)] = f
    tmp = Path(tempfile.mkdtemp(prefix='rescflow-llm-'))
    inp = tmp / 'input.txt'
    inp.write_text('resource fixture\n')
    env = dict(os.environ, GOTOOLCHAIN='local')
    if goroot:
        env['PATH'] = f"{goroot}/bin:{env['PATH']}"
    rows = []
    for cid, c in sorted(labels.items()):
        row = dict(id=cid, name=c['name'], file=None, runs=[], extra_runs=[])
        if cid not in files:
            row['status'] = 'missing_translation'; rows.append(row); continue
        row['file'] = f"Claude_translations/{files[cid].name}"
        b = tmp / f'rs_{cid}'
        comp = subprocess.run(['rustc', '--edition', '2021', '-O', '-o', str(b), str(files[cid])],
                              capture_output=True, text=True)
        row['compile_warnings'] = comp.stderr.count('warning:')
        if comp.returncode != 0:
            row['status'] = 'compile_failed'; row['compile_stderr'] = comp.stderr[-3000:]; rows.append(row); continue
        for spec in c['inputs']:
            r = run([str(b), str(inp), spec['mode']], tmp)
            ref = next(x for x in go_runs[cid]['runs'] if x['mode'] == spec['mode'])
            r['mode'] = spec['mode']
            r['matches_go'] = r['stdout'] == ref['stdout'] and r['returncode'] == ref['returncode'] \
                and bool(r['stderr']) == bool(ref['stderr'])
            log = tmp / 'st.txt'
            subprocess.run(['strace', '-f', '-qq', '-s', '400', '-e', 'trace=openat,close,write', '-o', str(log),
                            str(b), str(inp), spec['mode']], cwd=tmp, capture_output=True, timeout=60)
            ev, leaked = os_events(log.read_text(), str(inp))
            g = go_os[(cid, spec['mode'])]
            r['os_trace'] = render(ev)
            r['os_trace_matches_go'] = r['os_trace'] == g['trace']
            r['handles_open_at_exit'] = leaked
            if not r['os_trace_matches_go']:
                r['go_os_trace'] = g['trace']
            row['runs'].append(r)
        # extra inputs: missing path, no arguments -> compare with Go built from the original
        gw = tmp / f'go_{cid}'
        gw.mkdir()
        shutil.copy(ROOT / c['file'], gw / 'main.go')
        (gw / 'go.mod').write_text('module x\n\ngo 1.26\n')
        gb = subprocess.run(['go', 'build', '-o', 'prog', '.'], cwd=gw, env=env, capture_output=True, text=True)
        if gb.returncode == 0:
            for name, args in [('missing_path', [str(tmp / 'nope.txt')]), ('no_args', [])]:
                rg = run([str(gw / 'prog'), *args], tmp)
                rr = run([str(b), *args], tmp)
                row['extra_runs'].append(dict(input=name, go_stdout=rg['stdout'], rust_stdout=rr['stdout'],
                                              go_rc=rg['returncode'], rust_rc=rr['returncode'],
                                              go_stderr1=(rg['stderr'].splitlines() or [''])[0],
                                              rust_stderr1=(rr['stderr'].splitlines() or [''])[0],
                                              matches_go=rg['stdout'] == rr['stdout'] and rg['returncode'] == rr['returncode']
                                              and (rg['stderr'].splitlines() or [''])[0] == (rr['stderr'].splitlines() or [''])[0]))
        row['status'] = 'executed'
        rows.append(row)
    shutil.rmtree(tmp, ignore_errors=True)
    runs = [x for r in rows for x in r['runs']]
    extra = [x for r in rows for x in r['extra_runs']]
    summary = dict(cases=len(rows), translations_present=sum(r['file'] is not None for r in rows),
                   compiled=sum(r['status'] == 'executed' for r in rows),
                   label_inputs_executed=len(runs), output_matches_go=sum(x['matches_go'] for x in runs),
                   os_trace_matches_go=sum(x['os_trace_matches_go'] for x in runs),
                   extra_inputs_matching=f"{sum(x['matches_go'] for x in extra)}/{len(extra)}",
                   missing=[r['id'] + ' ' + r['name'] for r in rows if r['file'] is None])
    (ROOT / 'results/llm_translations.json').write_text(json.dumps(dict(summary=summary, rows=rows), indent=2) + '\n')
    print(json.dumps(summary, indent=2))
    for r in rows:
        bad = [x['mode'] for x in r['runs'] if not (x['matches_go'] and x['os_trace_matches_go'])]
        bad += [x['input'] for x in r['extra_runs'] if not x['matches_go']]
        print(r['id'], r['name'], r['status'], r.get('file'), 'ISSUES:' if bad else 'ok', bad or '')


if __name__ == '__main__':
    main()
