# BoxProxy integration contract

This branch treats the BoxProxy APK as an external control plane. The APK is
never patched, repacked, or redistributed by this repository.

## Maintenance boundary

Changes owned by this repository are limited to:

1. the Mihomo core; and
2. Mihomo source configuration.

BoxProxy itself is observed only to keep compatibility with the startup
configuration it generates. When BoxProxy updates, compatibility is re-tested
against its generated schema rather than carrying an APK patch forward.

## Current Mihomo eBPF contract

Current BoxProxy eBPF mode generates a Mihomo listener under `listeners:`,
rather than the old repository-local top-level `ebpf:` block.

Representative local-only shape:

```yaml
listeners:
  - name: ebpf-in
    type: ebpf
    local:
      include-uid: [10001, 10002] # or exclude-uid
      ipv6-mode: off              # written when IPv6 interception is disabled
    network: [tcp]                # omitted for TCP+UDP, or [udp]
```

The compatible core must therefore accept the native `type: ebpf` listener
surface, including the legacy `local.ipv6-mode` key that current BoxProxy
generators may emit.

The underlying liuran001 eBPF implementation also supports the newer local and
shared policy blocks, including UID/range/package policy, shared interfaces,
MAC policy, DNS policy, and cgroup/TC data-plane selection.

## Routing ownership

When BoxProxy selects full eBPF network mode, BoxProxy does not need its normal
TPROXY/REDIRECT iptables and policy-routing data plane. Do not reintroduce a
second transparent ingress from the core configuration.

CNIP's BoxBPF matcher is a separate feature used by non-eBPF modes; it must not
be confused with the full Mihomo eBPF listener.

## Configuration policy

Keep the source configuration mode-neutral where practical. Let BoxProxy
populate application UID filters and protocol selection. Advanced eBPF
behavior that BoxProxy intentionally preserves (for example
`local.data-plane`, `local.dns-mode`, or
`local.bypass-private-address`) may be declared in the source configuration.

Do not hard-code changes into BoxProxy to work around a core parser or data
plane deficiency. Fix the core or the source configuration instead.

## Validation gate

Before a new core is used on-device:

- parse a BoxProxy-shaped `listeners.type=ebpf` fixture;
- run eBPF listener tests;
- run Smart regressions;
- build Android arm64 with `with_gvisor with_ebpf`;
- verify Smart and eBPF symbols are present;
- then perform a read-only device capability audit before switching the live
  network mode.
