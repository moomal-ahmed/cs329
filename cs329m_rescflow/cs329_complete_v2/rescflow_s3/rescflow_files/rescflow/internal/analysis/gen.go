package analysis

// Step 5: deterministic Rust generation (option 2 of the design).
//
// Programs whose step-3 category is A or B are translated to Rust that links against
// rescflow_rt; category C programs get their step-6 review report instead. Nothing here is
// learned or sampled: the same input always gives the same Rust.
//
//   - Resource cleanup comes from the defer's facts (registration always / conditional / loop,
//     arguments fixed at registration vs closure reading variables at exit, recover), the same
//     facts step 3 decides from: DeferOwn (OWNED_GUARD), Option<Defer> (OPTION_GUARD),
//     DeferStack (DEFER_STACK), Defer over RefCell cells (CELL_CLOSURE_GUARD / RESULT_SLOT_GUARD),
//     recover() (CATCH_UNWIND), ownership moves for returns, globals and out-parameters.
//   - Go's nil-able *os.File is Option<File>; error is rescflow_rt::go::Error. Close/Stat on nil
//     fail as in Go. Resource *objects* are tracked to where they currently live (variable,
//     guard, struct field, cell, global, defer stack), so aliases and moves resolve statically.
//   - The non-resource code (strings, ints, if/for, helpers, structs, globals, fmt.Println,
//     os.Args, panic) goes through a small direct translation.
//
// Anything outside the supported subset is reported as unsupported, never guessed.

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// GenOutput reports what the generator did for one program.
type GenOutput struct {
	Package     string   `json:"package"`
	Category    string   `json:"category"`
	Status      string   `json:"status"` // generated | review | unsupported
	Dir         string   `json:"dir,omitempty"`
	Forms       []string `json:"forms,omitempty"`
	Unsupported []string `json:"unsupported,omitempty"`
}

// place: where a resource currently lives, and how to read it (ref) or move it out (take).
type place struct {
	kind  string // opt | refparam | cell | cellref | guard | stack | field | cellfield | global
	name  string
	field string
	slot  string
}

func (p place) ref() string {
	switch p.kind {
	case "opt":
		return p.name + ".as_ref()"
	case "refparam":
		return p.name
	case "cell", "cellref":
		return p.name + ".borrow().as_ref()"
	case "guard":
		return "(*" + p.name + ").as_ref()"
	case "stack":
		return p.name + ".file(" + p.slot + ")"
	case "field":
		return p.name + "." + p.field + ".as_ref()"
	case "cellfield":
		return p.name + ".borrow()." + p.field + ".as_ref()"
	case "global":
		return p.name + ".lock().unwrap().as_ref()"
	}
	return "/*?*/None"
}

func (p place) take() (string, bool) {
	switch p.kind {
	case "opt":
		return p.name + ".take()", true
	case "cell", "cellref":
		return p.name + ".borrow_mut().take()", true
	case "field":
		return p.name + "." + p.field + ".take()", true
	case "cellfield":
		return p.name + ".borrow_mut()." + p.field + ".take()", true
	case "global":
		return p.name + ".lock().unwrap().take()", true
	}
	return "", false
}

type varInfo struct {
	rust                     string
	res, cell                bool
	captured, mutated, addr  bool
	condDefer, deferRecv     bool
	alias                    *types.Var
	isFuncLit, isStructValue bool
}

type gen struct {
	a     *Analyzer
	pkg   *packages.Package
	info  *types.Info
	errs  []string
	forms []string
	// per function
	fd        *ast.FuncDecl
	sum       *Summary
	recv      *types.Var
	vars      map[*types.Var]*varInfo
	varObj    map[*types.Var]string
	places    map[string]place
	fieldObj  map[string]string
	tmp       int
	guardN    int
	hoist     *[]string
	stackName string
	namedRes  []*types.Var
	loops     [][2]string
	inLoop    int
	inIf      int
	nested    int
	recovered string
}

func (g *gen) fail(n ast.Node, f string, a ...any) {
	_, line := g.a.pos(n.Pos())
	g.errs = appendUnique(g.errs, fmt.Sprintf("line %d: %s", line, fmt.Sprintf(f, a...)))
}

func (g *gen) newTmp(prefix string) string { g.tmp++; return fmt.Sprintf("__%s%d", prefix, g.tmp) }

var rustKeywords = map[string]bool{"as": true, "break": true, "const": true, "continue": true, "crate": true,
	"else": true, "enum": true, "extern": true, "false": true, "fn": true, "for": true, "if": true, "impl": true,
	"in": true, "let": true, "loop": true, "match": true, "mod": true, "move": true, "mut": true, "pub": true,
	"ref": true, "return": true, "self": true, "Self": true, "static": true, "struct": true, "super": true,
	"trait": true, "true": true, "type": true, "unsafe": true, "use": true, "where": true, "while": true,
	"async": true, "await": true, "dyn": true, "abstract": true, "become": true, "box": true, "do": true,
	"final": true, "macro": true, "override": true, "priv": true, "typeof": true, "unsized": true,
	"virtual": true, "yield": true, "try": true, "gen": true}

func local(name string) string {
	if rustKeywords[name] {
		return name + "_"
	}
	return name
}

func snake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return local(b.String())
}

func camel(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func isFilePtrPtr(t types.Type) bool {
	p, ok := types.Unalias(t).(*types.Pointer)
	return ok && isOSFile(p.Elem())
}

func (g *gen) structName(t types.Type) (string, bool) {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	n, ok := t.(*types.Named)
	if !ok {
		return "", false
	}
	if _, ok := n.Underlying().(*types.Struct); !ok || n.Obj().Pkg() != g.pkg.Types {
		return "", false
	}
	return camel(n.Obj().Name()), true
}

// rtype maps a Go type to Rust.
func (g *gen) rtype(t types.Type, n ast.Node) string {
	t = types.Unalias(t)
	if isOSFile(t) {
		return "Option<File>"
	}
	if isErrorType(t) {
		return "go::Error"
	}
	if s, ok := g.structName(t); ok {
		return s
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.String:
			return "String"
		case types.Int, types.Int64, types.UntypedInt:
			return "i64"
		case types.Bool, types.UntypedBool:
			return "bool"
		}
	case *types.Slice:
		return "Vec<" + g.rtype(u.Elem(), n) + ">"
	}
	g.fail(n, "type %s is outside the generator's subset", t)
	return "()"
}

func (g *gen) zero(t types.Type, n ast.Node) string {
	switch g.rtype(t, n) {
	case "String":
		return "String::new()"
	case "i64":
		return "0"
	case "bool":
		return "false"
	case "Option<File>", "go::Error":
		return "None"
	}
	if strings.HasPrefix(g.rtype(t, n), "Vec<") {
		return "Vec::new()"
	}
	g.fail(n, "no zero value for %s", t)
	return "Default::default()"
}

// ------------------------------------------------------------------ driver

