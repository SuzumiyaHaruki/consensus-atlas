# 当前阶段

日期：2026-08-16

分支：`feature/agentic-consensus-testing`

阶段：A9e4c11 单次传输与轻量查询已校准；portfolio 调用超时，尚未形成完整 Episode

## 一句话状态

ConsensusAtlas 当前是一个面向 leader-based CFT 共识库、运行在受控协议/host-order 环境中的可信 Agentic
测试原型。A9d6 已用真实模型跑通一条 OmniPaxos 双 Agent 闭环；A9e1 已把 Agent 输入从“预置 Risk/测试题”
迁移为 Primer、Property 与 Historical Issue Pattern。A9e2a 用真实 etcd/raft 前缀复现并修复了 client result
过早结束调查的问题：它现在是返回 Scenario Planner 的机械 checkpoint，而不是 episode 终点；RiskReached 也不再
跳过本轮可信 natural-progress closure。A9e2b 的负校准证明累计摘要会漏掉 unequal-frontier 冲突；随后两个
Target 都改为逐位置前缀承诺，同一个通用 Agreement 已能检出共享位置冲突并放过一致的不同前沿。A9e2c 又把
etcd/raft log-progress 从 incarnation 内比较扩展为逻辑节点的跨重启 commit、applied 和已应用前缀比较；正确
持久化重启通过，受控的重启丢失被检出。
A9e2d 在真实 OmniPaxos worker 上确认了第三个缺口：Action context 到期不会中止当前 worker call；进程随后
异常退出时，Runtime 只返回通用 `INVALID_EXECUTION`。A9e3a 已让 worker call 尊重 context，并增加协议无关的
terminal outcome sidecar：失败动作仍不进入成功 Trace，但其成功前缀、enabled 集、尝试 Action、稳定类别与决策号
会独立封存。失败尝试计入 work；Campaign 和 Agentic Episode 都可持久化这一结果，fresh execution 可重复得到
相同类别。A9e3b 又把 PSS 拆为独立规范化的 protocol/control 视图，并保留旧 joint 计数。新执行会同时报告三列，
stateless novelty 只使用 protocol key。该结果是探索统计修正，不是共识性质 finding。
A9e4a 已让完整计划内的 natural-progress 不再受固定 24-action planning checkpoint 限制，并没有新建第二套 Plan DSL。
现有 `ScenarioPlan` 的后续步骤可用 `after_milestone` 引用假设中已有的有序 Observation；
可信执行器在条件满足前只逐步执行 effect completion、message delivery 和自然到期 timer，
并将每次选择记入 `automatic_progress`。真实 etcd/raft 测试已以一个完整计划完成
`crash old leader -> 等待 coordinator-changed-while-inflight -> restart old leader`，中间实际推进 24 个动作，
最终 Risk reached 且含自动动作的 Trace 可编译为 exact policy。24 现在是实际轨迹长度，不是代码上限。
A9e4b 已将两个活动 `agentic-calibration` 输入从 1 步/1 Scenario call 迁移为最多 4 步/3 calls。
3 calls 是“首次完整生成 + 最多两次机械修复”的上限，不是正常情况下必然调用三次。
完整意图成功后使用全部剩余 decision 预算至机械终点并结束；语法、milestone 或 selector 拒绝才返回
当前 prefix 继续修复。etcd/raft 校准证明 complete mode 在 client terminal 后只有 1 次 Planner 调用；
OmniPaxos 校准也证明预留 3 次的预算下，合法计划仍只消耗 1 次 Scenario 调用。
A9e4c1 已把真实 provider 输出从单个 Risk candidate 扩展为 2–3 项有序 portfolio。候选顺序只表示
Agent 的优先级；可信代码仍逐项执行既有语法、知识引用、Observation 和 Action 能力审查，并选择第一个
机械合格项交给 Scenario Agent。一次响应中第一项引用未知 Property、第二项合格的校准证明选择不依赖
Agent 自报分数；OmniPaxos Agentic fixture 也已通过新的结构化输出端到端运行。旧单候选 JSON 仍可读取，
仅用于历史工件与直接 planner 测试兼容。
A9e4c2 没有增加新的持久化账本。可信代码从已恢复的 episode summary 和 Bundle 重新派生最近最多 8 轮的
Exploration Memory，包括候选是否重复、Risk reached/near-miss、首个缺失 milestone、当轮与跨轮新增的
protocol PSS 状态数、机械拒绝原因、Oracle finding 数和实际 model/search/execution 成本。相同 OmniPaxos
episode 连续输入两次时，首轮新增 protocol state 等于当轮 protocol state，第二轮新增为 0；该摘要已进入
Risk Agent 的结构化请求，并在 provider journal 中随请求保存。Memory 只影响 Agent proposal，不参与执行、
PSS 或 verdict。
普通全量测试与 race manifest 在 A9e1 时为绿色。完整 race 的
method/execution 分片已通过；原 agent 聚合分片触发 20 分钟编排上限，已重分片但按本轮时间选择不再重跑。
真实两轮 OmniPaxos Investigation 已完成 6 次模型调用与 57,727 tokens。第二轮读取第一轮 Memory 后更换了
Risk candidate，并让 Scenario Agent 使用三次计划修复；但两轮均未达到 Risk、Oracle 为 0，第二轮没有新增
protocol PSS state。更重要的是，第二轮候选的文字机制声称需要 dropped accept message，机械 predicates 却没有
`message-dropped`。这说明当前资格审查只证明 witness 可执行、可观察，不能证明它忠实表达 Agent 的机制解释。
当前仍未证明 Agent 优于 baseline、达到 Agora 同等召回，也尚无新问题或 private holdout 结果。
A9e4c5 已针对该真实反例修改活动 provider contract：Agent 不再输出一段与测试条件相互独立的
`suspected_mechanism`，而是为每个 predicate 输出一个同序、同 milestone ID、同 Observation kind 的
`mechanism_step`。可信代码从这些绑定步骤生成机制说明；缺失、错序或 kind 不同会产生
`risk-candidate-mechanism-unaligned` 并进入现有 Risk 修复循环。旧单候选工件仍可读取，但新的 portfolio
必须使用绑定步骤。该修复保证执行 witness 与系统保存的机制结构一致，不声称可信代码理解自然语言 rationale。

