package generator

import (
    "encoding/base64"
    "testing"
)

func TestShadowsocksPasswordGeneratorLengths(t *testing.T) {
    var seen = map[string]bool{}
    for _, bits := range []int{128, 256, 512} {
        password, err := GenShadowsocksPassword(bits)
        if err != nil { t.Fatalf("bits %d: %v", bits, err) }
        decoded, err := base64.StdEncoding.DecodeString(password)
        if err != nil || len(decoded) != bits/8 {
            t.Fatalf("bits %d: encoded password size mismatch: len=%d err=%v", bits, len(decoded), err)
        }
        if seen[password] { t.Fatal("password collision in tested sizes") }
        seen[password] = true
    }
}
func TestShadowsocksPasswordRejectsPartialBytes(t *testing.T) {
    for _, bits := range []int{1, 7, 9, 129} {
        if _, err := GenShadowsocksPassword(bits); err == nil {
            t.Fatalf("non-byte aligned %d-bit password was accepted", bits)
        }
    }
}