func (a *Analyzer) generate(pkgs []*packages.Package, rep *Report, outDir, rtPath string) []*GenOutput {
	var outs []*GenOutput
	cat := map[string]string{}
	for _, pd := range rep.ProgramDecisions {
		cat[pd.Package] = pd.Category
	}
	for _, p := range pkgs {
		name := p.PkgPath[strings.LastIndex(p.PkgPath, "/")+1:]
		o := &GenOutput{Package: name, Category: cat[name]}
		outs = append(outs, o)
		dir := filepath.Join(outDir, name)
		os.MkdirAll(dir, 0o755)
		if o.Category == "" {
			o.Category = "A" // no resource at all
		}
		if o.Category == "C" {
			o.Status = "review"
			var sub Report
			for _, r := range rep.Reviews {
				if r.Program == name {
					sub.Reviews = append(sub.Reviews, r)
				}
			}
			os.WriteFile(filepath.Join(dir, "REVIEW.md"), []byte(sub.ReviewMarkdown()), 0o644)
			o.Dir = dir
			continue
		}
		g := &gen{a: a, pkg: p, info: p.TypesInfo}
		src := g.file()
		if len(g.errs) > 0 {
			o.Status, o.Unsupported = "unsupported", g.errs
			os.WriteFile(filepath.Join(dir, "UNSUPPORTED.txt"), []byte(strings.Join(g.errs, "\n")+"\n"), 0o644)
			continue
		}
		o.Status, o.Dir, o.Forms = "generated", dir, g.forms
		os.MkdirAll(filepath.Join(dir, "src"), 0o755)
		rtRel := rtPath
		if rel, err := filepath.Rel(dir, rtPath); err == nil {
			rtRel = filepath.ToSlash(rel) // portable: the repo can move
		}
		cargo := fmt.Sprintf("[package]\nname = %q\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\nrescflow_rt = { path = %q }\n\n[[bin]]\nname = \"prog\"\npath = \"src/main.rs\"\n",
			"gen_"+strings.ToLower(strings.ReplaceAll(name, "-", "_")), rtRel)
		os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargo), 0o644)
		mainRs := filepath.Join(dir, "src", "main.rs")
		os.WriteFile(mainRs, []byte(src), 0o644)
		exec.Command("rustfmt", "--edition", "2021", mainRs).Run()
	}
	return outs
}

func (g *gen) file() string {
	var b strings.Builder
	name := g.pkg.PkgPath[strings.LastIndex(g.pkg.PkgPath, "/")+1:]
	fmt.Fprintf(&b, "// Generated by RescFlow (step 5, deterministic) from %s. Do not edit.\n", name)
	b.WriteString("#![allow(unused_mut, unused_variables, unused_parens, dead_code, non_snake_case, unused_assignments, unused_imports, unreachable_code, clippy::all)]\n")
	b.WriteString("use rescflow_rt::{go, recover, Defer, DeferOwn, DeferStack};\nuse std::cell::RefCell;\nuse std::fs::File;\nuse std::sync::Mutex;\n\n")
	var decls []ast.Decl
	for _, f := range g.pkg.Syntax {
		decls = append(decls, f.Decls...)
	}
	// globals and types first
	for _, d := range decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range gd.Specs {
			switch s := sp.(type) {
			case *ast.ValueSpec:
				for i, n := range s.Names {
					obj := g.info.Defs[n].(*types.Var)
					if len(s.Values) > 0 {
						g.fail(s, "package-level variable %s with an initializer", n.Name)
						continue
					}
					_ = i
					t := g.rtype(obj.Type(), s)
					fmt.Fprintf(&b, "static %s: Mutex<%s> = Mutex::new(%s);\n", strings.ToUpper(n.Name), t, g.zero(obj.Type(), s))
				}
			case *ast.TypeSpec:
				st, ok := s.Type.(*ast.StructType)
				if !ok {
					g.fail(s, "type %s (only struct types are supported)", s.Name.Name)
					continue
				}
				fmt.Fprintf(&b, "struct %s {\n", camel(s.Name.Name))
				for _, fl := range st.Fields.List {
					for _, fn := range fl.Names {
						fmt.Fprintf(&b, "    %s: %s,\n", local(fn.Name), g.rtype(g.info.TypeOf(fl.Type), fl))
					}
				}
				b.WriteString("}\n")
			}
		}
	}
	b.WriteString("\n")
	methods := map[string][]string{}
	for _, d := range decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		code := g.funcDecl(fd)
		if fd.Recv != nil {
			sn, _ := g.structName(g.info.TypeOf(fd.Recv.List[0].Type))
			methods[sn] = append(methods[sn], code)
		} else {
			b.WriteString(code + "\n")
		}
	}
	var keys []string
	for k := range methods {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "impl %s {\n%s}\n", k, strings.Join(methods[k], "\n"))
	}
	return b.String()
}

// ------------------------------------------------------------------ functions

func (g *gen) ssaFunc(fd *ast.FuncDecl) *ssa.Function {
	obj, _ := g.info.Defs[fd.Name].(*types.Func)
	if obj == nil {
		return nil
	}
	return g.a.prog.FuncValue(obj)
}

func (g *gen) summaryOf(obj *types.Func) *Summary {
	if obj == nil {
		return nil
	}
	if fn := g.a.prog.FuncValue(obj); fn != nil {
		return g.a.sums[fn]
	}
	return nil
}

// paramTakes: does the callee take ownership of resource input k?
func paramTakes(s *Summary, k int) bool {
	if s == nil || k >= len(s.In) {
		return false
	}
	return s.In[k].Release || len(s.In[k].Escapes) > 0
}

func (g *gen) funcDecl(fd *ast.FuncDecl) string {
	g.fd, g.vars, g.varObj, g.places, g.fieldObj = fd, map[*types.Var]*varInfo{}, map[*types.Var]string{}, map[string]place{}, map[string]string{}
	g.tmp, g.guardN, g.stackName, g.namedRes, g.recv, g.recovered = 0, 0, "", nil, nil, ""
	fn := g.ssaFunc(fd)
	g.sum = nil
	if fn != nil {
		g.sum = g.a.sums[fn]
	}
	g.classify(fd.Body)
	sig := g.info.Defs[fd.Name].(*types.Func).Type().(*types.Signature)
	var params []string
	k := 0
	if fd.Recv != nil {
		g.recv = sig.Recv()
		if _, ptr := types.Unalias(g.recv.Type()).(*types.Pointer); ptr {
			params = append(params, "&mut self")
		} else {
			params = append(params, "mut self")
		}
		g.vars[g.recv] = &varInfo{rust: "self"}
		k = 1
	}
	for i := 0; i < sig.Params().Len(); i++ {
		v := sig.Params().At(i)
		vi := g.vars[v]
		if vi == nil {
			vi = &varInfo{}
			g.vars[v] = vi
		}
		vi.rust = local(v.Name())
		t := v.Type()
		switch {
		case isOSFile(t):
			vi.res = true
			obj := "param:" + v.Name()
			g.varObj[v] = obj
			if paramTakes(g.sum, k+i) {
				params = append(params, fmt.Sprintf("mut %s: Option<File>", vi.rust))
				g.places[obj] = place{kind: "opt", name: vi.rust}
			} else {
				params = append(params, fmt.Sprintf("%s: Option<&File>", vi.rust))
				g.places[obj] = place{kind: "refparam", name: vi.rust}
			}
		case isFilePtrPtr(t):
			vi.cell = true
			params = append(params, fmt.Sprintf("%s: &RefCell<Option<File>>", vi.rust))
		default:
			params = append(params, fmt.Sprintf("mut %s: %s", vi.rust, g.rtype(t, fd)))
		}
	}
	var results []string
	for i := 0; i < sig.Results().Len(); i++ {
		results = append(results, g.rtype(sig.Results().At(i).Type(), fd))
		if sig.Results().At(i).Name() != "" {
			g.namedRes = append(g.namedRes, sig.Results().At(i))
		}
	}
	ret := ""
	switch len(results) {
	case 0:
	case 1:
		ret = " -> " + results[0]
	default:
		ret = " -> (" + strings.Join(results, ", ") + ")"
	}
	name := snake(fd.Name.Name)
	var body []string
	if len(g.namedRes) > 0 {
		for _, v := range g.namedRes {
			vi := g.vars[v]
			if vi == nil {
				vi = &varInfo{}
				g.vars[v] = vi
			}
			vi.rust = local(v.Name())
			z := g.zero(v.Type(), fd)
			if vi.cell {
				body = append(body, fmt.Sprintf("let %s = RefCell::new(%s);", vi.rust, z))
			} else {
				body = append(body, fmt.Sprintf("let mut %s = %s;", vi.rust, z))
			}
		}
		inner := g.topStmts(fd.Body.List)
		body = append(body, "'body: {")
		body = append(body, inner...)
		body = append(body, "}")
		var rs []string
		for _, v := range g.namedRes {
			vi := g.vars[v]
			if vi.cell {
				rs = append(rs, vi.rust+".into_inner()")
			} else {
				rs = append(rs, vi.rust)
			}
		}
		if len(rs) == 1 {
			body = append(body, rs[0])
		} else {
			body = append(body, "("+strings.Join(rs, ", ")+")")
		}
	} else {
		body = g.topStmts(fd.Body.List)
	}
	if fd.Name.Name == "main" && fd.Recv == nil {
		return fmt.Sprintf("fn main() {\n    go::main(|| {\n%s\n    });\n}\n", strings.Join(body, "\n"))
	}
	return fmt.Sprintf("fn %s(%s)%s {\n%s\n}\n", name, strings.Join(params, ", "), ret, strings.Join(body, "\n"))
}

