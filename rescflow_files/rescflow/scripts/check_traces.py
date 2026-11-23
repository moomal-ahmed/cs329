#!/usr/bin/env python3
"""Step 2 check: does the analysis predict what the real Go programs do with files?

Usage: python3 scripts/check_traces.py <rescflow binary> <Go 1.26 GOROOT>

For every program in benchmarks/ and heldout/:
  1. predicted: `rescflow -traces` lists, per path from main, the sequence of file opens and
     closes, canonicalized by order of opening ("o1 o2 c1 c2").
  2. real: the Go program runs under strace on
       - every input in its labels.json (including labelled fault injections),
       - automatic injections: for each mode, "the k-th open of the input fails" for every k,
         and "the first close fails",
       - a path that does not exist, and no arguments at all.
     The openat/close calls on the input file are canonicalized the same way.
  3. verdict per run: MATCH (real sequence is one of the complete predicted paths),
     COVERED-BY-INCOMPLETE (matches only a path the analysis marked incomplete),
     NOT-COVERED (no match, but some predicted paths are incomplete), MISMATCH (no match and
     every predicted path claims to be complete: the analysis is wrong).
Also lists predicted complete paths that no run exercised.
Writes results/trace_check.json and results/trace_check.txt.
"""
import json, os, shutil, subprocess, sys, tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from os_trace import os_events  # noqa: E402

ROOT = Path(__file__).resolve().parents[1]


def real_canonical(binary, args, inp, inject, cwd, attempts=25):
    """strace counts `when=N` per thread, and Go may issue the file syscalls from more than
    one OS thread. An injected run is accepted only if strace reports the injection AND all
    syscalls on the input file came from one thread; otherwise it is retried. Returns
    canon=None if no reliable run was obtained."""
    log = Path(cwd) / 'strace.log'
    cmd = ['strace', '-f', '-qq', '-o', str(log), '-P', str(inp), '-e', 'trace=openat,close']
    if inject:
        call, nth, errno = inject.split(':')
        cmd += ['-e', f'inject={call}:error={errno}:when={nth}']
    for _ in range(attempts if inject else 1):
        p = subprocess.run(cmd + [str(binary)] + args, cwd=cwd, capture_output=True, text=True, timeout=60)
        text = log.read_text()
        tids = {l.split()[0] for l in text.splitlines() if l.strip()}
        if not inject or ('INJECTED' in text and len(tids) <= 1):
            break
    else:
        return None, p.returncode, p.stdout.splitlines()
    events, _ = os_events(text, str(inp))
    canon = []
    for e in events:
        if e[0] == 'OPEN':
            canon.append(f'o{e[1]}')
        elif e[0] == 'CLOSE':
            canon.append(f'c{e[1]}')
    return ' '.join(canon), p.returncode, p.stdout.splitlines()


def fifo_mutant(canon):
    """Negative control: a wrong analysis that runs the closes at an exit in FIFO order.
    Reverses every maximal run of consecutive closes."""
    toks, out, run = canon.split(), [], []
    for t in toks + ['END']:
        if t.startswith('c'):
            run.append(t)
        else:
            out += run[::-1]
            run = []
            if t != 'END':
                out.append(t)
    return ' '.join(out)


