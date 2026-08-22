# ConsensusAtlas 总体规划

## 1. 一句话目标

构建一个利用 Agent/多 Agent 对分布式共识实现进行深入测试的系统：Agent
负责理解协议、提出风险和修订调查；可信控制层负责确定性执行、Replay 和
证据；独立 Oracle/evaluator 负责接受 finding 和比较方法效果。

系统的研究价值必须同时体现三个关键词：

- **Agent**：不是 Random/DFS 的自然语言包装，而是调查主体；
- **共识**：输入、Observation、Risk 和 Oracle 能表达协议语义；
- **测试**：从初始状态执行真实实现，产出可 Replay、可审计的故障证据。

## 2. 用户流程

### 2.1 输入

接入一个新 Target 时提供：

1. 固定提交的本地 SUT 源码；允许实验性修改，但正式运行必须封存内容身份；
2. 协议知识包：角色、轮次/任期、消息族、关键边界和待检验性质；
3. 薄 Adapter：将目标实现 API 翻译为统一 Action/Item/Observation；
4. capability/fidelity 声明：实际可控、可观测、可 Replay 的边界；
5. Target composition：语义投影、PSS 映射和确定性 Oracle registry；
6. Agent 可查询的只读源码目录；
7. 模型、时间、调用、token 和 Runtime 工作预算。

协议知识、预算和可变实验配置优先使用 JSON；必须调用官方 API 或实现复杂
证据转换的部分使用 Go/Rust。公共 Core 不包含具体协议分支。

Go SUT 使用 Git submodule 固定上游提交，并通过根 `go.mod replace` 强制普通构建
解析本地 checkout。Adapter import path 保持官方路径，因此修改 SUT 不要求改写
控制层。未封存本地构建只能用于开发和公开能力校准；正式 finding/control 对比须由
build audit 对实际源码树和二进制建立内容身份。共享修改时，子模块提交必须存在于
可获取的远端 fork，不能只在主仓库记录一个其他机器无法获取的本地对象。

Agent 源码读取必须显式授权，并与实际 SUT 输入一致。对于已完成绑定的 etcd/raft，
可信 composition 校验可执行文件 local replacement、Go 解析目录和源码 mount 为同一
checkout，再将 module path/version/reference prefix 与完整 tree digest 纳入 MethodSpec；
运行中继续检查 tree digest。未提供 mount 时 Agent 不读取源码。该开发态一致性不能替代
formal build audit 对 staged source 与二进制的封存，也不能自动推广到尚未接入的 Target。

### 2.2 处理

```text
Risk Agent
  读取知识/中立源码查询/历史机械反馈，生成候选 portfolio
        ↓ typed executable assessment
Scenario Agent
  单路径选择一个战略 Action，按可信反馈 continue/revise/abandon
        ↓ trusted binding
Control Runtime
  执行 enabled Action，保存完整 Trace 和成本
        ↓
fresh Replay → Observation/PSS/witness 重算 → Oracle registry
        ↓
artifact/evaluator → finding、探索、能力缺口和完整成本
```

Agent 可以提出不确定或错误的候选；本地类型、capability 和 enabled frontier
决定它能否执行。拒绝应生成精确、可修复反馈，不能伪装成协议失败。

### 2.3 输出

- 可 Replay 的 Action Trace 与 Execution Bundle；
- 独立 Oracle finding、首次违例位置及 control false positive；
- witness milestone、PSS state/transition 和探索增量；
- missing-action/observation/oracle、fidelity gap 和预算停止原因；
- Agent calls/tokens、primary/replay work、时间和资源成本；
- 可用于同预算方法对照的结构化结果。

## 3. 控制层

公共 Action 只表达跨实现控制语义：消息交付/丢弃/复制、自然 temporal
callback、crash/restart、partition/heal、host effect、workload invoke/result。
Action 只引用当前 Runtime 产生并冻结的 ID；Agent 不能制造 ID 或修改状态。

时间推进不是任意跳时。只有最早到期 temporal callback 或目标实现的合法
sleep/clock 边界可以推进虚拟时间，从而构造“自然超时”。tick 型实现由
Adapter 将 tick callback 映射为 temporal item，不在 Core 中写 Raft 规则。

能力分三层：

- **declared**：Target 声称支持；
- **composable**：当前测试配置允许组合；
- **enabled**：当前状态真实可执行。

