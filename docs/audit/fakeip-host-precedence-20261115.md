# Fake-IP / HOST 优先级修复

范围：cgroup IPv4 双路径、IPv6 单路径，共享 packet-rewrite 双栈。
修改：仅在非 Fake-IP 目的地址时使用 HOST Hash 绕过；Fake-IP 优先代理，与 TC 策略一致。
保留：safety 地址、UID、DNS 与缓存等原有检查顺序，没有更改 BPF 控制记录 ABI。
验证：源代码契约测试、NDK r29 Clang 21 重新生成、make ebpf_check。
GitHub Actions 机器人生成提交不触发常规 push CI；需对生成后的 HEAD 再次运行/核实构建。
注意：特权 eBPF attach 和 OnePlus 13 Android 17 真机测试均未验证。
