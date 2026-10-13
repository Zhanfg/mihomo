package outboundgroup

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/adapter/outbound"
	C "github.com/metacubex/mihomo/constant"
)

func pinnedTestProxy(name string) C.Proxy {
	return outbound.NewDirectWithOption(outbound.DirectOption{Name: name})
}

func proxyNames(ps []C.Proxy) []string {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name())
	}
	return names
}

func TestFreshAfterPinnedFailure(t *testing.T) {
	a, b, c := pinnedTestProxy("a"), pinnedTestProxy("b"), pinnedTestProxy("c")
	for _, tc := range []struct {
		name string
		fresh []C.Proxy
		failed []C.Proxy
		want string
	}{
		{"single node survives", []C.Proxy{a}, []C.Proxy{a}, "a"},
		{"failed winner skipped", []C.Proxy{a, b, c}, []C.Proxy{a}, "b,c"},
		{"new ranking remains ordered", []C.Proxy{c, b, a}, []C.Proxy{a}, "c,b"},
		{"multiple failed nodes skipped", []C.Proxy{a, b, c}, []C.Proxy{a, b}, "c"},
		{"no exclusion when all failed", []C.Proxy{a, b}, []C.Proxy{a, b}, "a,b"},
		{"unrelated failure unchanged", []C.Proxy{b, c}, []C.Proxy{a}, "b,c"},
		{"no failed candidate", []C.Proxy{a, b}, nil, "a,b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := freshAfterPinnedFailure(tc.fresh, tc.failed)
			var joined string
			for i, n := range proxyNames(got) {
				if i != 0 { joined += "," }
				joined += n
			}
			if joined != tc.want {
				t.Fatalf("got %q; want %q", joined, tc.want)
			}
		})
	}
}

func TestGroupCanceledFailureNeverRequestsProbe(t *testing.T) {
	gb := NewGroupBase(GroupBaseOption{MaxFailedTimes: 1})
	var called atomic.Int32
	for i := 0; i < 20; i++ {
		gb.onDialFailed(C.Shadowsocks, fmt.Errorf("cancel: %w", context.Canceled), func() { called.Add(1) })
	}
	if gb.failedTimes != 0 || called.Load() != 0 {
		t.Fatal("canceled dial must not count or trigger a health probe")
	}
}

func TestGroupFailureBurstOnlySchedulesOneProbe(t *testing.T) {
	gb := NewGroupBase(GroupBaseOption{MaxFailedTimes: 1})
	var called atomic.Int32
	first := make(chan struct{}, 1)
	probe := func() {
		if called.Add(1) == 1 {
			first <- struct{}{}
		}
	}
	for i := 0; i < 300; i++ {
		gb.onDialFailed(C.Shadowsocks, errors.New("i/o timeout"), probe)
	}
	select {
	case <-first:
	case <-time.After(2 * time.Second):
		t.Fatal("first health probe never started")
	}
	if called.Load() != 1 {
		t.Fatalf("concurrent failure burst scheduled %d checks, want 1", called.Load())
	}
}

func BenchmarkGroupFailedDialCooldown(b *testing.B) {
	gb := NewGroupBase(GroupBaseOption{MaxFailedTimes: 1})
	gb.lastAutoHealthCheck = time.Now()
	err := errors.New("i/o timeout")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gb.onDialFailed(C.Shadowsocks, err, func() { panic("cooldown ignored") })
	}
}
