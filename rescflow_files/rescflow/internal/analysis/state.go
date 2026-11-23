package analysis

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// ObjID names an abstract resource. Real resources are named by their acquisition site
// ("path/main.go:8"); "in:K" is the symbolic resource reachable from input K of the
// function being summarized (parameters first, then closure free variables).
type ObjID string

func symObj(k int) ObjID { return ObjID(fmt.Sprintf("in:%d", k)) }

func symIndex(o ObjID) (int, bool) {
	var k int
	if _, err := fmt.Sscanf(string(o), "in:%d", &k); err == nil && strings.HasPrefix(string(o), "in:") {
		return k, true
	}
	return 0, false
}

type ObjSet map[ObjID]bool

func (s ObjSet) clone() ObjSet {
	c := ObjSet{}
	for k := range s {
		c[k] = true
	}
	return c
}

func (s ObjSet) addAll(o ObjSet) bool {
	ch := false
	for k := range o {
		if !s[k] {
			s[k] = true
			ch = true
		}
	}
	return ch
}

func (s ObjSet) sorted() []string {
	r := make([]string, 0, len(s))
	for k := range s {
		r = append(r, string(k))
	}
	sort.Strings(r)
	return r
}

// RState is a may-set of resource states.
type RState uint8

const (
	SOpen   RState = 1 << iota // acquired and not yet released
	SClosed                    // released
	SFailed                    // acquisition failed: the handle is nil
	SOuter                     // live in a caller / another function; state unknown here
)

func (s RState) String() string {
	if s == 0 {
		return "unacquired"
	}
	var p []string
	if s&SOpen != 0 {
		p = append(p, "Open")
	}
	if s&SClosed != 0 {
		p = append(p, "Closed")
	}
	if s&SFailed != 0 {
		p = append(p, "Failed")
	}
	if s&SOuter != 0 {
		p = append(p, "Outer")
	}
	if len(p) == 2 && s&SOpen != 0 && s&SFailed != 0 {
		return "MaybeOpen"
	}
	return strings.Join(p, "|")
}

type typeSet map[types.Type]bool

// capture is one input of a deferred call as recorded at registration time.
// Go evaluates the deferred call's function value and arguments when the defer
// statement executes: a plain value is fixed then (eager); a pointer - including a
// closure's captured variable cell - is fixed, but what it points to is read at exit.
type capture struct {
	eager ObjSet
	cells []string
	types typeSet
}

type pend struct {
	d       *ssa.Defer
	inputs  []capture
	targets []*ssa.Function // resolved at registration (static / closure); invoke re-resolves from captured types
	kind    string          // static | invoke | builtin | dynamic
}

type state struct {
	live    bool
	cells   map[string]ObjSet   // memory cell -> resources it may hold
	cptr    map[string][]string // memory cell -> cells that a stored pointer refers to
	ctypes  map[string]typeSet  // memory cell -> concrete types stored (interfaces)
	cmean   map[string]string   // memory cell -> success meaning of the stored error/bool
	facts   map[string]bool     // branch conditions already decided on this path
	res     map[ObjID]RState    // resource -> may-states
	pending []*pend             // defer stack, registration order
}

func newState() *state {
	return &state{live: true, cells: map[string]ObjSet{}, cptr: map[string][]string{}, ctypes: map[string]typeSet{}, cmean: map[string]string{}, facts: map[string]bool{}, res: map[ObjID]RState{}}
}

func (s *state) clone() *state {
	c := &state{live: s.live, cells: map[string]ObjSet{}, cptr: map[string][]string{}, ctypes: map[string]typeSet{}, cmean: map[string]string{}, res: map[ObjID]RState{}}
	for k, v := range s.cmean {
		c.cmean[k] = v
	}
	c.facts = map[string]bool{}
	for k, v := range s.facts {
		c.facts[k] = v
	}
	for k, v := range s.cells {
		c.cells[k] = v.clone()
	}
	for k, v := range s.cptr {
		c.cptr[k] = append([]string(nil), v...)
	}
	for k, v := range s.ctypes {
		t := typeSet{}
		for x := range v {
			t[x] = true
		}
		c.ctypes[k] = t
	}
	for k, v := range s.res {
		c.res[k] = v
	}
	c.pending = append([]*pend(nil), s.pending...)
	return c
}

// key is a canonical string used to detect fixpoint.
func (s *state) key() string {
	if s == nil || !s.live {
		return "dead"
	}
	var b strings.Builder
	ks := make([]string, 0, len(s.cells))
	for k := range s.cells {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Fprintf(&b, "c%s=%v;", k, s.cells[k].sorted())
	}
	ks = ks[:0]
	for k := range s.cptr {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Fprintf(&b, "p%s=%v;", k, s.cptr[k])
	}
	ks = ks[:0]
	for k := range s.ctypes {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		var ts []string
		for t := range s.ctypes[k] {
			ts = append(ts, t.String())
		}
		sort.Strings(ts)
		fmt.Fprintf(&b, "t%s=%v;", k, ts)
	}
	ks = ks[:0]
	for k := range s.cmean {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Fprintf(&b, "m%s=%s;", k, s.cmean[k])
	}
	ks = ks[:0]
	for k := range s.facts {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Fprintf(&b, "F%s=%v;", k, s.facts[k])
	}
	rs := make([]string, 0, len(s.res))
	for k, v := range s.res {
		rs = append(rs, fmt.Sprintf("%s:%d", k, v))
	}
	sort.Strings(rs)
	b.WriteString(strings.Join(rs, ","))
	for _, p := range s.pending {
		fmt.Fprintf(&b, "|d%p", p.d)
		for _, c := range p.inputs {
			fmt.Fprintf(&b, "(%v %v %d)", c.eager.sorted(), c.cells, len(c.types))
		}
	}
	return b.String()
}

func unionStrings(a, b []string) []string {
	m := map[string]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		m[x] = true
	}
	r := make([]string, 0, len(m))
	for x := range m {
		r = append(r, x)
	}
	sort.Strings(r)
	return r
}

func mergePend(a, b *pend) *pend {
	if a == b {
		return a
	}
	m := &pend{d: a.d, kind: a.kind, targets: append([]*ssa.Function(nil), a.targets...)}
	for _, t := range b.targets {
		found := false
		for _, x := range m.targets {
			found = found || x == t
		}
		if !found {
			m.targets = append(m.targets, t)
		}
	}
	for i := range a.inputs {
		c := capture{eager: a.inputs[i].eager.clone(), cells: unionStrings(a.inputs[i].cells, b.inputs[i].cells), types: typeSet{}}
		c.eager.addAll(b.inputs[i].eager)
		for t := range a.inputs[i].types {
			c.types[t] = true
		}
		for t := range b.inputs[i].types {
			c.types[t] = true
		}
		m.inputs = append(m.inputs, c)
	}
	return m
}
