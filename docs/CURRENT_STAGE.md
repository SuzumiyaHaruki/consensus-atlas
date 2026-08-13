# 当前阶段

日期：2026-08-13

分支：`feature/agentic-consensus-testing`

阶段：A6c 真实 OpenRouter session 校准（完成）

## 一句话状态

ConsensusAtlas 已用真实 `deepseek/deepseek-v4-flash` 完成两 episode session：后一轮消费前一轮机械反馈，
两轮都进入 qualified execution、fresh Replay、PSS/Risk 和 Oracle；瞬时 OpenRouter 连接失败会在同一逻辑调用内
有界重试并记录实际传输次数。

## 输入什么

- etcd/raft、OmniPaxos 或 HashiCorp Raft 目标及其 Adapter；
- `plans/agent/*.json` 中的 protocol knowledge、TestHypothesis、workload 和 experiment authoring 输入；
- 代码中的协议族 RiskWitness/PSS 映射；
- 当前的 workload、root corpus 和 qualification 输入；
- 显式 opt-in 时才提供模型 key。

## 如何处理

1. Qualification 机械检查目标实际具备的控制能力；
2. Explorer 读取可信生成的 semantic candidate queue，只提交候选排序；
3. exact-prefix search 和 Control Runtime 物化真实 Action；
4. Trace/Evidence 由可信代码映射为 PSS/Risk；
5. fresh Replay、Oracle 和 evaluator 独立验证结果；
6. Campaign 以 episode、work、model token 和 wall clock 上限组织多 episode session。
7. ScenarioPlan 每一步重新构造可信 RiskFrontier，唯一匹配后使用原 stateless materializer 执行并 fresh Replay。
8. Scenario Agent 通过现有 durable journal 调用 OpenRouter；恢复时复用精确响应，不再次访问 provider。
9. DeepSeek、Claude、GPT 等模型只改变 `model_id`，不改变 Planner、journal、Runtime 或 evaluator。
10. A4c CLI 将 episode 派生为紧凑 summary；恢复会重建并核对结果，不重复终止调用。
11. `TestHypothesis` 是唯一共享语义假设；A2/A4 各自负责 queue proposal 与 short plan，不再经过 A1 兼容层。
12. `-semantic-input` 严格读取可编辑 JSON，再交给现有构造器生成 schema/digest 并验证 A2/A4 backend。
13. Adapter factory、Runtime、FaultEnvelope、DFS/Explorer/Scenario 预算和 OpenRouter 输出上限都使用
    JSON 值；与 root corpus/qualification 不一致时由现有 manifest/Replay 拒绝。
14. A6a 把每个现有 Scenario episode 作为一个 Campaign attempt；下一 episode 只读取前一个紧凑 artifact
    的最后一条 `ScenarioAgentFeedback`，不会读取其 Oracle/PSS/testing 字段。
15. A6b 从 qualified bundle 提取规范 Core PSS 状态键，在 session 结束或恢复时跨 episode 去重；Risk 最佳进展
    按 reached 优先、否则已满足里程碑数更多选择，Oracle 只汇总独立检查结果。
16. OpenRouter 对无响应的传输错误、HTTP 408/429/5xx 最多重试 2 次；401、合法 HTTP 响应中的非法模型内容、
    非法计划和预算终止不重试。`model_calls` 统计逻辑调用，`transport_attempts` 单独保留实际网络尝试数。

## 得到什么

- 当前能得到确定 prefix、Risk progress、provider 调用审计和可恢复公开校准 artifact；
- Explorer 成功后，同一 artifact 中包含所选 prefix、qualified ExecutionBundle、Core PSS 统计、RiskWitness、
  strict Replay 和 TraceIntegrity/Agreement 结果；
- 短计划执行输出逐步 choice、Risk progress、最终 Trace 和明确的 `no-match`、`ambiguous`、
  `budget-exhausted` 反馈；
- Agent 修正成功后输出同一多步 Trace 对应的 qualified Bundle、Core PSS、Replay 和 Oracle；
- 公开 direct-DeepSeek 历史校准只证明首个扩展发生变化，不证明 Agent 优势。
- OpenRouter 请求形态、模型切换和成功/失败恢复已由本地模拟服务验证。
- 首次真实 OpenRouter 调用因瞬时传输问题形成 1 call、0 token 的 `provider-failed` 工件；第二个新目录中的
  重试成功获得 2 次模型响应和 qualified testing，恢复保持 provider call 数为 2。
