# 当前阶段

更新时间：2026-08-17
分支：`feature/agentic-consensus-testing`
阶段：M4k5c Risk 解释边界校准（实现完成，待重新运行配对）。

## 一句话状态

活动系统已经收敛为“Risk Agent → Scenario Agent → 两个真实 Target → 确定性执行/Replay → PSS/Risk/Oracle”。
client terminal 只表示 workload 已返回；Risk 未达且预算尚存时继续调查。旧 A2/A8/Campaign 路径已从生产代码
删除。当前 Agentic Episode 的 `summary.json`/`bundle.json` 已能直接进入现有 private pair/exposure/
Oracle 评测边界，不启动第二套搜索或执行器。
Scenario Agent 现在输出显式 Investigation Proposal；branch/control/ablate 复用现有 Runtime、Trace、Replay
和 Oracle，不引入第二套分支执行引擎。实验分支只保存候选；`continue + from_branch_id` 可边继续执行边晋升，
零 Action 的 `select + from_branch_id` 可直接选择已有分支。无论 Agent 是否选择，所有唯一且 fresh-Replay 稳定的
候选分支都会离线进入可信 Target Oracle；因此最后一个 decision 上出现的 finding 不会因预算同时耗尽而丢失。
跨 Episode 的 Agent-facing Memory 只包含机械执行状态、Risk 进展、PSS 和成本，不包含 Oracle finding
数量，也不使用 Oracle 派生的 assessment status 选择 outcome 或代表路径。
一次调查当前最多使用 512 个成功 Action。预算按剩余 Agent 调用次数自动切成周期反馈片段，使长时间自然推进
不会一次耗尽全部 decision budget；每个片段的战略计划和自然推进共享同一个 live Runtime。
协议无关校准之外，etcd/raft 与 OmniPaxos 现在都已有 128 Action 的真实 Adapter → Bundle → PSS →
Observation → Oracle 垂直回归。
正式 Agentic evaluator 现在核算 `Scenario frontier + Scenario search + 所有 qualified primary`，
单独汇总 Replay，并将模型 calls/tokens 与执行工作都对照同一份
`AgenticLogicalBudget`。正式 Episode 的主路径和分支 Bundle 必须为 V3，且
`MethodSpecDigest`、Trace digest 和 work 必须与 summary、branch evidence 及 formal contract 交叉一致。
搜索中的 fresh child verification 归入 Replay，而不是 primary。活动 CLI 从实际模型 transport、
prompt/semantic input、源码暴露、Episode 数与预算派生 typed `AgenticMethodSpec`。正式多轮 trial
只能提交完整、连续的 Investigation；所有 Episode 的模型、搜索与候选执行成本一起核算。
Scenario Agent 在获得可信 `ProgressDelta` 后还可以用零 Runtime 成本的 `abandon` 主动结束低收益假设。
它不是 verdict；停止原因只作为机械 Memory 交给下一 Episode 的 Risk Agent，已产生的证据和成本仍然保留。
Risk Agent 的源码入口不再在首个成功片段后关闭：一次调查最多读取 4 个 Dossier 声明片段、每次调用最多
2 个、每个最多 80 行；可从前一结果的 `end_line + 1` 继续同一文件，或查询另一个声明 reference。
重复或重叠窗口、未声明 reference、越界路径和非文本内容仍由可信读取器机械拒绝。实际源码暴露模式、prompt
版本和实现版本继续进入 typed `AgenticMethodSpec`，因此旧方法工件不会被误认为当前 M4b 方法。

M4c 已在用户授权的四文件隔离 mount 上运行真实 `deepseek/deepseek-v4-flash`。首个得到完整响应的
Episode 使用 3 次 Risk 调用和 1 次 Scenario 调用，共 59,063 tokens；Risk Agent 自主读取
`worker/src/main.rs:tick` 的 50 行窗口并生成通过机械资格检查的恢复假设。原 50,000-token 阈值在
Scenario 响应返回后被越过，因此该响应没有进入可信 Scenario 解析，Runtime/PSS/Replay/Oracle 均未执行。
系统现已允许“已计费但因 token 阈值未交给 planner”的 provider audit 独立于 Scenario attempt 落盘，
且 token-stopped Episode 不会自动启动下一轮。显式实验预算已提高到每 Episode 120,000、六轮合计
720,000 tokens，但后续同字节请求在 900 秒内未返回，记为 transport ambiguous；它不是 Agent 或协议结论。

M4c 的主校准通道调整为 DeepSeek 官方 API；OpenRouter 保留为使用独立 MethodSpec、独立工件和独立成本的
对照通道，同一 Investigation 内不自动 fallback。这个决定只减少动态 provider 路由变量，不把官方接口视为
无超时或无费用歧义。共享 transport 收口已经完成：`session_wall_clock_ms` 现在创建真实 Episode deadline；
provider、endpoint、thinking、structured-output 模式、stream/timeout、路由与 fallback 已进入现有 MethodSpec；
usage unknown 会保留一次已派发调用并报告 `unreconciled_model_calls`，不能进入正式 Episode 工件充当零 token。
provider-neutral journal 下已有 OpenRouter strict `json_schema` 与 DeepSeek 官方 `json_object` 两个独立实现，
且没有跨 provider 自动 fallback。本地 typed parser、Schema/语义校验和 repair feedback 继续拥有接受权。

