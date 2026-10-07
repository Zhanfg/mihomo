# CoreFlow IR (CFIR) v1 Architecture

Status: experimental foundation. CFIR is additive and does not replace the live Mihomo data plane yet.

## 1. Purpose

CFIR is a protocol-neutral, in-memory semantic IR for the universal networking core.
It is not a wire protocol and must never require payload serialization.

External protocols are codecs/adapters around CFIR. Routing, Smart/Neural decisions,
flow lifecycle, telemetry, security policy and platform selection consume CFIR
semantics rather than protocol names.

The long-term invariant is:

> Protocols, upstream implementations, TCP/IP stacks and operating systems may
> change independently; CFIR semantics and runtime invariants remain stable.

## 2. Stable core

CFIR v1 has four primitives:

- Stream
- Datagram
- Packet
- Session

Common behavior is expressed as capabilities, not protocol-name switches.

Standard capabilities occupy an append-only 256-bit namespace for O(1) checks.
Unusual or future capabilities use validated extension namespaces such as:

- `quic.path-migration-v2`
- `future.foo.parallel-path-stripe`

An extension may later graduate into a standard capability when multiple
protocols depend on the same semantic. Minor CFIR versions are append-only.
Breaking semantic changes require a new major version.

## 3. Protocol composition

A protocol is allowed to compose reusable layers:

- Security
- Transport
- Framing
- Session

Example:

`future-proto = tls13 + quic + future-frame + h3-session`

Layer capabilities are inherited into the protocol's effective descriptor.
A new protocol therefore implements only genuinely new semantics instead of
copying QUIC, TLS, mux, congestion-control or lifecycle glue.

Protocol composition is resolved at registration time. Missing or mismatched
layers are rejected before the protocol becomes visible.

## 4. Protocol SDK contract

New integrations should implement `ProtocolModule`, not the legacy
`ProtocolAdapter` migration interface.

Every module must declare:

- Protocol descriptor and minimum CFIR version
- Supported primitives
- Capabilities and extension capabilities
- Security profile
- Resource profile
- Reusable composition layers
- Conformance invariants

Required conformance includes cancellation, handshake deadline, idle deadline,
absolute lifetime, idempotent/concurrent close, backpressure, error
classification, network generation handling, memory pressure, security-context
preservation and prohibition of unowned global route mutation.

Primitive-specific invariants add stream half-close, datagram expiry, packet
MTU handling and session draining where applicable.

## 5. Security boundary

CFIR security data is semantic evidence, not protocol marketing metadata.

Protocol descriptors can state the security properties a wire protocol is
designed to provide. Runtime `SecurityContext` has a separate
`AttestedByCore` bit so untrusted adapters cannot claim that authentication,
encryption or replay protection was actually negotiated.

Execution plans carry a security floor. A candidate whose effective security
profile is below that floor is rejected rather than silently downgraded.

CFIR does not invent cryptography. Wire adapters should continue to use mature
primitives such as TLS 1.3, QUIC, Noise/WireGuard and audited AEADs.

## 6. Execution planning

Smart/Neural code should evolve from a node selector into an execution planner.

A plan can describe:

- protocol
- primitive
- required capabilities
- security floor
- IP-family policy
- congestion-control policy
- connection reuse
- multiplexing
- delayed hedging
- Network / Path / Socket generation

The planner asks which registered implementations satisfy the requirements.
It must not contain `if protocol == X` policy branches.

## 7. Upstream-provider boundary

Mihomo, a future Mihomo Next, sing-box, Xray or another upstream is a provider,
not the owner of the core architecture.

A provider bundle may contribute:

- protocol modules
- reusable protocol layers
- TUN/transport/route/resolver backends
- flow observers
- kernel accelerators
- crypto or congestion-control providers

Provider registration is atomic. All descriptors, conflicts, layer references
and CFIR-version requirements are validated before anything is published.

This changes upstream synchronization from source merging to semantic intake.

## 8. Upstream intake process

For a major upstream rewrite:

1. Mirror the upstream revision unchanged.
2. Generate a semantic/capability diff, not only a source diff.
3. Classify each improvement:
   - dependency/leaf update
   - semantic port
   - backend replacement
   - protocol/layer update