三者必须由普通端到端测试证明连通。Target 缺少表达窗口时报告 fidelity gap，
不挑选“恰好适配”的实现来粉饰普适性。

## 4. 协议语义扩展缝

统一执行证据，不统一所有协议语义。

- 通用 Observation 支持跨协议状态和 Oracle；
- namespaced Target-local Observation 表达持久化、promise/QC、消息 leaf type
  等专用事实；
- Core 校验、保存、匹配和 Replay 类型化字段，但不理解其协议含义；
- Scenario Agent 可用通用 selector 和 Target-local metadata 收窄当前 frontier；
- Target Oracle 从同一个 registry 同时供在线执行与正式 evaluator 使用。

当前大多数目标可使用 leader/coordinator、round/term/ballot 等 leader-CFT
profile；少数无 leader 协议通过可选字段扩展，不为了假想协议增加全局复杂度。

## 5. Agent 角色

### Risk Agent

根据知识、中立源码搜索/窗口和 Exploration Memory 生成多个可检验风险，说明前置条件、
milestone、需要的能力和 fidelity。它不能编写 Oracle 或预设 verdict。

一个 portfolio 中所有机械 `executable` 候选都必须保留。多 Episode Investigation
按 Agent 优先级、语义去重后，在同一总 calls/tokens/Runtime decision 预算内依次调查，
不能只保留第一个候选或 cherry-pick 成功 Episode。

### Scenario Agent

把 executable Risk 逐步实例化为语义轨迹。默认协议是单路径、每次一个战略 Action；
它获得当前 target surface、enabled frontier
摘要、首个缺失 milestone、附近 Trace slice、ProgressDelta、循环深度和能力
缺口。活动协议只含 `continue`、`revise`、`abandon`；对照、消融和多候选调查由
外层多个 Episode 在统一预算中完成，不在 Episode 内扩展分支状态机。

### 分析与自我修复

源码导航和 proposal repair 由 Agent 承担；control/ablation 与 trace minimization 若有真实需求，
由外层实验对保存 Trace 编排，但任何修改后的候选仍必须重新经过可信执行和 Replay。当前不
因为“多 Agent”名义增加第三套协议或第二个执行框架。

## 6. 可信性与成本

- live Runtime 用于增量调查，减少重复恢复 prefix；
- 候选晋升或终止时做 fresh Replay，最终 finding 必须 Replay 稳定；
- 所有已执行、唯一且 Replay 稳定的候选都可离线运行 Oracle；
- Oracle 结果不回流为在线私有标签；
- durable provider journal 支持歧义调用审计和恢复；
- 方法身份由真实 transport、prompt、semantic input、源码暴露和预算配置派生；
- 搜索、候选 primary、Replay 和模型成本都进入完整 trial 账本。

不要追求每个中间结构都冻结或哈希。只有出现普通类型、主键、版本、唯一约束
和测试无法防止的具体失败场景时，才增加新的身份或 gate。

## 7. 评价

最终报告同时包含四个面，不能合成一个自嗨分数：

1. **发现效果**：独立 root cause 命中、control false positive、可 Replay finding；
2. **探索效果**：相同预算下 PSS states/transitions 与 Risk milestone 增量；
3. **能力边界**：缺少 Action/Observation/Oracle 与 target fidelity；
4. **成本**：模型、primary、Replay、时间和资源。

PSS 没有已知完整分母，不代表测试完整度；obligation 只在有稳定、可达且有意义
的组合/时间关系时使用，不能让 Agent 通过逐项简单测试刷覆盖。当前 PSS vocabulary
冻结，并降为保存 Trace 的离线探索指标。

正式效果实验至少比较 Random、单 Agent、双 Agent 和专家策略，使用相同模型与
Runtime 总预算。公开 calibration、private holdout 和新发现 case study 分开报告。

## 8. 当前实现与删减决定

已完成：

- etcd/raft 与 OmniPaxos 两个真实 Target，均由仓库内固定、可修改的 SUT checkout 构建；
- 统一 Action、自然时间、消息所有权、crash/restart 与 Replay；
- etcd/raft 静态成员规模由 Target JSON 的 `adapter_config.node_count` 配置（当前最多 64），生成
  `n1..nN`/Raft ID `1..N`；显式 `nodes` 继续服务自定义身份。qualification、admission、
  root Trace 与正式 Runtime 必须绑定同一份成员配置，不能用三节点资格运行 N 节点实验；
  活动 Agent 输入完全省略该对象时解析成三节点默认值，不保留独立的固定三节点执行器；
