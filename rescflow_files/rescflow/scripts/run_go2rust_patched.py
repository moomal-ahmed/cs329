#!/usr/bin/env python3
"""SECONDARY experiment: isolate Go2Rust's defer/resource translation from two stdlib-stub bugs.

Every Go2Rust output in results/baselines.json fails to compile with the same two errors,
both inside Go2Rust's generated `os` stand-in, not in translated benchmark code:
  E0530  os::open's local `let path` collides with the benchmark's global `path`
  E0599  the os_File stand-in has no `stat` method (used by the useFile helper)
This script applies two minimal textual patches to that stand-in only:
  1. rename the stub-local `path` to `__go_path`
  2. add os_File::stat returning an error iff the stand-in's own closed flag is set
     (mirrors Go: Stat on a closed *os.File fails). Close() is NOT modified.
No translated scenario/helper code is touched. Results are reported separately from the
unmodified baseline and must not be presented as Go2Rust's own success rate.
"""
import json, os, shutil, subprocess, sys, tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run_baselines import compare, run  # noqa: E402

ROOT = Path(__file__).resolve().parents[1]
ART = ROOT / 'results/baseline_artifacts/go2rust'

PATCH1 = ('let path = _arg0.into_go_string();\n        match std::fs::read(&path) {',
          'let __go_path = _arg0.into_go_string();\n        match std::fs::read(&__go_path) {')
CLOSE = '''    pub fn close(&self) -> Rc<RefCell<Option<Box<dyn StdError>>>> {
        self.__go_closed.store(true, std::sync::atomic::Ordering::SeqCst);
        Rc::new(RefCell::new(None::<Box<dyn StdError>>))
    }
'''
STAT = CLOSE + '''    // RESCFLOW PATCH (not Go2Rust output): minimal Stat stand-in for useFile.
    pub fn stat(&self) -> (Rc<RefCell<Option<fs_FileInfo>>>, Rc<RefCell<Option<Box<dyn StdError>>>>) {
        if self.__go_closed.load(std::sync::atomic::Ordering::SeqCst) {
            (Rc::new(RefCell::new(None)), Rc::new(RefCell::new(Some(Box::new(std::io::Error::other("file already closed")) as Box<dyn StdError>))))
        } else {
            (Rc::new(RefCell::new(Some(fs_FileInfo::default()))), Rc::new(RefCell::new(None::<Box<dyn StdError>>)))
        }
    }
'''


OPEN_OK = '                (Rc::new(RefCell::new(Some::<os_File>(file))), no_error())'
OPEN_PROBE = ('                println!("PROBE open {:p}", std::sync::Arc::as_ptr(&file.__go_closed));\n' + OPEN_OK)
CLOSE_BODY = ('        self.__go_closed.store(true, std::sync::atomic::Ordering::SeqCst);\n'
              '        Rc::new(RefCell::new(None::<Box<dyn StdError>>))')
CLOSE_PROBE = ('        let __was = self.__go_closed.swap(true, std::sync::atomic::Ordering::SeqCst);\n'
               '        println!("PROBE close {:p} {}", std::sync::Arc::as_ptr(&self.__go_closed), __was);\n'
               '        Rc::new(RefCell::new(None::<Box<dyn StdError>>))')


def identity_trace(stdout):
    """Map each Close() to the resource it really acted on (probe build only).
    Returns events like 'close A (printed A)' / 'close A (printed B) ALREADY-CLOSED'."""
    owner, pending, events, opened = {}, None, [], []
    for line in stdout.splitlines():
        p = line.split()
        if p[:2] == ['PROBE', 'open']:
            pending = p[2]
        elif p[:1] == ['acquire'] and pending:
            owner[pending] = p[1]; opened.append(p[1]); pending = None
        elif p[:2] == ['PROBE', 'close']:
            events.append(dict(actual=owner.get(p[2], '?'), already_closed=p[3] == 'true'))
        elif p[:1] == ['close'] and events and 'printed' not in events[-1]:
            events[-1]['printed'] = p[1]
    closed = {e['actual'] for e in events}
    return dict(closes=events, never_closed=[x for x in opened if x not in closed],
                mislabeled=sum(e.get('printed') != e['actual'] for e in events),
                double_closes=sum(e['already_closed'] for e in events))