// classify decides each local variable's representation from how the function uses it.
func (g *gen) classify(body *ast.BlockStmt) {
	get := func(v *types.Var) *varInfo {
		vi := g.vars[v]
		if vi == nil {
			vi = &varInfo{rust: local(v.Name())}
			g.vars[v] = vi
		}
		return vi
	}
	varOf := func(e ast.Expr) *types.Var {
		if id, ok := ast.Unparen(e).(*ast.Ident); ok {
			if v, ok := g.info.Uses[id].(*types.Var); ok {
				return v
			}
			if v, ok := g.info.Defs[id].(*types.Var); ok {
				return v
			}
		}
		return nil
	}
	isLocal := func(v *types.Var) bool {
		return v != nil && v.Parent() != nil && v.Parent() != g.pkg.Types.Scope() && !v.IsField()
	}
	var walk func(n ast.Node, inLoop, inIf bool, lits []*ast.FuncLit)
	walk = func(n ast.Node, inLoop, inIf bool, lits []*ast.FuncLit) {
		ast.Inspect(n, func(x ast.Node) bool {
			switch s := x.(type) {
			case *ast.FuncLit:
				if s != n {
					walk(s.Body, false, false, append(append([]*ast.FuncLit{}, lits...), s))
					return false
				}
			case *ast.ForStmt:
				if s != n {
					if s.Init != nil {
						walk(s.Init, inLoop, inIf, lits)
					}
					if s.Post != nil {
						walk(s.Post, true, inIf, lits)
					}
					if s.Cond != nil {
						walk(s.Cond, inLoop, inIf, lits)
					}
					walk(s.Body, true, inIf, lits)
					return false
				}
			case *ast.IfStmt:
				if s != n {
					if s.Init != nil {
						walk(s.Init, inLoop, inIf, lits)
					}
					walk(s.Cond, inLoop, inIf, lits)
					walk(s.Body, inLoop, true, lits)
					if s.Else != nil {
						walk(s.Else, inLoop, true, lits)
					}
					return false
				}
			case *ast.Ident:
				if v, ok := g.info.Uses[s].(*types.Var); ok && isLocal(v) {
					for _, l := range lits {
						if v.Pos() < l.Pos() || v.Pos() > l.End() {
							get(v).captured = true
						}
					}
				}
			case *ast.AssignStmt:
				for _, l := range s.Lhs {
					if v := varOf(l); isLocal(v) && (s.Tok != token.DEFINE || g.info.Defs[l.(*ast.Ident)] == nil) {
						get(v).mutated = true
					}
					if st, ok := l.(*ast.StarExpr); ok {
						_ = st
					}
				}
			case *ast.IncDecStmt:
				if v := varOf(s.X); isLocal(v) {
					get(v).mutated = true
				}
			case *ast.UnaryExpr:
				if s.Op == token.AND {
					if v := varOf(s.X); isLocal(v) {
						get(v).addr = true
					}
				}
			case *ast.DeferStmt:
				if inIf && !inLoop {
					for _, arg := range s.Call.Args {
						if v := varOf(arg); isLocal(v) && isOSFile(v.Type()) {
							get(v).condDefer = true
						}
					}
				}
				if sel, ok := s.Call.Fun.(*ast.SelectorExpr); ok {
					if v := varOf(sel.X); isLocal(v) {
						if _, ok := g.structName(v.Type()); ok {
							get(v).deferRecv = true
						}
					}
				}
			}
			return true
		})
	}
	walk(body, false, false, nil)
	// every variable the function defines gets a representation
	for id, obj := range g.info.Defs {
		if v, ok := obj.(*types.Var); ok && !v.IsField() && id.Pos() >= g.fd.Pos() && id.Pos() <= g.fd.End() {
			get(v)
		}
	}
	for v, vi := range g.vars {
		vi.res = isOSFile(v.Type())
		_, isStruct := g.structName(v.Type())
		switch {
		case vi.res:
			vi.cell = vi.captured || vi.addr || vi.condDefer
		case isStruct:
			vi.cell = vi.deferRecv
		default:
			vi.cell = vi.captured && vi.mutated
		}
	}
}

// ------------------------------------------------------------------ statements

// topStmts translates a function-level statement list; guards for defers in nested blocks are
// hoisted to just before the top-level statement that contains them.
func (g *gen) topStmts(list []ast.Stmt) []string {
	var out []string
	for i, s := range list {
		if ds, ok := s.(*ast.DeferStmt); ok && g.isRecoverDefer(ds) {
			if len(g.namedRes) > 0 || (g.fd.Type.Results != nil && len(g.fd.Type.Results.List) > 0) {
				g.fail(ds, "recover() in a function with results")
			}
			g.forms = append(g.forms, fmt.Sprintf("line %d: CATCH_UNWIND (recover)", g.line(ds)))
			rest := g.topStmts(list[i+1:])
			out = append(out, "let __r = recover(|| {")
			out = append(out, rest...)
			out = append(out, "});")
			out = append(out, "let __recovered = __r.err();")
			g.recovered = "__recovered"
			lit := ds.Call.Fun.(*ast.FuncLit)
			var h []string
			for _, st := range lit.Body.List {
				g.stmt(st, &h)
			}
			out = append(out, "{")
			out = append(out, h...)
			out = append(out, "}")
			return out
		}
		var hoisted []string
		saved := g.hoist
		g.hoist = &hoisted
		var body []string
		g.stmt(s, &body)
		g.hoist = saved
		out = append(out, hoisted...)
		out = append(out, body...)
	}
	return out
}

func (g *gen) line(n ast.Node) int { _, l := g.a.pos(n.Pos()); return l }

