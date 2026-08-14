# 当前阶段

日期：2026-08-14

分支：`feature/agentic-consensus-testing`

阶段：A7H2b1 Risk/Oracle 独立重算

## 一句话状态

同一个 Scenario episode/session core 已服务 etcd/raft 和 OmniPaxos；当前先恢复仓库验证、完整执行证据、成本
分类、worker 生命周期和方法身份，再进入 A8 效果实验。

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
7. 活动 Scenario 每次只让 Agent 选择一个当前 Action；成功后由可信 natural-progress closure 执行普通 effect、
   message 和 timer 动作，直到 Risk 里程碑变化、客户端返回、自然推进静止或预算结束。
8. Scenario Agent 通过现有 durable journal 调用 OpenRouter；恢复时复用精确响应，不再次访问 provider。
9. DeepSeek、Claude、GPT 等模型只改变 `model_id`，不改变 Planner、journal、Runtime 或 evaluator。
10. A4c CLI 将 episode 派生为紧凑 summary；恢复会重建并核对结果，不重复终止调用。
11. `TestHypothesis` 是唯一共享语义假设；A2/A4 各自负责 queue proposal 与 short plan，不再经过 A1 兼容层。
12. `-semantic-input` 严格读取可编辑 JSON，再交给现有构造器生成 schema/digest 并验证 A2/A4 backend。
13. Adapter factory、Runtime、FaultEnvelope、DFS/Explorer/Scenario 预算和 OpenRouter reasoning/output 都使用
    JSON 值；与 root corpus/qualification 不一致时由现有 manifest/Replay 拒绝。
14. A6a 把每个 Scenario episode 作为一个 Campaign attempt；episode 内 feedback 携带上一计划、失败步骤和
    已验证执行结果。下一 episode 从统一 Campaign root 独立开始，当前不再注入前一 episode feedback。
15. A6b 从 qualified bundle 提取规范 Core PSS 状态键，在 session 结束或恢复时跨 episode 去重；Risk 最佳进展
    按 reached 优先、否则已满足里程碑数更多选择，Oracle 只汇总独立检查结果。
16. OpenRouter 使用其 Chat Completions 公共 `max_tokens` 参数，并要求路由端点支持 reasoning 与 strict
    structured output；对无响应传输错误、HTTP 408/429/5xx 最多重试 2 次。`model_calls` 统计逻辑调用，
    `transport_attempts` 单独保留实际网络尝试数。
17. etcd/raft qualified Bundle 在 TraceIntegrity 和 Agreement 之后运行 target-local `etcdraft-log-progress`；
    它只解码 Adapter-owned Evidence，不读取 Agent、PSS 或 Risk 结论。
18. A7a 用 OmniPaxos target binding 投影 leader、decided frontier 与消息类别；通用 Scenario 引擎执行一次
    `Invoke -> DropMessage -> natural progress -> decided`，随后由全新 worker 精确 Replay。
19. A7b 从 `plans/agent/omnipaxos-message-loss-before-decision-v1.json` 构造可信输入；最终 Scenario 通过
    OmniPaxos 已验证的六项能力绑定 admission，再由唯一 Bundle executor 重新执行。
20. A7d 已用同一 Campaign coordinator 运行两个独立 OmniPaxos episode；当前 artifact 仍只保存 provider 调用
    审计与 compact summary，完整 Bundle 持久化与无 SUT 重访恢复属于 A7H 的明确缺口。
21. Campaign 现在保留 provider 实际返回的超预算 work，Scenario child verification 统一计入 Replay；
    `controlruntime.New` 明确接管 Adapter，初始化/Replay 失败自动清理，成功 Runtime 由调用者幂等关闭。
22. DFS、semantic best-first、Risk frontier、Scenario root、qualified primary/replay 均在一次临时使用后关闭
    Runtime；OmniPaxos episode 不再把所有 worker 累积到 episode 结束。
23. conformance manifest、raw Adapter case、各 Runtime 与 fresh Replay 也遵循相同所有权；HashiCorp Raft 和
    OmniPaxos qualification 已删除目标专用的批量回收 wrapper。
24. completed Scenario attempt 直接在现有 Campaign artifact 中保存完整 `ExecutionBundle`；恢复首先执行
    `Bundle.Validate()`，PSS 状态键与 session 并集从 Bundle 重新派生，不再持久化一份平行键列表。