首个 DeepSeek 官方 OmniPaxos 单 Episode 校准已真实运行。Risk 调用以 32,201 tokens 返回三个候选并接受
`timer-symmetry-recovery-lapse`；Scenario 调用以 24,918 tokens 返回四步计划，但因两个 node selector 字段使用
对象而不是字符串被本地 typed validator 拒绝。修正调用在 300 秒 Episode deadline 仅剩约五秒时被取消，
usage 为 unknown。两次已对账调用合计 57,119 tokens；没有 Action、PSS、Replay、Oracle 或 finding。
因此下一 MethodSpec 将 Episode wall-clock 调整为 1,200,000 ms，同时保持 6-call/120,000-token 上限；歧义调用
不自动重发，本轮工件也不作为协议或 Agent 效果结论。

使用 1,200,000 ms 的 fresh v2 单 Episode 已完整闭环：1 次 Risk 和 3 次 Scenario 共 116,912 tokens，
最终生成 29-record V3 Bundle，primary/replay 为 29/29，PSS protocol/control/joint 为 4/27/28，Oracle 为 0。
Risk 未达到，首个缺失 milestone 为 `request-at-leader`。三次 Scenario 调用分别暴露 node selector 类型错误、
stopped feedback 后仍使用 `continue`，以及把 `after_milestone=request-at-leader` 放在创建该 milestone 的 Invoke
步骤上。Scenario prompt v11 明确 node ID 必须为字符串、停止后必须 `revise`，并禁止 milestone 前置条件自依赖；
该修改不改变可信执行或 verdict 边界。

首次两 Episode 尝试在 Episode 1 提前终止：1 次 Risk 与 3 次 Scenario 共使用 121,149 observed tokens；
第三次 Scenario 占满 32,000 output tokens 且未以 `stop` 结束，被记录为 `agent-response-rejected`。Episode 2
没有启动，也没有 terminal summary、Bundle、PSS、Replay 或 Oracle。该失败明确说明 Risk 与 Scenario 不应
共享 high-thinking transport。M4c-v4 保留 Risk `thinking=high`/32K，同时将 Scenario 独立绑定为
`thinking=disabled`/16K；两者都进入同一个既有 MethodSpec，Episode 总预算和可信执行边界不变。

fresh v4 两 Episode 已终止并完整落盘：8 次 DeepSeek 官方调用均一次成功且 usage 可对账，
共 115,194 tokens。两轮 Risk Agent 都一次返回 3 个候选，并展示了新候选与 fidelity gap；
但 Scenario disabled-thinking 的 6 次响应均未通过 proposal contract，实际 Action、PSS、Bundle、Replay 为 0。
所有响应都使用了本地禁止的 `continue + branch_id`；停止后又重复 `continue`。这次失败同时
暴露本地契约矛盾：prompt 说 stopped 后必须 revise，但 `available_intents` 仍提供 continue/branch/revise；
parse/validate 失败又只返回通用 `proposal-invalid`，未保留已解码 proposal 的字段级错误。

M4d 因此不修改共识算法、Adapter、PSS 或 Oracle，而是先将 Scenario 协议收敛为阶段化最小契约：

- 首次调用只推进当前路径；
- proposal/step 停止后只暴露修复所需的 intent，已有 ProgressDelta 时才可放弃；
- branch/control/ablate/select 只在已有可执行路径或存储分支后出现；
- 精确验证 issue、allowed intents 与有界 previous proposal 进入下一轮反馈和 compact summary；
- 第二步以后的当前-frontier ActionID 由 typed validator 机械拒绝。

上述实现已经落地：Scenario prompt v12 在单一 `continue`/`revise` 阶段只暴露 `intent + plan`；
可信解析器在可解码 proposal 失败时保留结构并返回稳定字段级 issue；`allowed_intents` 与 attempt feedback
进入下一轮和 compact summary；真实 v4 的六份失败响应已成为普通回归输入。Scenario transport 恢复为
high-thinking/32K，并以 `m4d-v1` 进入既有 MethodSpec。受影响包和全仓普通测试均已通过。

M4e 的付费实验以“至少一个 Action 执行并形成 fresh-Replay 稳定 Bundle”为成功门槛，不要求 Risk 到达或
finding。失败必须根据精确 issue 区分 proposal repair、selector no-match/ambiguous 与 Target capability gap，
不再把三者统一解释为 Scenario Agent 能力不足。

M4e 的无费用预检已复用现有 OmniPaxos 垂直测试完成，没有新增 fixture provider 或第二套执行路径。
预检机械确认：实际 Scenario provider 请求只暴露 `continue + plan`；fixture 返回同一合法形状后，系统执行
真实 frontier Action，产生非零 Scenario decisions、qualified primary/replay、PSS 和 Oracle 检查，并完成
fresh Replay。该结果只证明 prompt v12 与可信执行底座相容，不证明真实模型会生成同样的计划，也不构成
Agent 有效性或协议问题证据。

经授权的 M4e DeepSeek 官方单 Episode 已完成。Risk 1 次、Scenario 3 次，共 4 次调用和 105,334 tokens；
三次 Scenario proposal 都越过 typed proposal contract 并进入可信执行。最终执行 8 个 Scenario decisions，
形成 31-record V3 Bundle，execution primary/replay work 为 33/33；PSS protocol/control/joint 为
6/28/29，共 32 samples，Oracle finding 为 0。终态工件可用 `-campaign-resume` 在不调用 provider 的情况下
完整恢复。由此 M4e 的“Action → Bundle → Replay”最低成功条件已经满足。