func (g *gen) isRecoverDefer(ds *ast.DeferStmt) bool {
	lit, ok := ds.Call.Fun.(*ast.FuncLit)
	if !ok {
		return false
	}
	found := false
	ast.Inspect(lit, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok {
				if b, ok := g.info.Uses[id].(*types.Builtin); ok && b.Name() == "recover" {
					found = true
				}
			}
		}
		return true
	})
	return found
}

func terminates(list []ast.Stmt) bool {
	if len(list) == 0 {
		return false
	}
	switch s := list[len(list)-1].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		if c, ok := s.X.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return true
			}
		}
	case *ast.BlockStmt:
		return terminates(s.List)
	}
	return false
}

func copyPlaces(m map[string]place) map[string]place {
	c := map[string]place{}
	for k, v := range m {
		c[k] = v
	}
	return c
}

func (g *gen) stmts(list []ast.Stmt, out *[]string) {
	for _, s := range list {
		g.stmt(s, out)
	}
}

func (g *gen) stmt(s ast.Stmt, out *[]string) {
	emit := func(f string, a ...any) { *out = append(*out, fmt.Sprintf(f, a...)) }
	switch x := s.(type) {
	case *ast.ExprStmt:
		c, ok := x.X.(*ast.CallExpr)
		if !ok {
			g.fail(x, "expression statement")
			return
		}
		code := g.call(c, out)
		if strings.HasPrefix(code, "println!") || strings.HasPrefix(code, "go::panic(") {
			emit("%s;", code)
			return
		}
		if t, ok := g.info.TypeOf(c).(*types.Tuple); (ok && t.Len() > 0) || (!ok && g.info.TypeOf(c) != nil) {
			emit("let _ = %s;", code)
		} else {
			emit("%s;", code)
		}
	case *ast.AssignStmt:
		g.assign(x, out)
	case *ast.IncDecStmt:
		op := "+="
		if x.Tok == token.DEC {
			op = "-="
		}
		emit("%s %s 1;", g.lvalue(x.X), op)
	case *ast.DeclStmt:
		gd := x.Decl.(*ast.GenDecl)
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				g.fail(x, "local type declaration")
				continue
			}
			for i, n := range vs.Names {
				v := g.info.Defs[n].(*types.Var)
				vi := g.vars[v]
				if vi == nil {
					vi = &varInfo{rust: local(n.Name)}
					g.vars[v] = vi
				}
				val := g.zero(v.Type(), x)
				if i < len(vs.Values) {
					val = g.valueFor(vs.Values[i], v.Type(), out)
				}
				if vi.res {
					obj := fmt.Sprintf("var:%s@%d", n.Name, g.line(n))
					g.varObj[v] = obj
				}
				if vi.cell {
					emit("let %s = RefCell::new(%s);", vi.rust, val)
				} else {
					emit("let mut %s = %s;", vi.rust, val)
				}
				if vi.res && !vi.cell {
					g.places[g.varObj[v]] = place{kind: "opt", name: vi.rust}
				}
			}
		}
	case *ast.ReturnStmt:
		g.ret(x, out)
	case *ast.BlockStmt:
		g.nested++
		emit("{")
		g.stmts(x.List, out)
		emit("}")
		g.nested--
	case *ast.IfStmt:
		if x.Init != nil {
			emit("{")
			g.stmt(x.Init, out)
		}
		cond := g.expr(x.Cond, out)
		before := copyPlaces(g.places)
		g.inIf++
		emit("if %s {", cond)
		g.stmts(x.Body.List, out)
		thenPlaces, thenTerm := g.places, terminates(x.Body.List)
		g.places = copyPlaces(before)
		elseTerm := false
		if x.Else != nil {
			emit("} else {")
			switch e := x.Else.(type) {
			case *ast.BlockStmt:
				g.stmts(e.List, out)
				elseTerm = terminates(e.List)
			case *ast.IfStmt:
				g.stmt(e, out)
			}
		}
		emit("}")
		g.inIf--
		elsePlaces := g.places
		switch {
		case thenTerm && elseTerm:
		case thenTerm:
			g.places = elsePlaces
		case elseTerm:
			g.places = thenPlaces
		default:
			for k, p := range thenPlaces {
				if q, ok := elsePlaces[k]; ok && q != p {
					g.fail(x, "a resource is moved on one branch only and used afterwards")
				}
			}
			g.places = thenPlaces
		}
		if x.Init != nil {
			emit("}")
		}
	case *ast.ForStmt:
		g.tmp++
		lbl := fmt.Sprintf("'loop%d", g.tmp)
		cont := fmt.Sprintf("'iter%d", g.tmp)
		emit("{")
		if x.Init != nil {
			g.stmt(x.Init, out)
		}
		emit("%s: loop {", lbl)
		if x.Cond != nil {
			emit("if !(%s) { break %s; }", g.expr(x.Cond, out), lbl)
		}
		before := copyPlaces(g.places)
		g.loops = append(g.loops, [2]string{lbl, cont})
		g.inLoop++
		emit("%s: {", cont)
		g.stmts(x.Body.List, out)
		emit("}")
		g.inLoop--
		g.loops = g.loops[:len(g.loops)-1]
		for k, p := range before {
			if q := g.places[k]; q != p {
				g.fail(x, "a resource from outside the loop is moved inside it")
			}
		}
		g.places = before
		if x.Post != nil {
			g.stmt(x.Post, out)
		}
		emit("}")
		emit("}")
	case *ast.BranchStmt:
		if len(g.loops) == 0 {
			g.fail(x, "break/continue outside a loop")
			return
		}
		l := g.loops[len(g.loops)-1]
		switch x.Tok {
		case token.BREAK:
			emit("break %s;", l[0])
		case token.CONTINUE:
			emit("break %s;", l[1])
		default:
			g.fail(x, "%s", x.Tok)
		}
	case *ast.DeferStmt:
		g.deferStmt(x, out)
	case *ast.GoStmt:
		g.fail(x, "go statement (concurrency is outside the subset)")
	default:
		g.fail(s, "statement %T", s)
	}
}

// ------------------------------------------------------------------ assignment & return

func (g *gen) varOfIdent(e ast.Expr) *types.Var {
	if id, ok := ast.Unparen(e).(*ast.Ident); ok {
		if v, ok := g.info.Uses[id].(*types.Var); ok {
			return v
		}
		if v, ok := g.info.Defs[id].(*types.Var); ok {
			return v
		}
	}
	return nil
}

func (g *gen) isGlobal(v *types.Var) bool { return v != nil && v.Parent() == g.pkg.Types.Scope() }