25. attempt 同时保存完整 Risk/Oracle 判定；etcd/raft 与 OmniPaxos 各自用现有 projector 和 monitor 从持久化
    Bundle 重新计算 Risk、Oracle 和 outcome，再与 compact episode summary 核对，全程不执行 SUT。

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
- A6a 本地 provider 集成测试连续完成 2 个 episode；每个 episode 内的第二次规划收到第一次干预及 24 个
  natural-progress 动作的反馈，而第二 episode 的首次规划没有收到伪造 continuation。恢复没有重复 provider
  或 key 访问。
- A6b 同一测试验证了重复 episode 的 PSS 状态按键取并集而非相加，并检查第二次模型请求不包含 PSS 状态键、
  bundle digest 或 Oracle violation 字段；恢复重建出完全相同的终端汇总。
- A6c 真实 session 一次完成 2 个 episode。第一轮模型调用经历 3 次传输后成功，第二轮 1 次成功，证明新增重试
  实际避免了把瞬时连接问题直接封成 terminal failure；总计 2 个逻辑调用、10,984 tokens。
- 第一轮选择 1 个消息投递动作，得到 20 个唯一 Core PSS 状态；第二轮收到第一轮 `completed` 机械反馈后选择
  `deliver-message -> crash -> fire-temporal-event -> deliver-message`，得到 23 个状态，session 并集仍为 23。
  两轮 Replay 都稳定，0 Oracle violation，Risk 都只满足 workload 初始里程碑，仍为 `not-reached`。
- A7a 真实 OmniPaxos episode 的 root 有 23 个决策；Planner 从可信 frontier 选择一条
  `sequence-paxos` 消息执行 `DropMessage`，closure 再执行 7 个普通动作后达到
  `workload-invoked -> message-dropped -> workload-decided`，fresh worker Replay digest 完全相同。
- A7a 单独只证明第二协议复用最短闭环；当时尚无可编辑 JSON、qualification-bound Bundle 或 PSS/Oracle 汇总。
- A7b 已补齐前三项：资格化执行产生 31 个 Core PSS samples、28 个唯一状态、stable Replay，并由
  TraceIntegrity 和 Agreement 得到 0 violation。尚未接 session CLI 或真实 OpenRouter。
- A7c 本地 OpenRouter fixture 经正式运行入口调用 1 次，选择一条 replication message 执行 DropMessage；结果仍为
  31/28 PSS、Risk reached、Replay stable、0 Oracle violation。resume 得到字节等价 summary，provider/key 均未重访。
- 当前同时保留单 episode 校准入口和多 episode Campaign；OmniPaxos 尚未进行真实付费模型效果实验。
- 对已完成目录执行 `-campaign-resume` 得到完全相同的 episode、work、token、PSS/Risk/Oracle 汇总，没有新增
  provider 调用。
- A6e 首次配对校准使用同一 root、semantic input、`deepseek/deepseek-v4-flash` 和 session 预算。
  `full` 的 2 个 episode 均一次生成合法的相同 4 步计划，共 2 calls、17,593 tokens、966 primary work；
  `masked` 的首个 episode 经历一次 `no-match` 后修正，第二个只执行 1 步，共 3 calls、
  28,312 tokens、987 primary work。
- 两组都得到 2 个 qualified testing episode、2/2 stable Replay、0 Oracle violation，session 并集都是
  23 个 Core PSS 状态，Risk 都只满足初始 workload milestone。对两个完成目录的恢复产生完全相同的
  汇总，没有新模型调用。
- A6eR 从相同 28-decision root 立即中止旧 leader，然后只选择正常 effect completion、消息投递和
  自然 temporal event，在新 leader 出现后重启旧 leader。这条 26-decision extension 达到全部三个
  Risk milestones，新 leader 出现前没有客户端返回，最终 Trace 可 fresh Replay。
- 当前执行器会提交 stopped 计划中所有 leading applied steps，只把首个 rejected step 留作修正反馈；活动 JSON
  把 `scenario_max_steps` 设为 1，消除 future ActionID。一次战略干预后的 deterministic natural-progress closure
  与干预一起累计编译为 qualified execution。真实 Adapter 测试已验证 24-step closure 到达下一 Risk milestone、
  同 episode continuation、跨 episode root 隔离和恢复。