这不是 Agent 或协议效果成功：`timer-symmetry-recovery-lapse` 未达到，三次尝试依次停止于
`milestone-unreachable`、`no-match`、`ambiguous`，最终为 `scenario-call-budget-exhausted`。首个缺失
milestone 是 `client-invoked`。具体根因之一已经明确：Risk predicate 将 `participant` 与
`participant-node` 同时绑定为 `n1`，而实际值分别为 `n1@1` 与 `n1`，因此即使 Trace 已包含 Invoke，
该谓词仍机械不可满足。下一阶段优先修复 binding domain 表达与检查，并改善既有 selector 的
no-match/ambiguous 反馈；不增加新 Action DSL。

Scenario 落地能力是并行的第二条阻塞线。当前 `actor_role`、`message_class`、`epoch_relation` 和
`operation_state` 只提供提示、不参与 selector。下一步先让这些已有、跨协议的可信语义参与当前 frontier 的
唯一 Action 解析；只有普通语义仍不足且出现明确 no-match/ambiguous 样本时，才增加 Target-local selector，
不扩大全局 ActionKind，也不让 Agent 直接制造 ActionID。

M4f 已根据上述真实失败样本完成。Observation capability 现在机械派生 Agent 可见的 `binding_domains`：
`participant`/`related-participant` 属于包含 incarnation 的 `node-incarnation`，
`participant-node`/`related-participant-node` 以及 Target 声明的 `node-id` attribute 属于 `node-id`；
其他 Target-local field 保持各自的字段域。Risk Agent 若让同一 `bind_as` 跨不兼容域复用，会在
candidate assessment 阶段收到 `risk-candidate-binding-domain-mismatch` 精确反馈，不再消耗 Scenario 调用和
Runtime 决策。该域表完全由现有 capability 派生，Adapter 不维护第二套配置。

Scenario 的 selector 失败反馈新增 `selector_trace`。它按固定字段顺序记录每个过滤条件之后的候选数：
`no-match` 的第一个零候选字段就是具体冲突，`ambiguous` 的最终非一候选数说明还需要哪个稳定字段进行收窄。
完整信息进入下一次 `revise` 的 trusted feedback；compact summary 也保留失败 step、最终 match count 和 trace。
M4e accepted Risk 的六个实际 predicate 形状以及三份真实 Scenario proposal 已成为普通回归输入。这个修复没有
增加 Action/selector DSL、评测 gate、hash 或 baseline，也没有改变 Runtime、Replay、PSS 和 Oracle。

M4g 已闭合 `ScenarioSemanticExposure` 与 frontier matcher 之间的断层。
`actor_role`、`message_class`、`epoch_relation`、`operation_state` 现在是可选的通用 selector 字段；
可信执行器在每次 live frontier 刷新后，用 Action ID/digest 绑定的 Target projector 重新计算提示，
再同时过滤普通 Action 字段和语义字段。Agent 仍不能制造 ActionID，`unknown` 也不能被当作选择值。

为保留既有 Action-only API，直接调用 `ExecuteBoundedScenarioPlan` 时使用 masked unknown 语义；
Agent 路径调用 `ExecuteSemanticBoundedScenarioPlan`，必须提供 Target 的 `ScenarioSemanticProjector`。
这避免了 Core 引入 Raft/Paxos 分支，也不要求 Adapter 新增协议专用 selector。

本地垂直回归已证明：OmniPaxos 用 `message_class=replication` 配合通用目标字段选择并执行真实
message Action；etcd/raft 与 OmniPaxos 的 128-Action 长轨迹都使用同一
`actor_role/operation_state` selector 完成。Scenario prompt 升为 v13，structured output 升为 v5，
因此新 MethodSpec digest 与 M4e 工件分离。

M4h DeepSeek 官方单 Episode 已完成。Risk 1 次、Scenario 3 次，共 4 次调用和 97,522 tokens；
accepted Risk `recovery-after-single-drop` 的绑定全部位于 node-ID 域，没有重现 M4e 的跨域复用。
三次 Scenario proposal 都进入可信执行，且不再出现 `no-match` 或 `ambiguous`，而是统一停止于
`milestone-unreachable`。最终 16 个 Scenario decisions 形成 39-record V3 Bundle，primary/replay work
为 41/41，fresh Replay digest 与 primary 完全一致；PSS protocol/control/joint 为 4/37/38，Oracle 为 0。

本轮没有发现 OmniPaxos 问题：Risk 未达到，首个缺失 milestone 是 `leader-handoff`，最终因三次 Scenario
调用耗尽成为 `search-budget-exhausted`。它只证明 M4f/M4g 消除了本样本中的 binding-domain 与
selector no-match/ambiguous 失败。这一结果成为 M4i 的回归输入：反馈必须解释“已完成 invoke/drop/timer、
但 coordinator 尚未变化”，而不是先增加 Episode 数、Action DSL 或协议专用 selector。

M4i 已完成本地收口。等待 `after_milestone` 时，客户端结果已返回和自然闭包无 Action 分别报告
`milestone-wait-client-terminal` 与 `milestone-wait-quiescent`；它们不再共用 `milestone-unreachable`。
新的 `ProgressDelta` 会机械报告 milestone advanced/stalled/repeated/reached、Action kind 计数、新 milestone
evidence 的 step/kind、Timer callback 与真实逻辑时钟推进、fault allowance/usage/remaining，以及当前可用的
非闭包干预类型。Scenario prompt 升为 v14，并明确这些字段只指导 revise/continue/abandon，不创建可达性或缺陷结论。
紧凑 Episode summary 会持久化每轮同一 ProgressDelta，离线恢复因此可以审计 Agent 实际获得的反馈。