func (g *gen) assign(x *ast.AssignStmt, out *[]string) {
	emit := func(f string, a ...any) { *out = append(*out, fmt.Sprintf(f, a...)) }
	// a tuple-valued call on the right: f, err := os.Open(p)
	if len(x.Rhs) == 1 && len(x.Lhs) > 1 {
		c, ok := x.Rhs[0].(*ast.CallExpr)
		if !ok {
			g.fail(x, "multi-value assignment from a non-call")
			return
		}
		code := g.call(c, out)
		var tmps []string
		for range x.Lhs {
			tmps = append(tmps, g.newTmp("t"))
		}
		emit("let (%s) = %s;", strings.Join(tmps, ", "), code)
		objs := g.resultObjects(c, len(x.Lhs))
		for i, l := range x.Lhs {
			if id, ok := l.(*ast.Ident); ok && id.Name == "_" {
				continue
			}
			g.bind(x, l, tmps[i], objs[i], out)
		}
		return
	}
	if len(x.Lhs) != len(x.Rhs) {
		g.fail(x, "assignment shape")
		return
	}
	// alias: g := f (both resource variables, neither reassigned)
	if x.Tok == token.DEFINE && len(x.Lhs) == 1 {
		if v := g.varOfIdent(x.Lhs[0]); v != nil && g.info.Defs[x.Lhs[0].(*ast.Ident)] != nil && isOSFile(v.Type()) {
			if src := g.varOfIdent(x.Rhs[0]); src != nil && !g.isGlobal(src) && g.vars[src] != nil && !g.vars[src].cell && !g.vars[v].cell && !g.vars[v].mutated && !g.vars[src].mutated {
				g.vars[v].alias = src
				g.varObj[v] = g.varObj[src]
				return
			}
		}
	}
	// composite literal of a struct that takes resources
	if x.Tok == token.DEFINE && len(x.Lhs) == 1 {
		rhs := ast.Unparen(x.Rhs[0])
		if u, ok := rhs.(*ast.UnaryExpr); ok && u.Op == token.AND {
			rhs = u.X
		}
		if cl, ok := rhs.(*ast.CompositeLit); ok {
			if sn, ok := g.structName(g.info.TypeOf(cl)); ok {
				v := g.varOfIdent(x.Lhs[0])
				vi := g.vars[v]
				lit, fields := g.structLit(sn, cl, out)
				if vi.cell {
					emit("let %s = RefCell::new(%s);", vi.rust, lit)
				} else {
					emit("let mut %s = %s;", vi.rust, lit)
				}
				for f, obj := range fields {
					g.fieldObj[vi.rust+"."+f] = obj
					if vi.cell {
						g.places[obj] = place{kind: "cellfield", name: vi.rust, field: f}
					} else {
						g.places[obj] = place{kind: "field", name: vi.rust, field: f}
					}
				}
				return
			}
		}
		if fl, ok := rhs.(*ast.FuncLit); ok {
			v := g.varOfIdent(x.Lhs[0])
			g.vars[v].isFuncLit = true
			var body []string
			g.stmts(fl.Body.List, &body)
			emit("let %s = || {", g.vars[v].rust)
			*out = append(*out, body...)
			emit("};")
			return
		}
	}
	// general: evaluate every right-hand side first (Go assigns simultaneously)
	vals := make([]string, len(x.Rhs))
	objs := make([]string, len(x.Rhs))
	for i, r := range x.Rhs {
		lt := g.info.TypeOf(x.Lhs[i])
		if lt == nil {
			lt = g.info.TypeOf(r)
		}
		vals[i] = g.valueFor(r, lt, out)
		objs[i] = g.objOfExpr(r)
	}
	if len(x.Rhs) > 1 {
		for i := range vals {
			t := g.newTmp("v")
			emit("let %s = %s;", t, vals[i])
			vals[i] = t
		}
	}
	for i, l := range x.Lhs {
		if id, ok := l.(*ast.Ident); ok && id.Name == "_" {
			emit("let _ = %s;", vals[i])
			continue
		}
		g.bind(x, l, vals[i], objs[i], out)
	}
}

// bind stores val into lhs (declaring it if this is its definition).
func (g *gen) bind(x *ast.AssignStmt, l ast.Expr, val, obj string, out *[]string) {
	emit := func(f string, a ...any) { *out = append(*out, fmt.Sprintf(f, a...)) }
	if id, ok := l.(*ast.Ident); ok {
		if v, ok := g.info.Defs[id].(*types.Var); ok && x.Tok == token.DEFINE {
			vi := g.vars[v]
			if vi == nil {
				vi = &varInfo{rust: local(v.Name()), res: isOSFile(v.Type())}
				g.vars[v] = vi
			}
			if vi.cell {
				emit("let %s = RefCell::new(%s);", vi.rust, val)
			} else {
				emit("let mut %s = %s;", vi.rust, val)
			}
			if vi.res {
				if obj == "" {
					obj = fmt.Sprintf("var:%s@%d", v.Name(), g.line(id))
				}
				g.varObj[v] = obj
				if !vi.cell {
					g.places[obj] = place{kind: "opt", name: vi.rust}
				}
			}
			return
		}
	}
	lhs := g.lvalue(l)
	emit("%s = %s;", lhs, val)
	if v := g.varOfIdent(l); v != nil && isOSFile(v.Type()) {
		if g.isGlobal(v) {
			if obj != "" {
				g.places[obj] = place{kind: "global", name: strings.ToUpper(v.Name())}
			}
		} else if vi := g.vars[v]; vi != nil && !vi.cell {
			if obj == "" {
				obj = fmt.Sprintf("var:%s@%d", v.Name(), g.line(l))
			}
			g.varObj[v] = obj
			g.places[obj] = place{kind: "opt", name: vi.rust}
		}
	}
}

// lvalue renders an assignable Go expression.
func (g *gen) lvalue(e ast.Expr) string {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		v := g.varOfIdent(x)
		if g.isGlobal(v) {
			return "*" + strings.ToUpper(v.Name()) + ".lock().unwrap()"
		}
		vi := g.vars[v]
		if vi == nil {
			g.fail(x, "assignment to %s", x.Name)
			return x.Name
		}
		if vi.cell {
			return "*" + vi.rust + ".borrow_mut()"
		}
		return vi.rust
	case *ast.StarExpr:
		if v := g.varOfIdent(x.X); v != nil && isFilePtrPtr(v.Type()) {
			return "*" + g.vars[v].rust + ".borrow_mut()"
		}
	case *ast.SelectorExpr:
		if v := g.varOfIdent(x.X); v != nil {
			if vi := g.vars[v]; vi != nil {
				if vi.cell {
					return vi.rust + ".borrow_mut()." + local(x.Sel.Name)
				}
				return vi.rust + "." + local(x.Sel.Name)
			}
		}
	}
	g.fail(e, "assignment target %T", e)
	return "_"
}

func (g *gen) ret(x *ast.ReturnStmt, out *[]string) {
	emit := func(f string, a ...any) { *out = append(*out, fmt.Sprintf(f, a...)) }
	sig := g.info.Defs[g.fd.Name].(*types.Func).Type().(*types.Signature)
	var vals []string
	for i, r := range x.Results {
		vals = append(vals, g.valueFor(r, sig.Results().At(i).Type(), out))
	}
	if len(g.namedRes) > 0 {
		if len(vals) > 0 {
			var tmps []string
			for _, v := range vals {
				t := g.newTmp("r")
				emit("let %s = %s;", t, v)
				tmps = append(tmps, t)
			}
			for i, v := range g.namedRes {
				vi := g.vars[v]
				if vi.cell {
					emit("*%s.borrow_mut() = %s;", vi.rust, tmps[i])
				} else {
					emit("%s = %s;", vi.rust, tmps[i])
				}
			}
		}
		emit("break 'body;")
		return
	}
	switch len(vals) {
	case 0:
		emit("return;")
	case 1:
		emit("return %s;", vals[0])
	default:
		emit("return (%s);", strings.Join(vals, ", "))
	}
}

// ------------------------------------------------------------------ resources