A9e4c6 完成 Agent Input v2 的第一阶段。活动 etcd/raft 与 OmniPaxos JSON 现在除 Primer 外还提供结构化
Target Dossier：目标范围、假设、实现组件、公共契约、控制语义、当前实验和已知盲区；每项可附可审查的
源码位置。Property 增加 `hypothesis-only`、`observable-only`、`oracle-backed` 证据级别，Issue Pattern
增加适用条件和边界。两个活动 Catalog 都从 2 个 Property / 3 个 Pattern 扩为 6 个 Property / 6 个 Pattern。
Dossier 会随接受的 Risk 继续进入 Scenario Agent，不授予 Action、Observation 或 verdict 权限，也没有新增
digest、冻结 contract 或 admission gate。聚焦测试已证明严格 JSON 加载、Risk→Scenario 传递和 provider prompt 注入。

A9e4c7 已把本轮可机械获得的上下文从 Dossier 文本中分离。通用 `AgentTargetSurface` 直接从已经验证的
Adapter Manifest、WorkloadPlan、RuntimeConfig 和 FaultEnvelope 派生 target/adapter/implementation、节点、
真实 workload 输入、时钟与 clone 参数、temporal/crash/effect 能力和故障额度；同一视图进入 Risk Agent，随后
原样进入 Scenario Agent。JSON 中重复描述 workload 和 fault allowance 的条目已删除，Dossier 只保留无法从
公共 Manifest 自动获得的实现配置、契约和盲区。prompt 明确动态 Surface 优先于可编辑 Dossier。该变化没有
增加身份摘要、冻结契约、admission gate 或 Agent 执行权限。

