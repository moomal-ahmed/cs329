#!/usr/bin/env python3
"""Step 5 check: generate Rust for every program and compare the generated programs with Go.

Usage: python3 scripts/check_generated.py <rescflow binary> <Go 1.26 GOROOT> [set ...]
Sets: benchmarks heldout (default both). The sealed final set is only run with `final_unseen`.
For each set: `rescflow -emit-rust generated/<set>` writes one Cargo project per A/B program
(REVIEW.md for C programs, UNSUPPORTED.txt when the generator cannot translate). Every
generated program is built against rescflow_rt and run next to the Go original on: each
labelled input; for each mode, "the k-th open fails" for every k and "the first close fails";
a missing file; no arguments. Compared: stdout, exit code, first stderr line, and the OS
open/close order. Writes results/generated_check.json / .txt.
"""
import json, os, shutil, subprocess, sys, tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from check_rt_ports import run  # noqa: E402

ROOT = Path(__file__).resolve().parents[1]
SETS = {'benchmarks': ('./benchmarks/...', 'benchmarks/labels.json'),
        'heldout': ('./heldout/...', 'heldout/labels.json'),
        'final_unseen': ('./testdata/final_unseen/...', 'testdata/final_unseen/labels.json')}


def main():
    binary, goroot = sys.argv[1], sys.argv[2]
    sets = sys.argv[3:] or ['benchmarks', 'heldout']
    env = dict(os.environ, GOTOOLCHAIN='local', PATH=f"{goroot}/bin:{os.environ['PATH']}")
    target = Path(tempfile.mkdtemp(prefix='rescflow-gen-target-'))
    tmp = Path(tempfile.mkdtemp(prefix='rescflow-gen-run-'))
    inp = tmp / 'input.txt'
    inp.write_text('resource fixture\n')
    report, lines = {}, []
    for s in sets:
        pattern, lf = SETS[s]
        out = ROOT / 'generated' / s
        shutil.rmtree(out, ignore_errors=True)
        rep = json.loads(subprocess.run([binary, '-emit-rust', str(out), pattern], cwd=ROOT,
                                        capture_output=True, text=True).stdout)
        labels = {c['file'].split('/')[-2]: c for c in json.loads((ROOT / lf).read_text())}
        rows = []
        for g in rep['generated']:
            row = dict(program=g['package'], category=g['category'], status=g['status'], forms=g.get('forms', []),
                       unsupported=g.get('unsupported', []), runs=[])
            rows.append(row)
            if g['status'] != 'generated':
                continue
            b = subprocess.run(['cargo', 'build', '--offline', '--release', '-q'], cwd=g['dir'],
                               env=dict(os.environ, CARGO_TARGET_DIR=str(target), RUSTFLAGS='-Awarnings'),
                               capture_output=True, text=True)
            if b.returncode != 0:
                row['status'] = 'compile_failed'
                row['compile_errors'] = b.stderr[-3000:]
                continue
            exe = tmp / f"rs_{g['package']}"
            shutil.copy(target / 'release' / 'prog', exe)
            c = labels[g['package']]
            w = tmp / ('go_' + g['package'])
            w.mkdir()
            shutil.copy(ROOT / c['file'], w / 'main.go')
            (w / 'go.mod').write_text('module x\n\ngo 1.26\n')
            subprocess.run(['go', 'build', '-o', 'prog', '.'], cwd=w, env=env, check=True)
            go = w / 'prog'
            plan = [([str(inp), i['mode']], i.get('inject')) for i in c['inputs']]
            for mode in sorted({i['mode'] for i in c['inputs']}):
                toks = run(go, [str(inp), mode], inp, None, tmp)['os'].split()
                n = sum(1 for t in toks if t.startswith('o'))
                plan += [([str(inp), mode], f'openat:{k}:EMFILE') for k in range(1, n + 1)]
                if any(t.startswith('c') for t in toks):  # only inject a close failure where a close happens
                    plan.append(([str(inp), mode], 'close:1:EIO'))
            plan += [([str(tmp / 'missing.txt')], None), ([], None)]
            seen = set()
            for args, inject in plan:
                key = (tuple(args[1:]), inject)
                if key in seen:
                    continue
                seen.add(key)
                gr, rr = run(go, args, inp, inject, tmp), run(exe, args, inp, inject, tmp)
                if gr is None or rr is None:
                    row['runs'].append(dict(args=args[1:], inject=inject, verdict='injection not reproducible'))
                    continue
                diff = [k for k in ('stdout', 'rc', 'stderr1', 'os') if gr[k] != rr[k]]
                row['runs'].append(dict(args=args[1:], inject=inject, verdict='SAME' if not diff else 'DIFFERENT: ' + ', '.join(diff),
                                        go=gr, rust=rr))
            row['status'] = 'matches_go' if all(r['verdict'] == 'SAME' for r in row['runs']) else 'differs_from_go'
        counts = {}
        for r in rows:
            counts[r['status']] = counts.get(r['status'], 0) + 1
        runs = [x for r in rows for x in r['runs']]
        lines.append(f"== {s}: {len(rows)} programs: " + ', '.join(f"{k} {v}" for k, v in sorted(counts.items()))
                     + f"; runs identical to Go: {sum(x['verdict'] == 'SAME' for x in runs)}/{len(runs)}")
        for r in rows:
            extra = ''
            if r['status'] == 'unsupported':
                extra = ' | ' + '; '.join(r['unsupported'])
            elif r['status'] == 'compile_failed':
                extra = ' | ' + r['compile_errors'].strip().splitlines()[0][:160]
            elif r['status'] == 'differs_from_go':
                extra = ' | ' + '; '.join(f"{x['args']} {x['inject']}: {x['verdict']}" for x in r['runs'] if x['verdict'] != 'SAME')
            lines.append(f"   {r['program']:30} {r['category']}  {r['status']:16} {'; '.join(r['forms'])}{extra}")
        report[s] = rows
    shutil.rmtree(tmp, ignore_errors=True)
    shutil.rmtree(target, ignore_errors=True)
    text = '\n'.join(lines)
    print(text)
    name = 'generated_check' if sets != ['final_unseen'] else 'generated_check_final_unseen'
    (ROOT / f'results/{name}.json').write_text(json.dumps(report, indent=2) + '\n')
    (ROOT / f'results/{name}.txt').write_text(text + '\n')


if __name__ == '__main__':
    main()
