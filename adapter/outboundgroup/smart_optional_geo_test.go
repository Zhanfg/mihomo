package outboundgroup

import (
	"net/netip"
	"testing"

	C "github.com/metacubex/mihomo/constant"
)

func TestMissingASNDatabaseDoesNotTerminateSmart(t *testing.T) {
	oldHome := C.Path.HomeDir()
	C.SetHomeDir(t.TempDir())
	defer C.SetHomeDir(oldHome)

	s := &Smart{preferASN: true}
	metadata := &C.Metadata{DstIP: netip.MustParseAddr("1.1.1.1")}

	if got := s.getASNCode(metadata); got != "" {
		t.Fatalf("missing ASN database returned %q, want empty", got)
	}
	if metadata.DstIPASN != "unknown" {
		t.Fatalf("DstIPASN=%q, want unknown", metadata.DstIPASN)
	}
}