本阶段没有改动具体共识、Adapter、PSS、Oracle 或公共 ActionKind，也没有再次调用模型。生成共识可测试性接口的
非冻结草案已写入 `docs/generated-consensus-testability.md`，继续复用 PortableCFTProfile/Manifest/组合测试，
不建立第二套资格 contract。具体协议 host/wrapper 改造按用户要求暂缓。

M4j1 已把草案中已有、但此前未贯通的事实接入现有主链。Manifest 可选声明消息 type hint/metadata key，
以及每类 HostEffect 的 phase、durability 和允许完成/失败结果；Runtime 会拒绝超出声明的 emission。
同一声明由 `AgentTargetSurface.capabilities` 机械投影，当前 enabled frontier 进一步公开实际依赖、消息提示/
metadata、effect phase 和本 Action 的 outcome。Scenario selector 可用 `message_type_hint`、`effect_phase`、
`effect_outcome` 精确绑定真实 Action，不能创建新消息、效果或结果。

协议无关 described fixture 已覆盖 `Manifest → emission validation → enabled frontier → complete/fail → Trace →
fresh Replay`，并另有超出消息声明即进入 terminal failure 的负向回归。普通历史 fixture 保持原 identity 和 Trace，
因此没有更新旧 digest 或 baseline。本阶段没有修改 etcd/raft、OmniPaxos、Hashicorp Raft、公共 ActionKind、PSS
或 Oracle，也没有调用模型。Scenario prompt 升为 v15，structured output 升为 v6，新的 MethodSpec 会与 M4h
工件自然区分。

M4j2 没有增加新的测试 DSL 或资格 contract，而是把现有 Scenario selector 与同一份
`AgentTargetSurface` 做机械预检。若计划请求非 composable Action，或请求可选富能力声明明确排除的消息 type hint、
HostEffect phase/durability/outcome，本轮在执行前返回 `missing-action` 或 `missing-control-capability`；失败 step、完整
proposal 和 capability gaps 会进入下一轮 `revise`，且不消耗 Runtime decision。当前 frontier 的精确 ActionID 也会先
反查 Action kind，因此不能绕过 composable 边界。若旧 Target 没有可选消息/HostEffect 细节声明，Core 不猜测其能力，
仍交给真实 frontier 匹配，避免把“未评估”误写成“不支持”。协议无关回归已证明错误计划不产生 Runtime work、修订后
计划正常执行并 fresh Replay；Scenario prompt 升为 v16。本阶段仍未修改任何具体共识实现，也未调用模型。

M4j3 修正了最后一轮能力缺口被覆盖为普通 call-budget/planning failure 的归因问题。如果没有可交付路径且最后一次
可信预检返回 capability gap，Scenario 以独立 `capability-gap` 停止；Episode assessment 保留具体
`missing-action`/`missing-control-capability`，compact artifact 保存 gap 与失败 step。后续 Episode 的 Risk Agent
只能看到这些机械 reason code，不会看到 Oracle 结论，因此可以避开当前 Target 无法执行的方向，而不会把基础设施
缺口误学成协议假设无效。该收口未增加 Action、预算、hash、baseline 或新资格 gate。

M4k1 开始把重心转向 Agent 能力。跨 Episode Memory 不再只给 Risk Agent 一个宽泛 reason code，还会携带可信
Scenario preflight 已保存的 `code/reference/summary` 结构化 gap。Risk prompt v3 明确要求：若上一轮机制依赖该 Target
不可用的控制，应结合当前 `target_surface` 改用可执行机制或其他性质，而不是重复请求或宣称协议问题。消息 type、
Action kind 以及 effect kind/phase/outcome/durability 的具体请求会保留在摘要中；Memory 深拷贝和类型校验防止 Agent
回写调用方证据。该信息完全来自 Manifest/TargetSurface/Scenario 机械检查，不包含 Oracle finding，也不赋予新 Action。

M4k2 从现有 durable Scenario attempt 顺序机械计算四个计数：`capability_gap_attempts`、
`repeated_capability_gap_attempts`、`capability_repair_attempts` 和 `capability_repair_executions`。重复签名使用
gap code + 请求摘要而忽略可被 Agent 重命名的 step ID；repair success 只在 gap 后的下一次计划没有新 gap 且真实进入
可信执行时计数。Episode summary 保存局部计数，Investigation CLI 按所有 Episode 的连续 attempt 顺序重算，因此能观察
跨 Episode 修订。它们只衡量 Agent 是否利用能力反馈，不创建 coverage、finding 或协议 verdict。

M4k3 使用同一个协议无关 fixture Target、相同 root Trace、一次 Runtime decision 额度和两次 Scenario 调用做了本地
scripted 消融。忽略结构化 gap 的基线连续两次请求同一非 composable `fail-effect`，结果为 gap attempts=2、
repeated=1、repair attempts=1、repair executions=0、Runtime decisions=0；读取 gap 的处理组第二次改用 `crash`，
结果为 1/0/1/1、Runtime decisions=1，并达到同一可信 Risk milestone。两组均经过真实 Scenario preflight，处理组
进入 Runtime/fresh Replay；这只证明反馈与指标接线有效，不证明真实 LLM 会采用反馈，也不属于缺陷发现实验。

