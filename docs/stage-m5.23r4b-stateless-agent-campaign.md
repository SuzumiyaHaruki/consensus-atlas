# M5.23R4b：Stateless Agent 进入共同 Campaign

日期：2026-08-12

状态：实现完成，离线门禁通过；未调用真实模型

## 阶段目标

R4b 不增加一种新的 Agent 权限，而是把 M5.23g 已验证的 frontier permutation 权限迁入 R2/R3 的
共同 Stateless Campaign。canonical、seeded-uniform 和 restricted Agent 现在共用同一组：

- 显式冻结的 root corpus 与 method sequence；
- exact-prefix bounded search；
- qualified execution、fresh strict Replay 与只读 PSS discovery；
- Campaign config/checkpoint/content-addressed artifact/summary；
- `StatelessCampaignObservation/v1` 与完整逻辑 WorkLedger。

## 当前真实入口

`campaign-etcdraft-stateless-agent-v1` 是唯一现行 etcd/raft Agent Campaign strategy。必须显式提供：

- `-stateless-corpus`；
- `-campaign-dir`、attempt 数、wall-clock ceiling 与 model-token allowance；
- `-agent-key-file`；
- summary 和 observation 的新输出路径。

`make experiment-etcdraft-v2-stateless-agent-campaign` 不提供 key 或 artifact directory 默认值，也不被任何
测试/验证目标依赖。已冻结的 `workload-stateless-agent-m5.23g` 在线入口被退休；其公开工件和只读验证仍保留。

## 调用和恢复边界

每个 attempt 有独立 protocol-neutral sidecar namespace。每次调用按以下顺序持久化：

1. 在读取凭证前冻结 exact prompt/request bytes 并写 `intent.json`；
2. 仅当调用所需 key 可用时写 `dispatch.json`，随后消费 ordinal；
3. transport、response 和 strict ActionID permutation 校验形成唯一 `result.json`；
4. terminal attempt 将 secret-free call audit、model work、search work、18 次 qualified execution 和
   discovery 一起封入内容寻址 artifact。

恢复规则是：

- prepared：Campaign head 不前移，不生成 summary/observation；同一终态路径可在 resume 时继续使用；
- completed：重算 exact request 后重放保存结果，不再次调用 provider；
- dispatch-only：按 ambiguous terminal failure 提交一次，不重试；
- rejected/failed：保留 terminal 状态和已经发生的调用成本；
- committed artifact：生成 observation 前重新读取 sidecar，逐项比对 intent/dispatch/result 导出的 audit；
  任一文件篡改、未知文件、symlink、ordinal/root/request drift 都 fail closed。

旧 v3 非模型 Campaign 的空 `plans/`、`model-calls/` 目录仍可读取，以保留恢复兼容性；只要其中出现旧
Planner 状态，新 runtime 就拒绝解释。sidecar root 可在完整旧布局验证后惰性增加。

## 离线集成结果

mock provider 只返回当前 trusted frontier ActionID 的合法反序排列，不访问外部服务：

| 场景 | provider 调用 | key 读取 | durable 结果 |
|---|---:|---:|---|
| 首次 key 不可用 | 0 | 1 | sequence=0，prepared intent；无终态输出 |
| prepared resume | 6 | 6 | 1 completed attempt |
| completed resume | 0 | 0 | 重读相同 artifact/sidecar |
| dispatch-only resume | 0 | 0 | 1 failed attempt，`ambiguous` audit |
| committed sidecar 篡改 | 0 | 0 | 拒绝 observation，且不占用输出路径 |

completed mock attempt 包含 6 份独立 call audit、18 次 qualified execution，模型账本为
`calls=6, input=24, output=18, total=42`。这只证明 composition、恢复和计费边界，不是 Agent 搜索效果。

## 主线删除

在上述门禁通过后，HEAD 删除了旧 macro Agent/Campaign 闭包：

- GuardedTestIntent、macro compiler、planned attempt、Campaign Planner/model-call；
- feedback/follow-up/B4 与无消费者 Cross-target Planner 组合；
- 旧 Agent Campaign CLI、runner 和专用测试。

保留的 `ProtocolKnowledgePack`、transport freeze/provider、restricted frontier-order validator、Campaign
durable core 和 formal evaluator 都有当前消费者。`audit-no-retired-experiment` 阻止旧类型重新进入编译代码。

当前 `cmd/control-experiment` 与 `internal/controlexperiment` 合计 **21,294 行 Go**，相比 M5.23g
检查点 33,832 行净减 **12,538 行（37.1%）**；本轮 tracked diff 删除量显著高于增加量。

## 可信边界与已知限制

- Agent 只能重排 Runtime 已枚举的当前 frontier，不能构造 Action、root、预算、PSS、Oracle 或 verdict；
- `AgentCalls` 是 secret-free digest/work 索引，exact bytes 只存在 sidecar；
- deferred 恢复前已经发生的 reconstruction 是运维中断开销，不进入 terminal attempt 的逻辑 WorkLedger；
  因此发生 deferred/restart 的 run 不得直接参加单位成本方法排名；
- wall-clock ceiling 是单进程运行控制，不是跨进程累计资源审计；
- 本阶段没有读取真实 key、调用模型、产生新效果数据或启动 holdout。

## 验证

- R4b create/resume/tamper 与 ambiguous 集成测试通过；
- frozen M5.23g 工件仍逐调用验证，并可迁入新的 AgentCalls artifact 契约；
- Campaign legacy-empty-layout 兼容门与 populated-retired-state 拒绝门通过；
- 完整 `make test` 通过，`cmd/control-experiment` 用时 231.550 秒；
- `go vet ./...`、race manifest、v1/退休路径审计和 `git diff --check` 通过；
- race method 分片运行约 8 分钟未完成，期间无 race report/panic，按既定停止线终止；其他两个分片未开始。
  该项状态是未知，不冒充通过，也不为得到绿色结果延长或重跑。

## 下一阶段：M5.24a 评测冻结

先冻结方法比较协议，而不是继续扩展 Agent：

1. 同一 private candidate/control pair、root corpus、attempt 数、逻辑预算和运行环境；
2. canonical、预声明 uniform seeds、restricted Agent 三臂；
3. 主要指标为 candidate root-cause kill、correct-control false positive 与 work-to-kill；
4. PSS discovery/义务只作解释性诊断，不参与 verdict；
5. formal run 若发生 deferred/restart、sidecar ambiguity 或不完整计费，必须单列 invalid，不得替换 trial。

在 curator pack、blind exposure audit 和重复次数冻结前，不调用新的真实模型。
