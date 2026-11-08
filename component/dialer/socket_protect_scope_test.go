package dialer

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
)

func TestSocketProtectFailedDialRollsBack(t *testing.T) {
	// Reserve then close a loopback port so dialContext passes through
	// ControlContext, registers a cookie-cleanup callback, and fails connect.
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback unavailable: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	var registrations, rollbacks atomic.Int32
	cancel := RegisterSocketProtectOwner(func(ctx context.Context, _, _ string, _ syscall.RawConn) error {
		registrations.Add(1)
		RegisterSocketProtectRollback(ctx, func() { rollbacks.Add(1) })
		return nil
	})
	defer cancel()

	conn, err := dialContext(context.Background(), "tcp4",
		netip.MustParseAddr("127.0.0.1"), strconv.Itoa(port), option{})
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Skip("another process occupied the recently released loopback port")
	}
	if registrations.Load() == 0 {
		t.Fatal("failed dial did not invoke socket-protect hook")
	}
	if got, want := rollbacks.Load(), registrations.Load(); got != want {
		t.Fatalf("failed dial left stale socket entries: rollbacks=%d registrations=%d", got, want)
	}
}

func TestSocketProtectSuccessfulDialKeepsRegistration(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback unavailable: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	var registrations, rollbacks atomic.Int32
	cancel := RegisterSocketProtectOwner(func(ctx context.Context, _, _ string, _ syscall.RawConn) error {
		registrations.Add(1)
		RegisterSocketProtectRollback(ctx, func() { rollbacks.Add(1) })
		return nil
	})
	defer cancel()

	conn, err := dialContext(context.Background(), "tcp4",
		netip.MustParseAddr("127.0.0.1"), strconv.Itoa(port), option{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if registrations.Load() == 0 {
		t.Fatal("successful dial did not register protect callback")
	}
	if n := rollbacks.Load(); n != 0 {
		t.Fatalf("successful dial rolled back live socket registration: %d", n)
	}
}

func TestSocketProtectRollbackIdempotent(t *testing.T) {
	cancel := RegisterSocketProtectOwner(func(context.Context, string, string, syscall.RawConn) error {
		return nil
	})
	defer cancel()
	ctx, scope := withSocketProtectScope(context.Background())
	if scope == nil {
		t.Fatal("active owner did not produce a dial scope")
	}
	var count int
	RegisterSocketProtectRollback(ctx, func() { count++ })
	scope.Rollback()
	scope.Rollback()
	if count != 1 {
		t.Fatalf("rollback cleanup ran %d times", count)
	}
}
