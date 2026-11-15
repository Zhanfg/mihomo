package provider

import "testing"

func TestSubscriptionInfoInvalidFieldKeepsValidData(t *testing.T) {
    got := NewSubscriptionInfo("upload=invalid;download=1024;total=2048;expire=3600")
    if got.Upload != 0 || got.Download != 1024 || got.Total != 2048 || got.Expire != 3600 {
        t.Fatalf("malformed field corrupted valid subscription info: %+v", got)
    }
}