- OmniPaxos 使用相同的“显式 `node_count` 优先、缺省为 3”规则（当前 3–64）；配置必须
  贯穿本地 worker reset、Manifest、qualification、Agent root、Scenario、Bundle recipe
  和 evaluator-owned Replay。HashiCorp 能力样本的底层 Adapter 同样可配置，但在成为
  活动 Agent Target 前不为它增加独立的 Agent 输入路径；
- target-local Observation/Oracle registry；
- Risk portfolio、受限源码导航、机械资格和 feedback repair；
- live Scenario、ProgressDelta、durable journal 和完整成本；
- Agentic artifact 到 formal/private evaluator；
- 两个协议上的 target-local 消息 leaf type 选择校准。

可信评测边界：

- Agent 生成的 ordered witness 在可信侧按固定操作额度匹配，不能用合法输入触发组合爆炸；
- 模型调用成本由正式 evaluator 从 Episode durable journal 重建，并与 summary 交叉核对；
- Scenario 搜索成本由逐尝试 ledger 聚合，最终 Bundle 成本单独核算；
- 当前 Agentic formal evaluator 的 Replay authority 是 `evaluator-owned-sut-replay`：
  当前方法的 V3 Bundle 必须封存机器无关的可执行 recipe（完整 Config、seed、
  policy、workload 与 Target config）；private input 额外提供 build audit、SUT binary
  和可信 executor。evaluator 在隔离子进程中调用现有 qualified executor，逐 Action
  比较 fresh Trace，然后才重新投影并运行 Oracle；
- Scenario attribution boundary 随主 Bundle 和每个候选 Bundle 一起进入 evaluator 输入；formal loader
  从主路径 Trace 长度减去 selected-path decisions 推导它，并与保存值交叉核对。该 boundary 位于首个
  Agent 战略 Action 之前，公共 bootstrap 和自动 Invoke 仍属于 setup。这是方法侧边界，不是
  独立 evaluator-owned root boundary；当前 formal 威胁模型不声称防止 artifact producer 一致篡改
  这两项事实。完整 Oracle
  结果不删除 root violation，但正式 finding 只由 evaluator-owned Replay 中 root 之后的 violation 产生。
  root 中已有的异常单独报告为 Oracle sensitivity，不能归因于 Agent；
- Agentic semantic input 显式声明 `root_mode`。正式盲测使用 `bootstrap`：只清空启动
  Ready/effect，不预先完成选举、coordinator 形成或 Invoke；历史窄 closure 校准保留
  `workload-ready`。两种模式由 semantic input digest/MethodSpec 区分，不能靠移动 root
  把自然启动已出现的问题改记为 Agent finding；
- 每个活动 Target 的 bootstrap root 必须以普通零模型测试证明三/五节点均可精确 Replay、
  现有 Oracle registry 为 clean 且仍存在 timer/message 调查空间。这一检查复用现有 Trace、Replay
  和 Oracle，不新增 contract、hash 或 admission gate；
- Target preparation 受独立 wall-clock deadline 约束；qualification report/case 数量、
  root 构造的 primary/replay work 和实际耗时进入 Episode 工件。root work 进入 formal
  primary/replay 成本；qualification 通过协议中立 Adapter `WorkMeter` 记录真实 Reset、
  Submit 和 ApplyRuntimeAction，并进入 formal primary decisions/work。只读 Adapter 查询不
  冒充 scheduler work，仍由 wall time 表达；report/case 数只作为结构统计。
  多 Episode CLI 只构造一次重准备，并只在实际承担该成本的首个新 Episode 记账；
- `public-fixed` 是 CLI 默认方法；`target-local` 必须显式选择，且两者的 MethodSpec
  identity 不同。Agent 选择、Target closure 和公共自然推进的 decisions 在 Episode
  与 formal trial 中分开报告，不将 closure 动作归功于 Agent；Risk 重复按结构语义判断，
  不按 Agent 自报 CandidateID 判断。

长轨迹证据先通过 etcd/OmniPaxos prefix 增量缓存减少重复计算。Trace 仍保存完整 Evidence，
存储 delta 化必须在有真实长时瓶颈并能保持 Replay/Oracle 兼容时再做，不为压缩体积引入第二套轨迹语义。

主线收敛决定：

