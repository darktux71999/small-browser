// Package js embeds QuickJS via cgo: scripts evaluate in a persistent
// context, thrown exceptions surface as oracle-format errors, and objects
// or functions that outlive one Eval are addressed through a handle
// registry (T2 hangs document/window/timers off SetGlobalFunc/Call/Get).
//
// Bridge layout mirrors go/css: real C headers, C definitions in glue.c,
// and no Go pointer ever enters C. JSValues cross the boundary only as
// 16-byte blobs; Go functions are reached from JS through the runtime
// cookie parked in the context opaque (an integer cookie, never a Go
// pointer) plus a per-runtime magic id.
//
// A Runtime is not safe for concurrent use: QuickJS contexts are
// single-threaded, and Eval/Call/Get/Free must not overlap.
package js

/*
#cgo LDFLAGS: -lqjs
#include <stdlib.h>
#include "glue.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// DefaultTimeLimit bounds every Eval when the caller sets no limit (S5
// mitigation: real-world page scripts run without a sandbox, so an
// unbounded script must not hang the engine loop forever). It is generous
// on purpose: ordinary page scripts finish in milliseconds. Use
// SetTimeLimit to tighten it per runtime, or 0 to disable the bound.
const DefaultTimeLimit = 10 * time.Second

// Kind classifies a marshaled JS value.
type Kind int

const (
	KindUndefined Kind = iota
	KindNull
	KindBool
	KindNumber
	KindString
	KindObject
	KindFunction
)

// Value is a JS value marshaled to Go. Primitives are carried by value;
// objects and functions are carried as registry handles that stay valid
// until Free (or the Runtime is closed).
type Value struct {
	Kind Kind
	B    bool
	N    float64
	S    string
	H    Handle
}

// Constructors for primitive values.
func Undefined() Value        { return Value{Kind: KindUndefined} }
func Null() Value             { return Value{Kind: KindNull} }
func Bool(b bool) Value       { return Value{Kind: KindBool, B: b} }
func Number(n float64) Value  { return Value{Kind: KindNumber, N: n} }
func String(s string) Value   { return Value{Kind: KindString, S: s} }
func Object(h Handle) Value   { return Value{Kind: KindObject, H: h} }
func Function(h Handle) Value { return Value{Kind: KindFunction, H: h} }

// Equal reports whether two marshaled values denote the same JS value.
// Object and function handles compare by identity.
func (v Value) Equal(o Value) bool {
	if v.Kind != o.Kind {
		return false
	}
	switch v.Kind {
	case KindBool:
		return v.B == o.B
	case KindNumber:
		return v.N == o.N
	case KindString:
		return v.S == o.S
	case KindObject, KindFunction:
		return v.H == o.H
	default:
		return true
	}
}

// Handle addresses a JS object or function retained in the runtime's
// registry. The zero value is never valid.
type Handle int64

// Valid reports whether the handle could address a retained value.
func (h Handle) Valid() bool { return h != 0 }

// JSError is a JS evaluation failure in the oracle's exact stderr shape:
// "browser-lite: js error: <message>", where <message> is the coerced
// exception (Error objects stringify to "Name: message", thrown strings
// pass through). Matching the oracle byte for byte is what closes the 9
// KNOWN_DIFFS JS fixtures.
type JSError struct {
	Msg string
}

func (e *JSError) Error() string { return "browser-lite: js error: " + e.Msg }

// GoFunc is a Go function exposed on the JS global object. A non-nil
// error return becomes a thrown JS value, which Eval reports as a JSError
// (a *JSError keeps its message without doubling the prefix).
type GoFunc func(args []Value) (Value, error)

// Runtime is a QuickJS runtime plus one persistent context. Create with
// New, release with Close (which frees every retained handle).
type Runtime struct {
	rt  unsafe.Pointer
	ctx unsafe.Pointer

	cookie int64

	limitNanos   atomic.Int64 // per-Eval budget set by SetTimeLimit
	deadlineNano atomic.Int64 // active Eval's wall-clock deadline, 0 = none

	mu       sync.Mutex // guards funcs, names, objs below; callbacks run on the Eval thread
	funcs    map[int32]GoFunc
	names    map[string]int32
	nextFunc int32
	objs     map[Handle]*[16]byte
	nextH    Handle
	closed   bool
}

// runtimes maps integer cookies back to Go runtimes for the C callbacks.
// Only the cookie (never a Go pointer) crosses into C.
var runtimes sync.Map // int64 -> *Runtime

var cookieSeq atomic.Int64

// New creates a runtime with a fresh context, a console matching the
// oracle's "[js] ..." lines, and the default evaluation bound.
func New() (*Runtime, error) {
	rt := C.jsgo_new_runtime()
	if rt == nil {
		return nil, &JSError{Msg: "quickjs: JS_NewRuntime failed"}
	}
	ctx := C.jsgo_new_context(rt)
	if ctx == nil {
		C.jsgo_free_runtime(rt)
		return nil, &JSError{Msg: "quickjs: JS_NewContext failed"}
	}
	id := cookieSeq.Add(1)
	r := &Runtime{
		rt:       rt,
		ctx:      ctx,
		cookie:   id,
		funcs:    make(map[int32]GoFunc),
		names:    make(map[string]int32),
		nextFunc: 1,
		objs:     make(map[Handle]*[16]byte),
		nextH:    1,
	}
	r.limitNanos.Store(int64(DefaultTimeLimit))
	runtimes.Store(id, r)
	C.jsgo_set_opaque(ctx, C.jsgo_cookie_ptr(C.intptr_t(id)))
	C.jsgo_set_interrupt(rt, C.jsgo_cookie_ptr(C.intptr_t(id)))
	C.jsgo_install_console(ctx)
	return r, nil
}

// Close frees every retained handle, the context and the runtime. It is
// idempotent; using the runtime after Close is an error.
func (r *Runtime) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	for h, slot := range r.objs {
		C.jsgo_free_val(r.ctx, unsafe.Pointer(&slot[0]))
		delete(r.objs, h)
	}
	r.funcs = nil
	r.names = nil
	C.jsgo_clear_interrupt(r.rt)
	C.jsgo_free_context(r.ctx)
	C.jsgo_free_runtime(r.rt)
	runtimes.Delete(r.cookie)
	r.rt = nil
	r.ctx = nil
}

// SetTimeLimit sets the wall-clock budget for each Eval on this runtime.
// An overrunning script aborts with a JSError ("InternalError:
// interrupted" from the engine, wrapped in the oracle prefix). A zero or
// negative duration disables the bound; the default is DefaultTimeLimit.
func (r *Runtime) SetTimeLimit(d time.Duration) {
	if d <= 0 {
		r.limitNanos.Store(0)
		return
	}
	r.limitNanos.Store(int64(d))
}

// Eval runs src as a global script and marshals the completion value.
// A thrown exception or syntax error returns a *JSError in the oracle's
// byte-exact shape. Objects and functions come back as handles; every
// other value is carried by value.
func (r *Runtime) Eval(src, filename string) (Value, error) {
	if strings.IndexByte(src, byte(0)) >= 0 {
		return Undefined(), &JSError{Msg: "JS source contains NUL"}
	}
	if r.isClosed() {
		return Undefined(), errors.New("js: Eval on closed runtime")
	}
	if limit := r.limitNanos.Load(); limit > 0 {
		r.deadlineNano.Store(time.Now().Add(time.Duration(limit)).UnixNano())
	} else {
		r.deadlineNano.Store(0)
	}
	defer r.deadlineNano.Store(0)

	csrc := C.CString(src)
	defer C.free(unsafe.Pointer(csrc))
	cfile := C.CString(filename)
	defer C.free(unsafe.Pointer(cfile))

	var slot [16]byte
	C.jsgo_eval_into(r.ctx, csrc, C.size_t(len(src)), cfile,
		unsafe.Pointer(&slot[0]))
	if C.jsgo_is_exc(unsafe.Pointer(&slot[0])) != 0 {
		C.jsgo_free_val(r.ctx, unsafe.Pointer(&slot[0]))
		return Undefined(), r.takeException()
	}
	v := r.toValue(&slot)
	C.jsgo_free_val(r.ctx, unsafe.Pointer(&slot[0]))
	return v, nil
}

// Get reads a property off a retained object or function handle.
func (r *Runtime) Get(h Handle, prop string) (Value, error) {
	slot, err := r.lookup(h)
	if err != nil {
		return Undefined(), err
	}
	cprop := C.CString(prop)
	defer C.free(unsafe.Pointer(cprop))
	var out [16]byte
	C.jsgo_get_str_into(r.ctx, unsafe.Pointer(&slot[0]), cprop,
		unsafe.Pointer(&out[0]))
	if C.jsgo_is_exc(unsafe.Pointer(&out[0])) != 0 {
		C.jsgo_free_val(r.ctx, unsafe.Pointer(&out[0]))
		return Undefined(), r.takeException()
	}
	v := r.toValue(&out)
	C.jsgo_free_val(r.ctx, unsafe.Pointer(&out[0]))
	return v, nil
}

// Call invokes a retained function handle with the given arguments.
// Calling a non-function throws the engine's own TypeError, reported as a
// *JSError like any other script error.
func (r *Runtime) Call(h Handle, args ...Value) (Value, error) {
	fn, err := r.lookup(h)
	if err != nil {
		return Undefined(), err
	}
	argv := make([][16]byte, len(args))
	for i, a := range args {
		r.toSlot(a, &argv[i])
	}
	var out [16]byte
	var argvPtr unsafe.Pointer
	if len(argv) > 0 {
		argvPtr = unsafe.Pointer(&argv[0][0])
	}
	C.jsgo_call_into(r.ctx, unsafe.Pointer(&fn[0]), C.int(len(argv)),
		argvPtr, unsafe.Pointer(&out[0]))
	for i := range argv {
		C.jsgo_free_val(r.ctx, unsafe.Pointer(&argv[i][0]))
	}
	if C.jsgo_is_exc(unsafe.Pointer(&out[0])) != 0 {
		C.jsgo_free_val(r.ctx, unsafe.Pointer(&out[0]))
		return Undefined(), r.takeException()
	}
	v := r.toValue(&out)
	C.jsgo_free_val(r.ctx, unsafe.Pointer(&out[0]))
	return v, nil
}

// SetGlobalFunc attaches a Go function to the JS global object under name
// (the T2 registration point for document/window/timers). Re-registering
// a name replaces the binding and drops the previous callback.
func (r *Runtime) SetGlobalFunc(name string, fn GoFunc) error {
	if r.isClosed() {
		return errors.New("js: SetGlobalFunc on closed runtime")
	}
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	r.mu.Lock()
	magic := r.nextFunc
	r.nextFunc++
	if old, ok := r.names[name]; ok {
		delete(r.funcs, old)
	}
	r.funcs[magic] = fn
	r.names[name] = magic
	r.mu.Unlock()

	if C.jsgo_set_global_func(r.ctx, cname, C.int(magic)) < 0 {
		r.mu.Lock()
		delete(r.funcs, magic)
		if r.names[name] == magic {
			delete(r.names, name)
		}
		r.mu.Unlock()
		return &JSError{Msg: "quickjs: cannot define global function " + name}
	}
	return nil
}

// Free releases a retained handle. It is idempotent per handle only in
// the sense that a second Free reports an error instead of crashing;
// reading or calling a freed handle is an error.
func (r *Runtime) Free(h Handle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	slot, ok := r.objs[h]
	if !ok || r.closed {
		return
	}
	C.jsgo_free_val(r.ctx, unsafe.Pointer(&slot[0]))
	delete(r.objs, h)
}

// takeException converts the pending JS exception to a *JSError using the
// oracle's own recipe: coerce with JS_ToCString (Error objects stringify
// to "Name: message"). Call only when an exception is pending.
func (r *Runtime) takeException() error {
	var slot [16]byte
	C.jsgo_get_exc_into(r.ctx, unsafe.Pointer(&slot[0]))
	msg := r.coerce(&slot)
	C.jsgo_free_val(r.ctx, unsafe.Pointer(&slot[0]))
	if msg == "" {
		msg = "unknown JS error"
	}
	return &JSError{Msg: msg}
}

// toValue marshals a transient result slot to a Go value, retaining
// objects and functions in the registry. The caller still owns the slot.
func (r *Runtime) toValue(slot *[16]byte) Value {
	p := unsafe.Pointer(&slot[0])
	switch C.jsgo_kind(r.ctx, p) {
	case C.JSG_BOOL:
		return Bool(C.jsgo_bool_val(p) != 0)
	case C.JSG_NUMBER:
		return Number(float64(C.jsgo_float64(p)))
	case C.JSG_STRING:
		var n C.size_t
		cs := C.jsgo_cstr(r.ctx, p, &n)
		if cs == nil {
			return String("")
		}
		s := C.GoStringN(cs, C.int(n))
		C.jsgo_cstr_free(r.ctx, cs)
		return String(s)
	case C.JSG_OBJECT, C.JSG_FUNCTION:
		kind := KindObject
		if C.jsgo_kind(r.ctx, p) == C.JSG_FUNCTION {
			kind = KindFunction
		}
		h := r.retain(slot)
		return Value{Kind: kind, H: h}
	case C.JSG_NULL:
		return Null()
	default:
		return Undefined()
	}
}

// coerce stringifies any value slot (used for exceptions), copying the
// bytes into a Go string.
func (r *Runtime) coerce(slot *[16]byte) string {
	var n C.size_t
	cs := C.jsgo_cstr(r.ctx, unsafe.Pointer(&slot[0]), &n)
	if cs == nil {
		return ""
	}
	s := C.GoStringN(cs, C.int(n))
	C.jsgo_cstr_free(r.ctx, cs)
	return s
}

// retain dups a transient object/function slot into the registry.
func (r *Runtime) retain(slot *[16]byte) Handle {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.nextH
	r.nextH++
	kept := new([16]byte)
	C.jsgo_dup_into(r.ctx, unsafe.Pointer(&slot[0]),
		unsafe.Pointer(&kept[0]))
	r.objs[h] = kept
	return h
}

// lookup resolves a handle to its retained slot.
func (r *Runtime) lookup(h Handle) (*[16]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("js: runtime is closed")
	}
	slot, ok := r.objs[h]
	if !ok {
		return nil, fmt.Errorf("js: invalid handle %d", int64(h))
	}
	return slot, nil
}

func (r *Runtime) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// toSlot builds an owned argument slot: primitives via the make_*
// constructors, handles by dup. The caller frees the slot.
func (r *Runtime) toSlot(v Value, slot *[16]byte) {
	p := unsafe.Pointer(&slot[0])
	switch v.Kind {
	case KindBool:
		b := 0
		if v.B {
			b = 1
		}
		C.jsgo_make_bool(C.int(b), p)
	case KindNumber:
		C.jsgo_make_num(C.double(v.N), p)
	case KindString:
		cs := C.CString(v.S)
		defer C.free(unsafe.Pointer(cs))
		C.jsgo_make_str(r.ctx, cs, C.size_t(len(v.S)), p)
	case KindObject, KindFunction:
		r.mu.Lock()
		kept, ok := r.objs[v.H]
		if !ok || r.closed {
			r.mu.Unlock()
			C.jsgo_make_undefined(p)
			return
		}
		C.jsgo_dup_into(r.ctx, unsafe.Pointer(&kept[0]), p)
		r.mu.Unlock()
	case KindNull:
		C.jsgo_make_null(p)
	default:
		C.jsgo_make_undefined(p)
	}
}

//export jsgoGoCall
func jsgoGoCall(rtCookie unsafe.Pointer, magic C.int, ctx unsafe.Pointer,
	argc C.int, argv unsafe.Pointer, ret unsafe.Pointer) C.int {
	v, ok := runtimes.Load(int64(C.jsgo_cookie_id(rtCookie)))
	if !ok {
		C.jsgo_throw_str(ctx, C.CString("js: unknown runtime"))
		return 1
	}
	r := v.(*Runtime)
	r.mu.Lock()
	fn, ok := r.funcs[int32(magic)]
	r.mu.Unlock()
	if !ok {
		C.jsgo_throw_str(ctx, C.CString("js: unknown callback"))
		return 1
	}
	args := make([]Value, 0, int(argc))
	for i := 0; i < int(argc); i++ {
		var slot [16]byte
		C.jsgo_arg_at_into(argv, C.int(i), unsafe.Pointer(&slot[0]))
		args = append(args, r.toValue(&slot))
		C.jsgo_free_val(r.ctx, unsafe.Pointer(&slot[0]))
	}
	out, err := fn(args)
	if err != nil {
		var jserr *JSError
		msg := err.Error()
		if errors.As(err, &jserr) {
			msg = jserr.Msg
		}
		cmsg := C.CString(msg)
		defer C.free(unsafe.Pointer(cmsg))
		C.jsgo_throw_str(ctx, cmsg)
		return 1
	}
	r.valueToRet(out, ret)
	return 0
}

// valueToRet fills the callback return slot: primitives are constructed,
// handles are dup'd out of the registry.
func (r *Runtime) valueToRet(v Value, ret unsafe.Pointer) {
	switch v.Kind {
	case KindBool:
		b := 0
		if v.B {
			b = 1
		}
		C.jsgo_make_bool(C.int(b), ret)
	case KindNumber:
		C.jsgo_make_num(C.double(v.N), ret)
	case KindString:
		cs := C.CString(v.S)
		defer C.free(unsafe.Pointer(cs))
		C.jsgo_make_str(r.ctx, cs, C.size_t(len(v.S)), ret)
	case KindObject, KindFunction:
		r.mu.Lock()
		defer r.mu.Unlock()
		if kept, ok := r.objs[v.H]; ok && !r.closed {
			C.jsgo_ret_take(r.ctx, ret, unsafe.Pointer(&kept[0]))
		}
	case KindNull:
		C.jsgo_make_null(ret)
	default:
		C.jsgo_make_undefined(ret)
	}
}

//export jsgoExpired
func jsgoExpired(cookie unsafe.Pointer) C.int {
	v, ok := runtimes.Load(int64(C.jsgo_cookie_id(cookie)))
	if !ok {
		return 0
	}
	r := v.(*Runtime)
	if deadline := r.deadlineNano.Load(); deadline != 0 &&
		time.Now().UnixNano() > deadline {
		return 1
	}
	return 0
}
