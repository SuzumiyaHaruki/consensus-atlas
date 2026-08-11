# efficient/epaxos M5.8a 可行性工件

本目录冻结第三目标的接入前检查，不包含完整 Adapter，也不授予任何 Control Runtime capability。

固定对象：

- repository: `https://github.com/efficient/epaxos.git`
- commit: `791b115669fca472d3136f6a2eda46c00b3f8251`
- tree: `708b8e37f6b50a4e6b6fcbb95045dc53ad7d6344`
- source archive SHA-256: `9ffde18a3bfb7763c0995ce20064d34c7ddfcf944510a845cbc655a94a676068`

`report.json` 由仓库内测试模型机械推导并逐字节比较。它记录：生产包可构建；官方三节点进程可完成一个命令；
外部输入、peer TCP 写入和协议状态字段可观察；但真实消息尚未被 Runtime 冻结并选择性释放，后台 goroutine
没有静止点握手，墙钟 sleep 不可注入，持久状态没有稳定恢复路径，严格 replay 尚未证明。

因此当前决策只能是 `proceed-limited`，唯一获准的后续工作是 test-only message-port worker spike。
若该实验不能在不修改协议算法、公共 Action、Runtime 或 Core PSS 的条件下稳定冻结并释放一个真实 peer frame，
则将 `runtime-owned-message` 记为 Unsupported，并停止扩张该目标。

复核冻结报告：

```bash
make audit-efficient-epaxos-feasibility
```

三节点 smoke 使用真实墙钟和 goroutine，只是接入存在性证据，不是确定性执行、严格重放、完整测试或缺陷检出证据。