A9e4c8 增加协议无关的只读 Knowledge Discovery 服务。来源目录只从 Target Dossier 已声明的
`evidence_refs` 派生，不允许仓库枚举；请求必须引用目录中的精确 reference。服务只读取给定仓库根内的普通
UTF-8 文本，自动按 locator 找到附近代码，并返回最多 160 行及实际行号、总行数和截断状态。未声明路径、
`..` 越界、符号链接逃逸、缺失文件、非文本、过大文件和 locator 缺失都会返回稳定机械原因。
etcd/raft 与 OmniPaxos 活动 Dossier 的真实 Adapter 来源均已通过读取测试。该服务不执行命令、不修改文件，
读取内容不是 Trace、Observation 或 Oracle evidence；主动请求的协议已在 A9e4c10 接入。

A9e4c9 为仓库外的已声明实现源码增加显式虚拟前缀 mount。CLI 可重复使用
`-knowledge-source-mount repo=<directory>` 和
`-knowledge-source-mount <reference-prefix>=<directory>`；后者例如把
`go.etcd.io/raft/v3@v3.6.0/` 映射到本机官方模块源码。最长前缀优先，重复、缺失、非目录或含路径折叠的
mount 会在模型调用前被拒绝；目录被解析为真实路径，但不会写入 Protocol Pack 或发送给模型。
真实测试已通过该 mount 读取 etcd/raft `doc.go` 的 Ready 持久化—消息发布顺序，同时保留仓库 Adapter mount。
Agentic composition 已携带 mounts。

A9e4c10 已把该服务接入现有 Risk Agent 循环。配置 mount 后，首次调用只选择最多 2 个已声明
reference，可信层读取后第二次调用才提交 2–3 个候选。每次读取最多 80 行且返回片段还受 24 KiB
限制。可信层按精确 allowlist 读取，并把片段或稳定的机械停止原因放入下一次 `RiskAgentView`。查询会消耗原有
model-call/token 预算，request/result 随既有 attempt 和 provider journal 保存；没有新增 Discovery Agent、shell、
执行权限或第二套账本。源码文本只是不可信规划上下文，不能成为 Trace、Observation、Replay 或 Oracle evidence。
未配置 mount 时结构化输出保持原 portfolio-only 形式。fixture 已实际完成“读取 OmniPaxos Manifest → 第二次调用
提交合格 portfolio”，并验证第二次 provider 请求包含有界源码片段。

A9e4c11 的首次真实校准暴露了计费负证据：OpenRouter 官方账本显示已消耗 token，但客户端未收到可解析
usage，本地工件因此显示 0 token，旧传输层还对这种已发送 POST 自动重试。这个 0 只表示本地未观察到
usage，不是 provider 零成本。当前实现已收紧为单次 POST；超时或连接中断记为 `agent-transport-ambiguous`
且 `provider_usage_status=unknown`，收到合法 usage 才记为 `observed`。首次知识选择另用轻量 query-only schema。
默认沙箱不能连接 OpenRouter；沙箱外不带凭据的连通检查为 HTTP 200。得到明确授权后的真实 etcd/raft
校准证明了两阶段路径：知识查询在 26.56 秒返回，消费 3,661 input、971 output、合计 4,632 tokens，选择
`raft.go:msgsAfterAppend` 与 `model.go:readyRecord` 两项；可信读取分别只发送 50 行/1,559 字节和
50 行/1,391 字节。随后 portfolio 调用只发送一次 POST，但在 120 秒读取响应正文时到期，本地未观察到 usage，
因此记为 `unknown`；没有 portfolio、Scenario 执行或 Oracle 结果。该负证据还发现正文读取超时曾被误分为
`agent-response-rejected`，实现已修正为 `agent-transport-ambiguous`。本次调用不自动重发，也未再次访问 provider。

