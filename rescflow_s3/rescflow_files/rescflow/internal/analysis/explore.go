package analysis

// Path exploration: predicted open/close sequences.
//
// After the fixpoint analysis (which merges paths at loop heads and beyond 16 states),
// the explorer walks concrete paths from main: it follows every feasible branch, unrolls
// each CFG edge at most maxEdgeVisits times, inlines calls to analyzed functions (so a
// callee's opens and closes appear at the call), gives every loop iteration its own
// resource instance, and keeps values per path. At each exit it records the sequence of
// open/close events the program performs on that path - in the same order the OS sees them.
//
// The result is checked against strace recordings of the real Go program
// (scripts/check_traces.py): every real run must match one predicted path.

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// TraceEv is one predicted OS-visible event on a resource.
type TraceEv struct {
	Ev     string // open | close
	Obj    ObjID
	Via    string
	Failed bool // the open failed (the OS records no descriptor)
}

// ProgramTrace lists the predicted paths of one program (package with func main).
type ProgramTrace struct {
	Package   string      `json:"package"`
	Paths     []PathTrace `json:"paths"`
	Truncated bool        `json:"truncated"`
}

// PathTrace is one predicted path. Canonical renames resources by order of opening
// ("o1 o2 c1 c2" = open first, open second, close first, close second), which is also
// how the strace recording is canonicalized, since the OS only sees descriptor numbers.
type PathTrace struct {
	Canonical  string   `json:"canonical"`
	Events     []string `json:"events"`
	Exit       string   `json:"exit"`
	Incomplete string   `json:"incomplete,omitempty"`
}

const (
	maxEdgeVisits = 4     // each CFG edge at most 4 times per path (loops up to ~4 iterations)
	maxDepth      = 8     // inlining depth
	maxExits      = 20000 // total recorded exits per program
)

type exitRes struct {
	st   *state
	kind string // return | panic | os.Exit
	ret  []ObjSet
}

func (fc *fctx) emit(st *state, ev TraceEv) {
	if fc.enum && st != nil {
		st.trace = append(st.trace, ev)
	}
}

func (fc *fctx) markIncomplete(st *state, why string) {
	if fc.enum && st != nil && st.incomplete == "" {
		st.incomplete = why
	}
}

func instBase(o ObjID) (ObjID, bool) {
	s := string(o)
	if i := strings.LastIndex(s, "#"); i > 0 && !strings.HasSuffix(s, "#older") {
		return ObjID(s[:i]), true
	}
	return o, false
}

func (a *Analyzer) instLabel(o ObjID) string {
	b, ok := instBase(o)
	if !ok {
		return a.label(o)
	}
	return a.label(b) + string(o[len(b):])
}

// successors computes the states on each outgoing edge of b (branch refinement and
// correlation of repeated conditions included).
func (fc *fctx) successors(b *ssa.BasicBlock, st *state) []*state {
	outs := make([]*state, len(b.Succs))
	if !st.live {
		return outs
	}
	if iff, ok := b.Instrs[len(b.Instrs)-1].(*ssa.If); ok && len(b.Succs) == 2 {
		key, neg := condKey(iff.Cond)
		if key != "" {
			if v, known := st.facts[key]; known {
				taken := 0
				if v == neg {
					taken = 1
				}
				outs[taken] = fc.refine(iff, st, taken == 0)
				fc.note(iff, "condition %s already decided on this path: only the %s edge is feasible", key, map[int]string{0: "true", 1: "false"}[taken])
				return outs
			}
		}
		outs[0] = fc.refine(iff, st.clone(), true)
		outs[1] = fc.refine(iff, st, false)
		if key != "" {
			outs[0].facts[key] = !neg
			outs[1].facts[key] = neg
		}
		return outs
	}
	for i := range outs {
		outs[i] = st.clone()
	}
	return outs
}

// inlineTarget returns the analyzed function a call should be explored into, if any.
func (fc *fctx) inlineTarget(c *ssa.CallCommon) *ssa.Function {
	targets, _, kind := fc.resolveCall(c)
	if (kind != "static" && kind != "invoke") || len(targets) != 1 {
		return nil
	}
	fn := targets[0]
	if classifyAPI(fn) != apiNone || !fc.a.local[fn] || fc.a.fcs[fn] == nil {
		return nil
	}
	return fn
}

