# 当前阶段

阶段：M4n21 第三 Adapter composition 试接入

## 当前结论

ConsensusAtlas 的活动链路已经收敛为：

```text
协议知识 / 有限源码读取
→ Risk Agent 候选与 typed milestones
→ 可信资格检查
→ Scenario Agent 单路径战略干预
→ 公共确定性推进
→ recorded Schedule
→ qualified execution / fresh Replay / Oracle
```

Scenario Agent 只使用 `continue / revise / abandon`。Agent 不能创建 Action、修改
Runtime、决定 Replay 或产生 finding；finding 只来自独立 Oracle。

## M4n14–M4n19 收敛结果

- M4n14：固定延迟 Invoke 与 Risk portfolio length 两个真实失败回归。
- M4n15：删除 branch/control/ablate/select 及分支 artifact，Episode 只保留一条路径。
- M4n16：完整 Scenario Trace 成为唯一 Schedule；Invoke/Partition 使用记录参数准备，
  qualified execution、fresh Replay 与 evaluator-owned Replay 复用同一 recorded-action 语义。
- M4n17：公共 bootstrap 跨越普通 candidate/term/ballot 变化；typed milestone 允许在唯一
  coordinator 就绪时自动 Invoke；finding attribution 从首个 Agent 战略干预开始。
- M4n18：closure-disabled 回归证明 etcd/raft 与 OmniPaxos 的既有 Drop 路径均可由公共
  causal progress 闭合，专用 selector 只缩短路径，不增加可达性。
- M4n19：删除两个协议的 closure factory/selector、handoff、candidate、prompt、artifact
  和预算状态；公共执行与 Agent 执行不再包含协议专用闭合分支。

当前 MethodSpec implementation ID 为：

```text
consensus-atlas/agentic-method/m4n20-bootstrap-root-only-v1
```

Risk/Scenario prompt 分别为 `risk-agent-navigation-v11` 与
`scenario-agent-investigation-v23`。历史 MethodSpec 的
`closure_mode` 仍可读取，但当前 CLI 不再暴露 `-closure-mode`，活动组合固定使用公共推进。

## 当前执行语义

### Agent 选择

Agent 只选择当前 trusted frontier 中的战略 Action，例如 Drop、Duplicate、Crash、
Restart、Partition、Heal，或在多个不能机械判定的方向中选择一个。每次计划最多一个
战略 Action；普通消息、effect 和 timer 不持续占用模型调用。

### 公共推进

公共 causal progress 只选择 Runtime 已 enabled 的：

- host/effect completion；
- ordinary message delivery；
- naturally due temporal callback；
- typed automatic Invoke（仅在 workload、route 与 coordinator 唯一时）。

优先级只使用记录 Action 的 item dependency 与参与者方向，不理解 Raft term、
Paxos ballot、quorum 或具体消息语义。公共层不会自动执行新的故障干预。

### 证据

每个 Agent 或自动选择都成为普通 ActionRecord。完整 Trace 直接作为 recorded Schedule；
没有 Agent Trace → Policy 的逐 decision 二次翻译。Bundle 保存 preparation、Trace、Replay、
Oracle attribution 和成本。正式 finding 只来自 evaluator-owned Replay 后的 Oracle。

Decision provenance 只分：

- `agent_selected`
- `public_progress`

历史 `target_closure` 不再由当前代码生成。

## 跨协议验收

### etcd/raft

- 三节点和五节点 bootstrap 建立 leader，并在 typed milestone 需要时自动 Invoke；
- Drop `MsgAppResp` 后，公共推进在 17 或 19 个 post-intervention decisions 内闭合；
- 同一 RequestID committed；
- qualified execution、fresh Replay 与五个 Oracle 保持稳定。

### OmniPaxos

- 三节点和五节点 bootstrap 建立 coordinator，并自动 Invoke；
- Drop operation-carrying replication message 后，公共推进可到达同一请求 decision；
- qualified execution、fresh Replay 与三个 Oracle 保持稳定。

以上只证明公共执行底座跨两个 leader-based CFT Target 可用，不证明协议正确、
测试完整或 Agent 方法优于基线。

## 保留的可信边界

- Adapter Manifest / qualification / admission；
- deterministic Runtime、Action ownership 和 virtual time；
- durable Provider journal 与 usage accounting；
- local SUT source/build identity；
- recorded preparation、fresh Replay 和 evaluator-owned Replay；
- target-local Observation 与 Oracle registry；
- root/post-root Oracle attribution；
- calls、tokens、primary/replay work 和 wall-clock 预算。

没有新增 hash、baseline、冻结 contract 或 gate。

## M4n20 决定

- 活动 Target 无条件构造 bootstrap root；`root_mode` JSON/CLI 覆盖和两个生产
  workload-ready builder 已删除。窄 public-progress 校准只在 `_test.go` 内按 recorded
  Action 构造 workload-ready prefix。
- PSS 仍保留在 Bundle，供保存 Trace 的离线探索分析和历史 evaluator 使用；它已从
  Agent-facing Memory、Episode metrics 与 CLI 摘要删除，不再影响 Risk Agent 的下一轮选择。
- 没有直接删除 PSS schema：当前 CorePSS 仍绑定 Bundle validation 和历史正式工件；直接移除会
  同时改写 Bundle、Report、evaluator 与两个 Adapter mapper，不属于安全的次要瘦身。

## M4n21 HashiCorp composition 结果

现有 HashiCorp Raft Adapter 与 qualification 无模型复核通过，但资格结果仍为：

```text
Total=9 Required=8 Validated=3 Unsupported=6 Qualified=false
```

它已经具备 Adapter factory、Runtime-owned message、crash/restart 和 opaque Invoke；尚缺活动
WorkloadRouter、Observation/Decision projector、Oracle registry、Agent knowledge 与本地源码 mount。
更关键的是 wall-clock/random timeout 不受调度器控制，strict Replay、natural temporal progress 与
audited entropy Replay 明确未支持。

试接入没有要求修改 Scenario 状态机、recorded Schedule executor、Provider journal、Bundle 或公共
progress。当前结论不是“第三 Target 已接入”，而是“公共边界未被推翻，阻塞准确位于 SUT 可测试性和
Target-local 组件”。详见 `docs/hashicorp-composition-trial.md`。

下一步只准备两个活动 Target 的单 Episode canary；外部模型调用必须另行获得本轮明确授权。HashiCorp
在提供可测试性端口和本地源码 checkout 前不进入付费 Agent 实验。

## 验证

M4n21 完成前必须通过：

```text
go test ./...
go vet ./...
make audit-no-v1 audit-race-shards
git diff --check
```

并对公共推进、bootstrap、artifact/recovery 和 evaluator-owned Replay 做聚焦 race。