- 删除旧 Campaign/Profile/固定 Scenario 与独立 DFS 方法；
- 删除 M4l2/M4l3 一次性 runner，保留语义实现、精简报告和小回归；
- 历史 benchmark 不作为普通单元测试依赖；
- 不恢复旧 Explorer/Campaign 执行路径；
- PSS 暂不删除，但冻结；
- `minimize` 在有真实 Oracle finding 和明确需求前不开放；
- 不为第三个同类配对实验搭建大型 calibration framework。

## 9. 后续阶段

### S1：瘦身与唯一主线（已完成）

- 完成历史资产清理、命名收敛和全仓验证；
- 保证唯一活动 Episode CLI 不依赖一次性 runner；
- 形成可阅读、可版本化的精简仓库。

### S2：Agent 能力释放（当前）

- etcd/raft 与 OmniPaxos Target 均支持可配置静态节点数；协议专属 closure
  按实际多数派计算所需参与者。多个等价 quorum 子集存在时，后端只返回当前
  enabled/admissible 且能够绑定候选参与者的 `closure_candidates`；Agent 通过
  `revise` 执行 exact Action 后，可信代码从真实 `FrontierChoice` 恢复选择。不把任意
  节点排序写进公共 Core，也不允许 closure 候选引入新干预；

- 五节点零模型垂直回归已证明两个协议的多参与者路径可进入 Agent 主流程：
  etcd/raft 由 Agent 选择两个备用 follower；OmniPaxos 在已有 prepare quorum 基础上
  选择额外闭合参与者。两者均以同一 RequestID 达到 Risk，fresh Replay stable，
  Target Oracle clean。`public-fixed` 仍是任意配置节点数的默认路径；

- OmniPaxos 0.2.2 已固定到 `suts/omnipaxos` 的上游提交 `e3e989b...`；Rust worker
  通过本地 Cargo path dependency、锁定离线构建和规范 worker 路径校验使用该源码。
  Agent 若获显式源码读取授权，只能挂载同一 checkout，源码树身份沿用 MethodSpec
  的既有 SUT binding，不为此增加第二套 hash 或 contract；

- 现有 SUT Audit 已扩展 Cargo producer：它绑定协议源码树、worker 源码/锁文件、
  精确离线 Cargo 命令和 worker binary，并复用正式 evaluator 已消费的 build-audit
  结构。worker BuildID 必须等于 Audit 的 `sha256:<binary>` identity；这不是新的
  evidence ledger。首次克隆只显式执行一次 `cargo fetch --locked`，实验构建保持
  `--locked --offline`。端到端回归使用该 Audit 构建 worker、生成 OmniPaxos V3 Bundle，
  再从 sealed recipe 运行 evaluator-owned Replay，并同时核对 fresh Trace 与 BuildID；

- target-local closure 的方法资格同时受节点规模和调用预算约束；预算不足以容纳一次
  干预及必要 quorum 参与者选择时，在 Agent 调用前拒绝。etcd/raft 后续消息绑定原干预
  term/index，OmniPaxos 绑定 ballot/sequence/RequestID，避免换届、重试或旧消息被误消费；

- semantic input 的文件位置不再决定本地 SUT 身份：CLI 可用 `-repository-root` 显式
  指向 ConsensusAtlas 工作区，原有向上发现只作为兼容默认。OmniPaxos worker 在响应中
  回传实际规范化配置，Adapter 与 Manifest 使用同一配置事实，避免 Go/Rust 常量漂移；

- 已用 target-local closure selector 闭合 Agent 已正确选择的协议因果路径，并将
  可选 factory 接入 Scenario Agent 的 `after_milestone` 与计划结束推进；公共
  Runtime 只提供 enabled frontier、执行、Trace 和 fresh Replay，不解释闭合语义；
  closure selector 只能选择 Effect/Deliver/Temporal，歧义或无可选动作必须返回
  正常反馈，不能在闭合阶段继续制造新的故障干预；公共层必须保留不可变权威
  frontier，且预算最后一个 Action 产生的 terminal/Risk 结果必须在预算判定前采集；
- factory 只在可信 Trace 已记录真实干预后激活；未配置 factory 的 Target 行为不变，
  已激活 factory 的歧义/无候选不能回退公共固定顺序，fresh Replay 不调用 selector；
- 已晋升路径必须跨 `continue/revise` 保存最近的可信干预上下文；闭合预算、歧义和
  无候选均作为可修订反馈返回，已满足 milestone 时不提前构造 selector；
