package dialer

import (
	"context"
	"syscall"
	"errors"
	"sync"
	"testing"
)

func TestSocketProtectOwnersRemainIndependent(t *testing.T) {
	UnregisterSocketProtectFunc()
	var mu sync.Mutex
	called := make(map[string]int)
	owner := func(name string) func() {
		return RegisterSocketProtectOwner(func(_ context.Context, _, _ string, _ syscall.RawConn) error {
			mu.Lock()
			called[name]++
			mu.Unlock()
			return nil
		})
	}
	closeA := owner("A")
	closeB := owner("B")
	defer closeA()
	defer closeB()
	if err := ApplySocketProtect("tcp", "127.0.0.1:443", nil); err != nil { t.Fatal(err) }
	closeA()
	closeA() // cleanup is idempotent
	if err := ApplySocketProtect("tcp", "127.0.0.1:443", nil); err != nil { t.Fatal(err) }
	mu.Lock()
	defer mu.Unlock()
	if called["A"] != 1 || called["B"] != 2 { t.Fatalf("owners after cleanup: %+v", called) }
}

func TestSocketProtectOwnerDoesNotRemoveLegacyHook(t *testing.T) {
	var legacy, owned int
	RegisterSocketProtectFunc(func(_ context.Context, _, _ string, _ syscall.RawConn) error { legacy++; return nil })
	defer UnregisterSocketProtectFunc()
	cancel := RegisterSocketProtectOwner(func(_ context.Context, _, _ string, _ syscall.RawConn) error { owned++; return nil })
	if err := ApplySocketProtect("tcp", "example.com:443", nil); err != nil { t.Fatal(err) }
	cancel()
	if err := ApplySocketProtect("tcp", "example.com:443", nil); err != nil { t.Fatal(err) }
	if legacy != 2 || owned != 1 { t.Fatalf("legacy=%d owned=%d", legacy, owned) }
}

func TestSocketProtectOwnerErrorPropagates(t *testing.T) {
	want := errors.New("register failed")
	cancel := RegisterSocketProtectOwner(func(_ context.Context, _, _ string, _ syscall.RawConn) error { return want })
	defer cancel()
	if err := ApplySocketProtect("udp", "127.0.0.1:53", nil); !errors.Is(err, want) { t.Fatalf("err=%v", err) }
}