def main():
    labels = json.loads((ROOT / 'benchmarks/labels.json').read_text())
    go_obs = {p['id']: p for p in json.loads((ROOT / 'results/go_runs.json').read_text())['programs']}
    env = dict(os.environ, RUSTFLAGS='-Awarnings', CARGO_TERM_COLOR='never')
    target = Path(tempfile.mkdtemp(prefix='rescflow-g2r-patched-'))
    rows = []
    for c in labels:
        art = ART / f"{c['id']}_{c['name']}"
        src = (art / 'main.rs').read_text()
        row = dict(tool='go2rust_patched', id=c['id'], name=c['name'], runs=[])
        if src.count(PATCH1[0]) != 1 or src.count(CLOSE) != 1:
            row['status'] = 'patch_not_applicable'; rows.append(row); continue
        patched = src.replace(PATCH1[0], PATCH1[1]).replace(CLOSE, STAT)
        (art / 'main.patched.rs').write_text(patched)
        with tempfile.TemporaryDirectory() as t:
            w = Path(t)
            (w / 'main.rs').write_text(patched)
            shutil.copy(art / 'Cargo.toml', w / 'Cargo.toml')
            b = run(['cargo', 'build', '--release', '--quiet'], w, dict(env, CARGO_TARGET_DIR=str(target)))
            (art / 'build.patched.log').write_text(b.get('stderr', '') + b.get('error', ''))
            pkg = [l.split('"')[1] for l in (w / 'Cargo.toml').read_text().splitlines() if l.startswith('name')][0]
            binary = target / 'release' / pkg
            if b.get('returncode') != 0:
                row['status'] = 'build_failed'; row['build_stderr'] = b.get('stderr', '')[-3000:]; rows.append(row); continue
            (w / 'input.txt').write_text('resource fixture\n')
            for spec in c['inputs']:
                r = run([str(binary), str(w / 'input.txt'), spec['mode']], w, env, timeout=60)
                r['mode'] = spec['mode']
                ref = next(x for x in go_obs[c['id']]['runs'] if x['mode'] == spec['mode'])
                r['matches_observed_go'] = r.get('stdout') == ref['stdout'] and r.get('returncode') == ref['returncode']
                r['go_stdout'] = ref['stdout']
                r['diff'] = compare(ref['stdout'].splitlines(), r.get('stdout', '').splitlines())
                r['stderr'] = r.get('stderr', '')[-1500:]
                row['runs'].append(r)
            row['status'] = 'executed'
            binary.unlink(missing_ok=True)
            # identity probe: separate build, never used for the match verdict above
            probed = patched.replace(OPEN_OK, OPEN_PROBE, 1).replace(CLOSE_BODY, CLOSE_PROBE, 1)
            (w / 'main.rs').write_text(probed)
            b = run(['cargo', 'build', '--release', '--quiet'], w, dict(env, CARGO_TARGET_DIR=str(target)))
            if b.get('returncode') == 0:
                for spec in c['inputs']:
                    r = run([str(binary), str(w / 'input.txt'), spec['mode']], w, env, timeout=60)
                    x = next(x for x in row['runs'] if x['mode'] == spec['mode'])
                    x['identity'] = identity_trace(r.get('stdout', ''))
                binary.unlink(missing_ok=True)
        rows.append(row)
    shutil.rmtree(target, ignore_errors=True)
    runs = [x for r in rows for x in r['runs']]
    summary = dict(cases=len(rows), built=sum(r['status'] == 'executed' for r in rows),
                   input_pairs_executed=len(runs), input_pairs_matching_go=sum(x['matches_observed_go'] for x in runs),
                   cases_all_inputs_match=sum(r['status'] == 'executed' and all(x['matches_observed_go'] for x in r['runs']) for r in rows))
    (ROOT / 'results/go2rust_patched.json').write_text(json.dumps(dict(
        note=__doc__, summary=summary, rows=rows), indent=2) + '\n')
    print(json.dumps(summary, indent=2))
    for r in rows:
        for x in r['runs']:
            i = x.get('identity', {})
            print(r['id'], r['name'], repr(x['mode']), 'MATCH' if x['matches_observed_go'] else 'DIFF',
                  'mislabeled', i.get('mislabeled'), 'double', i.get('double_closes'), 'never_closed', i.get('never_closed'),
                  '' if x['matches_observed_go'] else x['diff']['first_divergence'])
        if r['status'] != 'executed':
            print(r['id'], r['status'])


if __name__ == '__main__':
    main()