M4k4 将这项消融从脚本自报变成活动运行配置。Agentic CLI 新增
`-capability-feedback reason-codes|structured-gaps`，新运行默认使用 `structured-gaps`；Risk Agent 实际收到的
跨 Episode Memory 由同一字段机械投影。`reason-codes` 仅保留既有机械 reason codes，`structured-gaps` 额外保留
可信 `code/reference/summary`。该字段进入现有 typed `AgenticMethodSpec`，因此改变暴露方式必然改变方法 digest，
Episode 组合也会拒绝 MethodSpec 与实际投影不一致。历史 MethodSpec 缺少该可选字段时仍按旧的 reason-code 视图
验证原 digest，不重写历史工件。本阶段未增加第二套 hash、gate 或 baseline，也未调用模型。

首次 M4k5 `reason-codes` 真实尝试没有形成有效配对。v1 的第二次 Risk 响应打满 32,000 输出 tokens 并以
`agent-response-rejected` 终止，两次调用共 80,670 tokens；终态调用不能在原 journal 中被静默重试。fresh v2
接受 Risk 并执行 10 个 Scenario decisions，但 4 次调用累计 121,571 tokens，越过 120,000-token Episode 阈值，
第二 Episode 未启动。更关键的是 v2 的 `capability_gap_attempts=0`，因此反馈模式从未被激活。继续直接运行
structured arm 只能比较模型随机性，不能回答结构化 gap 是否有效，故没有派发该 arm。

M4k5a 将校准改成单 Episode 的真实 Target 机械探针。独立 JSON 只描述一个不执行的 ScenarioPlan；可信代码用
实际 `AgentTargetSurface.ScenarioCapabilityGaps` 预检，要求产生至少一个 `missing-action` 或
`missing-control-capability`，再形成零模型/零执行 work 的 calibration Memory。reason-code arm 隐去 gap 细节，
structured arm 保留同一 `code/reference/summary`。探针计划和单次 Scenario 限额进入既有 MethodSpec；实际 Memory
必须与 Target 重新推导结果一致。OmniPaxos v1 探针请求 `crash n2`，真实 surface 机械返回 missing-action。
两组各只运行一个 Episode、最多一次 Scenario 调用，从而在显著降低 token 成本的同时保证实验变量在首个 Risk
请求前已经激活。探针不是协议执行证据，也不能获得 finding credit。

M4k5b reason-code arm 已证明探针接线有效，但仍未到 Scenario：首轮 3 个 portfolio candidate 都因
`risk-candidate-mechanism-unaligned` 被拒绝，修订调用再次打满 32,000 输出 tokens。两次共 78,182 tokens。
原始响应检查表明 `suspected_mechanism` 已正确留空、milestone/kind 对齐，唯一共同失败是 6 个 rationale 中少量
长度为 161–182，而旧单步上限为 160；每个完整派生 mechanism 仅 1105–1186，远低于既有 2048 总上限。
M4k5c 因此把单步 rationale 上限放宽到 256，同时保留 2048 总上限，并在 prompt 明示字节边界。Risk prompt
升为 v5，旧失败 MethodSpec 不会与新实验混淆。该调整不改变候选数量、Action、执行、Replay 或 verdict 边界。

## 当前输入

每个活动 Target 提供：

1. `plans/agent/*.json`
   - `ProtocolKnowledgePack`；
   - Property、Historical Issue Pattern 和 Target Dossier；
   - workload；
   - Runtime/FaultEnvelope；
   - Scenario、模型和 Investigation 预算。
2. Target composition
   - Adapter factory；
   - Qualification/Admission；
   - composable Action；
   - target-local Observation projector；
   - target-local Oracle registry；
   - fidelity boundaries。
3. 可选只读源码 mount 和显式 provider/key/model；DeepSeek 官方与 OpenRouter 使用不同 MethodSpec。

输入不包含预置 Risk 或 TestHypothesis；两者由 Agent 候选和可信转换生成。

## 当前处理流程

```text
Protocol materials
    ↓
Risk Agent portfolio / source query / mechanical feedback
    ↓
Candidate qualification + fidelity assessment
    ↓
trusted TestHypothesis / RiskWitnessSpec
    ↓
Scenario Agent investigation proposal + bounded plan
    ↓
enabled frontier binding
    ↓
one live Runtime branch per proposed path
    ↓
fresh Replay + observations + PSS + Oracle
    ↓
durable summary/bundle/branch-evidence/journals
```

关键终止语义：

- client terminal：只结束当前自然推进段；
- Risk reached：可结束当前调查；
- 模型调用、token 或 Runtime decision 预算耗尽：机械停止；
- 调用耗尽但只有未选择的实验分支：Scenario 保留 `final-selection-required`，不任取最终路径；这些分支仍会独立
  生成 qualified evidence 并运行 Oracle；
- 已存在分支且进入最后 Scenario 调用或 decision 已耗尽：最后调用只允许 `select`，
  模型调用和 token 正常计费，Runtime decision 成本为零；
- quiescence：返回反馈；若仍有可规划战略 Action 和预算，可再次调用 Agent；
- failed Action：不进入成功 Trace，另存 terminal outcome。

## 本轮修复

### 1. 终止语义统一