// resultObjects names the resources a call returns (one per result), for place tracking.
func (g *gen) resultObjects(c *ast.CallExpr, n int) []string {
	objs := make([]string, n)
	if g.isPkgFunc(c, "os", "Open") {
		f, l := g.a.pos(c.Pos())
		objs[0] = fmt.Sprintf("%s:%d", f, l)
		return objs
	}
	if fobj := g.calleeFunc(c); fobj != nil {
		if s := g.summaryOf(fobj); s != nil {
			for i := range objs {
				if fr := s.Fresh[i]; fr != nil {
					objs[i] = string(fr.Site)
				}
			}
		}
	}
	return objs
}

func (g *gen) objOfExpr(e ast.Expr) string {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.Ident:
		if v := g.varOfIdent(x); v != nil {
			for vi := g.vars[v]; vi != nil && vi.alias != nil; vi = g.vars[vi.alias] {
				v = vi.alias
			}
			return g.varObj[v]
		}
	case *ast.SelectorExpr:
		if v := g.varOfIdent(x.X); v != nil && g.vars[v] != nil && isOSFile(g.info.TypeOf(x)) {
			vi := g.vars[v]
			key := vi.rust + "." + local(x.Sel.Name)
			if g.fieldObj[key] == "" {
				// a field we have not seen filled (e.g. a receiver's): it is its own object
				obj := "field:" + key
				g.fieldObj[key] = obj
				kind := "field"
				if vi.cell {
					kind = "cellfield"
				}
				g.places[obj] = place{kind: kind, name: vi.rust, field: local(x.Sel.Name)}
			}
			return g.fieldObj[key]
		}
	case *ast.CallExpr:
		objs := g.resultObjects(x, 1)
		return objs[0]
	}
	return ""
}

// placeOf finds where the resource an expression denotes lives now.
func (g *gen) placeOf(e ast.Expr) (place, bool) {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.Ident:
		v := g.varOfIdent(x)
		if v == nil {
			break
		}
		if g.isGlobal(v) {
			return place{kind: "global", name: strings.ToUpper(v.Name())}, true
		}
		for vi := g.vars[v]; vi != nil && vi.alias != nil; vi = g.vars[vi.alias] {
			v = vi.alias
		}
		vi := g.vars[v]
		if vi != nil && vi.cell {
			return place{kind: "cell", name: vi.rust}, true
		}
		if p, ok := g.places[g.varObj[v]]; ok {
			return p, true
		}
	case *ast.SelectorExpr:
		if v := g.varOfIdent(x.X); v != nil && g.vars[v] != nil {
			vi := g.vars[v]
			key := vi.rust + "." + local(x.Sel.Name)
			if obj := g.fieldObj[key]; obj != "" {
				if p, ok := g.places[obj]; ok {
					return p, true
				}
			}
			if vi.cell {
				return place{kind: "cellfield", name: vi.rust, field: local(x.Sel.Name)}, true
			}
			return place{kind: "field", name: vi.rust, field: local(x.Sel.Name)}, true
		}
	case *ast.StarExpr:
		if v := g.varOfIdent(x.X); v != nil && isFilePtrPtr(v.Type()) {
			return place{kind: "cellref", name: g.vars[v].rust}, true
		}
	}
	return place{}, false
}

// takeRes moves a resource expression out as Option<File>.
func (g *gen) takeRes(e ast.Expr, out *[]string) string {
	if id, ok := ast.Unparen(e).(*ast.Ident); ok && id.Name == "nil" {
		return "None"
	}
	if c, ok := ast.Unparen(e).(*ast.CallExpr); ok {
		return g.call(c, out)
	}
	p, ok := g.placeOf(e)
	if !ok {
		g.fail(e, "cannot locate the resource %s", types.ExprString(e))
		return "None"
	}
	t, ok := p.take()
	if !ok {
		g.fail(e, "resource %s cannot be moved out of a %s here", types.ExprString(e), p.kind)
		return "None"
	}
	if obj := g.objOfExpr(e); obj != "" {
		delete(g.places, obj)
	}
	return t
}

func (g *gen) refRes(e ast.Expr) string {
	if id, ok := ast.Unparen(e).(*ast.Ident); ok && id.Name == "nil" {
		return "None"
	}
	p, ok := g.placeOf(e)
	if !ok {
		g.fail(e, "cannot locate the resource %s", types.ExprString(e))
		return "None"
	}
	return p.ref()
}

// valueFor renders e as a value of Go type t (resources are moved).
func (g *gen) valueFor(e ast.Expr, t types.Type, out *[]string) string {
	if isOSFile(t) {
		return g.takeRes(e, out)
	}
	return g.expr(e, out)
}

// ------------------------------------------------------------------ defer

