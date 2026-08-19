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

1. 协议知识包：角色、轮次/任期、消息族、关键边界和待检验性质；
2. 薄 Adapter：将官方实现 API 翻译为统一 Action/Item/Observation；
3. capability/fidelity 声明：实际可控、可观测、可 Replay 的边界；
4. Target composition：语义投影、PSS 映射和确定性 Oracle registry；
5. Agent 可查询的只读源码目录；
6. 模型、时间、调用、token 和 Runtime 工作预算。

协议知识、预算和可变实验配置优先使用 JSON；必须调用官方 API 或实现复杂
证据转换的部分使用 Go/Rust。公共 Core 不包含具体协议分支。

### 2.2 处理

```text
Risk Agent
  读取知识/源码/历史机械反馈，生成候选 portfolio
        ↓ typed qualification
Scenario Agent
  生成语义 selector 与有限计划，按反馈 continue/revise/abandon
        ↓ trusted binding
Control Runtime
  执行 enabled Action，保存完整 Trace 和成本
        ↓
fresh Replay → Observation/PSS/Risk 重算 → Oracle registry
        ↓
artifact/evaluator → finding、探索、能力缺口和完整成本
```

Agent 可以提出不确定或错误的候选；本地类型、capability 和 enabled frontier
决定它能否执行。拒绝应生成精确、可修复反馈，不能伪装成协议失败。

### 2.3 输出

- 可 Replay 的 Action Trace 与 Execution Bundle；
- 独立 Oracle finding、首次违例位置及 control false positive；
- Risk milestone、PSS state/transition 和探索增量；
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

根据知识、源码窗口和 Exploration Memory 生成多个可检验风险，说明前置条件、
milestone、需要的能力和 fidelity。它不能编写 Oracle 或预设 verdict。

### Scenario Agent

把已接受 Risk 逐步变为语义计划。它获得当前 target surface、enabled frontier
摘要、首个缺失 milestone、附近 Trace slice、ProgressDelta、循环深度和能力
缺口。正常主线只需 `continue`、`revise`、`abandon`；复杂分支实验由真实需求
驱动，不默认扩展状态机。

### 分析与自我修复

源码导航、proposal repair、control/ablation 和 trace minimization 可以由后续
Agent 能力承担，但任何修改后的候选仍必须重新经过可信执行和 Replay。当前不
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

- etcd/raft 与 OmniPaxos 两个真实 Target；
- 统一 Action、自然时间、消息所有权、crash/restart 与 Replay；
- target-local Observation/Oracle registry；
- Risk portfolio、受限源码导航、机械资格和 feedback repair；
- live Scenario、ProgressDelta、durable journal 和完整成本；
- Agentic artifact 到 formal/private evaluator；
- 两个协议上的 target-local 消息 leaf type 选择校准。

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
- closure handoff 与正式 Risk 输入属于方法实现变化，当前 MethodSpec implementation
  identity 为 `m4l7-risk-input-closure-handoff-v1`；旧 `m4l5-closure-v1`/`m4d-v1`
  只用于读取历史工件，不能恢复为当前运行；
- 同一实现内的 `public-fixed` 与 `target-local` 必须由 Target composition 的实际
  factory 状态派生到现有 `AgenticMethodSpec.closure_mode`；调用方不能只改标签，
  composition 与字段不一致时拒绝运行，两个 arm 使用不同 digest；
- Risk 输入是正式的二选一方法参数：默认 `agent-generated`；`existing-candidate` 可从
  独立候选、assessment 或已有 Episode summary 读取。已有 qualification 不复用，必须
  按当前 Target 重算；规范化候选 digest 进入 MethodSpec，并允许跨 Episode、closure、
  搜索策略或模型进行同 Risk 对比；
- 扩大但仍声明式的只读源码查询；
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
  Risk 重跑验证，不靠单纯增加调用预算掩盖接口缺口；
- 再做同预算多 seed、长时 Random/单 Agent/双 Agent/专家对照；
- 预注册方法与预算后进入 private holdout；
- 依据 finding、探索增量、false positive 和完整成本判断价值。

### S4：第三协议与接入成本

选择 HotStuff/Tendermint 类非 Raft/Paxos 日志复制实现，测量新增代码是否主要
局限于 Adapter、target-local Observation 和 Oracle。目标是薄且可解释的适配，
不是承诺零代码黑盒接入。

## 10. 可行性检查

每项新工作先回答：

> 它是否让 Agent 更有能力发现共识实现问题，或让发现更可信、可复现、可比较？

若答案只是增加 schema、gate、阶段文件、历史工件或抽象层，则不应继续。
系统允许不完美的 Agent proposal，但不放松 Runtime、Replay、Oracle 和正式数据
隔离边界。
