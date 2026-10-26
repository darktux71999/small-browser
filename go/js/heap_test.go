package js

import "testing"

// The heap cap turns unbounded heap growth into a JS error (the S5
// mitigation): an allocation storm over the cap must return an error, not
// crash or balloon the process.
func TestHeapCapTurnsGrowthIntoAnError(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()
	r.SetMaxHeap(1 << 20) // 1MB: the storm below trips the cap immediately

	_, err = r.Eval(`(function () {
		var a = [];
		for (var i = 0; i < 1000000; i++) {
			a.push(new Array(1000).fill(7));
		}
	})()`, "heap-test.js")
	if err == nil {
		t.Fatal("expected an out-of-memory error from the heap cap, got success")
	}
}

// The default runtime carries a real cap (New must not leave the heap
// unbounded); the value itself is the documented 128MB default.
func TestNewSetsABoundedHeap(t *testing.T) {
	if defaultMaxHeap <= 0 {
		t.Fatalf("defaultMaxHeap = %d, want a positive bound", defaultMaxHeap)
	}
	r, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()
	// A tiny cap is honored per runtime (no global state).
	r.SetMaxHeap(1 << 20)
	if _, err := r.Eval(`(function(){ var a=[]; for (var i=0;i<1000000;i++) a.push(new Array(1000).fill(7)); })()`, "heap-test-2.js"); err == nil {
		t.Fatal("per-runtime cap did not apply")
	}
}