func (g *gen) deferStmt(ds *ast.DeferStmt, out *[]string) {
	emit := func(f string, a ...any) { *out = append(*out, fmt.Sprintf(f, a...)) }
	line := g.line(ds)
	g.guardN++
	gname := fmt.Sprintf("_d%d", g.guardN)
	c := ds.Call
	// arguments are evaluated now, as Go does
	var params []*types.Var
	var lit *ast.FuncLit
	var callee *types.Func
	switch f := c.Fun.(type) {
	case *ast.FuncLit:
		lit = f
		sig := g.info.TypeOf(f).(*types.Signature)
		for i := 0; i < sig.Params().Len(); i++ {
			params = append(params, sig.Params().At(i))
		}
	default:
		callee = g.calleeFunc(c)
	}
	resArg := -1
	var temps []string
	for i, arg := range c.Args {
		t := g.info.TypeOf(arg)
		tn := g.newTmp("a")
		if isOSFile(t) {
			takes := lit != nil || (callee != nil && paramTakes(g.summaryOf(callee), g.recvOffset(c)+i)) || g.isFileMethod(c, "Close")
			if !takes || resArg >= 0 {
				g.fail(ds, "deferred call that only borrows a resource")
				return
			}
			resArg = i
			if g.inIf > 0 && g.inLoop == 0 {
				// OPTION_GUARD: the resource stays in its cell; the guard takes it at exit
				p, _ := g.placeOf(arg)
				temps = append(temps, "@cell:"+p.name)
				continue
			}
			emit("let %s = %s;", tn, g.takeRes(arg, out))
			temps = append(temps, tn)
			continue
		}
		emit("let %s = %s;", tn, g.expr(arg, out))
		temps = append(temps, tn)
	}
	// the deferred body, as a closure over the evaluated arguments
	var body string
	resVar := ""
	switch {
	case lit != nil:
		var lines []string
		for i, p := range params {
			vi := &varInfo{rust: local(p.Name()), res: isOSFile(p.Type())}
			g.vars[p] = vi
			if i == resArg {
				resVar = vi.rust
				obj := "param:" + p.Name()
				g.varObj[p] = obj
				g.places[obj] = place{kind: "opt", name: vi.rust}
				continue
			}
			lines = append(lines, fmt.Sprintf("let mut %s = %s;", vi.rust, temps[i]))
		}
		var b []string
		saved := g.places
		g.places = copyPlaces(g.places)
		g.stmts(lit.Body.List, &b)
		g.places = saved
		body = strings.Join(append(lines, b...), "\n")
	case g.isFileMethod(c, "Close"):
		resVar = "__f"
		body = "let _ = go::close(__f);"
	case callee != nil:
		var args []string
		for i := range c.Args {
			if i == resArg {
				args = append(args, "__f")
			} else {
				args = append(args, temps[i])
			}
		}
		recv := ""
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok && g.recvOffset(c) == 1 {
			v := g.varOfIdent(sel.X)
			if v == nil || g.vars[v] == nil || !g.vars[v].cell {
				g.fail(ds, "deferred method call on %s", types.ExprString(sel.X))
				return
			}
			recv = g.vars[v].rust + ".borrow_mut()."
			body = fmt.Sprintf("let _ = %s%s(%s);", recv, snake(sel.Sel.Name), strings.Join(args, ", "))
		} else {
			body = fmt.Sprintf("let _ = %s(%s);", snake(callee.Name()), strings.Join(args, ", "))
		}
		if resArg >= 0 {
			resVar = "__f"
		}
	default:
		if id, ok := c.Fun.(*ast.Ident); ok {
			if v := g.varOfIdent(id); v != nil && g.vars[v] != nil && g.vars[v].isFuncLit && len(c.Args) == 0 {
				g.place(ds, out, gname, fmt.Sprintf("Defer::new(%s)", g.vars[v].rust), "CELL_CLOSURE_GUARD (stored closure)", line)
				return
			}
		}
		g.fail(ds, "deferred call through %s", types.ExprString(c.Fun))
		return
	}
	// choose the form from the defer's facts
	switch {
	case g.inLoop > 0:
		if g.stackName == "" {
			g.stackName = "__defers"
			*g.hoist = append(*g.hoist, "let mut __defers = DeferStack::new();")
		}
		if resArg >= 0 {
			slot := g.newTmp("slot")
			emit("let %s = %s.push_file(%s, move |mut %s| { %s });", slot, g.stackName, temps[resArg], resVar, body)
			if obj := g.objOfExpr(c.Args[resArg]); obj != "" {
				g.places[obj] = place{kind: "stack", name: g.stackName, slot: slot}
			}
		} else {
			emit("%s.defer(move || { %s });", g.stackName, body)
		}
		g.forms = append(g.forms, fmt.Sprintf("line %d: DEFER_STACK", line))
	case resArg >= 0 && strings.HasPrefix(temps[resArg], "@cell:"):
		cell := strings.TrimPrefix(temps[resArg], "@cell:")
		*g.hoist = append(*g.hoist, fmt.Sprintf("let mut %s = None;", gname))
		ref := g.newTmp("c")
		emit("let %s = &%s;", ref, cell)
		emit("%s = Some(Defer::new(move || { let mut %s = %s.borrow_mut().take(); %s }));", gname, resVar, ref, body)
		g.forms = append(g.forms, fmt.Sprintf("line %d: OPTION_GUARD", line))
	case resArg >= 0:
		code := fmt.Sprintf("DeferOwn::new(%s, move |mut %s| { %s })", temps[resArg], resVar, body)
		g.place(ds, out, gname, code, "OWNED_GUARD", line)
		if obj := g.objOfExpr(c.Args[resArg]); obj != "" {
			g.places[obj] = place{kind: "guard", name: gname}
		}
	default:
		form := "CELL_CLOSURE_GUARD"
		if lit != nil && g.writesNamedResult(lit) {
			form = "RESULT_SLOT_GUARD"
		}
		if lit == nil {
			form = "OWNED_GUARD (no resource argument)"
		}
		mv := "move "
		if lit != nil && len(params) == 0 {
			mv = "" // reads the variables it captures when it runs
		}
		g.place(ds, out, gname, fmt.Sprintf("Defer::new(%s|| { %s })", mv, body), form, line)
	}
}

// place emits a guard at function scope: here when the defer is a top-level statement,
// otherwise declared before the enclosing top-level statement and assigned here.
func (g *gen) place(ds *ast.DeferStmt, out *[]string, name, code, form string, line int) {
	if g.inIf > 0 {
		*g.hoist = append(*g.hoist, fmt.Sprintf("let mut %s = None;", name))
		*out = append(*out, fmt.Sprintf("%s = Some(%s);", name, code))
		form = "OPTION_GUARD (" + form + ")"
	} else if g.nested > 0 {
		*g.hoist = append(*g.hoist, fmt.Sprintf("let %s;", name))
		*out = append(*out, fmt.Sprintf("%s = %s;", name, code))
		form += " (hoisted to function scope)"
	} else {
		*out = append(*out, fmt.Sprintf("let %s = %s;", name, code))
	}
	g.forms = append(g.forms, fmt.Sprintf("line %d: %s", line, form))
}

func (g *gen) writesNamedResult(lit *ast.FuncLit) bool {
	found := false
	ast.Inspect(lit, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok {
			for _, l := range as.Lhs {
				if v := g.varOfIdent(l); v != nil {
					for _, r := range g.namedRes {
						if r == v {
							found = true
						}
					}
				}
			}
		}
		return true
	})
	return found
}

// ------------------------------------------------------------------ calls & expressions

func (g *gen) isPkgFunc(c *ast.CallExpr, pkg, name string) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	pn, ok := g.info.Uses[id].(*types.PkgName)
	return ok && pn.Imported().Path() == pkg && sel.Sel.Name == name
}

func (g *gen) isFileMethod(c *ast.CallExpr, name string) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name && isOSFile(g.info.TypeOf(sel.X))
}

func (g *gen) calleeFunc(c *ast.CallExpr) *types.Func {
	switch f := ast.Unparen(c.Fun).(type) {
	case *ast.Ident:
		fn, _ := g.info.Uses[f].(*types.Func)
		return fn
	case *ast.SelectorExpr:
		if s := g.info.Selections[f]; s != nil {
			fn, _ := s.Obj().(*types.Func)
			return fn
		}
		fn, _ := g.info.Uses[f.Sel].(*types.Func)
		return fn
	}
	return nil
}

func (g *gen) recvOffset(c *ast.CallExpr) int {
	if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
		if s := g.info.Selections[sel]; s != nil && s.Kind() == types.MethodVal {
			return 1
		}
	}
	return 0
}

