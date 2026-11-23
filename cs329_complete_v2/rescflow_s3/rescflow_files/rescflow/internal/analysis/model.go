// Package analysis is RescFlow's memory-aware resource analysis on top of go/ssa.
//
// go/ssa gives values, control flow, memory cells (Alloc), loads/stores, closure
// bindings, defer registration and exit points. This package adds what go/ssa does not
// know: which values are resources, what each memory cell holds at each point, whether
// each resource is open or closed, which deferred calls are pending, and - at every exit -
// which resource each pending defer actually acts on.
package analysis

import (
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// apiKind classifies a callee against the known resource API ("Models and rules" box).
type apiKind int

const (
	apiNone apiKind = iota
	apiAcquire
	apiRelease
	apiUse
	apiExit // os.Exit / log.Fatal*: terminates without running defers
)

// acquireFuncs are package-level functions in package os that return (*os.File, error).
var acquireFuncs = map[string]bool{"Open": true, "OpenFile": true, "Create": true, "CreateTemp": true, "OpenInRoot": true}

func isOSFile(t types.Type) bool {
	p, ok := types.Unalias(t).(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := types.Unalias(p.Elem()).(*types.Named)
	if !ok {
		return false
	}
	o := n.Obj()
	return o.Pkg() != nil && o.Pkg().Path() == "os" && o.Name() == "File"
}

func classifyAPI(fn *ssa.Function) apiKind {
	if fn == nil {
		return apiNone
	}
	obj, _ := fn.Object().(*types.Func)
	if obj == nil || obj.Pkg() == nil {
		return apiNone
	}
	path, name := obj.Pkg().Path(), obj.Name()
	recv := fn.Signature.Recv()
	if recv == nil {
		if path == "os" && acquireFuncs[name] {
			return apiAcquire
		}
		if (path == "os" && name == "Exit") || (path == "log" && (name == "Fatal" || name == "Fatalf" || name == "Fatalln")) {
			return apiExit
		}
		return apiNone
	}
	if path != "os" || !isOSFile(recv.Type()) {
		return apiNone
	}
	if name == "Close" {
		return apiRelease
	}
	return apiUse
}

// mayHold reports whether a value of type t can (transitively) refer to a resource.
// Interfaces other than `error` may hold anything; func values are handled separately.
func mayHold(t types.Type) bool { return mayHoldSeen(t, map[types.Type]bool{}) }

func mayHoldSeen(t types.Type, seen map[types.Type]bool) bool {
	t = types.Unalias(t)
	if seen[t] {
		return false
	}
	seen[t] = true
	if isOSFile(t) {
		return true
	}
	switch u := t.(type) {
	case *types.Named:
		if o := u.Obj(); o.Pkg() == nil && o.Name() == "error" {
			return false
		}
		return mayHoldSeen(u.Underlying(), seen)
	case *types.Pointer:
		return mayHoldSeen(u.Elem(), seen)
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if mayHoldSeen(u.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Interface:
		return true
	case *types.Slice:
		return mayHoldSeen(u.Elem(), seen)
	case *types.Array:
		return mayHoldSeen(u.Elem(), seen)
	case *types.Map:
		return mayHoldSeen(u.Elem(), seen) || mayHoldSeen(u.Key(), seen)
	case *types.Chan:
		return mayHoldSeen(u.Elem(), seen)
	case *types.Tuple:
		for i := 0; i < u.Len(); i++ {
			if mayHoldSeen(u.At(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}

func isErrorType(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() == nil && n.Obj().Name() == "error"
}

func isBool(t types.Type) bool {
	b, ok := types.Unalias(t).Underlying().(*types.Basic)
	return ok && b.Kind() == types.Bool
}
