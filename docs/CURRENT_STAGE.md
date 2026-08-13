# 当前阶段

日期：2026-08-12

阶段：M5.23R4b Stateless Agent Campaign composition 完成

## 输入什么

- 显式冻结的 etcd/raft root corpus（当前为 0/28/54-decision 三个 exact prefix）；
- 运行前预声明的 canonical repeat、seeded-uniform seed 序列或 restricted Agent method sequence；
- attempt 数、逻辑预算、wall-clock 运维上限；Agent 另需显式 model-token allowance 和 key-file 路径；
- qualified Adapter、opaque workload、Qualification、Core PSS Mapper 与 strict Replay 配置。

## 如何处理

三种方法共用同一 Stateless Campaign provider 和 target-local runner。每个 attempt 对 3 个 root 运行
depth=2、每 root 6 items 的 exact-prefix bounded search；18 条路径进入原 qualified executor，执行
primary + fresh strict Replay，再由可信 composition 只读重算 PSS discovery。

Agent 只在 search orderer 上工作：每次接收当前 Runtime 已枚举的 `ActionFrontierView`、冻结协议知识和
已完成 history，只能返回全部现有 ActionID 的排列。exact request 在凭证读取前落盘；dispatch 后禁止
盲重试；terminal result 的 digest/work 进入 call audit。attempt artifact 同时绑定 method、6 次 call audit、
search、18 次执行、discovery 与完整 WorkLedger。恢复读 artifact 时还会重新读取 sidecar 交叉核验。

## 得到什么

- 可恢复 `CampaignConfig/checkpoint` 与内容寻址 attempt artifact；
- `CampaignSummary/v2`；
- `StatelessCampaignObservation/v1`：每个方法的 outcome、PSS 集合、qualified execution 数、Agent call audit
  以及 source/search/primary/replay/model 分栏成本；
- prepared、completed、ambiguous、rejected/failed 的确定恢复语义；
- 不包含 Coverage 百分比、正确性概率或 Agent 自报 verdict。

## 离线 R4b 结果

mock provider 的完整 attempt 为 6 次调用、18 次 qualified execution，模型账本
`6 calls / 24 input / 18 output / 42 total tokens`。prepared resume 只 dispatch 各调用一次；completed
resume 为 0 provider/0 key read；dispatch-only 恢复为单个 failed attempt 且不重试；committed sidecar
篡改在 observation 前被拒绝。没有读取真实 key 或调用真实模型。

旧 `workload-stateless-agent-m5.23g` 在线入口及 macro Planner/Intent/planned-attempt/model-call 闭包已经
删除；M5.23g 冻结公开工件和只读验证保留。旧 v3 非模型 Campaign 的空 legacy 目录可恢复，包含旧
Planner 状态则明确拒绝。

## 代码规模

| 指标 | M5.23g 检查点 | R4a | R4b 当前 |
|---|---:|---:|---:|
| `cmd/control-experiment` + `internal/controlexperiment` Go LOC | 33,832 | 28,809 | 21,294 |
| 相对 M5.23g | — | -5,023 | -12,538（-37.1%） |

当前主线收缩为：Control Runtime/Adapter、qualified executor/Replay、Stateless Search、restricted Agent、
Campaign durability、PSS discovery 与 formal evaluator。历史阶段文档中的已退休 API 不代表 HEAD 仍可用。

## 尚未证明

- 没有证明 Agent 优于 canonical、uniform、DFS 或专家方法；
- M5.23g 的真实负结果仍然成立：Agent 与 canonical 的 Action 顺序和 15-state PSS 集合相同；
- 没有非公开 candidate/control 的重复方法比较、缺陷检出或统计显著性；
- PSS/义务 discovery 尚未证明能预测 root-cause kill；
- deferred/restart 的进程运维成本尚未形成可跨进程累计的正式资源账本；
- OmniPaxos 通过相同 Stateless Search API，但尚无 target-local Campaign/Agent composition。

## 验证状态

- R4b 定向 create/resume/tamper/ambiguous、frozen evidence migration 和 legacy layout 测试：通过；
- 完整 `make test`：通过；`cmd/control-experiment` 用时 231.550 秒；
- `go vet ./...`、`audit-race-shards`、`audit-no-v1`、`audit-no-retired-experiment`、
  `git diff --check`：通过；
- `test-race-control-shards` 的 method 分片运行约 8 分钟仍未完成，期间无 race report 或 panic；按既定
  停止线人工终止，execution/agent 分片未开始。因此本轮完整 race 状态是未知，不能写成通过，也不重跑。

## 下一阶段：M5.24a

冻结非公开方法评测协议：candidate/control curator pack、blind exposure、三臂 method spec、重复次数、
完整 work-to-kill 和 invalid-trial 规则。PSS/义务只作为解释性指标；正式结论由 candidate root-cause kill、
correct-control false positive 和成本共同决定。冻结完成前不进行新的真实 Agent 调用。

## 建议阅读顺序

1. `docs/stage-m5.23r4b-stateless-agent-campaign.md`
2. `cmd/control-experiment/stateless_campaign_agent_etcdraft.go`
3. `internal/controlexperiment/stateless_campaign.go`
4. `cmd/control-experiment/stateless_agent_call.go`
5. `internal/controlexperiment/stateless_campaign_observation.go`
6. `docs/stage-m5.23g-real-agent-multi-root-pilot.md`

历史阶段不复制进本文件；不可改写记录保留在 `docs/stage-*.md`。