// explore walks one path from instruction start of block b, appending exits to out.
func (fc *fctx) explore(b *ssa.BasicBlock, start int, pred *ssa.BasicBlock, st *state,
	visits map[[2]int]int, depth int, out *[]exitRes, budget *int) {
	if *budget <= 0 {
		return
	}
	for i := start; i < len(b.Instrs); i++ {
		ins := b.Instrs[i]
		fc.cur, fc.curPred, fc.curBlock = st, pred, b
		if c, ok := ins.(*ssa.Call); ok {
			if callee := fc.inlineTarget(&c.Call); callee != nil {
				if depth >= maxDepth {
					fc.markIncomplete(st, "call depth limit (recursion)")
				} else {
					for _, r := range fc.inline(c, callee, st, depth, budget) {
						ns := r.st
						switch r.kind {
						case "return":
							fc.explore(b, i+1, pred, ns, visits, depth, out, budget)
						case "panic":
							fc.cur, fc.curPred, fc.curBlock = ns, pred, b
							fc.recoverTo = nil
							fc.panicExit(c, ns, fc.line(c.Pos()), "panic inside "+fc.a.fnName(callee))
							fc.finishDead(ns, visits, depth, out, budget)
						default:
							*out = append(*out, exitRes{st: ns, kind: r.kind})
							*budget--
						}
					}
					return
				}
			}
		}
		if v, ok := ins.(ssa.Value); ok {
			delete(st.venv, v)
			delete(st.tenv, v)
		}
		var rets []ObjSet
		if r, ok := ins.(*ssa.Return); ok {
			for _, v := range r.Results {
				rets = append(rets, fc.reach(v, st).clone())
			}
		}
		fc.recoverTo = nil
		fc.instr(ins, st)
		if !st.live {
			fc.finishDead(st, visits, depth, out, budget)
			return
		}
		if _, ok := ins.(*ssa.Return); ok {
			*out = append(*out, exitRes{st: st, kind: "return", ret: rets})
			*budget--
			return
		}
	}
	for i, s := range fc.successors(b, st) {
		if s == nil || !s.live {
			continue
		}
		e := [2]int{b.Index, b.Succs[i].Index}
		if visits[e] >= maxEdgeVisits {
			continue // loop unrolled far enough; longer runs are not predicted
		}
		nv := make(map[[2]int]int, len(visits)+1)
		for k, v := range visits {
			nv[k] = v
		}
		nv[e]++
		fc.explore(b.Succs[i], 0, b, s, nv, depth, out, budget)
	}
}

// finishDead handles a path that stopped (panic or os.Exit): resume in the recover block
// if a deferred call recovered, otherwise record the exit.
func (fc *fctx) finishDead(st *state, visits map[[2]int]int, depth int, out *[]exitRes, budget *int) {
	if rs := fc.recoverTo; rs != nil && fc.fn.Recover != nil {
		fc.recoverTo = nil
		fc.explore(fc.fn.Recover, 0, nil, rs, visits, depth, out, budget)
		return
	}
	kind := st.exitKind
	if kind == "" {
		kind = "panic"
	}
	*out = append(*out, exitRes{st: st, kind: kind})
	*budget--
}