- Agentic qualified execution 不再把 Scenario Trace 翻译成逐 decision Policy。完整 Trace
  直接作为 recorded schedule；主执行、Bundle fresh Replay 和 evaluator-owned Replay
  共用“必要时按原参数重建 Invoke/Partition → 查询 enabled/admissible → 精确 ActionID
  选择 → 完整 ActionRecord 比较”的解释语义。现有 Config 只保留 Action-kind surface
  以复用 admission/recipe，手写 Policy fixture 继续走历史执行入口；不新增调度 contract；
- Agent 只在可信 milestone 所需的新战略 frontier 上作选择。选主、普通 term/ballot 推进和
  coordinator 就绪属于公共 bootstrap；下一 milestone 为 workload invoke 时，由 Target 已有
  typed Action preparer 在唯一 coordinator 就绪后投放一次普通 Invoke。自动动作继续写入同一
  Trace、成本和 Replay，但 finding attribution 从首个 Agent 选择的战略 Action 才开始；若没有
  战略 Action，整条 Trace 都属于 setup；
- closure handoff、正式 Risk 输入、节点规模/调用预算、bootstrap root、紧凑 Action 前沿、因果推进和
  token-stop 证据封存属于方法实现变化，当前 MethodSpec implementation identity 为
  `m4n17-strategic-bootstrap-v1`；旧 M4n16 及更早版本只读兼容；
  `m4n12-bootstrap-root-and-action-coherence-v1`/
  `m4n11-provider-recovery-and-quorum-oracle-v1`/
  `m4n10-deep-candidate-investigation-v1`/
  `m4n8-agent-semantics-portfolio-search-v1`/`m4n6-causal-closure-build-evidence-v1`/
  `m4n5-multinode-closure-v1`/`m4m4-risk-fidelity-v1`/
  `m4m1-closure-ownership-v1`/`m4l7-risk-input-closure-handoff-v1`/
  `m4l5-closure-v1`/`m4d-v1` 只用于读取
  历史工件，不能恢复为当前运行；
- M4n11 及后续版本已执行的主路径必须封存 root/post-root Oracle attribution；
  campaign resume 按已有 MethodSpec implementation ID 机械区分。只有 M4n10 及更早版本
  可以缺省 attribution，方法版本前移不会将 M4n11 缺失字段默认恢复为 root=0；
- M4n10 的新工件只使用“机械可执行”（兼容 wire value `executable`）→
  `witness-instantiated → oracle-finding` 三层结论；
  `scenarioTestingResult.outcome` 为 `oracle-clean|oracle-finding`，不能再用 `passed`
  暗示实现或性质正确。底层历史 Bundle 的枚举只作兼容读取；
- Investigation 保留 Risk Agent 返回的全部 executable portfolio 候选，并在统一总预算内
  依次调查。accepted 候选只有在至少产生一次 Scenario attempt 后才算已调查；Risk 调用越过
  token 阈值而 Scenario 尚未开始时，下一 Episode 必须继续同一候选；
- Risk 源码导航采用显式 mount 内的中立搜索和随后 bounded read，不接受修改特定的文件/函数、
  源码差异或缺陷定向提示；通用协议不变量、公开性质与 Oracle-backed property 仍是正式知识输入。
  新 portfolio 前必须机械完成一次 Agent 自选搜索和一次对搜索结果的限行读取。
  盲测试运行必须让 Agent 读取与执行器编译使用同一独立 SUT checkout，并从该 checkout 排除
  解释性修改注释和专用验证测试。etcd/raft 已增加只读 Evidence 驱动的 election-safety Oracle；
  场景仍受公开分组控制能力限制，无 finding 不能被解释为实现通过；
- 同一物理文件经多个 mount/reference prefix 暴露时，搜索只保留按 prefix 稳定排序后的一个
  Agent-visible reference；Dossier 在搜索前只给抽象 contract，不给缺陷相关精确函数 locator；
- 协议知识使用现有 KnowledgeStatement/ProtocolProperty/TargetDossier 承载紧凑 CFT fault model
  与协议不变量。Raft 的 quorum 由 `Q=floor(n/2)+1` 和实际 Target 节点数推导；OmniPaxos
  使用 ballot/prepare/promise/accepted/decided/recovery 术语，不把协议语义写入公共 Core；