- A6g 首次真实 strict-schema 单步运行用 2 calls 选择 `crash n1 -> restart n1`。中间 24 个普通动作由 closure
  完成，最终 55 个 PSS samples、38 个唯一 Core PSS 状态、Risk `reached`、stable Replay、0 Oracle violation。
  完整 closure 仍保留在审计工件，但不再逐步复制进下一次模型 prompt；相同 Trace/Bundle 的复验把总 token
  从 33,215 降到 15,423，其中输入 token 从 31,870 降到 13,665。
- planning prefix 缺少 operation history 时，etcd/raft projector 会以 Adapter application-command 增长作为
  保守 terminal 事实；最终 qualified Risk 仍使用完整 OperationHistory，避免把“没有传入返回历史”当成
  “请求仍在途”。
- 首次真实 A6eR 单 episode 使用 `deepseek/deepseek-v4-flash` 完成 8 次逻辑调用且每次只有 1 次
  transport attempt，共 44,443 tokens。5 个合法短计划各提交 1 个决策，正式 prefix 从 28 增至 33；另外 3 个
  计划以 `no-match` 停止，其部分执行均按设计回滚。最终 qualified testing 产生 34 个 Core PSS samples、24 个唯一
  状态、stable Replay 和 0 Oracle violation，Risk 仍只满足 workload 初始里程碑。恢复没有新增模型调用。
- 在该成功运行前，一次 A6 session 被旧 journal 的 6-call 内部上限截断。sidecar 实际保存 6 个成功响应、
  42,664 tokens，但 Campaign failure marker 报告 0 calls/0 tokens。journal 上限已与 Scenario 全局上限对齐；
  Campaign 现在会把 provider error 返回的合法 WorkLedger 写入已有 failure marker，session 该成本由 durable
  call audit 机械汇总。本地故障回归得到 2 calls 且恢复不重复 provider。
- A6f `etcdraft-log-progress` 已在真实 Adapter 产生的 qualified Bundle 上通过。受控 Evidence 变异能在
  step 2 分别检出 commit frontier 从 2 回退到 1，以及 applied 3 超过 commit 2；结果不依赖 PSS 覆盖。
- `ready-advanced` 现在只携带当次 yield 的 applied-command witness，没有新增 item/Action 或在每个
  Evidence 快照重复历史。`etcdraft-client-application-binding` 在真实 Bundle 上通过；返回 value 变异
  被判为与 Invoke 不匹配，command witness 变异被判为缺少唯一精确见证。
- 反向检查要求每个 applied command 都匹配一个更早的 Invoke，同 request 在不同副本上的 `(index, term)`
  必须一致。将 witness request 改成未调用 ID、或为同 request 添加第二日志位置，均会在确切 step 报告 violation。
- A7H0 已恢复全仓普通测试和 race shard 清单；A7H1a 的资源回归测试验证 bounded DFS 创建的每个临时
  Adapter 都在对应重建或 Replay 完成后关闭，而不是由目标 wrapper 延迟批量回收。
- A7H1b 的通用 conformance 回归验证每个 factory Adapter 均被关闭；两套真实外部/进程型 qualification
  测试通过，不再需要目标 wrapper 兜底。
- A7H2a 的 etcd/raft 与 OmniPaxos 双 episode 回归均通过；退出后仅凭 Campaign attempt artifact 已可检查完整
  Trace、Replay、qualification、work、PSS、history 和 Bundle digest，并拒绝被修改的 Bundle。
- A7H2b1 进一步验证 Bundle、Risk 或 Oracle 任一处被修改都会使恢复失败；两协议正式 session 测试均从完整
  testing evidence 重新派生 summary，模型和 SUT 不参与判定重算。

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

当前规模为：Go 生产 31,325 行、Go 测试 14,620 行、Markdown 4,264 行、JSON 10,348 行。A6f 相对 A6eR
净增 320 行 Go 生产代码、286 行测试和 66 行 Markdown，JSON 不变。增量是两个 target-local monitor、
一份 yield-local command witness、qualified Bundle 接线和受控反例测试；没有新增 package、Runtime、Action、
评分公式、契约层或平行 ledger。完整真实工件
位于 ignored `artifacts/agentic/`，不进入 Git。

## 当前边界