删除了“完整计划在 client terminal 后不得再次规划”的旧测试契约。现在完整计划执行完后，如果 Risk 未达到且
模型/决策预算仍开放，系统生成 `ProgressDelta` 并继续调用 Scenario Agent。不存在按 `maxPlanSteps` 改变终止语义
的特殊分支。

同时删除了单步兼容路径中的固定 24-action planning checkpoint。自然推进使用 episode 剩余决策预算；不会因为
旧输入是单步计划而提前返回 Planner。

精确回归测试覆盖了 `Invoke → client-terminal → Risk 未达 → Crash 仍可达 → 第二次 Planner`，
避免只用 quiescent 间接证明该语义。

### 2. 最小执行底座

- namespaced target-local Observation 可由 Target 声明和投影，公共 Core 不枚举协议语义；
- etcd/raft partition/heal 已通过 offered → selected → executed → fresh Replay；
- 每个活动 Target 的 composable Action 都有实际 frontier 可达性测试；
- Oracle capability 与可执行 monitor 从同一 target-local registry 派生；
- evidence assessment 要求 property 对应 monitor 实际出现在 `Oracle.Checked`；
- 未声明 fidelity 时返回非阻塞 `fidelity-unassessed`，显式依赖不可表达边界时返回 capability gap；
- strategic step、`after_milestone` 和自然推进共享 plan-local live Runtime，仅 promotion 时 fresh Replay。
- OmniPaxos worker stderr 使用互斥保护的有限容量缓冲区；deadline kill 后通过同步 snapshot
  封存失败诊断，不再与 `os/exec` stderr-copy goroutine 并发读写。

### 3. A9 与历史 root 解耦

etcd/raft Agentic root 由活动 workload、Qualification、Runtime 和 Adapter config 直接构造，并在首次真实 Invoke 后截取
可信 prefix；不再读取 A2 root ID、stateless corpus 或 `workload-risk-witness-calibration` 策略。

OmniPaxos 继续由 Target-local root builder 在真实 worker 上自然推进到唯一协调节点，再执行 Invoke。公共
coordinator 不理解 leader、term、ballot 或协议消息类型。

### 4. 代码瘦身

已删除：

- A2 Semantic Explorer 和 Semantic Best First；
- A8 Scenario session、paired wrapper 及 formal paired launcher；
- 通用 Campaign config/coordinator/store/summary；
- stateless Campaign、root corpus、traversal/discovery wrapper；
- etcd/raft 旧 stateless runner 和多代 random/uniform CLI；
- 与上述路径一一对应的历史测试和兼容 checkpoint。

保留：

- Runtime、Trace、Replay、conformance、qualified execution；
- Scenario 的确定性 DFS/frontier kernel；
- Agent durable journal 与恢复；
- target-local Observation/Oracle；
- etcd/raft、OmniPaxos 的协议特有 composition；
- Bundle/MethodSpec evaluator 和 defectbench。

当前 Go 代码约 32.7k 生产行、14.6k 测试行；审计前约为 41.5k/18.9k。

### 5. Agentic holdout 桥接

`cmd/defect-eval -agentic-inputs` 接收私有 trial 到单 Episode 或完整 Investigation 目录的映射，并复用现有
`FormalBenchmarkContract` 和 `FormalExposureAudit`。评估器：

- 从 Episode summary 读取方法状态、搜索 work 和 model work；方法状态只是解释字段，
  但搜索 work 与 model work 必须进入正式预算核算；
- 对 completed Episode 的主 Bundle 和 `branch-evidence.json` 中每个候选 Bundle 重新执行私有
  projector/monitor；任一候选产生 finding 即成为该 trial 的可信结果；
- 在查看 finding 前先汇总 Scenario frontier/search 和所有候选的 decisions/qualified primary work，
  超过同一 formal trial 预算时整个 trial 为 `invalid`；Replay work 也汇总报告；
- 将 Risk/Scenario 调用次数和 tokens 与 formal contract 里的 `AgenticLogicalBudget` 比较；
- 要求所有 Bundle 使用 V3 method identity，并将 summary/branch 声明的 ID、Trace、work 与实际文件交叉校验；
- 将未完成、完全缺少候选 Bundle 或任一候选 contract/build/budget 不匹配的 trial 记为 `invalid`；
- 完全忽略 Agent 自报的 `oracle-finding` verdict，只由重算 monitor 生成 killed/false-positive；
- 同一评估入口支持 etcd/raft 和 OmniPaxos 的 DecisionProjector。
- 单 Episode 仅接受 `InvestigationEpisodes=1`；多轮输入必须是连续的 `episode-0001..N`，逐轮
  MethodSpec 相同且无漏轮/额外条目；预算使用完整 Investigation 的聚合值；
- search reconstruction/materialization 计入 primary，fresh child verification 计入 replay。

当前 holdout composition 只注册两个 Target 共用的 Agreement monitor。Target-local monitor 还未转移到
evaluator 可注册包；这是后续能力扩展，不影响本轮桥接语义。

### 6. 分支、对照与消融

Agent 可在当前可信前缀上选择 `continue`、`revise` 或创建命名 `branch`。`control` 从被引用 branch
的根 Trace 重新执行，`ablate` 也从同一根开始，并且其计划必须机械等于参考计划删除已实际执行干预后的结果。
reference branch 必须完整成功，删除 ID 必须存在于 `applied_interventions`；control/ablate 不能携带 exact ActionID。
每条路径继续使用现有 Action concretization 和 fresh Replay。