func (g *gen) call(c *ast.CallExpr, out *[]string) string {
	// builtins
	if id, ok := c.Fun.(*ast.Ident); ok {
		if b, ok := g.info.Uses[id].(*types.Builtin); ok {
			switch b.Name() {
			case "len":
				return "(" + g.expr(c.Args[0], out) + ".len() as i64)"
			case "panic":
				return "go::panic(" + g.expr(c.Args[0], out) + ")"
			case "recover":
				if g.recovered == "" {
					g.fail(c, "recover() outside a deferred recover closure")
				}
				return g.recovered
			}
			g.fail(c, "builtin %s", b.Name())
			return "()"
		}
	}
	switch {
	case g.isPkgFunc(c, "fmt", "Println"):
		var args, ph []string
		for _, a := range c.Args {
			args = append(args, g.expr(a, out))
			ph = append(ph, "{}")
		}
		if len(args) == 0 {
			return "println!()"
		}
		return fmt.Sprintf("println!(%q, %s)", strings.Join(ph, " "), strings.Join(args, ", "))
	case g.isPkgFunc(c, "fmt", "Sprint") && len(c.Args) == 1:
		return "format!(\"{}\", " + g.expr(c.Args[0], out) + ")"
	case g.isPkgFunc(c, "os", "Open"):
		return "go::open(&" + g.expr(c.Args[0], out) + ")"
	case g.isFileMethod(c, "Close"):
		return "go::close(" + g.takeRes(c.Fun.(*ast.SelectorExpr).X, out) + ")"
	case g.isFileMethod(c, "Stat"):
		return "go::stat(" + g.refRes(c.Fun.(*ast.SelectorExpr).X) + ")"
	}
	if id, ok := c.Fun.(*ast.Ident); ok {
		if v := g.varOfIdent(id); v != nil && g.vars[v] != nil && g.vars[v].isFuncLit {
			return g.vars[v].rust + "()"
		}
	}
	fobj := g.calleeFunc(c)
	if fobj == nil || fobj.Pkg() != g.pkg.Types {
		g.fail(c, "call to %s (no model for it)", types.ExprString(c.Fun))
		return "()"
	}
	sum := g.summaryOf(fobj)
	off := g.recvOffset(c)
	sig := fobj.Type().(*types.Signature)
	var args []string
	for i, a := range c.Args {
		pt := sig.Params().At(i).Type()
		switch {
		case isOSFile(pt):
			if paramTakes(sum, off+i) {
				args = append(args, g.takeRes(a, out))
			} else {
				args = append(args, g.refRes(a))
			}
		case isFilePtrPtr(pt):
			u, ok := a.(*ast.UnaryExpr)
			v := g.varOfIdent(u.X)
			if !ok || v == nil || g.vars[v] == nil || !g.vars[v].cell {
				g.fail(a, "pointer to a resource variable")
				return "()"
			}
			args = append(args, "&"+g.vars[v].rust)
		default:
			args = append(args, g.expr(a, out))
		}
	}
	if off == 1 {
		sel := c.Fun.(*ast.SelectorExpr)
		v := g.varOfIdent(sel.X)
		if v == nil || g.vars[v] == nil {
			g.fail(c, "method call on %s", types.ExprString(sel.X))
			return "()"
		}
		recv := g.vars[v].rust
		if g.vars[v].cell {
			recv += ".borrow_mut()"
		}
		if paramTakes(sum, 0) {
			// the method takes the resources in the receiver: they leave the caller
			for k := range g.fieldObj {
				if strings.HasPrefix(k, g.vars[v].rust+".") {
					delete(g.places, g.fieldObj[k])
				}
			}
		}
		return fmt.Sprintf("%s.%s(%s)", recv, snake(sel.Sel.Name), strings.Join(args, ", "))
	}
	return fmt.Sprintf("%s(%s)", snake(fobj.Name()), strings.Join(args, ", "))
}

func (g *gen) structLit(sn string, cl *ast.CompositeLit, out *[]string) (string, map[string]string) {
	st := g.info.TypeOf(cl).Underlying().(*types.Struct)
	fields := map[string]string{}
	vals := map[string]string{}
	for i, el := range cl.Elts {
		var name string
		var ve ast.Expr
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			name, ve = kv.Key.(*ast.Ident).Name, kv.Value
		} else {
			name, ve = st.Field(i).Name(), el
		}
		var ft types.Type
		for j := 0; j < st.NumFields(); j++ {
			if st.Field(j).Name() == name {
				ft = st.Field(j).Type()
			}
		}
		if isOSFile(ft) {
			if obj := g.objOfExpr(ve); obj != "" {
				fields[local(name)] = obj
			}
		}
		vals[name] = g.valueFor(ve, ft, out)
	}
	var parts []string
	for j := 0; j < st.NumFields(); j++ {
		f := st.Field(j)
		v, ok := vals[f.Name()]
		if !ok {
			v = g.zero(f.Type(), cl)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", local(f.Name()), v))
	}
	return fmt.Sprintf("%s { %s }", sn, strings.Join(parts, ", ")), fields
}

func (g *gen) expr(e ast.Expr, out *[]string) string {
	if tv, ok := g.info.Types[e]; ok && tv.Value != nil {
		switch tv.Value.Kind() {
		case constant.String:
			return fmt.Sprintf("String::from(%q)", constant.StringVal(tv.Value))
		case constant.Int:
			return tv.Value.ExactString() + "i64"
		case constant.Bool:
			return tv.Value.ExactString()
		}
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		if x.Name == "nil" {
			return "None"
		}
		v := g.varOfIdent(x)
		if v == nil {
			g.fail(x, "identifier %s", x.Name)
			return x.Name
		}
		if isOSFile(v.Type()) {
			g.fail(x, "resource %s used as a plain value", x.Name)
			return "None"
		}
		if g.isGlobal(v) {
			return strings.ToUpper(v.Name()) + ".lock().unwrap().clone()"
		}
		vi := g.vars[v]
		if vi == nil {
			g.fail(x, "variable %s", x.Name)
			return x.Name
		}
		if vi.cell {
			return vi.rust + ".borrow().clone()"
		}
		return vi.rust + ".clone()"
	case *ast.BinaryExpr:
		// comparisons with nil
		for _, pair := range [][2]ast.Expr{{x.X, x.Y}, {x.Y, x.X}} {
			if id, ok := ast.Unparen(pair[1]).(*ast.Ident); ok && id.Name == "nil" {
				var v string
				if c, ok := ast.Unparen(pair[0]).(*ast.CallExpr); ok && g.recovered != "" {
					if id2, ok := c.Fun.(*ast.Ident); ok && id2.Name == "recover" {
						v = g.recovered
					}
				}
				if v == "" {
					if isOSFile(g.info.TypeOf(pair[0])) {
						v = g.refRes(pair[0])
					} else {
						v = g.expr(pair[0], out)
					}
				}
				if x.Op == token.EQL {
					return v + ".is_none()"
				}
				return v + ".is_some()"
			}
		}
		l, r := g.expr(x.X, out), g.expr(x.Y, out)
		if x.Op == token.ADD && g.rtype(g.info.TypeOf(x), x) == "String" {
			return fmt.Sprintf("format!(\"{}{}\", %s, %s)", l, r)
		}
		return fmt.Sprintf("(%s %s %s)", l, x.Op, r)
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			return "!" + g.expr(x.X, out)
		}
		if x.Op == token.SUB {
			return "-" + g.expr(x.X, out)
		}
	case *ast.CallExpr:
		return g.call(x, out)
	case *ast.IndexExpr:
		if sel, ok := x.X.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				if pn, ok := g.info.Uses[id].(*types.PkgName); ok && pn.Imported().Path() == "os" && sel.Sel.Name == "Args" {
					return "go::args()[(" + g.expr(x.Index, out) + ") as usize].clone()"
				}
			}
		}
		return g.expr(x.X, out) + "[(" + g.expr(x.Index, out) + ") as usize].clone()"
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			if pn, ok := g.info.Uses[id].(*types.PkgName); ok {
				if pn.Imported().Path() == "os" && x.Sel.Name == "Args" {
					return "go::args()"
				}
				g.fail(x, "%s.%s", id.Name, x.Sel.Name)
				return "()"
			}
		}
		if v := g.varOfIdent(x.X); v != nil && g.vars[v] != nil {
			if g.vars[v].cell {
				return g.vars[v].rust + ".borrow()." + local(x.Sel.Name) + ".clone()"
			}
			return g.vars[v].rust + "." + local(x.Sel.Name) + ".clone()"
		}
	case *ast.CompositeLit:
		if _, ok := g.info.TypeOf(x).Underlying().(*types.Slice); ok {
			var els []string
			for _, el := range x.Elts {
				els = append(els, g.expr(el, out))
			}
			return "vec![" + strings.Join(els, ", ") + "]"
		}
	}
	g.fail(e, "expression %s", types.ExprString(e))
	return "()"
}
