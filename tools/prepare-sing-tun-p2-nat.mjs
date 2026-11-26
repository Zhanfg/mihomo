#!/usr/bin/env node
// P2 candidate: MetaCubeX/sing-tun 28b73db26483, pinned to v0.4.29.
// Copy the module *after* the existing Android/GRO overlays. Do not modify
// the verified module cache, repository go.mod/go.sum, or P0's modfile here.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import { execFileSync } from "node:child_process";

const moduleName = "github.com/metacubex/sing-tun";
const moduleVersion = "v0.4.29";
const expectedBlob = "bd0632a3a1e808be7a153bb508a7ecd5876107e0";
const githubEnv = process.env.GITHUB_ENV;
if (!githubEnv) throw new Error("Run in a candidate GitHub Actions build");
const go = (args, env = process.env) =>
  execFileSync("go", args, { encoding: "utf8", env }).trim();

const info = JSON.parse(go(["list", "-m", "-json", moduleName]));
if (info.Version !== moduleVersion || info.Replace || !info.Dir) {
  throw new Error("Unexpected sing-tun dependency; review NAT patch before upgrading");
}
const source = fs.readFileSync(path.join(info.Dir, "stack_system_nat.go"));
const blob = crypto.createHash("sha1")
  .update("blob " + source.length + "\0").update(source).digest("hex");
if (blob !== expectedBlob || fs.existsSync(path.join(info.Dir, "stack_system_nat_test.go"))) {
  throw new Error("Pinned NAT source or test layout changed; refusing unreviewed patch");
}

const flags = process.env.GOFLAGS || "";
const modfilePattern = /(?:^|\s)-modfile=([^\s]+)(?=\s|$)/g;
const matches = [...flags.matchAll(modfilePattern)];
if (matches.length > 1 || (flags.includes("-modfile") && matches.length !== 1)) {
  throw new Error("Unexpected GOFLAGS modfile syntax");
}
const activeMod = path.resolve(matches.length ? matches[0][1] : "go.mod");
if (!activeMod.endsWith(".mod")) throw new Error("Unexpected modfile extension");
const activeSum = activeMod.slice(0, -4) + ".sum";

const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "mihomo-tun-p2-"));
const localDir = path.join(workspace, "sing-tun");
const alternateMod = path.join(workspace, "p2.mod");
if (/\s/.test(workspace)) throw new Error("Temporary workspace path contains whitespace");
fs.copyFileSync(activeMod, alternateMod);
fs.copyFileSync(activeSum, path.join(workspace, "p2.sum"));
fs.cpSync(info.Dir, localDir, { recursive: true });
fs.chmodSync(localDir, 0o755);
fs.chmodSync(path.join(localDir, "stack_system_nat.go"), 0o644);
const patch = path.resolve(".github/patch/sing-tun-p2-nat.patch");
execFileSync("patch", ["-p1", "--batch", "--forward", "--fuzz=0", "-i", patch], {
  cwd: localDir, stdio: "inherit",
});
const patched = fs.readFileSync(path.join(localDir, "stack_system_nat.go"), "utf8");
if (!patched.includes("if len(n.portMap) == tcpNatPortCount {") ||
    !fs.existsSync(path.join(localDir, "stack_system_nat_test.go"))) {
  throw new Error("NAT source or regression test missing after patch");
}

// Retain P0's absolute local WireGuard replacements in the copied modfile.
const remaining = flags.replace(modfilePattern, "").trim();
const nextFlags = [remaining, "-modfile=" + alternateMod].filter(Boolean).join(" ");
const nextEnv = { ...process.env, GOFLAGS: nextFlags };
go(["mod", "edit", "-modfile=" + alternateMod, "-replace=" + moduleName + "=" + localDir], nextEnv);
const selected = JSON.parse(go(["list", "-m", "-json", moduleName], nextEnv));
if (!selected.Replace || selected.Replace.Dir !== localDir) {
  throw new Error("Build did not select the patched local sing-tun copy");
}
fs.appendFileSync(githubEnv, "GOFLAGS=" + nextFlags + "\n");
console.log("P2 TCP NAT candidate selected; source blob", blob, "upstream 28b73db26483");