- Risk 固定最多 4 次调用：正常路径为一次中立 search、一次 bounded read、一次 portfolio、
  最多一次可信资格反馈修复。一次 search/read 返回 `stopped` 时可重试一次，但会占用原本的
  portfolio repair 调用；每次只允许一个知识请求，read 完成后不继续翻页。`risk-agent-navigation-v10`
  按可信 phase 收窄 Schema：search 前只允许 search，成功后只允许读取真实 match，read 后只允许
  portfolio；query 是单行匹配的一个大小写不敏感字面子串，不分词且不执行 OR。oracle-backed 是 prompt
  的可验证性软偏好，不是可信侧准入条件；已完成的 read 按候选 mechanism step 是否引用 `source/...`
  报告为 `completed-used|completed-unused`，该口径只证明候选引用了真实读取片段，不证明源码阅读定位了
  缺陷，也不形成 gate；已通过机械审查的 observable-only/
  hypothesis-only 候选仍必须保留。源码只确认实现机制与 contract，不能作为 defect verdict；
- 取消共享调用池。每个新候选或队列候选都固定获得 8 次 Scenario 调用和单步计划；当前 etcd/raft
  有效输入给 64 decisions，初始五节点 frontier 更大的 OmniPaxos 给 128 decisions。协调者存在后每轮
  至多 4 个自然推进 decision；bootstrap 使用至多 16 个 decision 的有界因果切片，优先 item dependency、
  再按 Agent 所选参与者方向推进，并在可信协调/term/ballot/milestone 变化时返回。Episode 总预算为
  12 calls、240k observed tokens、30 分钟，
  Risk reasoning 保持 high，Scenario reasoning 降为 low，Scenario 输出上限仍为 8192 tokens；
  HTTP 200 的结构化响应失败必须区分 length/empty/malformed/too-large，并在已知时保留 usage、
  response identity 与 finish reason。首次 `finish_reason=length` 只允许在现有预算内做一次绑定同一
  frontier 的最小 JSON repair；repair 不执行或重放 Runtime Action，二次失败以 in-band stop 保存
  已验证前缀并继续 Bundle、fresh Replay 与 Oracle。journal 恢复只允许精确相邻、同 root、同冻结 view、
  绑定原调用序号的 length→repair；dispatch 后缺少 result 的调用保持显式 unreconciled，绝不静默重发。
  失败响应跨过 token 阈值时保留原 failure classification，只停止后续模型调用；已有 committed
  Scenario 前缀跨过阈值时仍封存 Bundle、fresh Replay 和 Oracle，Episode 结论保持 budget exhausted，
  并显式报告 `oracle_evaluated`；
- Exploration Memory 增加 `property_ref`/`evidence_level`，只用于识别语义重复和理解可验证性，
  不把 oracle-backed 当作 finding，也不参与可信 verdict；
- 六 Episode 长调查不设独立 portfolio 数量上限，只受统一 Episode/call/token/decision 预算约束；
  按原顺序调查最多六个候选，总上限 72 calls、
  1.44M observed tokens、384 Scenario decision allowance；不因首个 finding 提前停止，且暂不
  实现跨 Episode live Trace continuation；
- Scenario 的 stateless 压缩反馈必须优先展示 next missing milestone、resolved bindings、协调状态、上一条 Agent
  Action 的可信效果和当前候选 Action；每个 enabled Action 只在合并后的 `action_frontier` 中出现一次，
  并保留 previous proposal、outcome/reason、selector failure、
  capability gap、closure handoff 与 ProgressDelta；逐步反馈删除重复的 Choice、RiskProgress 和
  view/evidence digest。`token-stopped` 只停止当前 Episode，已 accepted 但未进入
  Scenario 的候选必须在 Investigation 总预算尚可启动时由下一 Episode 继续；
- M4n10 的零模型/fixture 校准已经机械验证 source search/read、一次 portfolio 修复、固定
  Scenario 深度、超过三次反馈循环、协议 quorum 知识、端点 binding、fresh Replay 与
  election-safety 合成冲突；这些校准结果不进入后续正式盲测 Memory；
- Investigation 同时报告预留 decision allowance 与实际 Scenario decisions；全部 Episode 完成时
  优先报告 `episode-limit-reached`，只有预留额度阻止下一 Episode 启动时才报告 decision limit；
- 同一实现内的 `public-fixed` 与 `target-local` 必须由 Target composition 的实际
  factory 状态派生到现有 `AgenticMethodSpec.closure_mode`；调用方不能只改标签，
  composition 与字段不一致时拒绝运行，两个 arm 使用不同 digest；
