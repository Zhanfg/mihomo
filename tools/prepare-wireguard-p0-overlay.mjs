// P0: compile three pinned WireGuard-family forks with the upstream
// IPv6 length overflow fix, without modifying Go's verified module cache.
// Go intentionally forbids overlays directly beneath GOMODCACHE. We instead
// copy the three modules to a temporary directory and pass an alternate
// go.mod (-modfile) with local replacements for this CI job only.
// Source: WireGuard/wireguard-go b3bc704ef0ee (2026-10-24).
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";

const modules = [
  "github.com/metacubex/wireguard-go",
  "github.com/metacubex/amneziawg-go",
  "github.com/metacubex/tailscale-wireguard-go",
];
const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "mihomo-wg-p0-"));
const originalMod = path.join(process.cwd(), "go.mod");
const originalSum = path.join(process.cwd(), "go.sum");
if (!fs.existsSync(originalMod) || !fs.existsSync(originalSum)) {
  throw new Error("Run at the mihomo repository root");
}
const alternateMod = path.join(workspace, "p0.mod");
fs.copyFileSync(originalMod, alternateMod);
fs.copyFileSync(originalSum, path.join(workspace, "p0.sum"));
const vulnerable = /(field := elem\.packet\[IPv6offsetPayloadLength\s*:\s*IPv6offsetPayloadLength\+2\]\r?\n[ \t]*length := )binary\.BigEndian\.Uint16\(field\)/g;
const fixed = /(field := elem\.packet\[IPv6offsetPayloadLength\s*:\s*IPv6offsetPayloadLength\+2\]\r?\n[ \t]*length := )uint32\(binary\.BigEndian\.Uint16\(field\)\)/g;

let patches = 0;
for (const moduleName of modules) {
  execFileSync("go", ["mod", "download", moduleName], { stdio: "pipe" });
  const moduleDir = execFileSync("go", ["list", "-m", "-f", "{{.Dir}}", moduleName], { encoding: "utf8" }).trim();
  if (!moduleDir) throw new Error("Cannot locate pinned module " + moduleName);
  const source = path.join(moduleDir, "device", "receive.go");
  const original = fs.readFileSync(source, "utf8");
  const oldMatches = Array.from(original.matchAll(vulnerable));
  const newMatches = Array.from(original.matchAll(fixed));
  if (oldMatches.length === 0 && newMatches.length === 1) {
    console.log("Already fixed:", moduleName);
    continue;
  }
  if (oldMatches.length !== 1 || newMatches.length !== 0) {
    throw new Error("Unexpected IPv6 receiver layout: " + moduleName);
  }
  const localDir = path.join(workspace, moduleName.split("/").pop());
  fs.cpSync(moduleDir, localDir, { recursive: true });
  const localFile = path.join(localDir, "device", "receive.go");
  fs.chmodSync(localFile, 0o644);
  const patched = original.replace(vulnerable, "$1uint32(binary.BigEndian.Uint16(field))");
  fs.writeFileSync(localFile, patched, "utf8");
  const replacementPath = localDir.split(path.sep).join("/");
  execFileSync("go", ["mod", "edit", "-modfile=" + alternateMod, "-replace=" + moduleName + "=" + replacementPath], { stdio: "pipe" });
  console.log("P0 fixed copy:", moduleName, crypto.createHash("sha256").update(patched).digest("hex").slice(0, 16));
  patches++;
}
if (patches === 0) {
  console.log("All three pinned receiver dependencies already fixed.");
  process.exit(0);
}
const goflags = [process.env.GOFLAGS, "-modfile=" + alternateMod.split(path.sep).join("/")].filter(Boolean).join(" ");
if (process.env.GITHUB_ENV) {
  fs.appendFileSync(process.env.GITHUB_ENV, "GOFLAGS=" + goflags + "\n");
} else {
  console.log("Build with: export GOFLAGS=" + JSON.stringify(goflags));
}
console.log("Local fork replacements:", patches, "(upstream modules unchanged)");
