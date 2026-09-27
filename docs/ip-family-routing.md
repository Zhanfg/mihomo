# Custom IP-family routing

This fork extends the Smart/eBPF core with measured address-family capability
routing. The feature is implemented in Mihomo only; BoxProxy does not need to
be patched.

## Capability probes

The core probes each concrete proxy identity independently and caches the
result:

- IPv4 egress: `api4.ipify.org`, with `api-ipv4.ip.sb/ip` as failure fallback
- IPv6 egress: `api6.ipify.org`, with `api-ipv6.ip.sb/ip` as failure fallback
- UDP egress: STUN (existing capability probe)

Only the fallback endpoint is contacted when the primary family probe fails, so
normal steady-state probe traffic does not double.

The IPv4/IPv6 probes use non-mutating `StatusTest`, so they do not rewrite the
normal proxy health/alive history.

Current cache policy:

- probe timeout: 5 seconds
- successful verdict TTL: 30 minutes
- failed verdict TTL: 10 minutes
- maximum concurrent capability probes: 4 on Android, 8 elsewhere

## Directives

### `prefer-ipv4: true`

Soft preference. A confirmed IPv4-capable node ranks ahead of a node missing
IPv4, but the latter remains a last-resort candidate.

Supported by `url-test` and `smart`.

### `prefer-ipv6: true`

Existing directive, now backed by the same measured single-stack capability
model. It remains a soft preference.

### `require-ipv4: true`

Hard IPv4 egress requirement. Unknown or confirmed-missing IPv4 capability is
not eligible. Use `empty-fallback: REJECT` or `REJECT-DROP` when strict
fail-closed behavior is desired.

Supported by `url-test` and `smart`.

### `require-ipv6: true`

Hard IPv6 egress requirement. This replaces name-only filters such as
`IPv6|V6|双栈` when correctness matters.

Supported by `url-test` and `smart`.

### `auto-ip-family: true`

Smart-only dynamic routing. The group follows the actual destination/fake-IP
address family of each connection:

- IPv4 destination -> prefer/require IPv4-capable egress
- IPv6 destination -> prefer/require IPv6-capable egress
- unresolved/unknown family -> keep normal Smart behavior

During initial probe warm-up an unknown node remains eligible with a small
ranking cost. A confirmed family mismatch receives a very large ranking cost
and a stale cached/pinned winner is invalidated. It is deliberately not a hard
group-wide filter: if every probe endpoint is unavailable, normal Smart
fallback remains usable instead of blackholing the group.

## Examples

Strict IPv6 discovery without relying on node names:

```yaml
- name: IPv6节点
  type: url-test
  use:
    - main-a
    - main-b
    - main-c
    - main-d
    - main-e
  url: https://www.apple.com/library/test/success.html
  interval: 600
  lazy: false
  require-ipv6: true
  prefer-ipv6: true
  empty-fallback: REJECT
```

Strict dual-stack pool:

```yaml
- name: 双栈节点
  type: url-test
  use:
    - main-a
    - main-b
  require-ipv4: true
  require-ipv6: true
  empty-fallback: REJECT
```

Family-aware Smart routing:

```yaml
- name: 主力智能
  type: smart
  use:
    - main-a
    - main-b
    - main-c
    - main-d
    - main-e
  auto-ip-family: true
  empty-fallback: REJECT
```

These directives are optional. Configurations that do not use them preserve
their previous behavior.


## Country-coherent dual-stack routing

Smart groups also support measured exit-country routing without another GeoIP
database or a second per-target cache.

### `country-affinity: true`

Country affinity is scoped to one Smart target (service/rule target). The first
successful family records the measured two-letter exit country in the existing
unwrap pin. IPv4 and IPv6 sibling flows for that same target are then selected
from nodes whose measured exit for the requested family is in the same country.

Different services in the same Smart group may independently use different
countries. This avoids the old group-wide latch where one service could force
every other service into its country.

If that country has no viable node for the other family, only that target's pin
is released and Smart reselects; unrelated target affinities are untouched.

The core reuses the existing single-stack public-IP probes and the existing
country MMDB. The target pin stores only a two-letter country string, so no
extra resident country map, worker, or database is added.

### `country: JP`

An explicit ISO-3166 alpha-2 country remains a strict group constraint. It
overrides target-local affinity and fails closed through the group's
`empty-fallback` when no node in that country is eligible.

Example:

```yaml
- name: AI智能
  type: smart
  use: [main-a, main-b, main-c, main-d, main-e]
  auto-ip-family: true
  country-affinity: true
  empty-fallback: REJECT
```

For a service that first exits over IPv4 in Japan, later IPv6 flows for the same
Smart target are kept in Japan when an IPv6-capable Japanese exit exists.