Agora 三小时 etcd/raft 实验的原始 metrics、action log 和测试文件仍然存在。该轮运行包含 352 次模型
调用、285 次命令和 9 次文件写入，候选场景经历了提出、编译失败、修复、对照和扩展被否定。这支持
“LLM 有能力形成深层协议假设”，不支持“LLM 可独立认定上游缺陷”：两个稳定现象最终分别属于绕过
Ready/Advance 持久化契约的集成风险，以及强制内部 timer 同步的假设边界。

该复盘改变下一阶段的重点：当前多轮小实验只证明流程可运行。它的固定少量 Scenario 调用、较短轨迹和单一
Risk 不足以证明 Agent 可以完成长时深入测试。下一步要放宽 proposal/revision 权限，保留 enabled Action、Runtime、
Replay 和 Oracle 权限边界。确定性作用于实际 Action 轨迹及其无 LLM fresh Replay，不要求 Agent 在启动前一次预知
整个调查过程。

## 输入什么

一次 Agentic Episode 使用：

- `-target etcdraft-v2|omnipaxos-v2`：选择目标 Target Pack；
- `plans/agent/*.json`：Consensus Primer、Property Catalog、Historical Issue Pattern、workload 和可编辑预算；
- `adapters/<target>/`：将官方实现映射为公共 Action/ProducedItem/Observation；
- `qualifications/<target>/`：机械声明目标实际可控、可观察的能力；
- 可选的 `-knowledge-source-mount`：把 Dossier 已声明的源码 reference 显式映射到只读本地目录；
- 显式的 OpenRouter model 和 key 文件；key 只在新模型调用时读取，不写入工件。

## 如何处理

```text
Protocol knowledge + qualified target surface
                     |
                     v
                 Risk Agent
                     |
       parse + mechanical compile/qualification
                     |
                     v
               Scenario Agent
                     |
       trusted binding to current enabled Actions
                     |
                     v
        Control Runtime + real implementation
                     |
        +------------+------------+
        |                         |
        v                         v
   fresh Replay          Observation / PSS / Risk
        |                         |
        +------------+------------+
                     v
             independent Oracle
                     |
                     v
          summary.json + bundle.json
```

Agent 可以提出一组有序 Risk 候选和完整但有界的 Scenario 意图，但不能构造 ActionID、执行事实、PSS 命中或 finding。Runtime 只执行
当前 enabled Action；Replay、Risk/PSS 投影和 Oracle 从真实 Trace 重算。

A9e1 不再向 Risk Agent 提供预置 `Risk` 或 `TestHypothesis`。Agent 输出仍只是待编译 proposal；执行证据和
verdict 的权力边界不变。第一篇论文应围绕 proposal、evidence、verdict separation，而不是把确定性执行、
PSS 数量或多 Agent 编排本身写成效果结论。

当前活动 CLI 仍执行单个 episode，因此默认 Exploration Memory 为空。A9e4c2 提供了从恢复工件重算并注入
下一轮的机制。A9e4c3 已加入内部多轮 coordinator：每轮写入独立 `episode-NNNN` 目录，随后从磁盘重新恢复
summary/Bundle，再重算 Memory 交给下一轮。两轮 OmniPaxos fixture 实际完成 4 次模型调用；第二轮请求读到
第一轮 Memory，最终被标记为重复候选且新增 protocol state 为 0。外层 episode、model call、token 和 runtime
decision allowance 在启动下一轮前检查；runtime decision 目前按每轮上限保守预留，不冒充实际使用量。

A9e4c4 已在同一 `agentic-episode-v1` 策略上增加 `-investigation-episodes N`。外层总预算直接由现有单轮预算
乘以 N，不增加重复的预算配置；`-campaign-resume` 会读取连续 `episode-NNNN`，恢复已完成轮次，并继续最后
一个 partial episode 或创建下一轮。OmniPaxos fixture 从已完成两轮恢复到三轮时只新增 2 次 provider 调用，
旧轮次未重放；累计 6 calls / 42 fixture tokens 和 Memory 均从工件恢复。根目录没有新增 manifest/session ledger。