- Risk 输入是正式的二选一方法参数：默认 `agent-generated`；`existing-candidate` 可从
  独立候选、assessment 或已有 Episode summary 读取。已有 qualification 不复用，必须
  按当前 Target 重算；规范化候选 digest 进入 MethodSpec，并允许跨 Episode、closure、
  搜索策略或模型进行同 Risk 对比；
- 使用显式 mount 内的中立关键词搜索与限行只读源码窗口；不提供修改文件、变更函数、
  control/candidate 差异或实验定向函数提示；
- 正式效果实验的材料与方法版本必须先于受控修改/历史问题选择固定；实验期间不因观察到的
  candidate 再增强对应 property 或 locator。Oracle-backed 是透明报告的 portfolio 软偏置，
  不允许直接比较 Oracle coverage 不同的 Target 发现率；
- 利用真实 ProgressDelta 做多轮 revise；
- 让 Risk Agent 根据执行 capability gap 切换假设；
- 在隔离工作区探索 native-test candidate，但可信 verdict 仍来自主执行链。

### S3：效果实验

- 先做三节点 etcd 短公开 capability pilot。首轮自由 Risk Agent 两臂生成了不同
  Risk，故只保留为可运行性样本；修正版使用经过当前 Target 重新资格审查的公开
  existing Risk，跳过 Risk provider，仅比较 Scenario Agent。Risk digest、输入模式
  和 `closure_mode` 均由现有 MethodSpec 绑定；除 `closure_mode` 外，leaf semantic
  view、模型/prompt/seed、预算、Replay 与 Oracle 必须相同；
- existing Risk 首次真实配对已消除 Risk 漂移，但两个 arm 均未 reached。target-local
  已执行正确 drop，随后因 Agent 继续猜测未 enabled 的闭合消息而 `no-match`。M4l7
  已增加可信前缀 handoff：Scenario view/prompt 暴露 closure，且 Target factory 在每个
  成功前缀后机械确认是否接管；确认后忽略剩余预测步骤并执行受限 closure。以相同
  Risk 重跑已经完成：两个 arm 共享到同一 `MsgAppResp n2→n1` drop 的精确 Trace 前缀；
  public-fixed 以 3 次调用/83,202 tokens 未 reached，target-local 在 drop 后 handoff，以
  2 次调用/41,133 tokens reached 并提交同一 RequestID，fresh Replay stable、Oracle
  finding 为 0。两臂 view/prompt 因 closure mode 而不同，public 还包含一次无效 revise；
  token 差额只能归于完整方法臂。保存 Bundle 已由 evaluator 侧按既有 Target registry
  重算四个 monitor，并作为 canonical gzip 保留；
- M4l8 已从上述真实 step-31 共同 Trace 做零模型后端消融：14-decision 等预算且执行
  work 相同时，只有 target-local reached；public-fixed 需要19个后干预 decision 才得到
  同一 committed RequestID。该结果隔离了 prompt、token 和 proposal 波动，但仍只是
  单 Risk/单拓扑 capability 证据；下一步用少量同预算模型重复估计方差，不把一次成功
  提升为总体方法结论；
- M4l9 已按预注册的五组交替顺序完成上述重复。两臂正确干预率均为 5/5；
  target-local reached 5/5，public-fixed reached 2/5。target-local 使用 12 次调用和
  265,819 observed tokens，public-fixed 使用 15 次调用和 395,236 tokens。10 个
  Bundle 均记录 stable Replay，并由 evaluator 校验 Bundle/projection、重算四个
  monitor、0 violation。干预是语义等价的 follower→leader `MsgAppResp`，并非全部
  固定为 n2→n1；target-local 直接闭合为 4/5，旧 per-call quantum 在一轮中导致
  handoff 后又出现额外 Crash；
- M4m1 将 closure 所有权提升到 Episode 剩余全局 decision 预算：接管后不因本轮
  quantum 耗尽而重新调用 Agent，除非歧义、无候选或全局预算真正耗尽；现有 etcd
  5+12 回归不再产生额外 Crash；
- M4m2 已用 OmniPaxos 验证第二条相同接口的垂直切片。真实首请求使用
  `accept-sync→accepted→decide` 路径；丢弃 step-26 `accept-sync n1→n2` 后，4-decision
  等预算下只有 target-local reached，公共顺序需 8 decisions。factory 仅选择实际
  enabled 的 `prepare/promise/accept-sync/accepted`，同一 RequestID completed、fresh
  Replay stable、Agreement 0 violation。该结果把跨协议复用从设计主张推进为两个
  Target 的代码证据，但仍不是 finding 或总体优越性证明；
