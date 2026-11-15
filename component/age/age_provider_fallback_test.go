package age_test

import (
    "bytes"
    "testing"

    "github.com/metacubex/mihomo/component/age"
)

// A provider may omit its own key and still rely on configured global keys.
// This reproduces MetaCubeX/mihomo c3bdf779 without logging real key material.
func TestDecryptBytesEmptyProviderKeyFallsBackToGlobal(t *testing.T) {
    secret, public, err := age.GenX25519KeyPair()
    if err != nil { t.Fatal(err) }
    plaintext := []byte("encrypted subscription payload")
    ciphertext, err := age.EncryptBytes(plaintext, public)
    if err != nil { t.Fatal(err) }

    age.SetGlobalSecretKeys(secret)
    t.Cleanup(func() { age.SetGlobalSecretKeys() })

    got, err := age.DecryptBytes(ciphertext, "")
    if err != nil { t.Fatalf("empty provider key prevented global decryption: %v", err) }
    if !bytes.Equal(got, plaintext) {
        t.Fatalf("decrypted payload differs: got %q", got)
    }
    got, err = age.DecryptBytes(ciphertext, "", secret)
    if err != nil || !bytes.Equal(got, plaintext) {
        t.Fatalf("empty key before local key should be ignored: %v", err)
    }
}

func TestDecryptBytesInvalidNonemptyProviderKeyStillFails(t *testing.T) {
    secret, public, err := age.GenX25519KeyPair()
    if err != nil { t.Fatal(err) }
    ciphertext, err := age.EncryptBytes([]byte("payload"), public)
    if err != nil { t.Fatal(err) }
    age.SetGlobalSecretKeys(secret)
    t.Cleanup(func() { age.SetGlobalSecretKeys() })

    if _, err := age.DecryptBytes(ciphertext, "not-an-age-identity"); err == nil {
        t.Fatal("non-empty invalid key was silently accepted")
    }
}