- A6b 汇总的是当前单一 RiskWitness 和 Core PSS；固定 Profile 义务覆盖尚未进入该 session 视图；
- session 终端视图目前在运行/恢复时派生并通过 CLI 输出，没有另存一份会与 Campaign 漂移的 summary 文件；
- A6e 只有一组、每组 2 episode 的公开配对；`full` 的无修正和低成本是正向观察，但它两次重复同一计划且没有
  改善 Risk/PSS，因此不能宣称协议语义已稳定提高 Agent 效果，23 个状态也不是覆盖百分比；
- 26 步是当前确定专家调度的长度，不是经过穷尽证明的最短 witness；它已足以证明“只给 Agent 四步且完成后立即终止”
  不适合当前正常控制路径，但不宣称所有更短路径均不存在；
- planning projector 的 application-command fallback 只适用于当前单 proposal workload；多并发请求仍必须依赖
  OperationHistory 或更精确的 target-local operation identity，不能用命令总数推断某个请求的终态；
- 旧 A6eR 的 3/8 次 `no-match` 来自未来 ActionID。当前单步接口已经消除该失配，并由真实运行验证；这只证明
  当前公开 Risk 的闭环可达，不证明模型在未知场景或同预算对照实验中更优。
- 成功运行的 24 个 PSS 状态只比旧 A6e 的 23 多 1，输入、prompt 和时域已经变化，不能把这一个状态差
  解释为 Agent 优势；
- provider/local episode error 发生在 Campaign attempt 提交前时，现在会记录已发生且可验证的 provider Work；
  其他无法从 durable audit 确认的 primary 部分仍不应被推测或补写；
- log-progress 当前按 `NodeRef(node, incarnation)` 分段，不把 crash 后未持久的 volatile commit 回退误判为跨重启违例；
  持久化边界的跨 incarnation 检查需要独立 storage Evidence，本阶段不猜测；
- token 统计只包含 provider 最终返回的 usage；无响应尝试是否已在 provider 端产生计费无法从本地确认，
  因此费用解释必须同时报告 `transport_attempts`；
- 还没有非公开 candidate/control 方法效果实验；
- resume 入口仍会在读取 Campaign 前准备 root/qualification 等目标输入；虽然 artifact 判定重算不执行 SUT，
  入口级恢复还不是无 SUT 重访，A7H2b2 需要调整恢复顺序；
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
每个已提交 attempt 的 artifact 是现有 A4c 紧凑 summary。下一 attempt 从统一 root 重新开始，不再提取前一
episode feedback；机械 feedback 只服务同一 episode 的真实 continuation。Campaign checkpoint 绑定预算、目标、
实验输入和 artifact，停止后恢复不再次访问 provider。

该阶段没有新增 Runtime、Replay、Oracle、Ledger、schema 或 digest。测试证明两次 episode 的组合与恢复可用，
不证明 Agent 优于 baseline，也不证明跨 episode 已发现更多语义状态。

## 已完成：A6b session 结果聚合

session attempt artifact 在原 A4c 紧凑 summary 外只增加 qualified Core PSS 状态键，不修改 A4c 历史格式。
终端视图报告 Agent completed/stopped episode、testing/replay-stable episode、PSS 样本总数与状态并集、最佳
Risk 进展、Oracle violation 总数，以及原 CampaignSummary 的 work/model/time 成本。

相同 episode 的状态键测试证明并集不会退化为数量相加。新 episode 的首次 provider 请求不含上一 episode
feedback，PSS、Bundle 和 Oracle 字段也不会进入 Agent 视图。恢复只读取 Campaign artifact，并重建完全相同的结果。

## 已完成：A6c 真实 OpenRouter session 校准

真实 DeepSeek session 在同一 Campaign 中完成两轮独立 qualified testing。第一轮 3 次传输才成功，第二轮 1 次
成功，因此有界传输重试是本轮实际需要的可用性修复，而不是把失败结果重命名。两轮共使用 10,984 tokens、
669 primary work 和 131 replay work，得到 23 个 Core PSS 状态并集、稳定 Replay、0 Oracle violation；
RiskWitness 未达到。恢复未重新读取 key 或调用 provider。

## 已完成：A6d 可信执行边界修复

本阶段按以下顺序落地：

