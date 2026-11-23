#!/usr/bin/env python3
"""Automated consistency review of benchmarks/labels.json and heldout/labels.json.

Usage: python3 scripts/review_labels.py      (after run_go.py, run_heldout_go.py and
                                              evaluate_analysis.py have written results/)
Checks, per program:
  sites    every `// RF:` comment in the source has a label site with the same line, kind,
           label and source text, and every label site points at such a comment
  outputs  every labelled input's expected stdout/exit equals what real Go printed
  closes   every `close X` / `use X` in an expected output names an acquired label (or a loop id)
  defers   the resources each labelled defer acts on, as the analysis resolves them
  category held-out programs carry an expected RescFlow category and plan
Writes results/label_review.json and prints one line per program.
"""
import json, re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def rf_comments(src):
    out = {}
    for n, line in enumerate(src.splitlines(), 1):
        if '// RF:' in line:
            code, tag = line.split('// RF:')
            kind, *label = tag.split()
            out[n] = dict(kind=kind, label=' '.join(label), source=code.strip())
    return out


def main():
    go_obs = {p['id']: p for p in json.loads((ROOT / 'results/go_runs.json').read_text())['programs']}
    held_obs = {p['id']: p for p in json.loads((ROOT / 'results/heldout_go_runs.json').read_text())['programs']}
    ev = json.loads((ROOT / 'results/analysis_eval.json').read_text())
    assoc = {p['id']: p['assoc'] for s in ev.values() for p in s['programs']}
    rows = []
    for setname, lf, obs in [('benchmarks', 'benchmarks/labels.json', go_obs), ('heldout', 'heldout/labels.json', held_obs)]:
        for c in json.loads((ROOT / lf).read_text()):
            src = (ROOT / c['file']).read_text()
            rf = rf_comments(src)
            issues = []
            for s in c['sites']:
                r = rf.get(s['line'])
                if not r:
                    issues.append(f"site line {s['line']} has no RF comment")
                elif (r['kind'], r['label'], r['source']) != (s['kind'], s['label'], s['source']):
                    issues.append(f"site line {s['line']} differs from its RF comment")
            for n in rf:
                if n not in {s['line'] for s in c['sites']}:
                    issues.append(f"RF comment on line {n} has no label site")
            runs = obs[c['id']]['runs']
            for i, spec in enumerate(c['inputs']):
                key = 'oracle_match' if setname == 'benchmarks' else 'label_match'
                match = [r for r in runs if r['mode'] == spec['mode'] and r.get('inject') == spec.get('inject')]
                if not match or not match[0].get(key):
                    issues.append(f"input {spec['mode']!r} {spec.get('inject') or ''}: expected output differs from real Go")
            acquired = {s['label'] for s in c['sites'] if s['kind'] == 'acquire'}
            for spec in c['inputs']:
                for line in spec['expected_stdout']:
                    p = line.split()
                    if p and p[0] in ('close', 'use') and len(p) >= 2:
                        lab = p[1]
                        ok = lab in acquired or lab.isdigit() or lab.rstrip('ab') .isdigit()
                        if not ok:
                            issues.append(f"output line {line!r} names {lab!r}, which is not an acquired label")
            if setname == 'heldout' and not (c.get('expected_rescflow_category') and c.get('expected_plan')):
                issues.append('missing expected category or plan')
            notes = []
            for line, a in (assoc.get(c['id']) or {}).items():
                if a['verdict'] != 'exact':
                    notes.append(f"defer@{line}: label {a['label']} vs analysis {a['analysis']} ({a['verdict']})")
            rows.append(dict(set=setname, id=c['id'], name=c['name'], sites=len(c['sites']), inputs=len(c['inputs']),
                             issues=issues, analysis_notes=notes))
            status = 'OK' if not issues else 'ISSUES: ' + '; '.join(issues)
            print(f"{c['id']:4} {c['name']:26} sites {len(c['sites']):2} inputs {len(c['inputs'])}  {status}"
                  + (f"  | note: {'; '.join(notes)}" if notes else ''))
    (ROOT / 'results/label_review.json').write_text(json.dumps(rows, indent=2) + '\n')
    bad = sum(1 for r in rows if r['issues'])
    print(f"\n{len(rows)} programs, {sum(r['sites'] for r in rows)} sites, {sum(r['inputs'] for r in rows)} inputs: "
          f"{len(rows) - bad} consistent, {bad} with issues")


if __name__ == '__main__':
    main()
