#!/usr/bin/env python3
"""Final evaluation on the sealed set (testdata/final_unseen, U01-U10).

Usage: python3 scripts/final_evaluation.py <rescflow binary> <Go 1.26 GOROOT> <go2rust bin> <rustygo bin>
                                           [--llm-plain DIR] [--llm-guided DIR]
Every tool's translation of every program is built and run next to the Go original on the same
inputs: each labelled input; for each mode, "the k-th open fails" for every k and "the first
close fails" (strace injection); a missing file; no arguments. Compared: stdout, exit code,
first stderr line, OS open/close order. Also recorded per tool: did it compile, and for the
programs whose expected category is C, did it surface the case for human review.
RescFlow additionally: analysis sites and categories against the labels.
Writes results/final_evaluation.json and results/final_evaluation.txt.
"""
import json, os, re, shutil, subprocess, sys, tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from check_rt_ports import run  # noqa: E402
from run_go2rust_patched import PATCH1, CLOSE, STAT  # noqa: E402

ROOT = Path(__file__).resolve().parents[1]
SET = ROOT / 'testdata/final_unseen'


def sh(cmd, cwd, env=None, timeout=900):
    return subprocess.run(cmd, cwd=cwd, env=env, capture_output=True, text=True, timeout=timeout)


def main():
    a = sys.argv[1:]
    rf, goroot, g2r, rgo = a[0], a[1], a[2], a[3]
    opt = {k: a[i + 1] for i, k in enumerate(a) if k.startswith('--')}
    env = dict(os.environ, GOTOOLCHAIN='local', PATH=f"{goroot}/bin:{os.environ['PATH']}")
    labels = {Path(c['file']).parent.name: c for c in json.loads((SET / 'labels.json').read_text())}
    work = Path(tempfile.mkdtemp(prefix='rescflow-final-'))
    target = work / 'cargo-target'
    inp = work / 'input.txt'
    inp.write_text('resource fixture\n')
    cenv = dict(os.environ, CARGO_TARGET_DIR=str(target), RUSTFLAGS='-Awarnings')

    # ---- RescFlow: analysis, categories, generation
    rep = json.loads(sh([rf, '-emit-rust', str(ROOT / 'generated/final_unseen'), './testdata/final_unseen/...'], ROOT).stdout)
    gen = {g['package']: g for g in rep['generated']}
    cats = {p['package']: p['category'] for p in rep['program_decisions']}
    analysis = {}
    for name, c in labels.items():
        want_acq = {s['line'] for s in c['sites'] if s['kind'] == 'acquire'}
        want_def = {s['line'] for s in c['sites'] if s['kind'] == 'defer'}
        got_acq = {r['line'] for r in rep['records'] if r['file'] == c['file']}
        got_def = {d['line'] for d in rep['defers'] if d['func'].split('.')[0] == name}
        analysis[name] = dict(acq_found=len(want_acq & got_acq), acq_labelled=len(want_acq), acq_extra=sorted(got_acq - want_acq),
                              def_found=len(want_def & got_def), def_labelled=len(want_def),
                              expected=c['expected_rescflow_category'], decided=cats.get(name, 'A'),
                              plans=[d['plan'] for d in rep['decisions'] if d['func'].split('.')[0] == name],
                              review=[r['question'] for r in rep['reviews'] if r['program'] == name])

    # ---- builders: name -> (exe or None, status, note, review_flag)
    def b_rescflow(name, d):
        g = gen.get(name)
        if g is None:
            return None, 'missing', '', False
        if g['status'] == 'review':
            return None, 'review report (no Rust, by design)', '', True
        if g['status'] == 'unsupported':
            return None, 'unsupported', '; '.join(g['unsupported'])[:200], False
        r = sh(['cargo', 'build', '--offline', '--release', '-q'], g['dir'], cenv)
        if r.returncode:
            return None, 'compile failed', r.stderr.strip().splitlines()[0][:200], False
        exe = work / f'rescflow_{name}'
        shutil.copy(target / 'release' / 'prog', exe)
        return exe, 'built', '; '.join(g['forms']), False

    def b_go2rust(name, d, patched):
        w = work / f"g2r{'p' if patched else ''}_{name}"
        w.mkdir()
        shutil.copy(d / 'main.go', w / 'main.go')
        (w / 'go.mod').write_text(f'module example.com/{name}\n\ngo 1.26\n')
        t = sh([g2r, 'main.go'], w, env)
        if t.returncode or not (w / 'main.rs').exists():
            return None, 'translation failed', (t.stderr or t.stdout).strip()[:200], False
        if patched:
            src = (w / 'main.rs').read_text()
            if src.count(PATCH1[0]) == 1:
                src = src.replace(PATCH1[0], PATCH1[1])
            if src.count(CLOSE) == 1:
                src = src.replace(CLOSE, STAT)
            (w / 'main.rs').write_text(src)
        r = sh(['cargo', 'build', '--offline', '--release', '-q'], w, cenv)
        if r.returncode:
            errs = sorted(set(re.findall(r'error\[E\d+\]', r.stderr)))
            return None, 'compile failed', ' '.join(errs) or r.stderr.strip()[:150], False
        pkg = [l.split('"')[1] for l in (w / 'Cargo.toml').read_text().splitlines() if l.startswith('name')][0]
        exe = work / f"g2r{'p' if patched else ''}_{name}_bin"
        shutil.copy(target / 'release' / pkg, exe)
        return exe, 'built', '', False

    def b_rustygo(name, d):
        w = work / f'rgo_{name}'
        w.mkdir()
        shutil.copy(d / 'main.go', w / 'main.go')
        (w / 'go.mod').write_text(f'module example.com/{name}\n\ngo 1.26\n')
        exe = work / f'rgo_{name}_bin'
        r = sh([rgo, 'build', '-o', str(exe), '.'], w, env, timeout=1800)
        if r.returncode or not exe.exists():
            return None, 'build failed', (r.stderr or r.stdout).strip()[:200], False
        return exe, 'built', '', False

    def b_llm_plain(name, d):
        src = Path(opt['--llm-plain']) / name / 'main.rs'
        if not src.exists():
            return None, 'no translation', '', False
        exe = work / f'llmp_{name}'
        r = sh(['rustc', '--edition', '2021', '-O', '-A', 'warnings', str(src), '-o', str(exe)], work)
        if r.returncode:
            return None, 'compile failed', r.stderr.strip().splitlines()[0][:200], False
        return exe, 'built', '', 'REVIEW' in src.read_text()

    def b_llm_guided(name, d):
        pd = Path(opt['--llm-guided']) / name
        if not (pd / 'main.rs').exists():
            return None, 'no translation', '', False
        r = sh(['cargo', 'build', '--offline', '--release', '-q'], pd, cenv)
        if r.returncode:
            return None, 'compile failed', r.stderr.strip().splitlines()[0][:200], False
        exe = work / f'llmg_{name}'
        shutil.copy(target / 'release' / 'prog', exe)
        return exe, 'built', '', '// REVIEW' in (pd / 'main.rs').read_text()

    tools = [('RescFlow', b_rescflow), ('Go2Rust (as emitted)', lambda n, d: b_go2rust(n, d, False)),
             ('Go2Rust (2 stand-in patches)', lambda n, d: b_go2rust(n, d, True)), ('Rustygo', b_rustygo)]
    if '--llm-plain' in opt:
        tools.append(('LLM, plain prompt', b_llm_plain))
    if '--llm-guided' in opt:
        tools.append(('LLM + RescFlow analysis', b_llm_guided))

    # ---- Go originals and the input plan per program
    gobins, plans = {}, {}
    for name, c in labels.items():
        w = work / f'go_{name}'
        w.mkdir()
        shutil.copy(SET / name / 'main.go', w / 'main.go')
        (w / 'go.mod').write_text('module x\n\ngo 1.26\n')
        sh(['go', 'build', '-o', 'prog', '.'], w, env)
        gobins[name] = w / 'prog'
        plan = [([str(inp), i['mode']], i.get('inject')) for i in c['inputs']]
        for mode in sorted({i['mode'] for i in c['inputs']}):
            toks = run(gobins[name], [str(inp), mode], inp, None, work)['os'].split()
            plan += [([str(inp), mode], f'openat:{k}:EMFILE') for k in range(1, sum(t.startswith('o') for t in toks) + 1)]
            if any(t.startswith('c') for t in toks):
                plan.append(([str(inp), mode], 'close:1:EIO'))
        plan += [([str(work / 'missing.txt')], None), ([], None)]
        seen, uniq = set(), []
        for p in plan:
            k = (tuple(p[0][1:]), p[1])
            if k not in seen:
                seen.add(k)
                uniq.append(p)
        plans[name] = uniq

    results = {}
    for tool, build in tools:
        rows = {}
        for name in sorted(labels):
            exe, status, note, flagged = build(name, SET / name)
            row = dict(status=status, note=note, flagged=flagged, runs=0, same=0, diffs=[])
            if exe is not None:
                for args, inject in plans[name]:
                    gr, rr = run(gobins[name], args, inp, inject, work), run(exe, args, inp, inject, work)
                    if gr is None or rr is None:
                        continue
                    row['runs'] += 1
                    diff = [k for k in ('stdout', 'rc', 'stderr1', 'os') if gr[k] != rr[k]]
                    row['same'] += not diff
                    if diff:
                        row['diffs'].append(f"{args[1:]} {inject or ''}: {', '.join(diff)}")
                row['status'] = 'identical to Go' if row['same'] == row['runs'] else 'differs from Go'
            rows[name] = row
        results[tool] = rows
        print(tool, 'done', flush=True)

    # ---- summary
    expected_c = [n for n, c in labels.items() if c['expected_rescflow_category'] == 'C']
    lines = [f"Final evaluation on the sealed set ({len(labels)} programs, {sum(len(p) for p in plans.values())} runs per tool)", ""]
    lines.append("| Tool | Translated & compiled | Programs identical to Go | Runs identical to Go | Expected-C programs surfaced for review |")
    lines.append("|---|---|---|---|---|")
    summary = {}
    for tool, rows in results.items():
        built = sum(r['runs'] > 0 for r in rows.values())
        ident = sum(r['status'] == 'identical to Go' for r in rows.values())
        runs = sum(r['runs'] for r in rows.values())
        same = sum(r['same'] for r in rows.values())
        flagged = sum(rows[n]['flagged'] for n in expected_c)
        summary[tool] = dict(built=built, identical_programs=ident, runs=runs, same_runs=same, flagged_c=flagged)
        lines.append(f"| {tool} | {built}/{len(rows)} | {ident}/{len(rows)} | {same}/{runs} | {flagged}/{len(expected_c)} |")
    lines.append("")
    lines.append("Per program (status; runs identical/total):")
    for name in sorted(labels):
        cells = [f"{t}: {results[t][name]['status']} {results[t][name]['same']}/{results[t][name]['runs']}"
                 + (' REVIEW' if results[t][name]['flagged'] else '') for t in results]
        lines.append(f"  {name} (expected {labels[name]['expected_rescflow_category']}, RescFlow decided {analysis[name]['decided']}): " + ' | '.join(cells))
    lines.append("")
    lines.append("RescFlow analysis on the sealed set:")
    acq = sum(v['acq_found'] for v in analysis.values()), sum(v['acq_labelled'] for v in analysis.values())
    dfs = sum(v['def_found'] for v in analysis.values()), sum(v['def_labelled'] for v in analysis.values())
    agree = sum(v['expected'] == v['decided'] for v in analysis.values())
    lines.append(f"  acquisition sites {acq[0]}/{acq[1]}, defer sites {dfs[0]}/{dfs[1]}, categories agree {agree}/{len(analysis)}")
    for n, v in sorted(analysis.items()):
        if v['expected'] != v['decided']:
            lines.append(f"  {n}: expected {v['expected']}, decided {v['decided']}; plans {v['plans']}; review: {v['review']}")
    text = '\n'.join(lines)
    print(text)
    (ROOT / 'results/final_evaluation.json').write_text(json.dumps(dict(summary=summary, results=results, analysis=analysis), indent=2) + '\n')
    (ROOT / 'results/final_evaluation.txt').write_text(text + '\n')
    shutil.rmtree(work, ignore_errors=True)


if __name__ == '__main__':
    main()