1. Replay 对 invoke/partition 使用正常 Offer API 重建，拒绝未知节点、不可接受输入和 Action 不一致；
2. `Trace.Validate` 验证记录内部结构，`ExecutionBundle` 结合 preparations 验证完整状态链；
3. native Action 改为 `validate -> Adapter apply -> Runtime commit`；
4. 新增重新封装的断步 Trace 与未知 partition 节点 Replay 回归测试。

实现后，Bundle 接受与 TraceIntegrity monitor 使用同一个 preparation-aware 状态链规则，不再出现 Bundle 先接受、
monitor 再以另一套规则判无效。`go test ./...` 已覆盖 etcd/raft、OmniPaxos、HashiCorp Raft、qualification 和
control-experiment 主路径。这里证明的是两个已知伪造路径被机械拒绝，并不代表 Trace 验证已经形式化完备。

## 已完成：A6e 协议语义提示接入

- 新增独立 `action_semantics`，不污染公共 `FrontierActionRef`，因此 A2/DFS 输入未扩大；
- 语义只允许 leader/replica/contender、vote/proposal/replication/heartbeat/recovery、stale/current/future、
  none/inflight/decided-not-applied 及 `unknown`；
- etcd/raft target projector 从可信 Evidence、消息 TypeHint/metadata 和 Risk progress 生成分类；
- 提示绑定当前 prefix、snapshot、ActionID 与 ActionDigest；
- masked 保留相同 Action 顺序和身份，只隐藏四个语义值；
- exposure mode 已绑定现有 run spec，修改模式不能恢复旧 Campaign；
- A6 session CLI 可用 `-scenario-semantic-exposure full|masked` 覆盖 JSON 输入，便于同一输入在两个独立目录
  运行配对校准；它复用现有 session、spec 和 summary，不增加配对 ledger；
- 本地测试验证提示不包含原生 `StateLeader`、`MsgApp`、payload、Oracle 或 verdict。

## 已完成：A6eR 单步干预与自然推进

活动模型每次只输出 1 步当前 Action。成功干预后，可信 closure 只执行 effect completion、message delivery
和自然 timer，直到 Risk 里程碑变化、客户端返回、自然推进静止或预算结束，再重建 RiskFrontier、Snapshot 和
target-local 语义继续调用 Planner。当前 episode 最多 8 calls、32 decisions，Campaign 继续限制
primary/replay work、token 和 wall time。stopped 计划保留 leading applied prefix，rejected step 不进入最终 Trace。

现有 semantic calibration spec 绑定 prompt version、calls、plan steps 和 decisions，防止同一运行目录在这些
作者输入改变后错误恢复。这里复用已有 spec；没有新增账本、评分、Runtime、hash 层或 gate。

本地 provider/真实 Adapter 集成测试验证一次 crash 后 24 个自然动作到达下一 Risk milestone、同 episode
第二次干预、Campaign 下一 episode 重置为 28，以及恢复不重复访问 provider。首次真实模型旧运行仍只证明
旧 receding-horizon 路径工作；新闭环尚未进行付费模型效果实验，也尚未与共享 closure 的 baseline 比较。

## 已完成：A6f 第一个最小共识 Oracle

`etcdraft-log-progress` 顺序读取 Trace 中的 Adapter Evidence，对每个 `NodeRef` 保留上一个 commit/applied
frontier。它检出 `Applied > Commit`、commit 回退和 applied 回退，并将首个反例绑定到确切 Trace step。
监视器保留在 etcd/raft composition；在第二个协议出现相同真实消费者前，不提前下沉公共抽象。

## 已完成：A6f applied-command witness

复用 etcd/raft Adapter 已有的 `ready-advanced` typed observation，只在该 yield 真正应用命令时输出
命令见证。monitor 通过已有 item transition 将见证、ClientResult 和实际 Invoke 绑到同一 return step。
不新增 Action、
全量命令快照、通用 linearizability DSL 或另一套记录。

## 已完成：A6f proposal/workload validity

在同一 command witness 上做反向检查：每个 applied user command 必须对应一个更早的实际 Invoke，且
request/origin/value 一致。这不要求命令已经产生 ClientResult，因此与当前“从成功返回找应用见证”
的方向独立。

## 已完成：A7a 第二协议最短 episode

