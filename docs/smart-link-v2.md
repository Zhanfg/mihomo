# Smart Link v2 architecture

This branch treats mobile proxy selection as four separate problems:

1. **local network state** — what the Android/Linux host can actually observe;
2. **tunnel path** — phone -> proxy-node transport evidence;
3. **egress identity** — what the proxy node actually exposes to the Internet;
4. **end-to-end service quality** — target-specific Smart history.

Do not collapse these layers into one "latency" number. They have different
lifetimes, scopes and truth guarantees.

## Module boundaries

| Module | Owns | Must not own |
| --- | --- | --- |
| `component/netstate` | network epoch, passive local interface snapshot | sockets, remote probes, Smart policy |
| `component/linkprofile` | transport-independent path metrics/scoring | proxy database, provider lifecycle |
| `adapter/path_profile.go` | per-proxy transient tunnel/egress evidence | target-specific Smart history |
| `adapter/capability.go` | measured family/UDP/egress capability evidence | Smart ranking policy |
| `component/smart` | target history, active/dirty target index, persisted learning | physical-link truth |
| `adapter/outboundgroup` | Smart selection, hysteresis, adaptive sampling | provider-global health scheduling |
| `adapter/provider` | provider health-check scheduler and probe budgets | Smart target selection |
| `component/netchange` | handover invalidation/debounce | node scoring |

## Complexity budget

Use:

- `N`: proxies in a provider/group;
- `T`: persisted Smart targets;
- `K`: candidates Smart can actually return, currently <= 10;
- `D`: targets dirtied by traffic since the previous prefetch;
- `A`: runtime-active targets, capped at 4096.

### Selection

Old cache-miss behavior could load all persisted Smart records before selecting
one target, then sort all candidate nodes:

- time: approximately `O(T*N + N log N)`;
- temporary memory: `O(T*N + N)` depending on store/cache state.

Smart Link v2:

- reads only the requested target;
- keeps top-K node weights with a bounded heap: `O(N log K)`;
- provider fallback uses bounded insertion selection: `O(N*K)`, `O(K)`
  temporary memory.

Because `K <= 10`, both current selection paths are effectively linear in
provider size.

### Active-target discovery and prefetch

Old periodic discovery scanned persisted target/node records.

Smart Link v2 records activity during real traffic:

- touch: expected `O(1)`;
- runtime index memory: `O(min(A, 4096))`;
- dirty prefetch: proportional to `D`, not all historical `T`;
- no work when no target changed.

### Telemetry

Stable Android closes are information-redundant. Full Smart telemetry includes
locking, model inference, JSON serialization and persistent queue work.

Current policy:

- failures and abnormal/high-information samples: 100%;
- long or >=2 MiB flows: 100%;
- high RTT/jitter/loss: 100%;
- stable short TCP successes: first sample + 1/4 thereafter;
- stable short UDP successes: first sample + 1/2 thereafter.

Sampling state is a fixed 256-counter stripe table, not an unbounded
target/node map.

This is a reduction in **heavy telemetry work**, not a claim that whole-device
battery consumption falls by the same percentage.

### Capability probes

Soft ranking is probe-free. Stale/unknown candidates are not all actively
tested during one selection.

Active verification belongs to:

- explicit hard requirements/diagnostics; or
- the node that actually wins traffic.

Android active capability-probe concurrency is capped at 2.

### Provider health checks

On Android:

- handover changes advance the network epoch immediately;
- an 8 s settle window coalesces Wi-Fi/cellular flapping;
- automatic providers receive their own coalesced scheduled-health request;
- synchronous fallback is retained for providers without an automatic loop;
- per-provider URLTest concurrency is 2;
- process-wide URLTest probe budget is 4.

Desktop behavior remains unchanged unless explicitly stated otherwise.

## Evidence model

### Local / physical host

Measured without remote traffic:

- network epoch;
- monitored default interface;
- MTU;
- IPv4/IPv6 availability.

These values describe the terminal, not the proxy node.

### Tunnel: phone -> node

Passive `TCP_INFO` currently contributes:

- smoothed RTT;
- RTT variance;
- retransmission/loss evidence;
- unacked packets;
- congestion window pressure.

Tunnel evidence is **transient and epoch-scoped**. Wi-Fi measurements are
invalid immediately after a cellular handover and are never persisted as
target history.

### Node egress

Measured through the selected proxy:

- IPv4/IPv6 public exit IP;
- independent HTTP egress sources;
- source agreement/divergence;
- country and ASN from local databases;
- UDP availability;
- STUN-observed UDP public mapping;
- TCP/UDP egress split.

Two independent HTTP services agreeing on an exit is stronger evidence than
one endpoint. A TCP/UDP split is exposed as evidence, not automatically
classified as malicious because legitimate providers may use separate NAT
pools.

### End-to-end service quality

Smart retains target-specific observed behavior such as connection success,
first response latency, throughput and loss. This is **not** interchangeable
with tunnel RTT: server latency and node-to-target routing are part of it.

## Truth rules

1. Never label inferred data as measured.
2. Never carry physical-link metrics across a network epoch.
3. Reading diagnostics must not itself trigger probes.
4. Soft ranking must not create an all-node probe storm.
5. Missing evidence is represented as unknown, not as a synthetic "good" value.
6. A single public probe endpoint is not sufficient for high-confidence node
   identity when corroboration is available.
7. TCP and UDP may have different exits; expose the distinction.

## What cannot be fully observed from a generic client

Without cooperation from the proxy server, a client cannot directly read:

- the node's kernel TCP/QUIC state for node -> destination;
- the node's routing table or exact upstream path;
- node-side PMTU after the tunnel;
- node-side resolver internals;
- radio/signal data that Android does not expose to the native core.

The current design therefore performs evidence-backed reconstruction rather
than fabricating a "complete simulation".

A future cooperative node observer should plug into `component/linkprofile`
and provide signed/validated remote observations. It should not bypass the
existing local/tunnel/egress/end-to-end segmentation.

## Extension points

Recommended future sources:

- Android host bridge: transport type, metered state, validated/captive state,
  Wi-Fi RSSI/link speed and cellular signal class;
- QUIC/TUIC/Hysteria transport stats;
- eBPF passive RTT/retransmission histograms;
- PMTU evidence collected only when confidence is low;
- node-side cooperative route/TCP/QUIC/DNS observer.

Any new source must define its cost, expiry, network-epoch behavior and whether
it is measured, inferred or corroborated before it is allowed to influence
selection.