- A6a 本地 provider 集成测试连续完成 2 个 episode；第二次规划收到了第一次的 `completed` 机械反馈，
  Campaign 达到 attempt limit 后恢复没有重复 provider 或 key 访问。
- A6b 同一测试验证了重复 episode 的 PSS 状态按键取并集而非相加，并检查第二次模型请求不包含 PSS 状态键、
  bundle digest 或 Oracle violation 字段；恢复重建出完全相同的终端汇总。
- A6c 真实 session 一次完成 2 个 episode。第一轮模型调用经历 3 次传输后成功，第二轮 1 次成功，证明新增重试
  实际避免了把瞬时连接问题直接封成 terminal failure；总计 2 个逻辑调用、10,984 tokens。
- 第一轮选择 1 个消息投递动作，得到 20 个唯一 Core PSS 状态；第二轮收到第一轮 `completed` 机械反馈后选择
  `deliver-message -> crash -> fire-temporal-event -> deliver-message`，得到 23 个状态，session 并集仍为 23。
  两轮 Replay 都稳定，0 Oracle violation，Risk 都只满足 workload 初始里程碑，仍为 `not-reached`。
- 对已完成目录执行 `-campaign-resume` 得到完全相同的 episode、work、token、PSS/Risk/Oracle 汇总，没有新增
  provider 调用。

## 已删除什么

- 被新 semantic episode 替代的 trace mutation、corpus mutation 和 batch PSS guidance；
- 被 Semantic Explorer 替代的 Agent-v1 frontier-order Campaign；
- 未取得 qualification 的 raft-rs Adapter/probe；
- 仓库中重复展开的完整 Trace/bundle 与旧字节级 baseline JSON；
- 只检查历史 artifact identity、没有当前生产消费者的测试；
- README/架构/总体规划中的历史流水账。
- 没有生产消费者的 A1 `SemanticEpisodeView`、`EpisodePlan`、`PlanningFeedback`、`EpisodeReport` 及其自循环测试；
- DeepSeek 专用默认模型、旧测试命名与过期 race 分片名称。

历史仍可从 Git 提交 `0106e2c` 恢复。当前保留的调用持久化、qualification、Runtime 校验、strict Replay、
Oracle 和 evaluator 安全边界没有删除。

当前规模为：Go 生产 30,236 行、Go 测试 13,874 行、Markdown 3,977 行、JSON 10,346 行。A6c 相对 A6b
净增 38 行 Go 生产代码、59 行测试、41 行 Markdown 和 1 行 JSON。增量只包含 OpenRouter 有界重试、传输次数审计、
配置字段和当前文档；没有新增 package、Runtime、评分公式或平行 session ledger。完整真实工件位于 ignored
`artifacts/agentic/`，不进入 Git。

## 当前边界

- A6b 汇总的是当前单一 RiskWitness 和 Core PSS；固定 Profile 义务覆盖尚未进入该 session 视图；
- session 终端视图目前在运行/恢复时派生并通过 CLI 输出，没有另存一份会与 Campaign 漂移的 summary 文件；
- 第二轮计划更长且 PSS 状态更多只是一次公开观察；由于没有重复 trial 和同预算 baseline，不能归因于反馈或
  Agent 能力，23 个状态也不是覆盖百分比；
- token 统计只包含 provider 最终返回的 usage；无响应尝试是否已在 provider 端产生计费无法从本地确认，
  因此费用解释必须同时报告 `transport_attempts`；
- 还没有非公开 candidate/control 方法效果实验；
- 还没有同预算多 Agent 消融；
- 还没有证明 PSS/义务能预测隐藏根因检出；
- 普通测试不读取 key，也不调用外部模型。

## 已完成：A3 单 episode 真正闭环

复用现有对象完成：

```text
TestHypothesis -> Explorer selection -> qualified execution
              -> ExecutionBundle/PSS/Risk -> fresh Replay/Oracle
              -> one testing result
```

禁止为此新增平行 Ledger、hash、冻结 contract、baseline 或 gate；只有已有类型无法表达一个明确失败场景时
才讨论扩展。

完成标准：Explorer 成功后，同一次运行必须得到它实际选择的 prefix、qualified `ExecutionBundle`、Core PSS
统计、RiskWitness、fresh Replay 状态和独立 Oracle 结果；恢复已完成运行时不得再次调用模型或执行目标。