CLI 与恢复数据通路已用真实 OpenRouter 跑完两轮。第二轮请求确实包含第一轮的 near-miss、PSS 和成本 Memory，
模型也改变了候选与计划；但第二轮四个 protocol PSS state 全部与第一轮重合，因此只能证明 revision 发生，
不能证明 revision 有效扩大了协议状态或缺陷发现集合。

两个活动输入都为 Risk Agent 保留最多三次 portfolio 提议/机械修复，并为 Scenario Agent 保留最多三次调用。etcd/raft
Agentic 输入也已去掉只供 legacy Semantic Explorer 使用的 depth/work-item/explorer budget；这些字段仍由旧路径
继续校验，不再成为新 Agentic 接入的前置负担。

`property_ref` 当前只提供规划上下文。candidate accepted 只证明语法、可观察性和动作资格成立，不证明
predicates 已充分刻画被引用性质，也不证明任何 Oracle 已验证该性质。

## 得到什么

每个终态 episode 输出：

- Risk candidate 是否被接受，以及稳定的机械 feedback；
- Scenario 是否完成、Risk 是否可达；
- Core PSS sample，以及 protocol/control/joint 三种唯一状态数；
- primary/replay/search/model work 与 token；
- fresh Replay 和独立 Oracle findings；
- 唯一完整 `bundle.json` 和不复制 Trace 的紧凑 `summary.json`；
- provider intent/dispatch/result 审计，以及明确失败原因。

PSS/Risk 是测试解释指标，不是缺陷判定。只有 replay-stable 的独立 Oracle/evaluator 能产生正式 finding。

## A9d6 真实实验

完整目录与说明：
[`benchmarks/experiments/agentic-episode-a9d6/`](../benchmarks/experiments/agentic-episode-a9d6/README.md)

| 目标 | 有效结果 | 模型成本 | 解释 |
|---|---|---:|---|
| OmniPaxos | completed，candidate accepted，Risk not reached，Replay stable，Oracle 0 | 2 calls / 9,863 tokens | 完成端到端真实闭环；31 PSS samples / 29 unique |
| etcd/raft | 未得到 completed episode | 详见实验 README | 分别暴露 output limit、binding token 约束和 provider response 不稳定 |

OmniPaxos 首轮用一次 binding 被拒绝，促成精确 `risk-candidate-single-use-binding` feedback。etcd/raft
一次完整候选使用大写 `nodeA`，促成 structured-output schema 和 prompt 的小写 token 约束。其余
180 秒有界失败已保留，不继续追加请求。

这一轮只证明：

- 同一上层 coordinator 能驱动非 Raft 目标的真实执行；
- 模型无效输出会被机械拒绝并产生可修正反馈；
- 终态可在不读取 model/key/worker/semantic input 时恢复。

它没有证明 Agent 优于 baseline，没有发现新协议问题，也没有证明长时间测试已完成。

## 当前实现边界

| 目标 | 控制与执行 | Agentic Episode | 结果验证 |
|---|---|---|---|
| etcd/raft | scheduler-owned 消息、自然时间、crash/restart、持久化 effect | 通用 binding 完成；本轮 provider 未稳定完成 | Replay、Core PSS、动态 Risk、TraceIntegrity、Agreement、target-local monitor |
| OmniPaxos | 外部 worker、消息控制、自然时间、workload | 真实双 Agent 闭环完成 | fresh worker Replay、Core PSS、动态 Risk、TraceIntegrity、Agreement |
| HashiCorp Raft | 部分 interceptable 黑盒能力 | 未进入正式 episode | 作为黑盒接入上限证据 |

公共 `internal/` 生产代码不导入具体共识实现。target-local Observation projector、monitor 和 composition
保留在 Adapter/Qualification/CLI 组合边界。