- M4m2R 已以当前 Target 重新资格审查的 existing Risk 走通
  `Scenario Agent→handoff→closure→qualified Bundle`。规范 Risk ID 保持精确匹配，
  不把 factory 放宽到任意消息丢失候选；零模型 Planner 只调用一次，三步计划加四步
  closure 形成与脚本消融相同的 Trace/Bundle，同一 RequestID、Replay 和 Oracle 均闭合。
  后续 M4m3 真实模型实验只替换 Planner，没有改变 Risk、Runtime、closure、Replay 或 Oracle；
- M4m2O 新增窄的 `omnipaxos-client-decision-binding` monitor。它从同一 Bundle 联结
  Invoke、completed ClientResult 中的 worker decision，以及对应 decided-prefix 位置，
  并把 `client-operation-continuity` 提升为 oracle-backed。正常、RequestID 篡改、决定
  内容/映射篡改、缺失 witness 与 pending 边界均由普通测试覆盖；公共 Action、Runtime、
  Bundle schema 和 worker evidence 不变；
- M4m3 已按预注册镜像顺序完成四个 Scenario-only Episode。Risk provider 为 0，总计
  10 次 Scenario 调用、181,093 observed tokens，无传输歧义。target-local 两轮均选择
  operation-carrying `accept-sync`、发生 handoff 并直接闭合，同一 RequestID、recorded
  fresh Replay 和三个 Oracle 均闭合；public-fixed 两轮机械 Risk 也 reached，但实际
  Drop 均为 `prepare`，不满足预注册干预口径。故当前证据是“跨协议真实模型 handoff
  2/2”，不是总体优越性；它还暴露出 Risk 可执行谓词弱于自然语言 summary 的明确缺口；
- M4m4 以 Target-local Observation 修复该缺口，没有扩展公共 Action 或时序 DSL。
  operation Drop 必须来自带 `entry_count > 0` 和 RequestID 的 `accept-sync|accept-decide`；
  Decision RequestID 从实际消息 dependency 链唯一回溯；v2 Risk 用同一请求绑定 Invoke、
  Drop 和 Decision。四个 M4m3 Bundle 离线重投影后，旧谓词 4/4 reached，严格谓词为
  public 0/2、target-local 2/2。该结果只作事后 fidelity 诊断，不覆盖预注册结论；
  implementation identity 同步更新为 `m4m4-risk-fidelity-v1`，旧 `m4m1` 只读有效；
- M4m4R 将 closure 激活收紧到可信 Risk 中与当前 intervention decision 相同的 Drop
  milestone，并要求当前 Trace 只有一个、且 RequestID 与被丢消息一致的 Invoke。
  这使现有单请求能力闭合，同时把多请求/批量消息明确保留为未支持边界；没有新增
  公共 Action、gate 或第二套执行器；
- 上述证据支持“Agent 选干预 + Target-local 闭合”的窄 capability 结论，但仍不是
  跨协议普适性、缺陷发现能力或总体方法优越性证据；
- 再做同预算多 seed、长时 Random/单 Agent/双 Agent/专家对照；
- 预注册方法与预算后进入 private holdout；
- 依据 finding、探索增量、false positive 和完整成本判断价值。

### S4：第三协议与接入成本

选择 HotStuff/Tendermint 类非 Raft/Paxos 日志复制实现，测量新增代码是否主要
局限于 Adapter、target-local Observation 和 Oracle。目标是薄且可解释的适配，
不是承诺零代码黑盒接入。

接入成本分别统计：SUT checkout/构建配置、Adapter、Target-local 语义和 Oracle；
上游源码本身不计入 ConsensusAtlas 主仓库代码行。需要修改协议时只改变 SUT 子模块，
不应因此向公共 Core 增加该协议专用分支。

## 10. 可行性检查

每项新工作先回答：

> 它是否让 Agent 更有能力发现共识实现问题，或让发现更可信、可复现、可比较？

若答案只是增加 schema、gate、阶段文件、历史工件或抽象层，则不应继续。
系统允许不完美的 Agent proposal，但不放松 Runtime、Replay、Oracle 和正式数据
隔离边界。
