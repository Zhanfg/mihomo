# BoxProxy local-core import contract

This project targets the BoxProxy Android application as an APK application, not as a
KernelSU/Magisk module.

## Verified application build

The inspected APK reports Git revision:

`1e2e00d2b9c1729d4946ed64daf7c3568d8efb70`

Its DEX contains:

- `com.box.app.CoreImportActivity`
- `core-import-stage`
- `core-import-install`
- `Mihomo Smart`
- the accepted Smart core filename pattern:
  `mihomo-android-arm64-v8-alpha-smart-[A-Fa-f0-9]+\.gz`

The APK also embeds the project URL `https://github.com/boxproxy/com.boxproxy.box`,
which is not publicly retrievable at the time this contract was recorded.

## Public Box runtime upstream

The public runtime/module repository is:

`https://github.com/boxproxy/box`

Box's runtime maps `mihomo_smart` to the normal executable name `mihomo`, and the
runtime target is:

`/data/adb/box/bin/mihomo`

Therefore the primary integration boundary is the Mihomo executable ABI and BoxProxy
local-core import format, not the Android UI implementation.

## Build contract

Primary user artifact:

`mihomo-android-arm64-v8-alpha-smart-<hex>.gz`

Requirements:

- GOOS: `android`
- GOARCH: `arm64`
- CGO: disabled
- gzip-compressed single Mihomo executable
- decompressed payload must be an AArch64 ELF
- filename must satisfy the BoxProxy Smart regex above
- the executable must retain normal Mihomo CLI/config compatibility
- Smart/eBPF features are compiled in by the project workflow

## Update model

When Mihomo/Smart upstream changes:

1. synchronize the source branch;
2. resolve source-level conflicts, if any;
3. run the existing full validation gates;
4. compile Android arm64 once;
5. package that one binary as the BoxProxy local-core `.gz`.

No APK rebuild or Root-module repackaging is required while this import contract remains
unchanged.

## Installation

Use BoxProxy's local core import UI and select the generated
`mihomo-android-arm64-v8-alpha-smart-<hex>.gz`.

The old Root-manager installer is only a compatibility/recovery path and is not the
primary BoxProxy installation method.
