#!/usr/bin/env node
// P1: guard and backport MetaCubeX/sing-tun commit 8daae1bf1858.
// CI-only module-cache mutation, never modifies go.mod/go.sum or the host.
// Exact pinned version and one-time source match prevent accidental patching
// of unrelated future dependency versions.
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";

const moduleName = "github.com/metacubex/sing-tun";
const moduleInfo = execFileSync("go", ["list", "-m", "-f", "{{.Version}}", moduleName], {encoding:"utf8"}).trim();
if (moduleInfo !== "v0.4.29") {
  throw new Error("Unexpected sing-tun version " + moduleInfo + "; review GRO patch before upgrading");
}
execFileSync("go", ["mod", "download", moduleName], {stdio:"pipe"});
const cache = execFileSync("go", ["env", "GOMODCACHE"], {encoding:"utf8"}).trim();
const filename = path.join(cache, moduleName + "@" + moduleInfo, "tun_offload_linux.go");
let source = fs.readFileSync(filename,"utf8");
const signature = "func handleGRO(bufs [][]byte, offset int, tcpTable *tcpGROTable, udpTable *udpGROTable, gro groDisablementFlags, skipChecksumValidation bool, toWrite *[]int) error {\n";
const addition = `\tif len(bufs) == 1 {
\t\tif offset < virtioNetHdrLen || offset > len(bufs[0])-1 {
\t\t\treturn errors.New("invalid offset")
\t\t}
\t\t// A single packet cannot coalesce. Avoid classifying or walking GRO maps.
\t\thdr := virtioNetHdr{}
\t\tif err := hdr.encode(bufs[0][offset-virtioNetHdrLen:]); err != nil {
\t\t\treturn err
\t\t}
\t\t*toWrite = append(*toWrite, 0)
\t\treturn nil
\t}
`;
if (source.includes(signature + addition)) {
  console.log("P1 sing-tun GRO single-packet optimization already applied");
  process.exit(0);
}
if (source.split(signature).length !== 2 || !source.includes(signature + "\tfor i := range bufs {")) {
  throw new Error("sing-tun GRO source differs; refusing an unreviewed patch");
}
source = source.replace(signature + "\tfor i := range bufs {", signature + addition + "\tfor i := range bufs {");
fs.chmodSync(filename, 0o644);
fs.writeFileSync(filename, source, "utf8");
const updated = fs.readFileSync(filename,"utf8");
if (!updated.includes(signature + addition)) {
  throw new Error("GRO single-packet patch verification failed");
}
console.log("P1 sing-tun GRO single-packet optimization installed, v0.4.29, upstream 8daae1bf1858");
