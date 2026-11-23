package analysis

import (
	"fmt"
	"go/token"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// Run loads the packages matching patterns (step 1: parse, type-check, build SSA) and runs
// the memory-aware resource analysis over every function in them.
func Run(dir string, patterns []string, explainSSA, traces bool) *Report {
	rep := &Report{Schema: "rescflow-analysis/v1", Go: runtime.Version(), Patterns: patterns,
		Packages: []PackageReport{}, Records: []*Record{}, Functions: []*FuncReport{}, Defers: []*DeferFact{}, Unresolved: []Unresolved{}}
	root := dir
	if root == "" {
		root, _ = os.Getwd()
	}
	cfg := &packages.Config{Mode: packages.LoadSyntax, Dir: dir}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		u := explain("load-list")
		u.Detail = err.Error()
		tailorVersion(&u, err.Error())
		rep.Unresolved = append(rep.Unresolved, u)
		return rep
	}
	var good []*packages.Package
	for _, p := range pkgs {
		pr := PackageReport{Path: p.PkgPath}
		// one explanation per kind; parse/type errors make the compiler's own ListError redundant
		byKind := map[string][]packages.Error{}
		for _, e := range p.Errors {
			kind := map[packages.ErrorKind]string{packages.ListError: "load-list", packages.ParseError: "load-parse",
				packages.TypeError: "load-type"}[e.Kind]
			if kind == "" {
				kind = "load-list"
			}
			byKind[kind] = append(byKind[kind], e)
		}
		if len(byKind["load-parse"])+len(byKind["load-type"]) > 0 {
			delete(byKind, "load-list")
		}
		for _, kind := range []string{"load-list", "load-parse", "load-type"} {
			errs := byKind[kind]
			if len(errs) == 0 {
				continue
			}
			u := explain(kind)
			u.File, u.Line = splitPos(errs[0].Pos)
			var msgs []string
			for i, e := range errs {
				if i == 3 {
					msgs = append(msgs, fmt.Sprintf("... and %d more", len(errs)-3))
					break
				}
				msgs = append(msgs, e.Pos+": "+e.Msg)
			}
			u.Detail = strings.Join(msgs, "; ")
			tailorVersion(&u, u.Detail)
			pr.LoadIssue = append(pr.LoadIssue, u)
		}
		if len(p.Errors) == 0 && p.Types != nil {
			good = append(good, p)
		}
		rep.Packages = append(rep.Packages, pr)
	}
	if len(good) == 0 {
		return rep
	}
	prog, ssaPkgs := ssautil.Packages(good, ssa.InstantiateGenerics|ssa.GlobalDebug)
	built := map[*ssa.Package]bool{}
	for i, sp := range ssaPkgs {
		if sp == nil {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					u := explain("ssa-build")
					u.Detail = fmt.Sprint(r)
					for j := range rep.Packages {
						if rep.Packages[j].Path == good[i].PkgPath {
							rep.Packages[j].LoadIssue = append(rep.Packages[j].LoadIssue, u)
						}
					}
				}
			}()
			sp.Build()
			built[sp] = true
		}()
	}
	for j := range rep.Packages {
		for sp := range built {
			if sp.Pkg.Path() == rep.Packages[j].Path {
				rep.Packages[j].Analyzed = true
			}
		}
	}
	a := &Analyzer{prog: prog, fset: prog.Fset, root: root, explain: explainSSA,
		local: map[*ssa.Function]bool{}, sums: map[*ssa.Function]*Summary{}, busy: map[*ssa.Function]bool{},
		records: map[ObjID]*Record{}, globals: map[string]ObjSet{}, goFns: map[*ssa.Function]bool{},
		unresSeen: map[string]bool{}, funcs: map[*ssa.Function]*FuncReport{}, dfacts: map[*ssa.Defer]*DeferFact{},
		fcs: map[*ssa.Function]*fctx{}}
	var fns []*ssa.Function
	for fn := range ssautil.AllFunctions(prog) {
		if fn.Pkg != nil && built[fn.Pkg] && len(fn.Blocks) > 0 && fn.Synthetic == "" {
			a.local[fn] = true
			fns = append(fns, fn)
		}
	}
	sort.Slice(fns, func(i, j int) bool {
		pi, pj := prog.Fset.Position(fns[i].Pos()), prog.Fset.Position(fns[j].Pos())
		if pi.Filename != pj.Filename {
			return pi.Filename < pj.Filename
		}
		return pi.Offset < pj.Offset
	})
	// goroutine bodies are marked first so their resources are flagged as concurrent
	for _, fn := range fns {
		for _, b := range fn.Blocks {
			for _, ins := range b.Instrs {
				if g, ok := ins.(*ssa.Go); ok {
					if mc, ok := g.Call.Value.(*ssa.MakeClosure); ok {
						a.goFns[mc.Fn.(*ssa.Function)] = true
					} else if f := g.Call.StaticCallee(); f != nil {
						a.goFns[f] = true
					}
				}
			}
		}
	}
	// Functions that store into globals are analyzed before readers: run twice so that
	// readers analyzed earlier see the global contents (summaries are recomputed).
	for pass := 0; pass < 2; pass++ {
		if pass == 1 {
			if len(a.globals) == 0 {
				break
			}
			a.sums = map[*ssa.Function]*Summary{}
			a.records = map[ObjID]*Record{}
			a.unres, a.unresSeen = nil, map[string]bool{}
			a.funcs = map[*ssa.Function]*FuncReport{}
			a.dfacts = map[*ssa.Defer]*DeferFact{}
		}
		for _, fn := range fns {
			a.summary(fn)
		}
	}
	relabel := func(xs []string) {
		for i, x := range xs {
			xs[i] = a.label(ObjID(x))
		}
	}
	for _, fn := range fns {
		if fr := a.funcs[fn]; fr != nil {
			for i := range fr.Exits {
				for j := range fr.Exits[i].Pops {
					relabel(fr.Exits[i].Pops[j].Acts)
				}
			}
			rep.Functions = append(rep.Functions, fr)
		}
	}
	for _, df := range a.dfacts {
		for _, v := range df.Resolves {
			relabel(v)
		}
	}
	for i := range a.unres {
		relabel(a.unres[i].Resources)
	}
	rep.Defers = []*DeferFact{}
	for _, df := range a.dfacts {
		rep.Defers = append(rep.Defers, df)
	}
	sort.Slice(rep.Defers, func(i, j int) bool {
		if rep.Defers[i].Func != rep.Defers[j].Func {
			return rep.Defers[i].Func < rep.Defers[j].Func
		}
		return rep.Defers[i].Line < rep.Defers[j].Line
	})
	for _, r := range a.records {
		rep.Records = append(rep.Records, r)
	}
	sort.Slice(rep.Records, func(i, j int) bool {
		if rep.Records[i].File != rep.Records[j].File {
			return rep.Records[i].File < rep.Records[j].File
		}
		return rep.Records[i].Line < rep.Records[j].Line
	})
	rep.Unresolved = append(rep.Unresolved, a.unres...)
	Classify(rep)
	if traces {
		for _, fn := range fns {
			if fn.Name() == "main" && fn.Pkg.Pkg.Name() == "main" && fn.Signature.Recv() == nil && fn.Parent() == nil {
				rep.Traces = append(rep.Traces, a.ExploreMain(fn))
			}
		}
	}
	return rep
}

func tailorVersion(u *Unresolved, msg string) {
	if strings.Contains(msg, "requires go") || strings.Contains(msg, "requires newer Go") {
		u.Construct = "module requires a newer Go version"
		u.Why = "the go directive (or a //go:build version) asks for a Go release newer than the analyzer's toolchain (" + runtime.Version() + "); the go command and go/types refuse code whose language version they do not know"
		u.HumanInput = "analyze with a matching Go toolchain and golang.org/x/tools release, or lower the go directive if the code does not use newer language features"
	}
}

func splitPos(p string) (string, int) {
	parts := strings.Split(p, ":")
	if len(parts) >= 2 {
		if n, err := strconv.Atoi(parts[1]); err == nil {
			return parts[0], n
		}
	}
	return p, 0
}

var _ = token.NoPos
