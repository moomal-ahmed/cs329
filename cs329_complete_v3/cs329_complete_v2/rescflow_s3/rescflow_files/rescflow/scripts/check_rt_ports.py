#!/usr/bin/env python3
"""Step 4 check: the benchmark translations ported onto rescflow_rt behave exactly like Go.

Usage: python3 scripts/check_rt_ports.py <Go 1.26 GOROOT>
Builds rescflow_rt's examples (case01, 08, 10, 14, 17: one per kind of plan) and each Go
original, then runs both on: every labelled input; for each mode, "the k-th open fails" for
every k and "the first close fails" (strace injection, made deterministic as in
check_traces.py); a missing file; no arguments. Compares stdout, exit code, the first stderr
line, and the canonical OS open/close sequence. Writes results/rt_ports_check.json / .txt.
"""
import json, os, shutil, subprocess, sys, tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from os_trace import os_events  # noqa: E402

ROOT = Path(__file__).resolve().parents[1]
RT = ROOT / 'rescflow_rt'
PORTS = {'01': 'case01_local', '08': 'case08_loop', '10': 'case10_return_closed',
         '14': 'case14_late_closure', '17': 'case17_panic_unwind'}


def run(binary, args, inp, inject, cwd, attempts=25):
    log = Path(cwd) / 'st.log'
    cmd = ['strace', '-f', '-qq', '-o', str(log), '-P', str(inp), '-e', 'trace=openat,close']
    if inject:
        call, nth, errno = inject.split(':')
        cmd += ['-e', f'inject={call}:error={errno}:when={nth}']
    for _ in range(attempts if inject else 1):
        p = subprocess.run(cmd + [str(binary)] + args, cwd=cwd, capture_output=True, text=True, timeout=60)
        text = log.read_text()
        if not inject or ('INJECTED' in text and len({l.split()[0] for l in text.splitlines() if l.strip()}) <= 1):
            break
    else:
        return None
    ev, _ = os_events(text, str(inp))
    canon = ' '.join(f"o{e[1]}" if e[0] == 'OPEN' else f"c{e[1]}" for e in ev if e[0] in ('OPEN', 'CLOSE'))
    return dict(stdout=p.stdout, rc=p.returncode, stderr1=(p.stderr.splitlines() or [''])[0], os=canon)


def main():
    goroot = sys.argv[1]
    env = dict(os.environ, GOTOOLCHAIN='local', PATH=f"{goroot}/bin:{os.environ['PATH']}")
    b = subprocess.run(['cargo', 'build', '--offline', '--release', '--examples'], cwd=RT, capture_output=True, text=True)
    if b.returncode != 0:
        sys.exit(b.stderr)
    labels = {c['id']: c for c in json.loads((ROOT / 'benchmarks/labels.json').read_text())}
    tmp = Path(tempfile.mkdtemp(prefix='rescflow-rtports-'))
    inp = tmp / 'input.txt'
    inp.write_text('resource fixture\n')
    rows, lines, total, same = [], [], 0, 0
    for cid, ex in PORTS.items():
        c = labels[cid]
        w = tmp / cid
        w.mkdir()
        shutil.copy(ROOT / c['file'], w / 'main.go')
        (w / 'go.mod').write_text('module x\n\ngo 1.26\n')
        subprocess.run(['go', 'build', '-o', 'prog', '.'], cwd=w, env=env, check=True)
        go, rs = w / 'prog', RT / 'target/release/examples' / ex
        plan = [([str(inp), i['mode']], i.get('inject'), 'label') for i in c['inputs']]
        for mode in sorted({i['mode'] for i in c['inputs']}):
            n = sum(1 for t in run(go, [str(inp), mode], inp, None, tmp)['os'].split() if t.startswith('o'))
            plan += [([str(inp), mode], f'openat:{k}:EMFILE', 'injected') for k in range(1, n + 1)]
            plan.append(([str(inp), mode], 'close:1:EIO', 'injected'))
        plan += [([str(tmp / 'missing.txt')], None, 'extra'), ([], None, 'extra')]
        for args, inject, origin in plan:
            g, r = run(go, args, inp, inject, tmp), run(rs, args, inp, inject, tmp)
            if g is None or r is None:
                rows.append(dict(case=cid, args=args[1:], inject=inject, verdict='injection not reproducible'))
                continue
            diff = [k for k in ('stdout', 'rc', 'stderr1', 'os') if g[k] != r[k]]
            total += 1
            same += not diff
            rows.append(dict(case=cid, port=ex, args=args[1:], inject=inject, origin=origin,
                             verdict='SAME' if not diff else 'DIFFERENT: ' + ', '.join(diff), go=g, rust=r))
        mine = [x for x in rows if x['case'] == cid]
        bad = [x for x in mine if x['verdict'] != 'SAME']
        lines.append(f"  {cid} {ex:22} runs {len(mine):2}  same as Go {len(mine) - len(bad):2}"
                     + ('' if not bad else '  <-- ' + '; '.join(f"{x['args']} {x['inject']}: {x['verdict']}" for x in bad)))
    shutil.rmtree(tmp, ignore_errors=True)
    text = f"rescflow_rt ports vs Go: {same}/{total} runs identical (stdout, exit code, stderr, OS open/close order)\n" + '\n'.join(lines)
    print(text)
    (ROOT / 'results/rt_ports_check.json').write_text(json.dumps(rows, indent=2) + '\n')
    (ROOT / 'results/rt_ports_check.txt').write_text(text + '\n')


if __name__ == '__main__':
    main()