第一篇论文的实现范围收窄为 leader-based CFT 共识库的受控协议/host-order 执行。etcd/raft 使用官方
`RawNode` 与内存存储，OmniPaxos 使用单线程外部 worker 与内存存储；当前不能覆盖真实线程竞争、网络栈、
WAL/fsync、部分写入或进程资源故障。受限 BFT 留作后续扩展，除非加入真实 BFT Target、Byzantine Action
与相应可信证据，否则不作为本阶段能力声明。

## 当前证据缺口

- A9e2b 已闭合两个现有 Target 的 unequal-frontier Agreement 反例：Adapter/worker 生成逐位置累计承诺，通用
  Oracle 仍只比较 `(position, digest)`，没有导入 Raft/OmniPaxos 类型。该结果只证明已校准反例，不证明
  Agreement projector 对所有日志表示都完备。
- A9e2c 已闭合 etcd/raft Adapter 当前 durable-image 语义下的跨 incarnation 回退：正确重启保留 commit、applied
  与既有前缀，受控丢失会触发 target-local monitor。它不代表真实 WAL/fsync、部分写入或所有 Target 的持久性。
- A9e3a 已闭合 A9e2d 的已校准缺口：deadline 会终止并回收当前 worker，terminal outcome 在成功 Trace 外单独封存，
  失败尝试计费，Campaign/Agentic Episode 可落盘恢复。fresh Replay 仍只验证成功前缀；同一失败类别由 fresh
  execution 重试验证，而不是把失败动作写成已应用 record。panic 及部分执行后外部副作用尚未单独校准。
- A9e3b 已把原 Core PSS 联合计数拆开。A9d6 OmniPaxos 的 31 个 sample 现在派生为 4 个 protocol state、
  28 个 control state 和 29 个 joint state；简单删除 pending 得到的 5 仍受联合状态节点别名影响，因此不作为正式值。
  protocol/control 都没有完备分母，也都不构成正确性 verdict。
- 当前 A9 Risk+Scenario 方法尚未进入 formal evaluator；仓库只有公开 calibration pair，没有真实 private holdout。
- 活动 Agentic 输入已完成一次真实 OpenRouter 两轮运行。模型两轮都产生了可解析 portfolio，Scenario Agent
  也分别用一次和三次调用形成可执行计划；但两轮 Risk 均未达到，且第二轮出现文字机制与 predicates 不一致，
  因此不宣称 LLM 已能稳定产生忠实、有效的完整多步测试。
- 既有 31 sample 派生出的 4 protocol/28 control/29 joint PSS 只表达状态广度。Agora 的同步 PreVote
  场景表明，50 轮循环在我们的控制粒度下可能需要约 500–900 Action，却仍可能只占据 3–6 个 protocol PSS。
  当前尚未正式报告 `PSS-Action-PSS` 转换与重复无进展循环深度，因此不能用单一 PSS 计数判断调查是否深入。

## A9e2–A9e4c5 结论与后续路线

已完成 A9e2a：用 etcd/raft invoked root 证明旧流程会在 client result 后留下可执行 crash 却结束 episode；
修改后同一预算进入第二次规划并执行 crash；既有 deterministic etcd/raft Scenario 也确认 RiskReached 后仍记录
本轮 natural-progress stop。
这两项只修复已复现的终止语义，不引入新的 Property contract、Progress DSL 或 gate。

已完成 A9e2b 修复：负校准先证明两个 Target 的累计 frontier 摘要都会漏掉共享位置冲突；随后只改变
Target-owned evidence/projector，让每个已决定/已应用位置产生一个累计承诺。通用 Agreement 规则不变。
两个 Target 的聚焦测试均验证“冲突的不同前沿检出、一致的不同前沿不误报”，真实 OmniPaxos worker Replay
也保持投影一致。

