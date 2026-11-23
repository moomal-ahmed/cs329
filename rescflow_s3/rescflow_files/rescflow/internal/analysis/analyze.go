package analysis

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// inEffect: what a function does to the resources reachable from one of its inputs.
type inEffect struct {
	Release        bool     `json:"release,omitempty"`
	Use            bool     `json:"use,omitempty"`
	Escapes        []string `json:"escapes,omitempty"`
	WritesCloseErr bool     `json:"writes_close_error,omitempty"`
}

type freshRes struct {
	Site     ObjID  `json:"site"`
	GuardIdx int    `json:"guard_result"` // result index that says "acquired" (-1: none)
	GuardErr bool   `json:"guard_is_error"`
	State    RState `json:"-"`
	StateStr string `json:"state_at_return"`
}

// Summary is the per-function contract used at call sites (bottom-up over the call graph).
type Summary struct {
	Inputs       []string          `json:"inputs"`
	In           []inEffect        `json:"effects"`
	ReturnsInput map[int][]int     `json:"returns_input,omitempty"`
	Fresh        map[int]*freshRes `json:"returns_fresh_resource,omitempty"`
	BoolNilOf    int               `json:"bool_result_is_nil_check_of_input"`
	CloseErrOf   int               `json:"result_is_close_error_of_input"`
	Panics       bool              `json:"always_panics,omitempty"`
	Recovers     bool              `json:"calls_recover,omitempty"`
	Unknown      bool              `json:"unknown,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	// deferOf: defer statements inside this function that release input K
	deferOf map[int][]*DeferFact
}

type Analyzer struct {
	prog      *ssa.Program
	fset      *token.FileSet
	root      string
	explain   bool
	local     map[*ssa.Function]bool
	sums      map[*ssa.Function]*Summary
	busy      map[*ssa.Function]bool
	records   map[ObjID]*Record
	globals   map[string]ObjSet
	goFns     map[*ssa.Function]bool
	unres     []Unresolved
	unresSeen map[string]bool
	funcs     map[*ssa.Function]*FuncReport
	dfacts    map[*ssa.Defer]*DeferFact
	fcs       map[*ssa.Function]*fctx
}

type meaning struct {
	root  string
	pol   bool // bool value true  <=> (root succeeded) == pol
	isErr bool // the value is an error: nil <=> root succeeded
	objs  ObjSet
}

type retObs struct {
	objs   []ObjSet
	states []map[ObjID]RState
	consts []ssa.Value
}

type fctx struct {
	a          *Analyzer
	fn         *ssa.Function
	inputs     []ssa.Value
	valPts     map[ssa.Value]ObjSet
	cellPts    map[ssa.Value][]string
	vtypes     map[ssa.Value]typeSet
	meaning    map[ssa.Value]meaning
	guard      map[ObjID]meaning
	closeErr   map[ssa.Value]ObjSet
	callIn     map[ssa.Value][]ObjSet
	inLoop     map[*ssa.BasicBlock]bool
	weakCell   map[string]bool
	collCell   map[string]bool
	resultCell map[string]string
	multi      map[ObjID]bool
	own        map[ObjID]bool
	escaped    map[ObjID]bool
	condDefer  map[*ssa.Defer]bool
	names      map[ssa.Value]string
	entry      map[*ssa.BasicBlock][]*state
	changed    bool
	rec        bool
	sum        *Summary
	report     *FuncReport
	notes      map[ssa.Instruction][]string
	retObs     []retObs
	curExit    *Exit
	released   ObjSet
	panicSeen  bool
	returnSeen bool
	curBlock   *ssa.BasicBlock
	curDefer   *ssa.Defer
	// path exploration (explore.go)
	enum      bool
	cur       *state
	curPred   *ssa.BasicBlock
	recoverTo *state
}

// ---------------------------------------------------------------- helpers

func (a *Analyzer) pos(p token.Pos) (string, int) {
	if !p.IsValid() {
		return "", 0
	}
	q := a.fset.Position(p)
	f := q.Filename
	if r, err := filepath.Rel(a.root, f); err == nil && !strings.HasPrefix(r, "..") {
		f = r
	}
	return filepath.ToSlash(f), q.Line
}

func (a *Analyzer) fnName(fn *ssa.Function) string {
	if fn.Pkg == nil {
		return fn.String()
	}
	p := fn.Pkg.Pkg.Path()
	return p[strings.LastIndex(p, "/")+1:] + "." + fn.RelString(fn.Pkg.Pkg)
}

func (a *Analyzer) addUnresolved(kind string, fn *ssa.Function, p token.Pos, objs ObjSet, detail string) {
	u := explain(kind)
	if fn != nil {
		u.Func = a.fnName(fn)
	}
	u.File, u.Line = a.pos(p)
	for _, o := range objs.sorted() {
		if _, sym := symIndex(ObjID(o)); !sym {
			u.Resources = append(u.Resources, a.label(ObjID(o)))
		}
	}
	u.Detail = detail
	key := fmt.Sprintf("%s|%s|%d|%v", kind, u.File, u.Line, u.Resources)
	if a.unresSeen[key] {
		return
	}
	a.unresSeen[key] = true
	a.unres = append(a.unres, u)
	for _, o := range objs.sorted() {
		if r := a.records[ObjID(o)]; r != nil {
			r.Notes = appendUnique(r.Notes, fmt.Sprintf("UNRESOLVED %s at line %d: %s", kind, u.Line, u.Construct))
		}
	}
}

func (a *Analyzer) label(o ObjID) string {
	if r := a.records[o]; r != nil {
		return fmt.Sprintf("%s@%d", r.Var, r.Line)
	}
	if k, ok := symIndex(o); ok {
		return fmt.Sprintf("input#%d", k)
	}
	return string(o)
}

func (fc *fctx) labels(s ObjSet) string {
	var r []string
	for _, o := range s.sorted() {
		r = append(r, fc.a.label(ObjID(o)))
	}
	return "{" + strings.Join(r, ", ") + "}"
}

func appendUnique(xs []string, x string) []string {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}

func (fc *fctx) line(p token.Pos) int { _, l := fc.a.pos(p); return l }

func (fc *fctx) note(i ssa.Instruction, f string, args ...any) {
	if fc.rec && fc.a.explain {
		fc.notes[i] = append(fc.notes[i], fmt.Sprintf(f, args...))
	}
}

func (fc *fctx) addPts(v ssa.Value, s ObjSet) {
	if len(s) == 0 {
		return
	}
	if fc.enum && fc.cur != nil {
		if fc.cur.venv[v] == nil {
			fc.cur.venv[v] = ObjSet{}
		}
		fc.cur.venv[v].addAll(s)
		return
	}
	cur := fc.valPts[v]
	if cur == nil {
		cur = ObjSet{}
		fc.valPts[v] = cur
	}
	if cur.addAll(s) {
		fc.changed = true
	}
}

func (fc *fctx) addCells(v ssa.Value, cs []string) {
	if len(cs) == 0 {
		return
	}
	n := unionStrings(fc.cellPts[v], cs)
	if len(n) != len(fc.cellPts[v]) {
		fc.cellPts[v] = n
		fc.changed = true
	}
}

func (fc *fctx) addTypes(v ssa.Value, ts typeSet) {
	if len(ts) == 0 {
		return
	}
	if fc.enum && fc.cur != nil {
		if fc.cur.tenv[v] == nil {
			fc.cur.tenv[v] = typeSet{}
		}
		for t := range ts {
			fc.cur.tenv[v][t] = true
		}
		return
	}
	cur := fc.vtypes[v]
	if cur == nil {
		cur = typeSet{}
		fc.vtypes[v] = cur
	}
	for t := range ts {
		if !cur[t] {
			cur[t] = true
			fc.changed = true
		}
	}
}

func (fc *fctx) setMeaning(v ssa.Value, m meaning) {
	if old, ok := fc.meaning[v]; !ok || old.root != m.root || old.pol != m.pol {
		fc.meaning[v] = m
		fc.changed = true
	}
}

func (fc *fctx) typesOf(v ssa.Value) typeSet {
	if fc.enum && fc.cur != nil {
		if ts, ok := fc.cur.tenv[v]; ok {
			return ts
		}
	}
	return fc.vtypes[v]
}

func (fc *fctx) pts(v ssa.Value) ObjSet {
	if fc.enum && fc.cur != nil {
		if s, ok := fc.cur.venv[v]; ok {
			return s
		}
	}
	if s := fc.valPts[v]; s != nil {
		return s
	}
	return ObjSet{}
}

func (fc *fctx) cells(v ssa.Value) []string {
	if g, ok := v.(*ssa.Global); ok {
		return []string{"global:" + g.RelString(nil)}
	}
	return fc.cellPts[v]
}

func (fc *fctx) contents(st *state, c string) ObjSet {
	r := ObjSet{}
	for k, v := range st.cells {
		if k == c || strings.HasPrefix(k, c+".") || strings.HasPrefix(c, k+".") {
			r.addAll(v)
		}
	}
	return r
}

// reach: resources a value refers to directly, or through memory it points to.
func (fc *fctx) reach(v ssa.Value, st *state) ObjSet {
	r := fc.pts(v).clone()
	for _, c := range fc.cells(v) {
		r.addAll(fc.contents(st, c))
	}
	return r
}

func (fc *fctx) resOf(st *state, o ObjID) RState {
	if s := st.res[o]; s != 0 {
		return s
	}
	return SOuter
}

// ---------------------------------------------------------------- driver

func (a *Analyzer) summary(fn *ssa.Function) *Summary {
	if s := a.sums[fn]; s != nil {
		return s
	}
	if !a.local[fn] || len(fn.Blocks) == 0 {
		s := &Summary{Unknown: true, Reason: "no analyzable body (outside the analyzed packages)", BoolNilOf: -1, CloseErrOf: -1}
		a.sums[fn] = s
		return s
	}
	if a.busy[fn] {
		return &Summary{Unknown: true, Reason: "recursion", BoolNilOf: -1, CloseErrOf: -1}
	}
	a.busy[fn] = true
	fc := newFctx(a, fn)
	fc.analyze()
	a.busy[fn] = false
	a.sums[fn] = fc.sum
	a.fcs[fn] = fc
	return fc.sum
}

func newFctx(a *Analyzer, fn *ssa.Function) *fctx {
	fc := &fctx{a: a, fn: fn,
		valPts: map[ssa.Value]ObjSet{}, cellPts: map[ssa.Value][]string{}, vtypes: map[ssa.Value]typeSet{},
		meaning: map[ssa.Value]meaning{}, guard: map[ObjID]meaning{}, closeErr: map[ssa.Value]ObjSet{},
		callIn: map[ssa.Value][]ObjSet{}, inLoop: map[*ssa.BasicBlock]bool{}, weakCell: map[string]bool{},
		collCell: map[string]bool{}, resultCell: map[string]string{}, multi: map[ObjID]bool{}, own: map[ObjID]bool{},
		escaped: map[ObjID]bool{}, condDefer: map[*ssa.Defer]bool{}, names: map[ssa.Value]string{},
		entry: map[*ssa.BasicBlock][]*state{}, notes: map[ssa.Instruction][]string{}}
	for _, p := range fn.Params {
		fc.inputs = append(fc.inputs, p)
	}
	for _, v := range fn.FreeVars {
		fc.inputs = append(fc.inputs, v)
	}
	fc.sum = &Summary{BoolNilOf: -1, CloseErrOf: -1, In: make([]inEffect, len(fc.inputs)),
		ReturnsInput: map[int][]int{}, Fresh: map[int]*freshRes{}, deferOf: map[int][]*DeferFact{}}
	for _, v := range fc.inputs {
		fc.sum.Inputs = append(fc.sum.Inputs, v.Name())
		fc.names[v] = v.Name()
	}
	_, line := a.pos(fn.Pos())
	file, _ := a.pos(fn.Pos())
	fc.report = &FuncReport{Name: a.fnName(fn), File: file, Line: line, Summary: fc.sum}
	a.funcs[fn] = fc.report
	fc.findLoops()
	results := map[string]bool{}
	for i := 0; i < fn.Signature.Results().Len(); i++ {
		if n := fn.Signature.Results().At(i).Name(); n != "" {
			results[n] = true
		}
	}
	for _, b := range fn.Blocks {
		for _, ins := range b.Instrs {
			switch x := ins.(type) {
			case *ssa.DebugRef:
				if o := x.Object(); o != nil && !x.IsAddr {
					if _, ok := fc.names[x.X]; !ok {
						fc.names[x.X] = o.Name()
					}
				}
			case *ssa.Alloc:
				fc.names[x] = x.Comment
				if fc.inLoop[b] {
					fc.weakCell[fc.allocKey(x)] = true
				}
				if results[x.Comment] && b == fn.Blocks[0] {
					fc.resultCell[fc.allocKey(x)] = x.Comment
				}
			}
		}
	}
	return fc
}

// findLoops marks blocks that lie on a cycle of the control-flow graph.
func (fc *fctx) findLoops() {
	for _, b := range fc.fn.Blocks {
		seen := map[*ssa.BasicBlock]bool{}
		stack := append([]*ssa.BasicBlock(nil), b.Succs...)
		for len(stack) > 0 {
			x := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if x == b {
				fc.inLoop[b] = true
				break
			}
			if seen[x] {
				continue
			}
			seen[x] = true
			stack = append(stack, x.Succs...)
		}
	}
}

func (fc *fctx) allocKey(x *ssa.Alloc) string {
	n := x.Comment
	if n == "" {
		n = "alloc"
	}
	return fmt.Sprintf("cell:%s(%s)", x.Name(), n)
}

func (fc *fctx) initial() *state {
	st := newState()
	for k, v := range fc.inputs {
		o := symObj(k)
		if mayHold(v.Type()) {
			fc.valPts[v] = ObjSet{o: true}
			st.res[o] = SOuter
		}
		if p, ok := types.Unalias(v.Type()).Underlying().(*types.Pointer); ok {
			key := fmt.Sprintf("in:%d*", k)
			fc.cellPts[v] = []string{key}
			fc.weakCell[key] = true
			if mayHold(p.Elem()) {
				st.cells[key] = ObjSet{o: true}
				st.res[o] = SOuter
			}
		}
		if isErrorType(v.Type()) {
			fc.meaning[v] = meaning{root: fmt.Sprintf("in:%d", k), pol: true, isErr: true}
		}
	}
	for c, objs := range fc.a.globals {
		st.cells[c] = objs.clone()
	}
	return st
}

func (fc *fctx) analyze() {
	fn := fc.fn
	fc.entry[fn.Blocks[0]] = []*state{fc.initial()}
	for round := 0; round < 200; round++ {
		moved := false
		fc.changed = false
		for _, b := range fn.Blocks {
			for _, in := range append([]*state(nil), fc.entry[b]...) {
				for i, s := range fc.block(b, in.clone()) {
					if s != nil && s.live && fc.addState(b.Succs[i], s) {
						moved = true
					}
				}
			}
		}
		if !moved && !fc.changed {
			break
		}
	}
	fc.rec = true
	for _, b := range fn.Blocks {
		for _, in := range fc.entry[b] {
			fc.block(b, in.clone())
		}
	}
	fc.finish()
}

// maxPaths bounds path sensitivity: up to this many distinct states are kept per block;
// beyond it, new states are joined into the last one (a may-analysis on that block).
const maxPaths = 16

// addState records s as an entry state of b. States are kept apart (not joined) so that
// facts such as "the defer was registered on this path" and "the cell holds B on this
// path" stay correlated.
func (fc *fctx) addState(b *ssa.BasicBlock, s *state) bool {
	k := s.key()
	list := fc.entry[b]
	for _, x := range list {
		if x.key() == k {
			return false
		}
	}
	for _, x := range list {
		fc.compareDefers(x, s)
	}
	if len(list) < maxPaths {
		fc.entry[b] = append(list, s.clone())
		return true
	}
	last := list[len(list)-1]
	m := fc.join(last, s)
	if m.key() == last.key() {
		return false
	}
	list[len(list)-1] = m
	return true
}

func (fc *fctx) compareDefers(a, b *state) {
	in := func(p *pend, st *state) bool {
		for _, q := range st.pending {
			if q.d == p.d {
				return true
			}
		}
		return false
	}
	for _, p := range a.pending {
		if !in(p, b) {
			fc.markConditional(p, b)
		}
	}
	for _, p := range b.pending {
		if !in(p, a) {
			fc.markConditional(p, a)
		}
	}
}

func (fc *fctx) block(b *ssa.BasicBlock, st *state) []*state {
	fc.curBlock = b
	for _, ins := range b.Instrs {
		if !st.live {
			break
		}
		fc.instr(ins, st)
	}
	return fc.successors(b, st)
}

// condKey canonicalizes a branch condition that compares loads of globals, parameters or
// constants, so a repeated test of the same condition on one path can be correlated.
// The prototype assumes the global is not written between the two tests (true for the
// corpus, where globals are set once in main); this is recorded as an assumption.
func condKey(v ssa.Value) (string, bool) {
	neg := false
	if u, ok := v.(*ssa.UnOp); ok && u.Op == token.NOT {
		k, n := condKey(u.X)
		return k, !n
	}
	b, ok := v.(*ssa.BinOp)
	if !ok || (b.Op != token.EQL && b.Op != token.NEQ) {
		return "", false
	}
	if b.Op == token.NEQ {
		neg = true
	}
	x, y := operandKey(b.X), operandKey(b.Y)
	if x == "" || y == "" {
		return "", false
	}
	if x > y {
		x, y = y, x
	}
	return x + "==" + y, neg
}

func operandKey(v ssa.Value) string {
	switch x := v.(type) {
	case *ssa.Const:
		return x.String()
	case *ssa.Parameter:
		return "param " + x.Name()
	case *ssa.UnOp:
		if g, ok := x.X.(*ssa.Global); ok && x.Op == token.MUL {
			return "global " + g.Name()
		}
	}
	return ""
}

func (fc *fctx) join(a, b *state) *state {
	if a == nil || !a.live {
		return b.clone()
	}
	if b == nil || !b.live {
		return a
	}
	r := a.clone()
	for k, v := range b.cells {
		if r.cells[k] == nil {
			r.cells[k] = ObjSet{}
		}
		r.cells[k].addAll(v)
	}
	for k, v := range b.cptr {
		r.cptr[k] = unionStrings(r.cptr[k], v)
	}
	for k, v := range b.ctypes {
		if r.ctypes[k] == nil {
			r.ctypes[k] = typeSet{}
		}
		for t := range v {
			r.ctypes[k][t] = true
		}
	}
	for k, v := range b.res {
		r.res[k] |= v
	}
	for k, v := range r.cmean {
		if b.cmean[k] != v {
			delete(r.cmean, k)
		}
	}
	for k, v := range r.facts {
		if bv, ok := b.facts[k]; !ok || bv != v {
			delete(r.facts, k)
		}
	}
	am, bm := map[*ssa.Defer]*pend{}, map[*ssa.Defer]*pend{}
	for _, p := range a.pending {
		am[p.d] = p
	}
	for _, p := range b.pending {
		bm[p.d] = p
	}
	var out []*pend
	for _, p := range a.pending {
		if q, ok := bm[p.d]; ok {
			out = append(out, mergePend(p, q))
		} else {
			out = append(out, p)
			fc.markConditional(p, b)
		}
	}
	for _, q := range b.pending {
		if _, ok := am[q.d]; !ok {
			out = append(out, q)
			fc.markConditional(q, a)
		}
	}
	r.pending = out
	return r
}

// markConditional: a defer is registered on one incoming path but not the other, while
// a resource it would act on is open on the other path => registration is conditional.
func (fc *fctx) markConditional(p *pend, other *state) {
	for _, c := range p.inputs {
		objs := c.eager.clone()
		for _, cell := range c.cells {
			objs.addAll(fc.contents(other, cell))
		}
		for o := range objs {
			if other.res[o]&SOpen != 0 && !fc.condDefer[p.d] {
				fc.condDefer[p.d] = true
				fc.changed = true
			}
		}
	}
}

func (fc *fctx) refine(iff *ssa.If, st *state, branch bool) *state {
	m, ok := fc.meaning[iff.Cond]
	if !ok || m.isErr {
		return st
	}
	good := m.pol == branch
	apply := func(o ObjID) {
		s := st.res[o]
		if s == 0 {
			return
		}
		if fc.enum {
			if b, isInst := instBase(o); (isInst && st.curInst[b] != o) || (!isInst && fc.multi[o]) {
				return // only the current loop instance is decided by this check
			}
			if good && s == SFailed || !good && s&SFailed == 0 && s&SOuter == 0 {
				st.live = false // this branch contradicts what already happened on the path
				return
			}
			if !good {
				for i := len(st.trace) - 1; i >= 0; i-- {
					if st.trace[i].Obj == o && st.trace[i].Ev == "open" {
						st.trace[i].Failed = true
						break
					}
				}
			}
		}
		if good {
			if s&(SOpen|SOuter|SClosed) != 0 {
				s &^= SFailed
			}
		} else if s&SFailed != 0 || s&SOuter != 0 {
			s = SFailed
		}
		st.res[o] = s
	}
	var touched []string
	for o, g := range fc.guard {
		if g.root == m.root {
			apply(o)
			touched = append(touched, fc.a.label(o)+"="+st.res[o].String())
		}
	}
	for o := range m.objs {
		apply(o)
		touched = append(touched, fc.a.label(o)+"="+st.res[o].String())
	}
	if len(touched) > 0 {
		sort.Strings(touched)
		edge := "false"
		if branch {
			edge = "true"
		}
		fc.note(iff, "%s edge: %s", edge, strings.Join(touched, ", "))
	}
	return st
}

// ---------------------------------------------------------------- transfer

func (fc *fctx) instr(ins ssa.Instruction, st *state) {
	switch x := ins.(type) {
	case *ssa.Alloc:
		k := fc.allocKey(x)
		fc.addCells(x, []string{k})
		if !fc.weakCell[k] {
			for c := range st.cells {
				if c == k || strings.HasPrefix(c, k+".") {
					delete(st.cells, c)
				}
			}
		}
	case *ssa.FieldAddr:
		var cs []string
		for _, c := range fc.cells(x.X) {
			if strings.HasSuffix(c, "*") {
				cs = append(cs, c) // inputs are field-insensitive
			} else {
				cs = append(cs, fmt.Sprintf("%s.f%d", c, x.Field))
				if fc.weakCell[c] {
					fc.weakCell[fmt.Sprintf("%s.f%d", c, x.Field)] = true
				}
			}
		}
		fc.addCells(x, cs)
	case *ssa.IndexAddr:
		var cs []string
		for _, c := range fc.cells(x.X) {
			k := c + ".[]"
			fc.weakCell[k] = true
			fc.collCell[k] = true
			cs = append(cs, k)
		}
		fc.addCells(x, cs)
	case *ssa.Field:
		if mayHold(x.Type()) {
			fc.addPts(x, fc.pts(x.X))
		}
	case *ssa.Index:
		fc.addPts(x, fc.pts(x.X))
	case *ssa.Lookup:
		fc.addPts(x, fc.pts(x.X))
	case *ssa.Slice:
		fc.addPts(x, fc.pts(x.X))
		fc.addCells(x, fc.cells(x.X))
	case *ssa.Store:
		fc.store(x, st)
	case *ssa.UnOp:
		switch x.Op {
		case token.MUL:
			fc.load(x, st)
		case token.NOT:
			if m, ok := fc.meaning[x.X]; ok && !m.isErr {
				m.pol = !m.pol
				fc.setMeaning(x, m)
			}
		}
	case *ssa.BinOp:
		fc.binop(x)
	case *ssa.Phi:
		if fc.enum && fc.curPred != nil {
			for i, p := range x.Block().Preds {
				if p == fc.curPred {
					e := x.Edges[i]
					fc.addPts(x, fc.pts(e))
					fc.addCells(x, fc.cells(e))
					fc.addTypes(x, fc.typesOf(e))
					if m, ok := fc.meaning[e]; ok {
						fc.setMeaning(x, m)
					}
					if ce := fc.closeErr[e]; ce != nil {
						fc.closeErr[x] = ce
					}
					break
				}
			}
			break
		}
		var ms []meaning
		for _, e := range x.Edges {
			fc.addPts(x, fc.pts(e))
			fc.addCells(x, fc.cells(e))
			fc.addTypes(x, fc.typesOf(e))
			if m, ok := fc.meaning[e]; ok {
				ms = append(ms, m)
			}
			if ce := fc.closeErr[e]; ce != nil {
				if fc.closeErr[x] == nil {
					fc.closeErr[x] = ObjSet{}
				}
				fc.closeErr[x].addAll(ce)
			}
		}
		if len(ms) == len(x.Edges) && len(ms) > 0 {
			same := true
			for _, m := range ms[1:] {
				same = same && m.root == ms[0].root && m.pol == ms[0].pol && m.isErr == ms[0].isErr
			}
			if same {
				fc.setMeaning(x, ms[0])
			}
		}
	case *ssa.Extract:
		fc.extract(x, st)
	case *ssa.MakeInterface:
		fc.addPts(x, fc.pts(x.X))
		fc.addCells(x, fc.cells(x.X))
		fc.addTypes(x, typeSet{x.X.Type(): true})
	case *ssa.ChangeType:
		fc.pass(x, x.X)
	case *ssa.ChangeInterface:
		fc.pass(x, x.X)
	case *ssa.TypeAssert:
		fc.pass(x, x.X)
	case *ssa.SliceToArrayPointer:
		fc.pass(x, x.X)
	case *ssa.Convert:
		fc.pass(x, x.X)
		if b, ok := x.Type().Underlying().(*types.Basic); ok && b.Kind() == types.UnsafePointer {
			if objs := fc.reach(x.X, st); len(objs) > 0 && fc.rec {
				fc.a.addUnresolved("unsafe", fc.fn, x.Pos(), objs, x.String())
			}
		}
	case *ssa.MakeClosure:
		for _, b := range x.Bindings {
			fc.addPts(x, fc.reach(b, st))
		}
	case *ssa.MapUpdate:
		fc.toCollection(x, x.Value, st)
	case *ssa.Send:
		fc.toCollection(x, x.X, st)
	case *ssa.Call:
		fc.call(x, &x.Call, st, x)
	case *ssa.Defer:
		fc.deferInstr(x, st)
	case *ssa.Go:
		fc.goInstr(x, st)
	case *ssa.RunDefers:
		fc.runDefers(st, "return", fc.retLineAfter(x), x)
	case *ssa.Return:
		fc.ret(x, st)
	case *ssa.Panic:
		fc.panicExit(x, st, fc.line(x.Pos()), "panic")
	}
}

func (fc *fctx) pass(v, from ssa.Value) {
	fc.addPts(v, fc.pts(from))
	fc.addCells(v, fc.cells(from))
	fc.addTypes(v, fc.typesOf(from))
	if ce := fc.closeErr[from]; ce != nil {
		fc.closeErr[v] = ce
	}
	if m, ok := fc.meaning[from]; ok {
		fc.setMeaning(v, m)
	}
}

func (fc *fctx) toCollection(ins ssa.Instruction, v ssa.Value, st *state) {
	if objs := fc.reach(v, st); len(objs) > 0 {
		fc.markIncomplete(st, "resource stored in a map or sent on a channel")
		fc.escapeSet(objs, "stored in a map or sent on a channel", fc.line(ins.Pos()))
		if fc.rec {
			fc.a.addUnresolved("collection", fc.fn, ins.Pos(), objs, ins.String())
		}
	}
}

func (fc *fctx) binop(x *ssa.BinOp) {
	if x.Op != token.EQL && x.Op != token.NEQ {
		return
	}
	v, other := x.X, x.Y
	if c, ok := v.(*ssa.Const); ok && c.IsNil() {
		v, other = other, v
	}
	c, ok := other.(*ssa.Const)
	if !ok || !c.IsNil() {
		return
	}
	if m, ok := fc.meaning[v]; ok && m.isErr {
		fc.setMeaning(x, meaning{root: m.root, pol: x.Op == token.EQL})
		return
	}
	if objs := fc.pts(v); len(objs) > 0 {
		fc.setMeaning(x, meaning{root: "nilcheck:" + v.Name(), pol: x.Op == token.NEQ, objs: objs})
	}
}

func (fc *fctx) store(x *ssa.Store, st *state) {
	cells := fc.cells(x.Addr)
	objs := fc.pts(x.Val)
	ptrs := fc.cells(x.Val)
	ts := fc.typesOf(x.Val)
	strong := len(cells) == 1 && !fc.weakCell[cells[0]]
	for _, c := range cells {
		if strong {
			st.cells[c] = objs.clone()
			st.cptr[c] = append([]string(nil), ptrs...)
			st.ctypes[c] = typeSet{}
		} else {
			if st.cells[c] == nil {
				st.cells[c] = ObjSet{}
			}
			st.cells[c].addAll(objs)
			st.cptr[c] = unionStrings(st.cptr[c], ptrs)
			if st.ctypes[c] == nil {
				st.ctypes[c] = typeSet{}
			}
		}
		for t := range ts {
			st.ctypes[c][t] = true
		}
		if m, ok := fc.meaning[x.Val]; ok && strong && m.objs == nil {
			st.cmean[c] = fmt.Sprintf("%s|%v|%v", m.root, m.pol, m.isErr)
		} else {
			delete(st.cmean, c)
		}
		line := fc.line(x.Pos())
		switch {
		case strings.HasPrefix(c, "global:"):
			if len(objs) > 0 {
				fc.escapeSet(objs, "stored in global "+strings.TrimPrefix(c, "global:"), line)
				if fc.a.globals[c] == nil {
					fc.a.globals[c] = ObjSet{}
				}
				fc.a.globals[c].addAll(objs)
			}
		case strings.HasPrefix(c, "in:"):
			var k int
			fmt.Sscanf(c, "in:%d*", &k)
			if len(objs) > 0 {
				fc.escapeSet(objs, fmt.Sprintf("stored into memory reachable from input %s", fc.sum.Inputs[k]), line)
			}
			if ce := fc.closeErr[x.Val]; ce != nil && k < len(fc.sum.In) {
				fc.sum.In[k].WritesCloseErr = true
			}
		case fc.collCell[c]:
			if len(objs) > 0 {
				fc.escapeSet(objs, "stored in a slice/array element", line)
				if fc.rec {
					fc.a.addUnresolved("collection", fc.fn, x.Pos(), objs, x.String())
				}
			}
		}
		if len(objs) > 0 || len(ptrs) > 0 {
			fc.note(x, "cell %s := %s", fc.cellName(c), fc.labels(st.cells[c]))
		}
	}
}

func (fc *fctx) cellName(c string) string {
	c = strings.TrimPrefix(c, "cell:")
	if strings.HasPrefix(c, "in:") {
		var k int
		fmt.Sscanf(c, "in:%d*", &k)
		if k < len(fc.sum.Inputs) {
			return "*" + fc.sum.Inputs[k]
		}
	}
	return c
}

func (fc *fctx) load(x *ssa.UnOp, st *state) {
	for _, c := range fc.cells(x.X) {
		if enc, ok := st.cmean[c]; ok {
			parts := strings.Split(enc, "|")
			fc.setMeaning(x, meaning{root: parts[0], pol: parts[1] == "true", isErr: parts[2] == "true"})
		}
		if mayHold(x.Type()) {
			fc.addPts(x, fc.contents(st, c))
		}
		fc.addCells(x, st.cptr[c])
		fc.addTypes(x, st.ctypes[c])
		if objs := fc.contents(st, c); len(objs) > 0 && mayHold(x.Type()) {
			fc.note(x, "%s = load %s -> %s", x.Name(), fc.cellName(c), fc.labels(objs))
		}
	}
}

func (fc *fctx) rootOf(v ssa.Value) string { return "call:" + v.Name() }

func (fc *fctx) extract(x *ssa.Extract, st *state) {
	call, ok := x.Tuple.(*ssa.Call)
	if !ok {
		fc.addPts(x, fc.pts(x.Tuple))
		return
	}
	targets, _, kind := fc.resolveCall(&call.Call)
	if kind == "static" && len(targets) == 1 && classifyAPI(targets[0]) == apiAcquire {
		o := fc.siteObj(call)
		if fc.enum && fc.cur != nil {
			if oi, ok := fc.cur.callObj[call]; ok {
				o = oi
			}
		}
		switch x.Index {
		case 0:
			fc.addPts(x, ObjSet{o: true})
			if fc.rec {
				if r := fc.a.records[o]; r != nil && fc.names[x] != "" {
					r.Var = fc.names[x]
				}
			}
		case 1:
			fc.setMeaning(x, meaning{root: fc.rootOf(call), pol: true, isErr: true})
		}
		return
	}
	if fc.enum && fc.cur != nil {
		if tr, ok := fc.cur.tret[call]; ok {
			if x.Index < len(tr) {
				fc.addPts(x, tr[x.Index])
			}
			return
		}
	}
	in := fc.callIn[call]
	for _, t := range targets {
		s := fc.a.summary(t)
		for _, k := range s.ReturnsInput[x.Index] {
			if k < len(in) {
				fc.addPts(x, in[k])
			}
		}
		if fr := s.Fresh[x.Index]; fr != nil {
			fc.addPts(x, ObjSet{fr.Site: true})
		}
		for _, fr := range s.Fresh {
			if fr.GuardIdx == x.Index {
				fc.setMeaning(x, meaning{root: fc.rootOf(call), pol: true, isErr: fr.GuardErr})
			}
		}
	}
}

func (fc *fctx) siteObj(call *ssa.Call) ObjID {
	f, l := fc.a.pos(call.Pos())
	o := ObjID(fmt.Sprintf("%s:%d", f, l))
	if fc.a.records[o] == nil && fc.rec {
		name := "?"
		for _, r := range *call.Referrers() {
			e, ok := r.(*ssa.Extract)
			if !ok || e.Index != 0 {
				continue
			}
			if fc.names[e] != "" {
				name = fc.names[e]
				continue
			}
			for _, rr := range *e.Referrers() {
				if st, ok := rr.(*ssa.Store); ok {
					if al, ok := st.Addr.(*ssa.Alloc); ok && al.Comment != "" {
						name = al.Comment
					}
				}
			}
		}
		fc.a.records[o] = &Record{ID: string(o), Var: name, Func: fc.a.fnName(fc.fn), File: f, Line: l,
			Acquire: call.Call.String(), InLoop: fc.inLoop[call.Block()], InGoroutine: fc.a.goFns[fc.fn],
			Events: []Event{}, Defers: []*DeferFact{}, Escapes: []string{}, Anomalies: []string{}, Notes: []string{}}
	}
	return o
}

func (fc *fctx) event(o ObjID, line int, kind, via string, prev RState) {
	if !fc.rec {
		return
	}
	if r := fc.a.records[o]; r != nil {
		e := Event{Func: fc.a.fnName(fc.fn), Line: line, Kind: kind, Via: via, State: prev.String()}
		for _, x := range r.Events {
			if x == e {
				return // the same event reached on another path state
			}
		}
		r.Events = append(r.Events, e)
	}
}

func (fc *fctx) anomaly(o ObjID, msg string) {
	if !fc.rec {
		return
	}
	if r := fc.a.records[o]; r != nil {
		r.Anomalies = appendUnique(r.Anomalies, msg)
	}
}

// resolveCall returns the possible callees, the callee inputs (params then free variables)
// and the kind: static | invoke | builtin | dynamic.
func (fc *fctx) resolveCall(c *ssa.CallCommon) ([]*ssa.Function, []ssa.Value, string) {
	if c.IsInvoke() {
		inputs := append([]ssa.Value{c.Value}, c.Args...)
		var ts []types.Type
		for t := range fc.typesOf(c.Value) {
			ts = append(ts, t)
		}
		sort.Slice(ts, func(i, j int) bool { return ts[i].String() < ts[j].String() })
		var fns []*ssa.Function
		for _, t := range ts {
			if f := fc.lookupMethod(t, c.Method); f != nil {
				fns = append(fns, f)
			}
		}
		return fns, inputs, "invoke"
	}
	if mc, ok := c.Value.(*ssa.MakeClosure); ok {
		fn := mc.Fn.(*ssa.Function)
		return []*ssa.Function{fn}, append(append([]ssa.Value(nil), c.Args...), mc.Bindings...), "static"
	}
	if fn := c.StaticCallee(); fn != nil {
		return []*ssa.Function{fn}, c.Args, "static"
	}
	if _, ok := c.Value.(*ssa.Builtin); ok {
		return nil, c.Args, "builtin"
	}
	return nil, c.Args, "dynamic"
}

func (fc *fctx) lookupMethod(t types.Type, m *types.Func) (fn *ssa.Function) {
	defer func() {
		if recover() != nil {
			fn = nil
		}
	}()
	sel := fc.a.prog.MethodSets.MethodSet(t).Lookup(m.Pkg(), m.Name())
	if sel == nil {
		return nil
	}
	return fc.a.prog.MethodValue(sel)
}

func (fc *fctx) call(ins ssa.Instruction, c *ssa.CallCommon, st *state, val ssa.Value) {
	line := fc.line(c.Pos())
	if b, ok := c.Value.(*ssa.Builtin); ok {
		if b.Name() == "recover" {
			fc.sum.Recovers = true
		}
		return
	}
	targets, inputs, kind := fc.resolveCall(c)
	inObjs := make([]ObjSet, len(inputs))
	inCells := make([][]string, len(inputs))
	for i, v := range inputs {
		inObjs[i] = fc.reach(v, st)
		inCells[i] = fc.cells(v)
	}
	if val != nil {
		fc.callIn[val] = inObjs
	}
	all := ObjSet{}
	for _, s := range inObjs {
		all.addAll(s)
	}
	if kind == "dynamic" {
		all.addAll(fc.reach(c.Value, st)) // a closure value may carry captured resources
	}
	switch {
	case kind == "dynamic":
		if len(all) > 0 {
			fc.markIncomplete(st, "call through a function value with a resource")
			fc.escapeSet(all, "passed to a call through a function value", line)
			if fc.rec {
				fc.a.addUnresolved("dynamic-call", fc.fn, c.Pos(), all, c.String())
			}
		}
		return
	case kind == "invoke" && len(targets) == 0:
		if len(all) > 0 {
			fc.markIncomplete(st, "interface call with unknown implementations")
			fc.escapeSet(all, "passed to an interface call with unknown implementations", line)
			if fc.rec {
				fc.a.addUnresolved("dispatch-unknown", fc.fn, c.Pos(), all, c.String())
			}
		}
		return
	}
	if len(targets) == 1 {
		fc.applyTarget(ins, targets[0], inputs, inObjs, inCells, st, line, "", val)
		return
	}
	fc.applyMulti(ins, targets, inputs, inObjs, inCells, st, line, "", val, c.Pos())
}

func (fc *fctx) applyMulti(ins ssa.Instruction, targets []*ssa.Function, inputs []ssa.Value, inObjs []ObjSet, inCells [][]string, st *state, line int, via string, val ssa.Value, p token.Pos) {
	if fc.enum {
		fc.markIncomplete(st, "interface call with several possible implementations on this path")
	}
	var joined *state
	releases := map[bool]bool{}
	var names []string
	for _, t := range targets {
		s := st.clone()
		fc.applyTarget(ins, t, inputs, inObjs, inCells, s, line, via, val)
		joined = fc.join(joined, s)
		sum := fc.a.summary(t)
		rel := classifyAPI(t) == apiRelease || (len(sum.In) > 0 && sum.In[0].Release)
		releases[rel] = true
		names = append(names, fc.a.fnName(t))
	}
	*st = *joined
	if len(releases) > 1 && fc.rec {
		all := ObjSet{}
		for _, s := range inObjs {
			all.addAll(s)
		}
		fc.a.addUnresolved("dispatch-multi", fc.fn, p, all, "possible targets: "+strings.Join(names, ", "))
		for o := range all {
			fc.anomaly(o, fmt.Sprintf("released by some but not all possible implementations at line %d (%s)", line, strings.Join(names, ", ")))
		}
	}
}

// applyTarget applies one callee: either the resource API model or the callee's summary.
func (fc *fctx) applyTarget(ins ssa.Instruction, fn *ssa.Function, inputs []ssa.Value, inObjs []ObjSet, inCells [][]string, st *state, line int, via string, val ssa.Value) {
	name := fc.a.fnName(fn)
	if via == "" {
		via = "call " + name
	}
	switch classifyAPI(fn) {
	case apiAcquire:
		if call, ok := ins.(*ssa.Call); ok {
			o := fc.siteObj(call)
			fc.own[o] = true
			if fc.inLoop[call.Block()] {
				fc.multi[o] = true
			}
			fc.guard[o] = meaning{root: fc.rootOf(call), pol: true}
			if fc.enum {
				oi := o
				if fc.multi[o] {
					st.inst[o]++
					oi = ObjID(fmt.Sprintf("%s#%d", o, st.inst[o]))
					st.curInst[o] = oi
					fc.guard[oi] = meaning{root: fc.rootOf(call), pol: true}
				}
				st.callObj[call] = oi
				st.res[oi] = SOpen | SFailed
				fc.emit(st, TraceEv{Ev: "open", Obj: oi, Via: name})
				return
			}
			prev := st.res[o]
			if fc.multi[o] {
				if prev != 0 {
					st.res[older(o)] |= prev
				}
				st.res[o] = SOpen | SFailed
			} else {
				st.res[o] = SOpen | SFailed
			}
			fc.event(o, line, "acquire", name, prev)
			fc.note(ins, "new resource %s: %s (open iff the call succeeds)", fc.a.label(o), st.res[o])
			if fc.rec && fc.a.goFns[fc.fn] {
				fc.a.addUnresolved("goroutine", fc.fn, call.Pos(), ObjSet{o: true}, "acquired inside a function started with `go`")
			}
		}
		return
	case apiRelease:
		if len(inObjs) > 0 {
			fc.release(ins, inObjs[0], line, via, st)
			if val != nil {
				fc.closeErr[val] = inObjs[0]
			}
		}
		return
	case apiUse:
		if len(inObjs) > 0 {
			fc.use(ins, inObjs[0], line, via, st)
		}
		return
	case apiExit:
		open := ObjSet{}
		for o, s := range st.res {
			if fc.own[o] && s&SOpen != 0 {
				open[o] = true
			}
		}
		if (len(st.pending) > 0 || len(open) > 0) && fc.rec {
			fc.a.addUnresolved("exit-bypass", fc.fn, ins.Pos(), open, name)
		}
		if fc.rec {
			fc.report.Exits = append(fc.report.Exits, Exit{Line: line, Kind: "os.Exit (defers skipped)", Pops: []Pop{}, Open: labelsOf(fc, open)})
		}
		st.live = false
		st.exitKind = "os.Exit"
		return
	}
	sum := fc.a.summary(fn)
	if sum.Unknown {
		all := ObjSet{}
		for _, s := range inObjs {
			all.addAll(s)
		}
		if len(all) > 0 {
			fc.markIncomplete(st, "resource passed to "+name+" ("+sum.Reason+")")
			fc.escapeSet(all, "passed to "+name+" ("+sum.Reason+")", line)
			if fc.rec {
				kind := "unknown-callee"
				if sum.Reason == "recursion" {
					kind = "recursion"
				}
				fc.a.addUnresolved(kind, fc.fn, ins.Pos(), all, name)
			}
		}
		return
	}
	for k, eff := range sum.In {
		if k >= len(inObjs) {
			break
		}
		if eff.Use {
			fc.use(ins, inObjs[k], line, via, st)
		}
		if eff.Release {
			fc.release(ins, inObjs[k], line, via, st)
			if fc.rec {
				for _, df := range sum.deferOf[k] {
					real := ObjSet{}
					for o := range inObjs[k] {
						if _, sym := symIndex(o); !sym {
							real[o] = true
						} else if kk, _ := symIndex(o); kk < len(fc.sum.In) {
							fc.sum.deferOf[kk] = append(fc.sum.deferOf[kk], df)
						}
					}
					if len(real) == 0 {
						continue
					}
					key := fmt.Sprintf("exit of %s, called from %s@%d", name, fc.a.fnName(fc.fn), line)
					df.Resolves[key] = labelsOf(fc, real)
					for o := range real {
						if r := fc.a.records[o]; r != nil {
							found := false
							for _, x := range r.Defers {
								found = found || x == df
							}
							if !found {
								r.Defers = append(r.Defers, df)
							}
						}
					}
				}
			}
		}
		for _, e := range eff.Escapes {
			fc.escapeSet(inObjs[k], e+" (inside "+name+")", line)
		}
		if eff.WritesCloseErr {
			for _, c := range inCells[k] {
				if rn, ok := fc.resultCell[c]; ok && fc.rec {
					fc.closeErrNote(ins, rn, inObjs, sum)
				}
				if strings.HasPrefix(c, "in:") {
					var kk int
					fmt.Sscanf(c, "in:%d*", &kk)
					if kk < len(fc.sum.In) {
						fc.sum.In[kk].WritesCloseErr = true
					}
				}
			}
		}
	}
	for idx, fr := range sum.Fresh {
		o := fr.Site
		fc.own[o] = true
		if fc.inLoop[fc.curBlock] {
			fc.multi[o] = true
		}
		prev := st.res[o]
		st.res[o] = fr.State
		if fr.GuardIdx >= 0 && val != nil {
			fc.guard[o] = meaning{root: fc.rootOf(val), pol: true}
		}
		fc.event(o, line, "acquire", fmt.Sprintf("returned by %s (result %d)", name, idx), prev)
		fc.note(ins, "receives resource %s from %s: %s", fc.a.label(o), name, fr.State)
	}
	if val != nil && fn.Signature.Results().Len() == 1 {
		for _, k := range sum.ReturnsInput[0] {
			if k < len(inObjs) {
				fc.addPts(val, inObjs[k])
			}
		}
		if fr := sum.Fresh[0]; fr != nil {
			fc.addPts(val, ObjSet{fr.Site: true})
		}
		if sum.BoolNilOf >= 0 && sum.BoolNilOf < len(inputs) {
			if m, ok := fc.meaning[inputs[sum.BoolNilOf]]; ok && m.isErr {
				fc.setMeaning(val, meaning{root: m.root, pol: true})
			}
		}
		if sum.CloseErrOf >= 0 && sum.CloseErrOf < len(inObjs) {
			fc.closeErr[val] = inObjs[sum.CloseErrOf]
		}
	}
	if sum.Panics {
		fc.panicExit(ins, st, line, "panic inside "+name)
	}
}

func labelsOf(fc *fctx, s ObjSet) []string {
	r := []string{}
	for _, o := range s.sorted() {
		r = append(r, fc.a.label(ObjID(o)))
	}
	return r
}

func (fc *fctx) closeErrNote(ins ssa.Instruction, result string, inObjs []ObjSet, sum *Summary) {
	objs := ObjSet{}
	for k, eff := range sum.In {
		if eff.Release && k < len(inObjs) {
			objs.addAll(inObjs[k])
		}
	}
	for o := range objs {
		if r := fc.a.records[o]; r != nil {
			r.Notes = appendUnique(r.Notes, fmt.Sprintf("close error is stored into named result %q of %s", result, fc.a.fnName(fc.fn)))
		}
		if fc.curDefer != nil {
			if df := fc.a.dfacts[fc.curDefer]; df != nil {
				df.CloseErrorTo = result
			}
		}
	}
}

func (fc *fctx) release(ins ssa.Instruction, objs ObjSet, line int, via string, st *state) {
	for o := range objs {
		if fc.released != nil {
			fc.released[o] = true
		}
		if k, ok := symIndex(o); ok {
			if k < len(fc.sum.In) {
				fc.sum.In[k].Release = true
			}
			continue
		}
		prev := fc.resOf(st, o)
		if !fc.multi[o] {
			switch {
			case prev == SClosed:
				fc.anomaly(o, fmt.Sprintf("double close: already closed when released at line %d (%s)", line, via))
			case prev&SClosed != 0:
				fc.anomaly(o, fmt.Sprintf("possible double close at line %d (%s): closed on some paths", line, via))
			case prev == SFailed:
				fc.anomaly(o, fmt.Sprintf("Close of a failed acquisition (nil *os.File) at line %d", line))
			}
		}
		fc.event(o, line, "release", via, prev)
		if fc.enum && prev&(SOpen|SOuter) != 0 {
			fc.emit(st, TraceEv{Ev: "close", Obj: o, Via: via})
		}
		loopDefer := fc.curDefer != nil && fc.inLoop[fc.curDefer.Block()] && fc.multi[o]
		if loopDefer {
			// one deferred entry stands for every iteration's registration: all instances close
			st.res[o] = SClosed
			if st.res[older(o)] != 0 {
				st.res[older(o)] = SClosed
			}
		} else if len(objs) == 1 {
			// a direct release closes the current instance (for loop resources: this iteration's)
			st.res[o] = (prev &^ (SOpen | SOuter)) | SClosed
		} else {
			st.res[o] = prev | SClosed
		}
		fc.note(ins, "release %s: %s -> %s (%s)", fc.a.label(o), prev, st.res[o], via)
	}
}

func (fc *fctx) use(ins ssa.Instruction, objs ObjSet, line int, via string, st *state) {
	for o := range objs {
		if k, ok := symIndex(o); ok {
			if k < len(fc.sum.In) {
				fc.sum.In[k].Use = true
			}
			continue
		}
		prev := fc.resOf(st, o)
		if !fc.multi[o] {
			if prev == SClosed {
				fc.anomaly(o, fmt.Sprintf("use after close at line %d (%s)", line, via))
			} else if prev&SClosed != 0 {
				fc.anomaly(o, fmt.Sprintf("possible use after close at line %d (%s)", line, via))
			}
		}
		fc.event(o, line, "use", via, prev)
		fc.note(ins, "use %s (%s)", fc.a.label(o), prev)
	}
}

func (fc *fctx) escapeSet(objs ObjSet, why string, line int) {
	for o := range objs {
		if k, ok := symIndex(o); ok {
			if k < len(fc.sum.In) {
				for _, e := range fc.sum.In[k].Escapes {
					if e == why {
						goto next
					}
				}
				fc.sum.In[k].Escapes = append(fc.sum.In[k].Escapes, why)
			}
			continue
		}
		fc.escaped[o] = true
		if fc.rec {
			if r := fc.a.records[o]; r != nil {
				r.Escapes = appendUnique(r.Escapes, fmt.Sprintf("line %d: %s", line, why))
			}
		}
	next:
	}
}

// deferInstr records the deferred call and what it captures at registration.
func (fc *fctx) deferInstr(d *ssa.Defer, st *state) {
	targets, inputs, kind := fc.resolveCall(&d.Call)
	p := &pend{d: d, targets: targets, kind: kind}
	var desc []string
	for _, v := range inputs {
		c := capture{eager: fc.pts(v).clone(), types: typeSet{}}
		if cs := fc.cells(v); len(cs) > 0 {
			c.cells = append([]string(nil), cs...)
		}
		for t := range fc.typesOf(v) {
			c.types[t] = true
		}
		p.inputs = append(p.inputs, c)
		n := fc.names[v]
		if n == "" {
			n = v.Name()
		}
		switch {
		case len(c.cells) > 0 && fc.mayHoldCells(c.cells, st):
			desc = append(desc, fmt.Sprintf("%s: cell %s, read at exit", n, fc.cellName(c.cells[0])))
		case len(c.eager) > 0:
			desc = append(desc, fmt.Sprintf("%s: value fixed at registration = %s", n, fc.labels(c.eager)))
		}
	}
	replaced := false
	for i, q := range st.pending {
		if q.d == d && !fc.enum {
			st.pending[i] = mergePend(q, p)
			replaced = true
		}
	}
	if !replaced {
		st.pending = append(st.pending, p)
	}
	if fc.rec {
		reg := "always"
		if fc.inLoop[d.Block()] {
			reg = "loop"
		} else if fc.condDefer[d] {
			reg = "conditional"
		}
		var tn []string
		for _, t := range targets {
			tn = append(tn, fc.a.fnName(t))
		}
		df := fc.dfact(d)
		df.Targets, df.Registration = tn, reg
		for _, c := range desc {
			df.Captures = appendUnique(df.Captures, c)
		}
		fc.note(d, "push defer@%d [%s] (%s); pending: %d", df.Line, strings.Join(desc, "; "), reg, len(st.pending))
	}
}

func (fc *fctx) dfact(d *ssa.Defer) *DeferFact {
	df := fc.a.dfacts[d]
	if df == nil {
		_, l := fc.a.pos(d.Pos())
		df = &DeferFact{Func: fc.a.fnName(fc.fn), Line: l, Call: d.Call.String(), Targets: []string{}, Captures: []string{},
			Registration: "always", Resolves: map[string][]string{}}
		fc.a.dfacts[d] = df
	}
	return df
}

func (fc *fctx) mayHoldCells(cs []string, st *state) bool {
	for _, c := range cs {
		if len(fc.contents(st, c)) > 0 || strings.HasPrefix(c, "cell:") || strings.HasPrefix(c, "in:") {
			return true
		}
	}
	return false
}

func (fc *fctx) goInstr(g *ssa.Go, st *state) {
	fc.markIncomplete(st, "goroutine: its file events are not predicted")
	_, inputs, _ := fc.resolveCall(&g.Call)
	all := ObjSet{}
	for _, v := range inputs {
		all.addAll(fc.reach(v, st))
	}
	if len(all) > 0 {
		fc.escapeSet(all, "passed to a goroutine", fc.line(g.Pos()))
		if fc.rec {
			fc.a.addUnresolved("goroutine", fc.fn, g.Pos(), all, g.String())
		}
	}
}

func (fc *fctx) retLineAfter(ins ssa.Instruction) int {
	for _, x := range ins.Block().Instrs {
		if r, ok := x.(*ssa.Return); ok {
			return fc.retLine(r)
		}
	}
	return fc.endLine()
}

func (fc *fctx) retLine(r *ssa.Return) int {
	if r.Pos().IsValid() {
		return fc.line(r.Pos())
	}
	return fc.endLine()
}

func (fc *fctx) endLine() int {
	if s := fc.fn.Syntax(); s != nil {
		return fc.line(s.End())
	}
	return 0
}

// runDefers pops the defer stack in reverse registration order, resolving each entry's
// captured cells against the state at this exit.
func (fc *fctx) runDefers(st *state, kind string, line int, at ssa.Instruction) {
	exit := &Exit{Line: line, Kind: kind, Pops: []Pop{}}
	label := fmt.Sprintf("%s@%d", kind, line)
	for i := len(st.pending) - 1; i >= 0; i-- {
		p := st.pending[i]
		acts := fc.applyPend(p, st, label, at)
		_, dl := fc.a.pos(p.d.Pos())
		exit.Pops = append(exit.Pops, Pop{DeferLine: dl, Acts: labelsOf(fc, acts)})
		fc.note(at, "pop defer@%d -> acts on %s", dl, fc.labels(acts))
	}
	st.pending = nil
	fc.curExit = exit
}

func (fc *fctx) applyPend(p *pend, st *state, label string, at ssa.Instruction) ObjSet {
	inObjs := make([]ObjSet, len(p.inputs))
	inCells := make([][]string, len(p.inputs))
	for i, c := range p.inputs {
		inObjs[i] = c.eager.clone()
		for _, cell := range c.cells {
			inObjs[i].addAll(fc.contents(st, cell))
		}
		inCells[i] = c.cells
	}
	_, dl := fc.a.pos(p.d.Pos())
	via := fmt.Sprintf("defer@%d at %s", dl, label)
	saved := fc.released
	fc.released = ObjSet{}
	targets := p.targets
	if p.kind == "invoke" && len(p.inputs) > 0 {
		targets = nil
		var ts []types.Type
		for t := range p.inputs[0].types {
			ts = append(ts, t)
		}
		sort.Slice(ts, func(i, j int) bool { return ts[i].String() < ts[j].String() })
		for _, t := range ts {
			if f := fc.lookupMethod(t, p.d.Call.Method); f != nil {
				targets = append(targets, f)
			}
		}
	}
	fc.curDefer = p.d
	defer func() { fc.curDefer = nil }()
	switch {
	case len(targets) == 1:
		fc.applyTarget(at, targets[0], nil, inObjs, inCells, st, dl, via, nil)
	case len(targets) > 1:
		fc.applyMulti(at, targets, nil, inObjs, inCells, st, dl, via, nil, p.d.Pos())
	default:
		all := ObjSet{}
		for _, s := range inObjs {
			all.addAll(s)
		}
		if len(all) > 0 {
			fc.escapeSet(all, "deferred call with unknown target", dl)
			if fc.rec {
				kind := "dynamic-call"
				if p.kind == "invoke" {
					kind = "dispatch-unknown"
				}
				fc.a.addUnresolved(kind, fc.fn, p.d.Pos(), all, p.d.Call.String())
			}
		}
	}
	acts := fc.released
	fc.released = saved
	if saved != nil {
		saved.addAll(acts)
	}
	if fc.rec {
		df := fc.dfact(p.d)
		for o := range acts {
			if k, sym := symIndex(o); sym {
				found := false
				for _, x := range fc.sum.deferOf[k] {
					found = found || x == df
				}
				if !found {
					fc.sum.deferOf[k] = append(fc.sum.deferOf[k], df)
				}
			}
		}
		{
			df.Resolves[label] = labelsOf(fc, acts)
			for o := range acts {
				if _, sym := symIndex(o); !sym {
					if r := fc.a.records[o]; r != nil {
						found := false
						for _, x := range r.Defers {
							found = found || x == df
						}
						if !found {
							r.Defers = append(r.Defers, df)
						}
					}
				}
			}
		}
	}
	return acts
}

func (fc *fctx) panicExit(ins ssa.Instruction, st *state, line int, why string) {
	fc.panicSeen = true
	recovers := false
	for _, p := range st.pending {
		for _, t := range p.targets {
			if fc.a.summary(t).Recovers {
				recovers = true
			}
		}
	}
	kind := "panic"
	if recovers {
		kind = "panic->recovered"
	}
	fc.runDefers(st, kind, line, ins)
	exit := fc.curExit
	exit.Kind = kind + " (" + why + ")"
	fc.curExit = nil
	if fc.rec {
		exit.Open = fc.openOwned(st, nil)
		fc.report.Exits = append(fc.report.Exits, *exit)
	}
	if recovers && fc.fn.Recover != nil {
		s := st.clone()
		s.live = true
		s.pending = nil
		if fc.enum {
			fc.recoverTo = s
		} else if fc.addState(fc.fn.Recover, s) {
			fc.changed = true
		}
	}
	st.live = false
	st.exitKind = "panic"
}

func older(o ObjID) ObjID { return o + "#older" }

func base(o ObjID) (ObjID, bool) {
	if strings.HasSuffix(string(o), "#older") {
		return ObjID(strings.TrimSuffix(string(o), "#older")), true
	}
	return o, false
}

func (fc *fctx) openOwned(st *state, returned ObjSet) []string {
	var open []string
	for k, s := range st.res {
		o, old := base(k)
		if fc.own[o] && s&SOpen != 0 && !fc.escaped[o] && !returned[o] {
			l := fc.a.label(o)
			if old {
				l += " (earlier iterations)"
			}
			open = append(open, l)
		}
	}
	sort.Strings(open)
	return open
}

func (fc *fctx) ret(r *ssa.Return, st *state) {
	fc.returnSeen = true
	line := fc.retLine(r)
	obs := retObs{}
	returned := ObjSet{}
	for idx, v := range r.Results {
		objs := fc.reach(v, st)
		if mc, ok := v.(*ssa.MakeClosure); ok && len(objs) > 0 && fc.rec {
			fc.a.addUnresolved("closure-escape", fc.fn, r.Pos(), objs, mc.String())
		}
		obs.objs = append(obs.objs, objs)
		states := map[ObjID]RState{}
		for o := range objs {
			states[o] = st.res[o]
			if k, ok := symIndex(o); ok {
				fc.sum.ReturnsInput[idx] = appendInt(fc.sum.ReturnsInput[idx], k)
			} else {
				returned[o] = true
				if fc.rec {
					if rec := fc.a.records[o]; rec != nil {
						rec.Escapes = appendUnique(rec.Escapes, fmt.Sprintf("line %d: returned to the caller of %s (ownership transfer)", line, fc.a.fnName(fc.fn)))
					}
				}
			}
		}
		obs.states = append(obs.states, states)
		obs.consts = append(obs.consts, v)
	}
	if !fc.rec {
		return
	}
	fc.retObs = append(fc.retObs, obs)
	exit := fc.curExit
	if exit == nil {
		exit = &Exit{Line: line, Kind: "return", Pops: []Pop{}}
	}
	if r.Block() == fc.fn.Recover {
		exit.Kind = "return after recover"
	}
	exit.Line = line
	exit.Open = fc.openOwned(st, returned)
	for _, o := range exit.Open {
		_ = o
	}
	for k, s := range st.res {
		o, old := base(k)
		if fc.own[o] && s&SOpen != 0 && !fc.escaped[o] && !returned[o] {
			which := ""
			if old {
				which = " (instances from earlier loop iterations)"
			}
			fc.anomaly(o, fmt.Sprintf("may still be open when %s returns at line %d%s (state %s)", fc.a.fnName(fc.fn), line, which, s))
		}
	}
	fc.report.Exits = append(fc.report.Exits, *exit)
	fc.curExit = nil
}

func appendInt(xs []int, x int) []int {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}

// finish derives the parts of the summary that need all return observations.
func (fc *fctx) finish() {
	s := fc.sum
	s.Panics = fc.panicSeen && !fc.returnSeen
	nres := fc.fn.Signature.Results().Len()
	for idx := 0; idx < nres; idx++ {
		var site ObjID
		ok := len(fc.retObs) > 0
		var st RState
		for _, ob := range fc.retObs {
			for o := range ob.objs[idx] {
				if _, sym := symIndex(o); sym || !fc.own[o] {
					continue
				}
				if site != "" && site != o {
					ok = false
				}
				site = o
				st |= ob.states[idx][o]
			}
			if len(ob.objs[idx]) == 0 {
				st |= SFailed
			}
		}
		if !ok || site == "" {
			continue
		}
		fr := &freshRes{Site: site, GuardIdx: -1, State: st}
		for j := 0; j < nres; j++ {
			if j == idx {
				continue
			}
			rt := fc.fn.Signature.Results().At(j).Type()
			good := len(fc.retObs) > 0
			for _, ob := range fc.retObs {
				openHere := ob.objs[idx][site] && ob.states[idx][site]&SOpen != 0 && ob.states[idx][site]&SFailed == 0
				c, isConst := ob.consts[j].(*ssa.Const)
				switch {
				case isBool(rt):
					good = good && isConst && c.Value != nil && constant.BoolVal(c.Value) == openHere
				case isErrorType(rt):
					good = good && (isConst && c.IsNil()) == openHere
				default:
					good = false
				}
			}
			if good {
				fr.GuardIdx, fr.GuardErr = j, isErrorType(rt)
				break
			}
		}
		fr.StateStr = fr.State.String()
		s.Fresh[idx] = fr
	}
	if nres == 1 && len(fc.retObs) > 0 {
		k := -2
		for _, ob := range fc.retObs {
			m, ok := fc.meaning[ob.consts[0]]
			kk := -1
			if ok && !m.isErr && m.pol {
				fmt.Sscanf(m.root, "in:%d", &kk)
				if !strings.HasPrefix(m.root, "in:") {
					kk = -1
				}
			}
			if k == -2 {
				k = kk
			} else if k != kk {
				k = -1
			}
		}
		if k >= 0 {
			s.BoolNilOf = k
		}
		k = -2
		for _, ob := range fc.retObs {
			ce := fc.closeErr[ob.consts[0]]
			kk := -1
			if len(ce) == 1 {
				for o := range ce {
					if x, ok := symIndex(o); ok {
						kk = x
					}
				}
			}
			if k == -2 {
				k = kk
			} else if k != kk {
				k = -1
			}
		}
		if k >= 0 {
			s.CloseErrOf = k
		}
	}
	if fc.a.explain {
		for _, b := range fc.fn.Blocks {
			hdr := fmt.Sprintf("%d: %s", b.Index, b.Comment)
			if fc.inLoop[b] {
				hdr += "  [loop]"
			}
			if len(fc.entry[b]) == 0 {
				hdr += "  [unreachable]"
			} else if len(fc.entry[b]) > 1 {
				hdr += fmt.Sprintf("  [%d path states]", len(fc.entry[b]))
			}
			fc.report.SSA = append(fc.report.SSA, hdr)
			for _, ins := range b.Instrs {
				if _, ok := ins.(*ssa.DebugRef); ok {
					continue
				}
				txt := ins.String()
				if v, ok := ins.(ssa.Value); ok && v.Name() != "" {
					txt = v.Name() + " = " + txt
				}
				line := "\t" + txt
				if n := fc.notes[ins]; len(n) > 0 {
					line += "\n\t\t  ⟹ " + strings.Join(n, "\n\t\t  ⟹ ")
				}
				fc.report.SSA = append(fc.report.SSA, line)
			}
		}
	}
}
