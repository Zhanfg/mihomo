# BoxProxy APK integration

This branch targets the **BoxProxy Android APK local-core import path**. It does
not require flashing the APK or this Mihomo build as a Magisk/KernelSU module.

## Upstream split

- BoxProxy Android app: the supplied APK embeds
  `https://github.com/boxproxy/com.boxproxy.box`, but that implementation
  repository is not publicly resolvable at the time this integration was made.
- Public app landing repository: `boxproxy/app` (README only).
- Native runtime used by the APK: `boxproxy/boxproxy` (`boxctl` + `boxbpf`).
- Legacy/root-module runtime: `boxproxy/box`.

For compatibility, this branch tracks the public native runtime contract in
`boxproxy/boxproxy`.

## Runtime contract

The native runtime currently defaults to:

```
/data/user/0/com.boxproxy.box/files/box
├── bin/
│   ├── boxctl
│   ├── boxbpf
│   └── mihomo
├── mihomo/
└── run/
```

`boxctl` launches Mihomo as:

```
mihomo -d <box-home>/mihomo -f <box-home>/run/state/startup-config
```

## Build once

Run:

```sh
./tools/build-boxproxy-app-core.sh
```

No command-line arguments are required.

The importable file is written to:

```
dist/boxproxy-app/mihomo-android-arm64-v8-alpha-smart-<sha>.gz
```

That filename matches the Mihomo Smart filename pattern embedded in the supplied
BoxProxy APK.

## Install

Use BoxProxy's **local core import** UI and select the generated `.gz` file.
The APK contains a dedicated `CoreImportActivity` using Android's
`OPEN_DOCUMENT` flow and stages/installs a selected core file.

Do not flash the APK or this core archive as a KernelSU/Magisk module.

## Upstream updates

When `boxproxy/boxproxy` changes, rerun the **BoxProxy APK Smart Core** workflow.
The workflow resolves the current upstream runtime commit and verifies the
runtime-home, core-path, and Mihomo launch contract before compiling the Smart
core. If BoxProxy changes that contract, the compatibility gate fails instead
of silently producing an incompatible core.