OmniPaxos 直接复用了 `RiskFrontierView`、`ExploreScenarioWithPlanner`、单步战略干预、可信 natural-progress
closure 和 fresh Replay。新增代码停留在目标边界：Adapter 导出只读语义 Evidence，组合层定义目标 Risk 和
Action hints。唯一被两个真实目标共同使用的“取最新 Trace Evidence”助手已移出 etcd/raft 文件。

本阶段没有新增 Action、Runtime、episode 契约、schema、hash、baseline 或 gate。A7b 在下节将该路径接到
OmniPaxos 的可编辑 authoring 输入和 qualification-bound testing result，仍未复制 etcd/raft session CLI。

## 已完成：A7b authoring 与 qualified testing result

活动输入位于 `plans/agent/omnipaxos-message-loss-before-decision-v1.json`。它只包含本目标实际消费的协议知识、
测试假设、单写 workload、Runtime/消息丢弃边界与 Scenario 预算；worker 路径和 provider 配置没有混入协议知识。
加载后仍由已有构造器产生 ProtocolKnowledge、TestHypothesis、PayloadEnvelope 和 WorkloadPlan。

目标组合运行现有 OmniPaxos qualification，并只申请已经验证的 yield、enabled check、自然时间、消息控制、
decision replay 与 opaque invoke 六项能力。成功 Scenario 被编译为 exact policy，进入唯一
`ExecuteQualifiedBundle`，结果包含同一 Trace、31 个 PSS samples、28 个唯一状态、stable Replay，以及
TraceIntegrity/Agreement 零异常。etcd/raft 与 OmniPaxos 相同的结果外壳已合并；协议 Risk 和 monitor 未合并。

## 已完成：A7c OpenRouter 与可调用入口

原 etcd/raft episode 中的 journal、Frontier/semantics 准备、Planner、closure 和 audit 组合已提为共享 core；
etcd/raft wrapper 只保留目标 projector 与 qualified executor。OmniPaxos wrapper 复用该 core，并继续负责 worker、
Risk、PSS/Decision projector 和 qualification。

新策略 `omnipaxos-openrouter-scenario-a7c` 接受 `-campaign-dir`、`-worker`、`-semantic-input`、`-agent-key-file`
和 `-agent-model`。虽然沿用现有目录参数名，但本阶段目录只承载单 episode journal 与 `summary.json`，不能被描述为
多 episode Campaign。共享 compact summary 删除了 etcd/raft 与 OmniPaxos 的重复结果格式。

下一步 A7d 再决定是否以同一 Campaign coordinator 组织多个独立 OmniPaxos episode，并核对预算/聚合是否能直接
复用；在此之前不复制 etcd/raft session 文件。

## 已完成：A7d OmniPaxos 多 episode Campaign

新策略 `omnipaxos-openrouter-session-a7d` 使用现有 Campaign coordinator 连续运行独立 OmniPaxos episode。
authoring JSON 直接声明 session 的 episode、primary/replay work、模型调用、token 与 wall-clock 上限；每轮仍从同一
可信 root 开始，并分别经过 qualification-bound execution、fresh Replay、Oracle、Risk 与 PSS 投影。

etcd/raft 原有 322 行目标内会话实现已收敛为共享 attempt journal、artifact 校验、PSS 并集、最佳 Risk、work/model
计费和恢复汇总；两个目标的 wrapper 只组装各自输入与 executor。新增 OmniPaxos wrapper 没有修改公共 Action、
Runtime、Replay、Oracle 或 episode 契约。Campaign 的实验摘要复用现有规范化摘要，绑定知识、假设、workload、
execution admission、运行预算和 provider transport，防止输入改变后错误复用已提交 artifact；未新增冻结文件、
版本、baseline 或 gate。

本地 provider 结果为 2 个 completed/testing/replay-stable episode、2 次模型调用、Risk reached、PSS 状态并集和
0 Oracle violation；停止原因为 attempt limit。恢复未再次读取 key 或访问 provider。该结果证明同一 Campaign
机制能服务第二个非 Raft 目标，不证明两个相同 episode 带来额外覆盖，也不构成 Agent 效果比较。

下一阶段先完成 A7H：恢复活动测试/race 清单，持久化完整 Bundle，修复成本与 worker 生命周期，显式绑定
natural-progress 方法身份和恢复输入；不增加新协议或 Agent 类型。完成后再进入 A8。

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