分支执行消耗统一的 Runtime decision allowance；branch/control/ablate 不自动覆盖最终路径。Agent 可通过
`continue + from_branch_id` 选择并继续，或通过无计划、零 Runtime Action 的 `select + from_branch_id` 直接选择。
反馈公开根/终点 decision、最终可用 Action、
计划、实际执行的干预、结果和 `ProgressDelta`，完整 Trace/frontier
留在可信协调器，避免分支数增长时重复扩大模型输入。

选择与缺陷检出相互独立：Coordinator 对每个不同 Trace digest 的分支只做一次 qualified execution 和 Target Oracle，
结果及 work 单独写入 `branch-evidence.json`。在线 Agent 看不到这些私有 Oracle 结果；离线评测也会重新检查所有候选，
因此 Agent 选错代理信号只影响解释路径，不会让已经执行到的可信 finding 从报告中消失。

`minimize` 尚未进入可选 intent：它必须在 target Oracle 产生可信 finding 后由上层 investigation
协调，当前 Scenario 阶段若收到该请求会返回 `trusted-finding-required`。

### 7. 长轨迹与周期反馈

`ScenarioAgentMaxDecisions` 从只适合短闭环的 64 提高到 512。Coordinator 根据 episode 的
`remainingDecisions/remainingCalls` 在每轮重新计算反馈片段，并向 Agent 明确提供本轮 `decision_allowance` 与全局
`remaining_decisions`。本轮 allowance 同时覆盖嵌套计划与确定性自然推进；合法计划不会再把全部剩余预算
一次消耗完，除非它已经进入最后一个调用片段。

计划 step、`after_milestone` 和计划后的自然推进在同一个 plan-local Runtime 上追加。片段结束时只做一次
fresh Replay；这个 Replay 既验证完整候选 Trace，也产生下一轮可信 frontier/snapshot，不再为 Agent 反馈额外
重建相同前缀。Snapshot 保留 Replay 生成时的精确 nil/empty 形态，避免 canonical digest 被复制过程改变。

协议无关 fixture 校准结果：

| Action | Agent calls | reconstruction setups | verification setups | verification decisions | total work |
|---:|---:|---:|---:|---:|---:|
| 256 | 4 | 4 | 4 | 645 | 1298 |

四轮 `ProgressDelta` 分别覆盖 65、65、64、62 个 Action；反馈只保留最近 8 个 Action，并能识别重复调度
模式。该结果证明数百 Action 调查不会退化为逐 Action Replay。

真实 Target 校准从确定性初态开始，不预先执行 workload；Agent fixture 每轮只选择当前真实 frontier 中一个
自然推进 Action，其余消息、effect 和 timer 由同一 live Runtime 推进。已有 Risk 的首个 workload milestone
因此保持未达，系统将结果诚实报告为 `not-reached`，而不是为了得到 finding 人为修改 Risk。

| Target | Action | Observation | target progress | protocol/control/joint PSS | Scenario work | qualified primary/replay |
|---|---:|---:|---:|---:|---:|---:|
| etcd/raft | 128 | 68 | 1 `raft/term-advanced` | 14 / 37 / 53 | 658 | 129 / 129 |
| OmniPaxos | 128 | 130 | 1 `omnipaxos/promise-raised` | 4 / 41 / 46 | 658 | 129 / 129 |

两条路径都使用 4 次 Agent 反馈、4 次 reconstruction、4 次 promotion Replay 和 325 个累计 verification
decision；最终 Trace 与 qualified Bundle digest 一致，Replay stable，所有注册 Oracle 均执行且零违规。
一次 term/promise 进展后主要进入稳定自然运行，所以这不是“反复选举/反复换主”的证据，也没有证明 Agent
具备长时假设修订能力。

## 当前输出

一次终态 Agentic Episode 生成：

- `risk-agent/`、`scenario-agent/` provider journal；
- `summary.json`；
- 达到可测试终态时的 `bundle.json`；
- 存在未选择候选时的 `branch-evidence.json`；
- accepted Risk、capability/fidelity 状态；
- protocol/control/joint PSS 与 sample 数；
- Risk milestones、首个缺失 milestone 和 `ProgressDelta`；
- Replay 状态、`Oracle.Checked` 和 violations；
- model calls/tokens、Runtime decision allowance 和停止原因。
- 总探索 Action、最终选择路径 Action、实验分支 Action，以及 branch/control/ablation 的实际干预差异。

一次 private holdout 评测另外生成按 trial 排序的 control/candidate 结果、invalid 原因、重算
Oracle 和汇总统计。私有 pair/root-cause 信息不进入 Agent 输入。

## 当前证据边界

已经证明：

- 两个真实 CFT Target 共用同一 Agentic coordinator；
- Action 只能从真实 frontier 具体化；
- 保存 Trace 可 fresh Replay；
- target-local Observation/Oracle 不要求公共 Core 理解协议；
- 能力不足与假设未达、预算耗尽、执行失败可以分开报告。
- 当前 Agentic artifact 的主路径和未选择分支都可被 private evaluator 机械消费，且方法自报 verdict 不会创建 finding。
- treatment/control/ablation 可从同一确定性检查点执行，并可继续 Agent 选中的分支。
- 两个真实 Target 都能在 128 Action 上完成周期反馈、fresh Replay、PSS、target-local Observation 和 Oracle。