该标准已由真实 etcd/raft Adapter 的集成测试满足。测试使用本地 provider stub，不读取 key、不访问外部模型。

## 已完成：A4a 最小短时域计划

当前支持当前 ActionID，或以 ActionKind、节点、ItemKind、消息源/目标、TemporalKind、effect/durability 构成的
有限 selector。唯一匹配才执行；计划不能设置预算、故障边界、assertion 或 verdict。fixture 与真实 etcd/raft
均已执行两步 `crash → restart`，其中第二步不依赖预知未来 ActionID。

## 已完成：A4b 单次反馈修正

现有 model-call journal 已能输出 ScenarioPlan；`no-match`、`ambiguous` 或非法计划只形成公开机械反馈，
最多再调用一次。测试中第一次 `restart` 得到 `no-match`，第二次修正为 `crash → restart`，随后 qualified
executor 重现完全相同的 Trace。恢复两个模型调用时 provider 调用数保持不变。

## 已完成：A4c0 OpenRouter 单一入口

活动代码固定使用 OpenRouter Chat Completions 端点，并把模型作为显式 `model_id` 写入已有 transport 和
调用审计。生产代码没有默认模型；显式 Agent 策略必须提供 `-agent-model`。
请求继续在读取凭据前形成并持久化；凭据不进入请求工件，单次 dispatch 后立即清除。响应同时记录请求模型
和 OpenRouter 返回的实际模型标识。旧 direct-DeepSeek 工件保留为历史证据，不参与新运行恢复。

## 已完成：A4c OpenRouter Scenario 运行器

显式策略 `etcdraft-openrouter-scenario-a4c` 复用 A4b journal 与 qualified episode，最多 2 次模型调用、每个
计划最多执行 4 步。运行目录保留无凭据 prompt/request/result sidecar，`summary.json` 只报告 transport、机械
feedback、最终计划、模型成本和 testing 摘要。provider failure 同样会落 summary，并在恢复时保持 terminal。

本地模拟服务已验证成功计划、qualified testing、summary 和无 provider 恢复。真实运行保留两个 ignored
目录：`v1` 是 terminal transport failure；`v2` 成功获得两次 `deepseek/deepseek-v4-flash` 响应。第一次计划
执行 `deliver-message -> crash` 后第三步 `no-match`；第二次修正为
`deliver-message -> crash -> restart -> fire-temporal-event` 并完成。结果为 14,172 total tokens、33 个 Core PSS
sample、23 个唯一状态、Replay stable、0 Oracle violation。RiskWitness 只满足初始 workload milestone，仍为
`not-reached`。

## 已完成：A4r Agent 契约与模型入口收敛

旧 A1 episode 类型没有活动生产消费者，只有自己的测试；A2 Explorer 和 A4 Scenario 已分别承担实际的队列
排序与短计划职责。因此 HEAD 删除 A1 episode 契约、测试和阶段文档，只保留独立 `TestHypothesis`。

`NewTestHypothesis` 与 `Validate` 现在都要求调用方传入实际 backend。A2 固定用
`bounded-semantic-best-first-v1` 验证，A4 固定用 `bounded-scenario-plan-v1` 验证，消除了旧默认 DFS 与真实 A2 knowledge
不兼容但因路径隔离而未暴露的问题。活动模型客户端只剩 OpenRouter，模型 ID 必须由运行配置显式给出；历史
DeepSeek 工件继续作为实验事实保留，不参与新运行配置。

## 已完成：A5a 可编辑语义输入

etcd/raft 的活动 A2/A4 路径不再在 Go composition 中写死 ProtocolKnowledge 文本和
TestHypothesis。两者从 `plans/agent/etcdraft-leader-change-inflight-v1.json` 读取，并在加载后立即
转成现有 `ProtocolKnowledgePack` 和 `TestHypothesis`。JSON 不存储 schema、digest 或 backend 选择结果；
这些仍由可信代码生成或验证。新运行必须显式传入 `-semantic-input`，运行 `spec.json` 继续绑定
生成后的 knowledge/hypothesis digest。

## 已完成：A5b 可编辑运行配置

