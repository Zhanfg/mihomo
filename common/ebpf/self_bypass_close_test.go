//go:build with_ebpf && (linux || android)

package ebpf

import (
    "errors"
    "testing"

    "github.com/cilium/ebpf/link"
)

type failCloseSelfBypassLink struct {
    link.Link
    closeErr error
    closes int
}

func (f *failCloseSelfBypassLink) Close() error {
    f.closes++
    return f.closeErr
}

func TestSelfBypassKeepsFailedDetachForRetry(t *testing.T) {
    want := errors.New("detach failed")
    stub := &failCloseSelfBypassLink{closeErr: want}
    owner := &SelfBypass{links: []link.Link{stub}}
    if err := owner.Close(); !errors.Is(err, want) {
        t.Fatalf("Close error = %v, want %v", err, want)
    }
    if owner.links[0] != stub || stub.closes != 1 {
        t.Fatalf("failed detach dropped link reference: link=%v closes=%d", owner.links[0], stub.closes)
    }
    stub.closeErr = nil
    if err := owner.Close(); err != nil {
        t.Fatalf("Close retry failed: %v", err)
    }
    if owner.links[0] != nil || stub.closes != 2 {
        t.Fatalf("retry did not release link: link=%v closes=%d", owner.links[0], stub.closes)
    }
}