尚未证明：

- Agent 比 Random、DFS 或专家方法发现更多问题；
- Coverage/PSS 能预测缺陷检出；
- 存在新的 etcd/raft 或 OmniPaxos 问题；
- 当前 RawNode/MemoryStorage Target 等价于生产部署；
- 已有真实非公开 holdout 数据集及 Agent 相对 baseline 的效果结论。

## 下一步

M3.2 已闭合最后 decision 分支保留、零 Action 选择、全候选 Target Oracle、分支证据持久化和 holdout 重算。
M3.3 进一步移除了跨 Episode Oracle Memory 回流，将多候选成本纳入 formal trial 聚合预算，
并预留了正常计入模型成本的 select-only 收尾调用。
M3.4 又将 Scenario frontier/search、所有 qualified evidence 和模型工作纳入同一正式预算，
并用现有 V3 `MethodSpecDigest` 绑定 Episode、分支 Bundle 和 formal contract。历史 V2
OpenRouter 校准工件不满足正式归属条件，不直接用于 private holdout。
M3.5abc 进一步让方法 digest 由真实运行配置机械派生，完整摄取多 Episode Investigation，并修正
search child verification 的 replay 分类。
M4a 已使 Scenario Agent 能根据真实 `ProgressDelta` 选择 `abandon`，把剩余 Investigation 预算交给下一
Episode 的 Risk Agent；该选择不执行 Action、不创建 verdict，也不能抹除候选证据。
M4b 已将一次性源码摘录扩展为最多 4 个受控片段的迭代导航，并保留 catalog、mount、路径、行数、字节、
调用预算和 durable journal 边界。
M4c 的首个有效样本证明源码查询和 portfolio 路径可用，默认 OpenRouter 路由又出现同请求
900 秒无响应。共享 deadline/成本边界和 DeepSeek 官方 transport 已完成。后续 M4e 单 Episode
已用 1,200,000 ms MethodSpec 形成 `summary.json`、Scenario execution 和 Replay；是否再放大由 M4f
修复后的新单 Episode 决定，不继承 M4e 不可满足的 Risk。
fixture 持续选择自然进展只能作为执行校准，不能作为 Agent
有效性的实验结果。

M4k4 已把结构化能力反馈变成可配对的方法变量。下一步 M4k5 在获得明确外部调用授权后，固定其他
MethodSpec 字段运行 `reason-codes`/`structured-gaps` 公开配对校准；在此之前不调用 provider，也不把本地脚本
差异解释为模型收益。

`minimize` 仍留到可信 Oracle finding 已存在时实现，不恢复旧 A8 paired session，也不因为长轨迹新增
hash、冻结 contract、baseline 或 gate。

M3.5abc 已通过全仓测试、vet、仓库审计和聚焦 race。M4a 新增回归覆盖：首次调用不能放弃、
`ProgressDelta → abandon` 不增加 Runtime decision、放弃不成为 verdict、下一 Episode Memory 可见机械原因，
以及“前序停止 Episode + 后续成功 Episode”的完整 Investigation 仍核算前序成本。本轮全仓测试、vet、
仓库审计和三个受影响包的聚焦 race 均通过。M4b 增加 locator 首读后的显式行续读、跨源迭代、
重叠窗口拒绝、provider envelope 和方法身份回归；完整 `test-race-full` 仍留给版本化或发布前里程碑。
M4f 的全仓普通测试、vet、`audit-no-v1`、`audit-race-shards` 和 `git diff --check` 通过；
`internal/semantic` 与 `internal/controlexperiment` 的聚焦 race 通过。同一命令中
`cmd/control-experiment` 在 360 秒总上限超时，当时长轨迹 etcd/raft 用例已运行 69 秒；
普通模式下该包 68.5 秒通过。这记为已知 race 成本超时，本阶段不再重跑或放宽 gate。
M4g 的全仓普通测试、vet、两项仓库审计和格式检查通过；两个真实 Target 的语义 selector
长轨迹定向回归用时 33.7 秒。本轮没有再运行已知会超过 360 秒的整包 race；这不改变上述
M4f race 结果，发布前仍需按已有 shard 策略处理长轨迹成本。
M4h 真实单 Episode 产生 `summary.json`、`bundle.json`、`method-spec.json` 和实验 README；离线
`-campaign-resume` 恢复成功且没有新增 provider 调用。实验工件位于
`benchmarks/experiments/agentic-investigation-m4h-deepseek-omnipaxos-v1/`。
M4i 的协议无关定向回归覆盖 wait-client-terminal、wait-quiescent、长周期反馈、fault 配额和
Timer callback/clock advance 分离。`go test ./... -count=1 -timeout=360s`、`go vet ./...`、
`audit-no-v1`、`audit-race-shards`、格式和 diff 检查通过；三个短路径聚焦 race 通过。
包含 256-Action 长调查的聚焦 race 在 240 秒上限仍停留于 Replay/前缀重建，未报告 race；按既有约定记录后
不再重复。Python discovery 成功但当前 `agents/` 下为 0 tests。
256 Action 协议无关 fixture 继续只作为成本校准。
M4k4 的全仓普通测试、vet、`audit-no-v1`、`audit-race-shards`、格式和 diff 检查通过；MethodSpec 身份和
Memory 投影的两个聚焦 race 用例通过。本阶段未读取 key、未调用 provider，历史实验目录未纳入改动。