// inline explores callee with the caller's actual resources bound to its inputs and maps
// each callee exit back to a caller state.
func (fc *fctx) inline(c *ssa.Call, callee *ssa.Function, st *state, depth int, budget *int) []exitRes {
	cfc := fc.a.fcs[callee]
	_, inputs, _ := fc.resolveCall(&c.Call)
	init := newState()
	for k, v := range st.res {
		init.res[k] = v
	}
	init.venv, init.tenv, init.tret = map[ssa.Value]ObjSet{}, map[ssa.Value]typeSet{}, map[ssa.Value][]ObjSet{}
	init.inst, init.curInst, init.callObj = map[ObjID]int{}, map[ObjID]ObjID{}, map[*ssa.Call]ObjID{}
	for k, v := range st.inst {
		init.inst[k] = v
	}
	for k, v := range st.curInst {
		init.curInst[k] = v
	}
	init.trace = append([]TraceEv(nil), st.trace...)
	init.incomplete = st.incomplete
	for k, v := range st.cells {
		if strings.HasPrefix(k, "global:") {
			init.cells[k] = v.clone()
		}
	}
	for k, v := range cfc.inputs {
		if k >= len(inputs) {
			break
		}
		fc.cur = st
		init.venv[v] = fc.reach(inputs[k], st).clone()
		init.tenv[v] = fc.typesOf(inputs[k])
		if cs := fc.cells(inputs[k]); len(cs) > 0 {
			objs := ObjSet{}
			for _, cell := range cs {
				objs.addAll(fc.contents(st, cell))
			}
			init.cells[fmt.Sprintf("in:%d*", k)] = objs
		}
	}
	saved := [4]any{cfc.enum, cfc.cur, cfc.curPred, cfc.recoverTo}
	cfc.enum = true
	var out []exitRes
	cfc.explore(callee.Blocks[0], 0, nil, init, map[[2]int]int{}, depth+1, &out, budget)
	cfc.enum = saved[0].(bool)
	cfc.cur, _ = saved[1].(*state)
	cfc.curPred, _ = saved[2].(*ssa.BasicBlock)
	cfc.recoverTo, _ = saved[3].(*state)
	fc.cur = st
	var res []exitRes
	for _, r := range out {
		ns := st.clone()
		ns.res, ns.inst, ns.curInst = r.st.res, r.st.inst, r.st.curInst
		ns.trace, ns.incomplete, ns.exitKind = r.st.trace, r.st.incomplete, r.st.exitKind
		for k, v := range r.st.cells {
			if strings.HasPrefix(k, "global:") {
				ns.cells[k] = v
			}
		}
		for k, v := range inputs {
			if cs := fc.cells(v); len(cs) == 1 {
				if objs, ok := r.st.cells[fmt.Sprintf("in:%d*", k)]; ok {
					ns.cells[cs[0]] = objs
				}
			}
		}
		ns.tret[c] = r.ret
		if len(r.ret) == 1 {
			ns.venv[c] = r.ret[0]
		}
		if r.kind != "return" {
			ns.live = false
		}
		res = append(res, exitRes{st: ns, kind: r.kind})
	}
	return res
}

// ExploreMain predicts the open/close sequences of a program from its main function.
func (a *Analyzer) ExploreMain(main *ssa.Function) *ProgramTrace {
	fc := a.fcs[main]
	pt := &ProgramTrace{Package: main.Pkg.Pkg.Path(), Paths: []PathTrace{}}
	if fc == nil {
		return pt
	}
	init := newState()
	init.venv, init.tenv, init.tret = map[ssa.Value]ObjSet{}, map[ssa.Value]typeSet{}, map[ssa.Value][]ObjSet{}
	init.inst, init.curInst, init.callObj = map[ObjID]int{}, map[ObjID]ObjID{}, map[*ssa.Call]ObjID{}
	fc.enum = true
	budget := maxExits
	var out []exitRes
	fc.explore(main.Blocks[0], 0, nil, init, map[[2]int]int{}, 0, &out, &budget)
	fc.enum, fc.cur = false, nil
	pt.Truncated = budget <= 0
	seen := map[string]bool{}
	for _, r := range out {
		canon, events := a.canonical(r.st.trace)
		key := canon + "|" + strings.Join(events, ";") + "|" + r.kind + "|" + r.st.incomplete
		if seen[key] {
			continue
		}
		seen[key] = true
		pt.Paths = append(pt.Paths, PathTrace{Canonical: canon, Events: events, Exit: r.kind, Incomplete: r.st.incomplete})
	}
	sort.Slice(pt.Paths, func(i, j int) bool {
		if pt.Paths[i].Canonical != pt.Paths[j].Canonical {
			return pt.Paths[i].Canonical < pt.Paths[j].Canonical
		}
		return strings.Join(pt.Paths[i].Events, ";") < strings.Join(pt.Paths[j].Events, ";")
	})
	return pt
}

func (a *Analyzer) canonical(tr []TraceEv) (string, []string) {
	idx := map[ObjID]int{}
	canon, events := []string{}, []string{}
	for _, e := range tr {
		l := a.instLabel(e.Obj)
		if e.Failed {
			events = append(events, "open "+l+" FAILED")
			continue
		}
		switch e.Ev {
		case "open":
			idx[e.Obj] = len(idx) + 1
			canon = append(canon, fmt.Sprintf("o%d", idx[e.Obj]))
			events = append(events, "open "+l)
		case "close":
			if k, ok := idx[e.Obj]; ok {
				canon = append(canon, fmt.Sprintf("c%d", k))
			} else {
				canon = append(canon, "c?")
			}
			events = append(events, "close "+l+" ("+e.Via+")")
		}
	}
	return strings.Join(canon, " "), events
}
