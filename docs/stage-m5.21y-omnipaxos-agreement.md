# M5.21y：OmniPaxos exact decided-prefix 与 Agreement 激活

日期：2026-08-11

## 结论

M5.21y 通过。OmniPaxos target Binding 现在能从真实 `read_decided_suffix(0)` 生成每节点 exact decided
prefix digest，并由 51 行 target-owned `DecisionProjector` 投影为现有协议无关
`participant + position + value_digest`。公共 Runtime、DecisionObservation、Agreement、Core PSS schema
和上游 OmniPaxos 源码均未修改。

一条真实 35-decision control 在 position 1 得到两个独立节点的相同 cumulative digest；Agreement 为
0 violation。第二个 fresh worker 按原 Action 序列 Replay 后，trace 与 projected observations 完全相同，
trace digest 为 `88fe7e0b3b2f94ba648dca0f6f8aeb2853cc277446aa95b0c004f0a0ce3f514c`。

在真实 Evidence 的独立 calibration 副本中，只把一个已决定节点的 digest 替换为另一个合法 SHA-256；
同一个 projector 和 Agreement 在相同 position 精确产生 1 个 violation。这只证明 monitor 不是恒空，
不是 OmniPaxos 执行、不是新漏洞，也不进入方法效果统计。

## 输入、处理、输出

输入：固定 `omnipaxos 0.2.2`、三节点 `MemoryStorage`、一个从 n2 提交的 opaque value、冻结 seed 和
Runtime Action 序列。

处理：

1. worker 对 `get_decided_idx()` 之前的完整 `read_decided_suffix(0)` 做 fail-closed 检查；
2. 摘要先写入 domain 与 decided count，再按顺序写入长度分隔的 request ID、big-endian origin 和长度
   分隔的 stored value；
3. Adapter 把小写 64 字符十六进制 SHA-256 放入 opaque Evidence；
4. target projector 只输出非零 decided frontier；
5. 通用 Agreement 只比较同一 exact position 的 digest。

输出：

| 项目 | 正确 control | calibration |
|---|---:|---:|
| decisions | 35 | 不新增执行 |
| projected observations | 2 | 2 |
| same-position digest | 相同 | 不同 |
| Agreement violations | 0 | 1 |
| fresh Replay | trace/projection 相同 | 不适用 |

## 为什么不是 ClientResult 或 PSS

ClientResult 只说明某个 origin 看见一个决定，不能代表两个副本对同一位置的一致性。Core PSS 又只保留
相对 frontier，不保留 exact value。因此两者都不能作为 Agreement 证据。本阶段把 exact value identity
留在 target-owned Evidence/Projector 中，Oracle 仍不解析 ballot、消息或日志条目。

## 精确性边界

当前摘要只对本 Binding 已验证的 `MemoryStorage`、无 compaction/snapshot/reconfiguration 路径作出声明。
若读取结果长度与 decided index 不一致，或出现 `Decided` 以外的条目，worker 直接失败，不猜测被压缩
前缀。未来若加入 snapshot，必须由目标接入层提供可验证的 prefix accumulator 或 snapshot digest，不能
继续沿用本 identity。

一次提交后两个节点已决定，第三个节点没有在当前优先调度前缀内立即追平。Agreement control 因而使用
两个真实的同位置节点观察；这足以验证安全性比较路径，但不证明所有副本最终追平，后者属于未来 liveness
实验。

## 代码与边界

- target 生产代码：1,102 → 1,191 行，增量 89 行；低于 140 行软目标和 200 行停止线；
- worker：268 → 302 行（+34）；DecisionProjector：51 行；Adapter/model 校验与字段：+4 行；
- 新 projection 测试：174 行，不计生产面；
- worker binary SHA-256：`2e02273494cb1a196e5111f8a0d0afa7350a7105963f09f1098b5246abeb6d66`；
- 公共 Oracle、Runtime、Core PSS 与上游算法改动：全部 0。

冻结工件位于 `benchmarks/feasibility/omnipaxos-agreement-m5.21y/`。

验证通过：完整 OmniPaxos binding gate、全仓 `go test ./...`、`go vet ./...`、原始 OmniPaxos core probe、
一次 `go test -race ./adapters/omnipaxosv2`（18.084 秒）、JSON 语法、`git diff --check` 和两份总体规划
字节一致性检查。

## 没有证明

- 没有发现真实 OmniPaxos 缺陷，也没有 control/candidate pair；
- 没有 liveness、restart 或 durable-image monitor；
- 没有证明 PSS/义务数能预测缺陷检出；
- 没有 Agent、Campaign、Qualification 或 formal private holdout；
- control 的 0 violation 不证明实现正确或测试完备。

## 下一阶段：M5.21z 机械资格差距审计

下一步先不继续扩 capability，而是把 OmniPaxos 当前 Manifest、已有 conformance witness、PSS Mapper、
DecisionProjector 和 monitor composition 放入现有机械资格模型：

1. 明确区分已验证的 Temporal/Message/Invoke/Replay/PSS/Agreement 与未提供的 crash/restart/durable；
2. 运行现有 Portable CFT Profile，保留 partial/unqualified 结果，不为通过而缩小 requirement；
3. 检查唯一 qualified executor 是否能接受“能力子集实验”，还是错误地把完整生命周期资格当作所有实验
   的统一门槛；
4. 只输出 admission gap 和最小修正建议，不在同阶段添加 Agent、restart 或新评分。
