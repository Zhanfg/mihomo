# Fake-IP / LPM 专项审计（2026-11-15）

范围：`Zhanfg/mihomo` P1 基线，PR #30 基线 SHA `4a3d92f6153b627dfdacc36c8a068aa06922d8a0`。本审计不自动修改 BPF ABI 或生成对象，不代表真机/特权验证完成。

## 结论与设计

- **不建议新增动态 Fake-IP LPM Trie**：当前 DNS Fake-IP 每族发布一个 `netip.Prefix`，控制结构已使用前缀和位掩码直接匹配。新增第二套 Trie 会引入生命周期、状态一致性和 BPF ABI 风险，而没有证明能减少现有流的处理成本。
- 现有 LPM Trie 服务于按目标 IP 的 bypass/host/按来源地址 include/exclude 规则，独立于 Fake-IP 前缀标志。`tc.go` 中的 map override 明确设置 `BPF_MAP_TYPE_LPM_TRIE` 以及 `BPF_F_NO_PREALLOC`。
- Fake-IP 前缀进入 `PolicyConfig` 的 IPv4/IPv6 两个字段；`listener/sing_ebpf/fakeip.go` 检查必需安全地址并进行 `Masked()`，然后在控制记录写入匹配位和前缀。
- 当前 Go 侧分别编码 TC、cgroup 与 shared packet-rewrite 的 Fake-IP 控制字段。共同 C helper 是 `native/fakeip_policy.h`。
- DNS 变更经 `component/resolver/fakeip_range.go` 的串行观察者传播；监听器先注册回调再读取快照，失败通过 `fakeIPRangeNeedsRetry` 通知重试。
- `fakeip_icmp` 是独立 native object 与 control map；更改范围时也必须更新 responder，失败时要考虑不同平面的部分成功及回退。

## 内核数据路径与优先级

| 路径 | Fake-IP 匹配 | LPM bypass 顺序 | 已见问题 |
| --- | --- | --- | --- |
| `native/tc.bpf.c` 本地/共享 | `must_intercept_fakeip` 在选路最前 | 假 IP 优先于 host/private/bypass CIDR | 依赖实际 attach/校验确认 |
| `native/cgroup.bpf.c` IPv4/IPv6 | 控制前缀掩码 | safety、UID、host 在 Fake-IP 之前；Fake-IP 优先于 private/bypass CIDR | host 映射与 Fake-IP 重合时可能绕过 |
| `native/shared_network_policy.h` IPv4/IPv6 | 控制前缀掩码 | safety/host 在 Fake-IP 之前；Fake-IP 优先于 LPM bypass | host 映射与 Fake-IP 重合时可能绕过 |
| DNS Fake-IP 发布 | `StoreFakeIPRanges` 每族一条范围 | 观察者顺序串行 | 运行时重合 redirect prefix 被拒绝，需重启 |

## 仍未消除的风险（不可标记通过）

1. **HOST 与 Fake-IP 优先级不一致。** cgroup/shared 的 HOST 映射会先于 Fake-IP 强制拦截进行 bypass，TC 的相同匹配则先强制拦截。需要先明确 HOST 必需安全豁免边界，再修改 native 程序；否则可能误拦内网本机地址。当前不能直接声称三种后端语义完全一致。
2. **运行时地址冲突。** DNS reload 的新 Fake-IP 前缀若与已选择的 redirect prefix 重合，listener 拒绝套用并提示重启，可能与新的 DNS 池短暂不一致；需要真机测试回退与重启行为。
3. **eBPF 对象重编译。** 若要修补上述原生策略，必须使用项目固定 Android NDK/Clang 生成并校验 `.o`，检测控制结构大小及对象一致性，避免源码与实际内核对象分离。
4. **特权加载缺失。** 公共 GitHub runner 可出现 privileged integration 步骤跳过，即使 eBPF 工作流总结果为 success，仍不能认定 attach 通过。
5. **Android 17 真机缺失。** Wi-Fi/5G、IPv6、Doze、TUN 长连接、规则热更新、共享/本地 TC 各路径尚未在 OnePlus 13 完整验证。

## 本 PR 的可自动验证部分

- 双栈 Fake-IP 前缀规范化、TC flags 与完整 IPv4/IPv6 掩码。
- LPM Trie key ABI 长度（v4:8、v6:20）与 bypass/Fake-IP 两套状态隔离。
- 重叠 bypass 网段识别、跨族不误判、mandatory safety 区间和映射 IPv4 拒绝。

**门禁：** 本轮是源码、策略和 Go 单元测试的完整静态审计，不是完成内核端到端验收；解决优先级冲突或引入动态 Trie 仍需独立高风险代码 PR 和特权/真机验证。
