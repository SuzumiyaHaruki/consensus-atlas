# M5.21sR：raft-rs Binding 收缩复核

日期：2026-08-11

## 结论

M5.21sR 没有扩展 workload、restart、PSS、RiskWitness、Agent 或 Qualification。它只收缩上一阶段的
target-owned Binding，并补齐跨进程协议的两个错误边界。结果是生产实现由 1,027 个物理行降到 981
行，三节点自然选主和 fresh-worker-process strict Replay 保持通过，共享 control/runtime/experiment
生产 Core Churn 仍为 0。

900 行软目标仍未达到。本阶段接受一个经过审查的 981 行暂定基线，而没有通过压缩排版、删除可信
检查，或把单一消费者代码移动到“共享包”来制造达标数字。它低于 1,200 行停止线，因此候选继续保留。

## 删除内容

- 将只会被立即读取的历史 emission map 收缩为当前 emission；
- 用初始 yield sequence 代替独立 bootstrap flag；
- 用稳定 JSON map 代替重复的 ready-node/pulse snapshot record；
- 删除 trace 已由 Manifest 绑定后在 Evidence 中重复出现的 worker digest；
- 删除不参与控制、Replay 或 Oracle 的 Ready 展示计数；
- 利用 Rust `BTreeMap` 的协议顺序直接校验节点集合，删除二次排序。

没有删除 Adapter `Check`、Ready digest、BuildID、Evidence、Entropy envelope、worker request/response ID
或 fresh-process Replay。这些部分虽然占行数，但属于当前信任边界，不是展示代码。

## 实际结果

- n1 在 17 个 Runtime 决策后自然成为 Leader；
- trace digest：`f768ab1042bc710fe61502f7a301260c38ac195ead97be30fe31dc20037ddfc8`；
- primary worker 关闭后，新进程 strict Replay 通过；
- 未知 worker operation 返回包含稳定 worker reason 的 typed rejection；
- worker 被异常终止时，Go client 返回 `RAFT_RS_WORKER_EXIT`，不会误报正常完成。

## Binding Composition

| 类别 | M5.21s | M5.21sR | 变化 |
|---|---:|---:|---:|
| target-native Rust worker | 324 | 315 | -9 |
| Rust/Go protocol + client | 193 | 182 | -11 |
| target normalization/bookkeeping | 510 | 484 | -26 |
| 生产实现合计 | 1,027 | 981 | -46（-4.48%） |
| conformance test | 128 | 168 | +40 |

Cargo manifest/lock 的 14/425 行继续独立记账。测试增长来自两条真实错误路径，不计入生产 Binding。

## 为什么不强行压到 900 行

剩余 981 行分别承担 RawNode/Ready/Storage/protobuf、类型化跨语言进程协议，以及完整 Adapter
eligibility/稳定身份/Evidence 生命周期。继续缩减的几个容易选项都不可靠：

- 用 map/raw JSON 替换 wire types 会削弱边界校验；
- 删除 Adapter 的二次 eligibility 检查会扩大 TOCTOU 风险；
- 删除 Evidence/BuildID/Ready digest 会削弱 Replay 的解释性；
- 把 worker client 移到尚无第二消费者的 Adapter Kit 只会重新分类行数；
- 合并代码行只会美化 physical LOC，不降低复杂度。

因此 981 是当前功能与可信边界下的暂定接入成本，不宣称是理论最小值。第三个真正消费者出现后，
才能用实际复用证据提取 process-binding kit，并重新计算边际 LOC。

## 下一阶段

M5.21t 只增加最小 opaque workload：已有 Leader 接收一个 `Propose(bytes)`，Runtime 经 Ready/message
路径观察到 commit，并输出一个 ClientResult。它不得同时增加 crash/restart、PSS、RiskWitness 或新
Action；新增生产代码应有单独 LOC 上限。这个切片回答“统一控制层能否完成输入到结果”，再决定是否
值得投入更复杂的 durable restart。
