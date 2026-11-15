package tls

import "testing"

func TestUTLSRandomizedX25519Options(t *testing.T) {
    hello, ok := GetFingerprint("randomized")
    if !ok || hello.Weights == nil {
        t.Fatal("randomized fingerprint has no weights")
    }
    weights := hello.Weights
    if weights.TLSVersMax_Set_VersionTLS13 != 1 ||
       weights.FirstKeyShare_Set_CurveP256 != 0 ||
       weights.CurveIDs_Append_X25519 != 1 ||
       weights.KeyShare_Append_RandomGroups != 1 {
        t.Fatalf("unexpected randomized fingerprint curve and keyshare weights: %+v", *weights)
    }
}
