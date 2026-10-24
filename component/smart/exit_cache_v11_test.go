package smart

import (
	"fmt"
	"net/netip"
	"sync"
	"testing"
	"time"
)
func TestExitCacheFamilyIsolationAndExpiry(t *testing.T) {
	c := NewExitCache(nil)
	now := time.Unix(1700000000,0)
	c.Observe("cfg","g","node",false,netip.MustParseAddr("1.1.1.1"),"us",now)
	got,ok := c.Cached("cfg","g","node",false,now.Add(time.Minute))
	if !ok || got.IP != "1.1.1.1" || got.Region != "US" { t.Fatalf("bad exit %+v, %v",got,ok) }
	if _,ok=c.Cached("cfg","g","node",true,now);ok{t.Fatal("family leak")}
	if _,ok=c.Cached("cfg","other","node",false,now);ok{t.Fatal("group leak")}
	if _,ok=c.Cached("cfg","g","node",false,now.Add(exitIdentityTTL));ok{t.Fatal("expired cache")}
	c.Observe("cfg","g","node",true,netip.MustParseAddr("2606:4700:4700::1111"),"us",now)
	if v,ok:=c.Cached("cfg","g","node",true,now);!ok||v.IP==""{t.Fatal("no IPv6 observation")}
}
func TestExitCacheRejectMismatchedOrPrivateIP(t *testing.T) {
	c := NewExitCache(nil)
	now := time.Now()
	c.Observe("cfg","g","node",false,netip.MustParseAddr("2001:4860:4860::8888"),"us",now)
	if _,ok:=c.Cached("cfg","g","node",false,now);ok{t.Fatal("mismatched family")}
	c.Observe("cfg","g","node",false,netip.MustParseAddr("192.168.0.1"),"",now)
	if _,ok:=c.Cached("cfg","g","node",false,now);ok{t.Fatal("private IP")}
}
func TestExitCacheBoundedAndConcurrent(t *testing.T) {
	c := NewExitCache(nil)
	now := time.Unix(1700000000,0)
	var wg sync.WaitGroup
	for i:=0;i<32;i++{
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for k:=0;k<150;k++ {
				name:=fmt.Sprintf("node-%d-%d",i,k)
				c.Observe("cfg","g",name,false,netip.MustParseAddr("8.8.8.8"),"us",now.Add(time.Duration(k)*time.Second))
				_,_ = c.Cached("cfg","g",name,false,now)
			}
		}(i)
	}
	wg.Wait()
	if len(c.items)>exitIdentityMaxEntries {t.Fatalf("cache exceeds bound: %d",len(c.items))}
}
