# M5.21s：raft-rs 最小 Binding spike

日期：2026-08-11

## 结论

M5.21s 的最小跨语言纵向切片已经通过：不修改 raft-rs 算法源码，也不修改公共
`internal/control`、`internal/controlruntime` 或 `internal/controlexperiment` 生产代码，新增的
target-owned Rust worker 和 Go Adapter 可以由同一 Control Runtime 驱动三节点自然选主，并在关闭
第一个 worker 后使用第二个新进程严格重放完整轨迹。

这证明统一 Action 模型可以承载第二个 Raft 实现的 `Temporal → Ready effect → Message` 最小路径。
它不是第二 target Qualification，更没有证明跨协议普适性。

## 实际执行

固定配置为 heartbeat tick 1，n1/n2/n3 的单值 election tick 分别为 5/12/13。Runtime 只选择
当前 enabled 的既有 Action；worker 每次只执行一个 `tick`、`step` 或 `complete-ready` 请求，不拥有
scheduler。实测结果：

- 3 个节点，n1 自然成为 Leader；
- 17 个 Runtime 决策；
- 轨迹含 `fire-temporal-event`、`complete-effect` 和 `deliver-message`；
- trace digest：`ea0ffe33200c873b0e944a5afee8e84c2947e5bf11c44ac81fc89a4253ed6307`；
- 两个不同 worker PID，fresh-process strict Replay 通过。

Ready 被规范化成一个合并的 `persisted-applied-advanced` host effect。只有该 effect 完成后，worker
才把 Ready/LightReady 中产生的消息交给 Runtime。这是本阶段刻意收窄的最小模型，不等价于 etcd
Adapter 已实现的 persist/advance 双阶段 durable image。

## Core Churn 与 Binding Composition

共享核心生产改动为 0 行。新增物理行统计如下：

| 类别 | 行数 | 说明 |
|---|---:|---|
| target-native worker | 324 | raft-rs RawNode、Ready/Storage、protobuf 边界 |
| Rust/Go wire protocol + client | 193 | JSONL request/response 与进程生命周期 |
| target normalization/bookkeeping | 510 | Adapter、稳定 ID、effect/message/pulse 映射 |
| conformance integration test | 128 | 自然选主与 fresh worker Replay |
| Cargo manifest/lock | 14 / 425 | 固定跨语言依赖，不计入生产实现总数 |

生产实现共 1,027 个物理行，超过 900 行软目标，但低于 1,200 行强制停止线。超出的主要原因不是增加
Action/PSS/Agent 功能，而是显式保留 worker 进程协议、BuildID、证据快照和 Runtime conformance
边界。这个负担必须作为下一轮收缩对象；在完成复核前不继续复制 workload、PSS 或 durable restart。

## 信任边界

本阶段证明：

- 官方 raft-rs 0.7.0 RawNode 无算法源码修改即可接入；
- Runtime 仍拥有 enabled、虚拟时间和消息调度；
- worker 只执行一次冻结命令并返回 Ready/消息/状态；
- fresh worker process 可以精确重放同一 BuildID 下的轨迹。

本阶段没有证明：

- raft-rs 内部 RNG 已被审计或重放；当前只用单值 election range 消除其结果差异；
- crash/restart、durable image、workload、PSS、RiskWitness、Oracle、Agent 或 Campaign 已迁移；
- `CapabilityManifest.StrictReplay=true` 足以获得正式 Qualification；
- 两个 Raft 实现能够证明 Paxos/EPaxos/HotStuff 等跨协议适配性。

## 下一阶段

下一步应先做 M5.21sR Binding contraction，而不是立刻扩展功能：

1. 逐项审查 1,027 行生产 Binding，识别真正 target-native、未来可共享和可以删除的部分；
2. 保持公共 Action/Runtime/PSS 继续为 0 churn；
3. 给 worker protocol 做错误路径和进程退出 conformance，避免成功路径掩盖协议问题；
4. 目标是回到 900 行附近，或用明确证据解释不可再缩减的最小成本；
5. 收缩通过后，才在 durable restart 与最小 workload 中二选一推进，禁止同时展开。