同一 JSON 现在还包含完整 etcd/raft Adapter config（节点/Raft ID/ElectionTick/HeartbeatTick）、
Runtime seed/clock/max clones、FaultEnvelope、DFS 上限、Explorer calls/tokens、Scenario attempts/steps 和
OpenRouter max output tokens。A2 的 `spec.json` 继续通过现有 SearchSpec/Transport 绑定这些值；修改预算后
恢复旧运行会在 provider dispatch 之前返回 `RUN_SPEC_DRIFT`。

这不意味着任意拓扑都能复用当前 corpus。例如把三节点改成四节点后，还需要一份与新 Adapter
manifest 相符的 root corpus/qualification；系统会拒绝不匹配组合，不会伪造可移植性。

## 已完成：A5c workload 输入

同一 JSON 现在还包含 `single-write-v1` 的可编辑 workload。加载器用目标已有 `InputPayload` 生成派生
`PayloadEnvelope`，再构造现有 `WorkloadPlan`；作者不填写 payload digest。source bundle、root corpus 校验和
A2/A4 qualified execution 使用这一份 workload，修改 value 后旧 corpus 会被拒绝。

Target-local projector、RiskWitness 证据解码和 Oracle 仍保留在代码中，没有被扩展为 JSON DSL。

## 已完成：A6a 多 episode session 骨架

显式策略 `etcdraft-openrouter-session-a6a` 复用现有 Campaign coordinator 和 Scenario episode。JSON 中的
`session_budget`/`session_wall_clock_ms` 限制 episode、primary/replay work、模型调用、token 和 wall time。
每个已提交 attempt 的 artifact 是现有 A4c 紧凑 summary；下一 attempt 只提取最后一条机械 feedback。
Campaign checkpoint 绑定预算、目标、实验输入和 artifact，停止后恢复不再次访问 provider。

该阶段没有新增 Runtime、Replay、Oracle、Ledger、schema 或 digest。测试证明两次 episode 的组合与恢复可用，
不证明 Agent 优于 baseline，也不证明跨 episode 已发现更多语义状态。

## 已完成：A6b session 结果聚合

session attempt artifact 在原 A4c 紧凑 summary 外只增加 qualified Core PSS 状态键，不修改 A4c 历史格式。
终端视图报告 Agent completed/stopped episode、testing/replay-stable episode、PSS 样本总数与状态并集、最佳
Risk 进展、Oracle violation 总数，以及原 CampaignSummary 的 work/model/time 成本。

相同 episode 的状态键测试证明并集不会退化为数量相加。第二次 provider 请求的原始 JSON 检查证明 PSS、Bundle
和 Oracle 字段没有作为跨 episode feedback 泄漏。恢复只读取 Campaign artifact，并重建完全相同的结果。

## 已完成：A6c 真实 OpenRouter session 校准

真实 DeepSeek session 在同一 Campaign 中完成两轮独立 qualified testing。第一轮 3 次传输才成功，第二轮 1 次
成功，因此有界传输重试是本轮实际需要的可用性修复，而不是把失败结果重命名。两轮共使用 10,984 tokens、
669 primary work 和 131 replay work，得到 23 个 Core PSS 状态并集、稳定 Replay、0 Oracle violation；
RiskWitness 未达到。恢复未重新读取 key 或调用 provider。

## 下一步：A7a 第二协议 session 接入差距

先以现有 OmniPaxos strict Adapter 为对象，列出 A6 组合入口中仍属 etcd/raft 的 Binding、Evidence projector、
Risk/Oracle 和 authoring 输入，随后只提取确实被两个目标共同消费的最小接口。目标是在不修改公共 Action、
Control Runtime 和 ScenarioPlan 的前提下跑通一个非 Raft 本地 provider session；真实模型比较留到该路径合格后。

## 阅读顺序

1. [总体规划](ConsensusAtlas-总体规划.md)
2. [架构](architecture.md)
3. [A2R 总结](stage-a2r-architecture-convergence.md)
4. [A3 总结](stage-a3-single-episode-testing-result.md)
5. [A4a 总结](stage-a4a-scenario-concretizer.md)
6. [A4b 总结](stage-a4b-agent-repair-qualified-testing.md)
7. [A4c0 OpenRouter 单一入口](stage-a4c0-openrouter-transport.md)
8. [A4c OpenRouter 校准运行器](stage-a4c-openrouter-scenario-calibration.md)
9. [A2b3b 真实模型公开校准](stage-a2b3b-etcdraft-real-model-calibration.md)
10. [Control Runtime](control-runtime-v2.md)