4. Implement or adapt only the useful capability behind CFIR contracts.
5. Run CFIR conformance.
6. Run differential/shadow tests against the old implementation.
7. Benchmark latency, throughput, CPU, allocations, RSS, power and recovery.
8. Promote only the implementation that wins the required gates.

A future Mihomo Next may therefore replace a TUN stack or transport backend
without forcing Smart, CFIR, platform semantics or unrelated protocols to move.

## 9. Five-platform boundary

Shared:

- CFIR
- Smart/Neural decision plane
- execution planner
- protocol composition
- DNS/routing semantics
- flow lifecycle
- telemetry schemas
- security policy

Platform backends:

- Android: VpnService/FD; optional rooted eBPF accelerator
- Linux: TUN/routing; optional TC/cgroup/eBPF accelerator
- Windows: Wintun/WFP/native route semantics
- macOS: utun/NetworkExtension
- iOS: NEPacketTunnelProvider/NetworkExtension

eBPF is an accelerator and sensor, not the brain. Its absence must not change
routing semantics.

iOS is a first-class platform rather than a generic Darwin build: current Go
toolchains require Apple external/cgo linking for ios/arm64, so CFIR CI uses a
native macOS/Xcode runner for that target.

## 10. Migration strategy

Migration is deliberately incremental.

Phase 0: CFIR contracts and tests only.
Phase 1: legacy Metadata <-> CFIR shadow bridge.
Phase 2: generate CFIR beside the existing live path and compare semantics.
Phase 3: move Smart/Router consumers to CFIR while legacy protocols remain
adapters.
Phase 4: decompose protocols into reusable Security/Transport/Framing/Session
layers.
Phase 5: platform backends consume the same execution plans.
Phase 6: remove legacy protocol-name coupling after differential gates prove
parity.

At no phase should a CFIR migration require a flag-day rewrite of all protocols.


## 11. Static build profiles

Logical plugin architecture does not imply dynamic loading.

CFIR build profiles select protocols and backends for a target platform. The
resolver automatically computes the closure of reusable layers required by the
selected protocols. A future registry generator can then emit only those static
imports, allowing the Go linker to remove unselected implementations.

Examples:

- Universal: all certified protocols + all target-compatible backends
- Android Lite: selected consumer protocols + VpnService backend
- Android Root: Lite + eBPF kernel accelerator
- Windows: selected protocols + Wintun/WFP backends
- Apple Lite: selected protocols + NetworkExtension backend

A profile cannot select a backend that does not declare support for the target
platform.

## 12. Semantic upstream manifests

Upstream intake snapshots protocol, layer and backend descriptors independently
of the upstream source tree.

The semantic diff classifies changes as:

- Additive: a new protocol/layer/backend
- Review: compatible semantic changes or newly added capabilities
- Breaking: removed protocols/layers/backends, removed capabilities, removed
  platform support, raised incompatible CFIR requirements, primitive loss or
  security-profile regression

This makes a large source rewrite manageable. Renaming packages or replacing an
implementation without changing its CFIR-visible semantics produces no false
architecture migration requirement.

## 13. Capability scopes

Protocol capabilities and platform/backend capabilities are separate.

Examples:

- ReliableDatagram, Multiplex and protocol PathMigration are protocol-side.
- ZeroCopy, kernel flow observation and platform route features can be
  backend-side.

An ExecutionPlan validates each requirement against the correct scope. A fast
backend is not allowed to make an otherwise incapable protocol appear to
support a wire semantic it cannot actually provide.

## 14. Deferred payload ABI

CFIR v1 deliberately does not freeze a new payload buffer or packet I/O ABI.

The live implementation can continue using standard Go connection interfaces
and existing optimized buffer/splice paths. A dedicated payload ABI will be
considered only after protocol-corpus and shadow testing prove the requirements
for QUIC datagrams, MASQUE, packet tunnels, multipath and future session types.

This avoids introducing an unnecessary encode/decode layer or freezing a
lowest-common-denominator buffer API before the capability model is mature.
