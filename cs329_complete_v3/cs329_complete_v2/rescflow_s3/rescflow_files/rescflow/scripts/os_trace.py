#!/usr/bin/env python3
"""OS-level resource trace: when is the real file descriptor opened and closed?

Builds each benchmark natively with Go, with Rustygo, and (patched, see
run_go2rust_patched.py) with Go2Rust; runs each under strace and interleaves
openat/close syscalls on the input file with the program's own stdout lines.
Output: results/os_traces.json. Requires strace, a Go 1.26 GOROOT, rustygo, cargo.
"""
import argparse, json, os, re, shutil, subprocess, tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ART = ROOT / 'results/baseline_artifacts'
LINE = re.compile(r'^\d+\s+(\w+)\((.*)\)\s+=\s+(-?\d+)')


def sh(cmd, cwd, env, timeout=900):
    p = subprocess.run(cmd, cwd=cwd, env=env, capture_output=True, text=True, timeout=timeout)
    if p.returncode != 0:
        raise RuntimeError(f"{cmd}: {p.stderr[-2000:]}")
    return p


def os_events(trace_text, input_path):
    """Return ordered events: ('OPEN', n) / ('CLOSE', n, ok) / ('OUT', line)."""
    fds, seq, events = {}, 0, []
    for raw in trace_text.splitlines():
        m = LINE.match(raw)
        if not m:
            continue
        call, args, ret = m.group(1), m.group(2), int(m.group(3))
        if call == 'openat' and f'"{input_path}' in args and ret >= 0:
            seq += 1
            fds[ret] = seq
            events.append(('OPEN', seq))
        elif call == 'close':
            fd = int(args.split(',')[0])
            if fd in fds:
                events.append(('CLOSE', fds.pop(fd), ret == 0))
        elif call == 'write' and args.startswith('1, "'):
            text = args[4:args.rfind('"')].encode().decode('unicode_escape')
            events.extend(('OUT', l) for l in text.splitlines())
    leaked = sorted(fds.values())
    return events, leaked


def render(events):
    out = []
    for e in events:
        if e[0] == 'OPEN':
            out.append(f'[os open #{e[1]}]')
        elif e[0] == 'CLOSE':
            out.append(f'[os close #{e[1]}{"" if e[2] else " FAILED"}]')
        else:
            out.append(e[1])
    return out


def lifetimes(events):
    """For each OS handle: printed lines while it was open (normalised lifetime)."""
    life, open_ = {}, {}
    for e in events:
        if e[0] == 'OPEN':
            open_[e[1]] = []
        elif e[0] == 'CLOSE':
            life[e[1]] = open_.pop(e[1], None)
        else:
            for k in open_:
                open_[k].append(e[1])
    for k, v in open_.items():
        life[k] = v + ['<process exit>']
    return life


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--goroot', required=True)
    ap.add_argument('--rustygo', required=True)
    ap.add_argument('--only', nargs='*')
    a = ap.parse_args()
    env = dict(os.environ, GOTOOLCHAIN='local', PATH=f"{a.goroot}/bin:{os.environ['PATH']}", RUSTFLAGS='-Awarnings')
    labels = json.loads((ROOT / 'benchmarks/labels.json').read_text())
    patched = {r['id']: r for r in json.loads((ROOT / 'results/go2rust_patched.json').read_text())['rows']}
    tmp = Path(tempfile.mkdtemp(prefix='rescflow-ostrace-'))
    inp = tmp / 'input.txt'
    inp.write_text('resource fixture\n')
    target = tmp / 'cargo-target'
    rows = []
    for c in labels:
        if a.only and c['id'] not in a.only:
            continue
        src = ROOT / c['file']
        w = tmp / c['id']
        w.mkdir()
        shutil.copy(src, w / 'main.go')
        (w / 'go.mod').write_text(f"module example.com/rescflow/benchmarks/{src.parent.name}\n\ngo 1.26\n")
        bins = {}
        sh(['go', 'build', '-o', str(tmp / f"go_{c['id']}"), '.'], w, env); bins['go'] = tmp / f"go_{c['id']}"
        sh([a.rustygo, 'build', '-o', str(tmp / f"rustygo_{c['id']}"), '.'], w, env); bins['rustygo'] = tmp / f"rustygo_{c['id']}"
        pr = ART / 'go2rust' / f"{c['id']}_{c['name']}"
        if (pr / 'main.patched.rs').exists():
            g = tmp / f"g2r_{c['id']}"
            g.mkdir()
            shutil.copy(pr / 'main.patched.rs', g / 'main.rs')
            shutil.copy(pr / 'Cargo.toml', g / 'Cargo.toml')
            sh(['cargo', 'build', '--release', '--quiet'], g, dict(env, CARGO_TARGET_DIR=str(target)))
            shutil.copy(target / 'release' / 'transpiled', tmp / f"go2rust_{c['id']}")
            bins['go2rust_patched'] = tmp / f"go2rust_{c['id']}"
        for spec in c['inputs']:
            row = dict(id=c['id'], name=c['name'], mode=spec['mode'], tools={})
            for tool, b in bins.items():
                log = tmp / 'strace.txt'
                subprocess.run(['strace', '-f', '-qq', '-s', '400', '-e', 'trace=openat,close,write', '-o', str(log),
                                str(b), str(inp), spec['mode']], cwd=tmp, capture_output=True, text=True, timeout=60)
                ev, leaked = os_events(log.read_text(), str(inp))
                row['tools'][tool] = dict(trace=render(ev), handles_open_at_exit=leaked,
                                          lifetimes={str(k): v for k, v in lifetimes(ev).items()})
            g = row['tools']['go']['trace']
            for tool, t in row['tools'].items():
                t['os_trace_matches_go'] = t['trace'] == g
            rows.append(row)
            print(c['id'], c['name'], repr(spec['mode']),
                  {k: v['os_trace_matches_go'] for k, v in row['tools'].items()}, flush=True)
    shutil.rmtree(tmp, ignore_errors=True)
    out = ROOT / ('results/os_traces.json' if not a.only else 'results/os_traces_partial.json')
    out.write_text(json.dumps(dict(note=__doc__, rows=rows), indent=2) + '\n')


if __name__ == '__main__':
    main()
