# M5.21zR：解耦 Qualification witness 与 Config admission 下界

日期：2026-08-11

## 结论

M5.21zR 完成，M5.21 在这里收口。系统现在不仅能把 OmniPaxos 接入统一 Runtime、PSS 和 Agreement，
还可以在不虚构 crash/entropy 能力的前提下，对其能力子集进行机械资格认证，并在 SUT 启动前验证实验
配置没有少报 capability。

新 `portable-cft-control-v3` 保持与 v2 相同的 8-required 严格分母，只把 Temporal、Message、Replay
witness 从 Crash/Restart lifecycle 中解开。OmniPaxos 的机械结果从 M5.21z 的 3 validated 提升到
6 validated、3 unsupported；`qualified=false` 保持不变。旧 v2 Profile digest、etcd/raft v2 qualification
和 M5.21z 冻结 bundle 均保持有效。

## v3 的解耦 witness

| capability | 新 witness | 验证内容 |
|---|---|---|
| natural-temporal-progress | natural-temporal-progress-independent | 从 enabled set 选择真实到期 temporal，检查 ClockAdvance/Yield/Evidence |
| runtime-owned-message | released-message-control | 对真实 released message 分别执行 Drop 与 Deliver，并检查终态 |
| strict-decision-replay | action-trace-replay | 执行 temporal + message control trace，并在 fresh Adapter 中精确 Replay |
| opaque-invoke-boundary | opaque-invoke-boundary | opaque Invoke 决策本身必须 fresh Replay，不再只检查 accepted |

同一个 independent witness 还在官方 etcd/raft Adapter 上通过，防止把 OmniPaxos 私有假设写入通用
Conformance。v2 没有被原地修改：`portable-cft-control-v2` digest 仍为
`8ceb2e870d0b5bc67fbdf5019f93f103a358c4f850bd3f6cebb2af3aa2a5f3c8`。

## Config capability 下界

`VerifyConfigAdmission` 在 Adapter factory 或 worker 启动前执行以下推导：

```text
Config
  ├─ fixed Policy Priority/Rules ──> ActionKind capability union
  ├─ Workload                    ──> opaque-invoke-boundary
  ├─ message/partition envelope  ──> runtime-owned-message
  ├─ crash envelope              ──> crash-restart-incarnation
  └─ replay=true                 ──> strict-decision-replay
                                      + strict-yield/pure-enabled baseline
```

固定 Action priority 中的 Invoke、Temporal、Message、Crash/Restart 可以精确映射。随机、action-class、
admissible-uniform、trace-mutation policy 会从 enabled frontier 选择未在 Config 中枚举的 Action，因此保守
要求 Profile 的完整 required set。CompleteEffect、FailEffect 和 Callback 当前没有独立 capability，也采用
完整 required set；这保证 partial target 不会借未知 Action 绕过资格，但不等于已经建立 effect 专用语义
witness。

少报测试只声明 strict-yield/pure/replay，却在 Policy 中加入 FireTemporal；系统返回
`EXPERIMENT_ADMISSION_CAPABILITY_UNDERDECLARED: natural-temporal-progress`，且 Adapter factory 调用次数为 0。
同一检查也进入 bundle-producing path 和带 Qualification 的 report validation。

## admitted OmniPaxos smoke

真实三节点执行声明并验证以下六项：

- strict-yield-evidence；
- pure-enabled-check；
- natural-temporal-progress；
- runtime-owned-message；
- strict-decision-replay；
- opaque-invoke-boundary。

target-owned WorkloadRouter 从 Evidence 中选择 self-reported coordinator；Runtime 通过自然时间和消息投递
完成一次 opaque Invoke。结果为 29 decisions、28 Core states、1 个完成 workload、configured-stop，fresh
Replay stable。trace digest 为 `b315eb2928762bb5ff0a8172ff771bea26f4608b772f96a9c6bab50ead755c2b`。

该 smoke 没有 Crash/Restart、durable capability、Agent 或搜索方法比较。它证明的是 admission 闭环可执行，
不是测试质量、协议正确性或缺陷发现结果。

## 代码与边界

- 通用生产增量：304 行，包括版本化 Profile、3 个独立 witness 和 Config requirement 下界；
- OmniPaxos target-owned WorkloadRouter：36 行；target 生产总量 1,227 行；
- qualification composition：85 行；冻结 v3 bundle：426 行；
- 公共 Action schema、Runtime 调度状态机、Core PSS schema、Oracle 和上游 OmniPaxos：0 修改；
- v2 历史身份和 artifact：未重写。

完整 bundle 位于 `benchmarks/qualifications/omnipaxos-v2-m5.21zr/report.json`，小型 reader summary 位于
同目录 `summary.json`。

## M5.21 最终边界

已经证明：

- 非 Raft、leader-based Sequence Paxos 可以复用统一 Action/Runtime；
- target 接入可以覆盖自然时间、消息、opaque workload、PSS、Agreement 与 partial Qualification；
- partial capability 实验可在机械下界检查后执行和 Replay；
- 不支持的 lifecycle/entropy 不会为了实验通过而被隐藏。

仍未证明：

- OmniPaxos 的 crash/restart/durability 或 full Portable qualification；
- leaderless/BFT 协议适配；
- PSS/义务覆盖与真实缺陷检出的相关性；
- Agent 优于 Random/DFS/人工计划；
- private holdout 或新缺陷。

## 下一阶段：M5.22

M5.22 不再扩张 Adapter 基础设施。优先用 etcd/raft 与 OmniPaxos 两个已认证执行面运行共同预算的
cross-target Campaign/Planner 实验，检查现有 Agent semantic view、计划编译和统计输出能否在不含 Raft
字段的条件下复用。任何新 capability 必须由该实验的具体失败证据驱动。
