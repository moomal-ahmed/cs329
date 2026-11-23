#!/usr/bin/env python3
"""Translate, build and run every benchmark with Go2Rust and Rustygo; compare with observed Go.

Adapter notes (verified 2026-11-09 against the revisions recorded in results/baselines.json):
- Go2Rust `go2rust main.go` writes main.rs + Cargo.toml next to the input (not stdout);
  the adapter copies each benchmark into its own work directory and builds with cargo.
- Rustygo overlays GOROOT sources written for Go 1.26, so it needs a standalone Go 1.26
  GOROOT (not a toolchain inside GOMODCACHE). Each benchmark is copied into a one-file
  module declaring `go 1.26`; the sources themselves are unchanged.
- Rustygo `emit` writes the crate tree for inspection; `build` regenerates and compiles,
  so a build failure is recorded as `combined_build_failed`.
Status values never call a missing tool a translation failure.
"""
import argparse, json, os, shutil, subprocess, sys, tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'results/baseline_artifacts'


def run(cmd, cwd, env=None, timeout=900):
    try:
        p = subprocess.run(cmd, cwd=cwd, text=True, capture_output=True, timeout=timeout, env=env)
        return dict(command=list(map(str, cmd)), returncode=p.returncode, stdout=p.stdout, stderr=p.stderr)
    except (OSError, subprocess.TimeoutExpired) as e:
        return dict(command=list(map(str, cmd)), error=str(e))


def tail(text, n=4000):
    return text if len(text) <= n else '...[truncated]...\n' + text[-n:]


def git_rev(path):
    r = run(['git', 'rev-parse', 'HEAD'], path)
    d = run(['git', 'status', '--porcelain'], path)
    return dict(head=r.get('stdout', '').strip(), dirty=bool(d.get('stdout', '').strip()))


