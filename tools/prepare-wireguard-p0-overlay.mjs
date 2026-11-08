// P0 dependency fix: compile three pinned WireGuard-family forks with the
// upstream IPv6 payload-length overflow correction, without mutating Go's
// checksummed module cache. Go's -overlay flag is scoped to this build.
// Upstream: WireGuard/wireguard-go b3bc704ef0ee (2026-10-24).
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
const replacements = {};
const vulnerable = /(field := elem\.packet\[IPv6offsetPayloadLength\s*:\s*IPv6offsetPayloadLength\+2\]\r?\n[ \t]*length := )binary\.BigEndian\.Uint16\(field\)/g;
const fixed = /(field := elem\.packet\[IPv6offsetPayloadLength\s*:\s*IPv6offsetPayloadLength\+2\]\r?\n[ \t]*length := )uint32\(binary\.BigEndian\.Uint16\(field\)\)/g;

for (const moduleName of modules) {
  execFileSync("go", ["mod", "download", moduleName], { stdio: "pipe" });
  const moduleDir = execFileSync("go", ["list", "-m", "-f", "{{.Dir}}", moduleName], { encoding: "utf8" }).trim();
  if (!moduleDir) throw new Error("Cannot locate pinned module " + moduleName);
  const input = path.join(moduleDir, "device", "receive.go");
  const original = fs.readFileSync(input, "utf8");
  const oldMatches = Array.from(original.matchAll(vulnerable));
  const newMatches = Array.from(original.matchAll(fixed));
  if (oldMatches.length === 0 && newMatches.length === 1) {
    console.log("Already fixed:", moduleName);
    continue;
  }
  if (oldMatches.length !== 1 || newMatches.length !== 0) {
    throw new Error("Unexpected IPv6 receiver layout; refusing unsafe patch: " + moduleName);
  }
  const patched = original.replace(vulnerable, "$1uint32(binary.BigEndian.Uint16(field))");
  const output = path.join(workspace, moduleName.split("/").pop() + "-receive.go");
  fs.writeFileSync(output, patched, "utf8");
  replacements[input] = output;
  console.log("WireGuard P0 overlay:", moduleName, crypto.createHash("sha256").update(patched).digest("hex").slice(0, 16));
}
if (Object.keys(replacements).length === 0) {
  console.log("All referenced WireGuard receivers already fixed; no overlay needed.");
  process.exit(0);
}
const overlay = path.join(workspace, "overlay.json");
fs.writeFileSync(overlay, JSON.stringify({ Replace: replacements }, null, 2) + "\n");
const goflags = [process.env.GOFLAGS, "-overlay=" + overlay].filter(Boolean).join(" ");
if (process.env.GITHUB_ENV) {
  fs.appendFileSync(process.env.GITHUB_ENV, "GOFLAGS=" + goflags + "\n");
} else {
  console.log("Apply before building: export GOFLAGS=" + JSON.stringify(goflags));
}
console.log("Active patched receivers:", Object.keys(replacements).length);
