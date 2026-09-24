package outboundgroup

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/component/loopback"
	C "github.com/metacubex/mihomo/constant"
)

func TestSmartParallelDialContextStopsOnFatalError(t *testing.T) {
	s := &Smart{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	proxies := make([]C.Proxy, 2)
	var calls atomic.Int32

	started := time.Now()
	_, conn, _, err := s.ParallelDialContext(
		ctx,
		proxies,
		&C.Metadata{},
		started,
		func(ctx context.Context, _ C.Proxy, _ *C.Metadata, _ time.Time) (C.Conn, int64, error) {
			if calls.Add(1) == 1 {
				return nil, 0, fmt.Errorf("wrapped fatal dial error: %w", loopback.ErrReject)
			}
			<-ctx.Done()
			return nil, 0, ctx.Err()
		},
	)

	if conn != nil {
		t.Fatal("unexpected connection returned")
	}
	if !errors.Is(err, loopback.ErrReject) {
		t.Fatalf("expected loopback.ErrReject, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("fatal error was masked until sibling timeout: elapsed=%s", elapsed)
	}
}