已完成 A9e2c：etcd/raft 的 target-local log-progress monitor 现在按逻辑节点跨 incarnation 保存前沿，比较
commit、applied 和所有既有 application prefix。正确的 durable-image restore 通过；新 incarnation 回到空状态的
受控反例在重启 step 被检出。该结论限定在当前 Adapter 的 durable-image 语义。

已完成 A9e2d 负校准：测试暂停真实 OmniPaxos worker，使选中的自然时间 Action 超过自己的 context deadline，
再终止该 worker。当前调用不会在 deadline 到期时返回；最终只得到 transport-shaped `INVALID_EXECUTION`。
失败前后的 sealed Trace 完全相同，fresh worker Replay 仍成功，因此 Replay 只证明成功前缀，不能证明 terminal
outcome 被复现。这个反例给 A9e3a 提供了明确失败依据。

已完成 A9e3a：Runtime 在 Action 已选中但未提交时生成 sealed terminal outcome，绑定成功前缀 digest、enabled-set
digest、尝试 Action、decision、稳定 class/code；Trace 保持为成功前缀。OmniPaxos worker RPC 现在传播 Action
context，deadline 后终止 worker 并返回 `deadline-exceeded/ACTION_DEADLINE_EXCEEDED`。失败的 scheduler decision
计入 work。聚焦测试覆盖了真实 worker deadline、fresh runtime 同结果重试、JSON/Campaign 传递，以及 Agentic
Episode 的 `execution-failed` 落盘与恢复。进程退出或 timeout 本身仍不升级为 Agreement/Validity finding。

已完成 A9e3b：同一个可信 Core PSS State 现在派生三个互不替代的统计视图。protocol view 只包含 mapping ID 与
SemanticGraph；control view 只包含 lifecycle/incarnation、partition 与 pending frontier；joint view 保留原
State digest。protocol/control 分别做 participant 对称归一化，避免一侧替另一侧选择节点别名。Agentic summary/CLI
明确输出 protocol/control/joint/sample；stateless discovery 的 novelty 改用 protocol key。三列从 Bundle 已保存的
Core PSS State 派生，不进入 Bundle 摘要，因此 root corpus 与历史 Bundle 身份保持不变。
新 stateless discovery 工件显式标注 `pss_view=protocol`；旧工件的缺省值解释为 `joint`，同一 Observation 拒绝
混合两种 key，防止恢复旧 Campaign 时把不兼容的状态集合相加。

接下来：

1. 在再次产生费用前，先把 provider 调用 deadline 作为显式实验配置并提高长响应校准上限；保留单次 POST，
   不把同一 intent 的超时重发伪装成恢复。随后另行授权一次 portfolio 校准，目标是得到完整 Risk → Scenario →
   Replay → Oracle Episode。
2. A9e4d——Adaptive Investigation 权限与预算：重用现有 coordinator/journal/Bundle/Memory，不新建
   Session Runtime 或 Plan DSL。将固定少量 Scenario call 从方法定义改为可编辑 calibration 配置；Agent 在总
   wall time/model work/Runtime work 内可修订 hypothesis、改换分支和请求对照/消融。
3. A9e4e——Agora-derived deterministic reconstruction：从初始 root 用公共 Action 分别重建一个持久化/重启时序场景
   和一个数百 Action 的长选举循环。候选必须无 LLM fresh Replay；越过公开契约或使用私有 hook 的轨迹只归类为
   integration/assumption-boundary 证据。
4. A9e5——长时公开效果校准：在 etcd/raft 和 OmniPaxos 上分别运行配置时间的 Investigation，报告
   Action/轨迹长度、三种 PSS、transition、temporal depth、Risk/义务、Replay/Oracle 与全部成本。
5. A9e6——formal evaluator 与 private matching pairs：在相同 root、source exposure、wall time 和 Runtime work 下
   重复比较，模型 token/调用作为 Agent 额外成本单列。

现有小型 calibration、长时公开调查和 private holdout 结果始终分开报告。前两者能证明流程与调查能力，
只有第三层能支持方法缺陷发现效果结论。