def main():
    binary, goroot = sys.argv[1], sys.argv[2]
    mutant = '--mutant-fifo' in sys.argv
    env = dict(os.environ, GOTOOLCHAIN='local', PATH=f"{goroot}/bin:{os.environ['PATH']}")
    out = {}
    lines = []
    for setname, labels_file, pattern in [('benchmarks', 'benchmarks/labels.json', './benchmarks/...'),
                                          ('heldout', 'heldout/labels.json', './heldout/...')]:
        rep = json.loads(subprocess.run([binary, '-traces', pattern], cwd=ROOT, capture_output=True, text=True).stdout)
        predicted = {t['package'].rsplit('/', 1)[-1]: t for t in rep.get('predicted_traces') or []}
        labels = json.loads((ROOT / labels_file).read_text())
        tmp = Path(tempfile.mkdtemp(prefix='rescflow-tracecheck-'))
        inp = tmp / 'input.txt'
        inp.write_text('resource fixture\n')
        programs, tot = [], dict(runs=0, match=0, incomplete_match=0, not_covered=0, mismatch=0, unexercised=0)
        for c in labels:
            pkg = Path(c['file']).parent.name
            pt = predicted.get(pkg, {'paths': [], 'truncated': False})
            if mutant:
                pt = dict(pt, paths=[dict(x, canonical=fifo_mutant(x['canonical'])) for x in pt['paths']])
            complete = {p['canonical'] for p in pt['paths'] if not p.get('incomplete')}
            incomplete = {p['canonical'] for p in pt['paths'] if p.get('incomplete')}
            w = tmp / pkg
            w.mkdir()
            shutil.copy(ROOT / c['file'], w / 'main.go')
            (w / 'go.mod').write_text('module x\n\ngo 1.26\n')
            b = subprocess.run(['go', 'build', '-o', 'prog', '.'], cwd=w, env=env, capture_output=True, text=True)
            if b.returncode != 0:
                print('build failed', pkg, b.stderr)
                continue
            prog = w / 'prog'
            runs, unreliable = [], []
            plan = [(i['mode'], i.get('inject'), 'label') for i in c['inputs']]
            for mode in sorted({i['mode'] for i in c['inputs']}):
                canon, _, _ = real_canonical(prog, [str(inp), mode], inp, None, tmp)
                n_opens = sum(1 for t in canon.split() if t.startswith('o'))
                for k in range(1, n_opens + 1):
                    plan.append((mode, f'openat:{k}:EMFILE', 'auto'))
                if any(t.startswith('c') for t in canon.split()):
                    plan.append((mode, 'close:1:EIO', 'auto'))
            seen = set()
            for mode, inject, origin in plan:
                if (mode, inject) in seen:
                    continue
                seen.add((mode, inject))
                canon, rc, _ = real_canonical(prog, [str(inp), mode], inp, inject, tmp)
                if canon is None:  # injection could not be made deterministic: report, do not score
                    unreliable.append(f"mode={mode!r} inject={inject}")
                    continue
                runs.append(dict(input=f"mode={mode!r}" + (f" inject={inject}" if inject else ''), origin=origin, real=canon, rc=rc))
            for name, args in [('missing path', [str(tmp / 'does-not-exist.txt')]), ('no arguments', [])]:
                canon, rc, _ = real_canonical(prog, args, inp, None, tmp)
                runs.append(dict(input=name, origin='auto', real=canon, rc=rc))
            hit = set()
            for r in runs:
                if r['real'] in complete:
                    r['verdict'] = 'MATCH'
                    hit.add(r['real'])
                elif r['real'] in incomplete:
                    r['verdict'] = 'COVERED-BY-INCOMPLETE'
                elif incomplete:
                    r['verdict'] = 'NOT-COVERED'
                else:
                    r['verdict'] = 'MISMATCH'
                tot['runs'] += 1
                tot[{'MATCH': 'match', 'COVERED-BY-INCOMPLETE': 'incomplete_match', 'NOT-COVERED': 'not_covered',
                     'MISMATCH': 'mismatch'}[r['verdict']]] += 1
            unex = sorted(complete - hit)
            tot['unexercised'] += len(unex)
            tot['unreliable_injections'] = tot.get('unreliable_injections', 0) + len(unreliable)
            programs.append(dict(id=c['id'], name=c['name'], predicted=pt['paths'], truncated=pt['truncated'],
                                 runs=runs, unexercised_predictions=unex, unreliable_injections=unreliable))
        shutil.rmtree(tmp, ignore_errors=True)
        out[setname] = dict(totals=tot, programs=programs)
        lines.append(f"== {setname}: {len(programs)} programs, {tot['runs']} real runs "
                     f"({sum(1 for p in programs for r in p['runs'] if r['origin'] == 'label')} labelled inputs, the rest injected/extra)")
        lines.append(f"   MATCH {tot['match']}   covered-by-incomplete {tot['incomplete_match']}   "
                     f"not-covered {tot['not_covered']}   MISMATCH {tot['mismatch']}   "
                     f"predicted-but-unexercised {tot['unexercised']}   "
                     f"injections-not-reproducible {tot.get('unreliable_injections', 0)}")
        for p in programs:
            bad = [r for r in p['runs'] if r['verdict'] != 'MATCH']
            flag = '' if not bad else '  <-- ' + '; '.join(f"{r['input']}: real [{r['real']}] {r['verdict']}" for r in bad)
            lines.append(f"   {p['id']:4} {p['name']:26} runs {len(p['runs']):2}  predicted paths {len(p['predicted']):2}"
                         f"  unexercised {p['unexercised_predictions']}{flag}")
    text = '\n'.join(lines)
    print(text)
    suffix = '_mutant_fifo' if mutant else ''
    (ROOT / f'results/trace_check{suffix}.json').write_text(json.dumps(out, indent=2) + '\n')
    (ROOT / f'results/trace_check{suffix}.txt').write_text(text + '\n')


if __name__ == '__main__':
    main()