def compare(go_lines, rs_lines):
    """First divergence plus multiset differences of trace events."""
    first = None
    for i in range(max(len(go_lines), len(rs_lines))):
        a = go_lines[i] if i < len(go_lines) else None
        b = rs_lines[i] if i < len(rs_lines) else None
        if a != b:
            first = dict(index=i, go=a, translated=b)
            break
    missing = [x for x in go_lines if go_lines.count(x) > rs_lines.count(x)]
    extra = [x for x in rs_lines if rs_lines.count(x) > go_lines.count(x)]
    return dict(first_divergence=first, missing_events=sorted(set(missing)), extra_events=sorted(set(extra)),
                same_events_different_order=(first is not None and sorted(go_lines) == sorted(rs_lines)))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--go2rust', required=True)
    ap.add_argument('--rustygo', required=True)
    ap.add_argument('--go2rust-src', help='checkout used to build go2rust (for revision recording)')
    ap.add_argument('--rustygo-src', help='checkout used to build rustygo (for revision recording)')
    ap.add_argument('--goroot', required=True, help='standalone Go 1.26.x GOROOT')
    ap.add_argument('--only', nargs='*', help='case ids to run')
    args = ap.parse_args()

    env = dict(os.environ, GOTOOLCHAIN='local', PATH=f"{args.goroot}/bin:{os.environ['PATH']}",
               CARGO_TERM_COLOR='never', RUSTFLAGS='-Awarnings')
    labels = json.loads((ROOT / 'benchmarks/labels.json').read_text())
    go_obs = {p['id']: p for p in json.loads((ROOT / 'results/go_runs.json').read_text())['programs']}
    environment = dict(
        go=run(['go', 'version'], ROOT, env)['stdout'].strip(),
        rustc=run(['rustc', '-V'], ROOT, env)['stdout'].strip(),
        cargo=run(['cargo', '-V'], ROOT, env)['stdout'].strip(),
        go2rust=git_rev(args.go2rust_src) if args.go2rust_src else None,
        rustygo=git_rev(args.rustygo_src) if args.rustygo_src else None,
        rustygo_version=run([args.rustygo, 'version'], ROOT, env).get('stdout', '').strip(),
        go_reference='results/go_runs.json (Go 1.27.0); see go_runs_go126.json for the Go 1.26.5 recheck',
    )
    rows = []
    shared_target = Path(tempfile.mkdtemp(prefix='rescflow-cargo-'))
    for c in labels:
        if args.only and c['id'] not in args.only:
            continue
        src = ROOT / c['file']
        for tool in ['go2rust', 'rustygo']:
            print(f"[{tool}] case {c['id']} {c['name']}", file=sys.stderr, flush=True)
            row = dict(tool=tool, id=c['id'], name=c['name'], runs=[])
            art = OUT / tool / f"{c['id']}_{c['name']}"
            if art.exists():
                shutil.rmtree(art)
            art.mkdir(parents=True)
            with tempfile.TemporaryDirectory(prefix=f'rescflow-{tool}-') as t:
                work = Path(t) / 'pkg'
                work.mkdir()
                shutil.copy(src, work / 'main.go')
                (work / 'go.mod').write_text(f"module example.com/rescflow/benchmarks/{src.parent.name}\n\ngo 1.26\n")
                binary = Path(t) / 'program'
                if tool == 'go2rust':
                    trans = run([args.go2rust, 'main.go'], work, env)
                    row['translation'] = dict(trans, stdout=tail(trans.get('stdout', '')), stderr=tail(trans.get('stderr', '')))
                    if trans.get('returncode') != 0 or not (work / 'main.rs').exists():
                        row['status'] = 'translation_command_failed'
                        rows.append(row); continue
                    shutil.copy(work / 'main.rs', art / 'main.rs')
                    shutil.copy(work / 'Cargo.toml', art / 'Cargo.toml')
                    build = run(['cargo', 'build', '--release', '--quiet'], work,
                                dict(env, CARGO_TARGET_DIR=str(shared_target)))
                    row['build'] = dict(build, stdout=tail(build.get('stdout', '')), stderr=tail(build.get('stderr', '')))
                    (art / 'build.log').write_text(build.get('stderr', '') + build.get('error', ''))
                    pkg = [l.split('"')[1] for l in (work / 'Cargo.toml').read_text().splitlines() if l.startswith('name')][0]
                    built = shared_target / 'release' / pkg
                    if build.get('returncode') != 0 or not built.exists():
                        row['status'] = 'build_failed'
                        rows.append(row); continue
                    shutil.copy(built, binary)
                else:
                    emit = run([args.rustygo, 'emit', '-o', str(Path(t) / 'emit'), '.'], work, env)
                    row['translation'] = dict(emit, stdout=tail(emit.get('stdout', '')), stderr=tail(emit.get('stderr', '')))
                    if emit.get('returncode') != 0:
                        row['status'] = 'emit_command_failed'
                        rows.append(row); continue
                    # keep only the translated benchmark package (the rest is the translated Go stdlib)
                    for f in (Path(t) / 'emit').rglob('*.rs'):
                        if 'rescflow' in f.name:
                            shutil.copy(f, art / 'scenario.rs')
                    build = run([args.rustygo, 'build', '-o', str(binary), '.'], work, env)
                    row['combined_build'] = dict(build, stdout=tail(build.get('stdout', '')), stderr=tail(build.get('stderr', '')))
                    (art / 'build.log').write_text(build.get('stderr', '') + build.get('error', ''))
                    if build.get('returncode') != 0 or not binary.exists():
                        row['status'] = 'combined_build_failed'
                        rows.append(row); continue
                rdir = Path(t) / 'run'
                rdir.mkdir()
                inp = rdir / 'input.txt'
                inp.write_text('resource fixture\n')
                ref_runs = go_obs.get(c['id'], {}).get('runs', [])
                for spec in c['inputs']:
                    r = run([str(binary), str(inp), spec['mode']], rdir, env, timeout=60)
                    r['mode'] = spec['mode']
                    ref = next((x for x in ref_runs if x['mode'] == spec['mode']), None)
                    if ref is None:
                        r['matches_observed_go'] = None
                    else:
                        r['matches_observed_go'] = all(r.get(k) == ref.get(k) for k in ['stdout', 'returncode']) \
                            and (bool(r.get('stderr')) == bool(ref.get('stderr')))
                        r['diff'] = compare(ref.get('stdout', '').splitlines(), r.get('stdout', '').splitlines())
                    r['stderr'] = tail(r.get('stderr', ''), 1500)
                    row['runs'].append(r)
                row['status'] = 'executed'
                rows.append(row)
    shutil.rmtree(shared_target, ignore_errors=True)
    summary = {}
    for tool in ['go2rust', 'rustygo']:
        rs = [r for r in rows if r['tool'] == tool]
        runs = [x for r in rs for x in r['runs']]
        summary[tool] = dict(
            cases=len(rs),
            translated=sum(r['status'] not in ('translation_command_failed', 'emit_command_failed') for r in rs),
            built=sum(r['status'] == 'executed' for r in rs),
            input_pairs_executed=len(runs),
            input_pairs_matching_go=sum(bool(x.get('matches_observed_go')) for x in runs),
            cases_all_inputs_match=sum(r['status'] == 'executed' and all(x.get('matches_observed_go') for x in r['runs']) for r in rs),
            statuses={s: sum(r['status'] == s for r in rs) for s in sorted({r['status'] for r in rs})},
        )
    result = dict(date='2026-11-09', environment=environment, summary=summary, rows=rows)
    out = ROOT / ('results/baselines.json' if not args.only else 'results/baselines_partial.json')
    out.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(summary, indent=2))
    return 0


if __name__ == '__main__':
    sys.exit(main())
