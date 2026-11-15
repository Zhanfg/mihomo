//go:build with_ebpf && (linux || android)
package ebpf
import ("os"; "strings"; "testing")
// Source contract guards all five native HOST bypass sites. Real kernel
// validation remains a separate privileged and device-only gate.
func TestNativeFakeIPHostPrecedenceContract(t *testing.T) {
    cg, err := os.ReadFile("native/cgroup.bpf.c")
    if err != nil { t.Fatal(err) }
    sh, err := os.ReadFile("native/shared_network_policy.h")
    if err != nil { t.Fatal(err) }
    cases := []struct{name, content, condition string; count int}{
        {"cgroup ipv4 both paths", string(cg), "if (!force_fakeip && (config->flags & SB_EBPF_CGROUP_FLAG_HOST_IPV4) != 0U && host_ipv4(destination)) return 1;", 2},
        {"cgroup ipv6", string(cg), "if (!force_fakeip && (config->flags & SB_EBPF_CGROUP_FLAG_HOST_IPV6) != 0U && host_ipv6(address)) return 1;", 1},
        {"shared ipv4", string(sh), "if (!force_fakeip && (map_policy_flags & SB_SHARED_FLAG_HOST_IPV4) != 0U &&", 1},
        {"shared ipv6", string(sh), "if (!force_fakeip && (map_policy_flags & SB_SHARED_FLAG_HOST_IPV6) != 0U &&", 1},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if got := strings.Count(tc.content, tc.condition); got != tc.count {
                t.Fatalf("HOST guarded native sites = %d, want %d", got, tc.count)
            }
        })
    }
    for _, required := range []string{
        "if (sb_ebpf_ipv4_safety_bypass(destination_bytes)) return 1;",
        "if (sb_ebpf_ipv6_safety_bypass((const __u8 *)address)) return 1;",
    } {
        if !strings.Contains(string(cg), required) { t.Fatal("cgroup mandatory safety bypass missing: ", required) }
    }
    for _, required := range []string{
        "if (sb_ebpf_ipv4_safety_bypass(destination)) return SB_SHARED_POLICY_BYPASS;",
        "if (sb_ebpf_ipv6_safety_bypass(destination)) return SB_SHARED_POLICY_BYPASS;",
    } {
        if !strings.Contains(string(sh), required) { t.Fatal("shared mandatory safety bypass missing: ", required) }
    }
}

func TestNativeFakeIPHostPriorityMatchesTC(t *testing.T) {
    tc, err := os.ReadFile("native/tc.bpf.c")
    if err != nil { t.Fatal(err) }
    for _, function := range []string{"local_selected", "shared_selected"} {
        prefix := "INLINE bool " + function + "("
        start := strings.Index(string(tc), prefix)
        if start < 0 { t.Fatalf("TC %s missing", function) }
        tail := string(tc)[start:]
        end := strings.Index(tail, "\n}\n")
        if end < 0 { t.Fatalf("TC %s missing closing brace", function) }
        body := tail[:end]
        fake := strings.Index(body, "must_intercept_fakeip(control, key)")
        host := strings.Index(body, "host_destination(control, key)")
        if fake < 0 || host < 0 || fake > host {
            t.Fatalf("TC %s Fake-IP must precede HOST bypass: fake=%d host=%d", function, fake, host)
        }
    }
}
