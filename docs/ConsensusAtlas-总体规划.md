# ConsensusAtlas 总体规划

> 文档性质：项目方向约束、总体架构和阶段验收基线
> 状态：Draft v1.72（Agentic Track A0 研究主线重置）
> 日期：2026-08-13
> 适用范围：`consensus-atlas` 仓库及围绕它开展的论文研究、实验和 Agent 系统

---

## 0. 这份文档的作用

ConsensusAtlas 的长期目标很容易在实现过程中退化成以下几类系统之一：

- 只会随机丢包、宕机的 chaos 工具；
- 专门绑定 etcd/raft 的测试程序；
- 让 LLM 自由生成测试代码的 Agent demo；
- 用一个含义不清楚的数字包装普通代码覆盖率；
- 要求每个新协议先提供完整形式化模型、因而无法落地的研究原型。

这份文档用于避免上述偏移。后续重要设计若与本文冲突，应先形成显式设计决策记录（ADR），说明：

1. 为什么原有约束不再成立；
2. 改动是否削弱确定性、可重放性、覆盖分母稳定性或跨协议复用能力；
3. 如何通过实验验证新的设计；
4. Profile、PSS、Driver 和结果格式是否需要升级版本。

本文不是某一个协议的实现说明，也不是完整 API 规范。它定义的是系统必须长期保持的研究主线和信任边界。

---

## 1. 项目定位

### 1.1 核心问题

给定一个新开发或已有的 CFT/BFT 共识系统，在有限但公开的测试边界内：

1. 如何受控地产生消息投递、延迟、丢弃、复制、分区、超时、宕机、重启和持久化切点？
2. 如何判断两个执行只是 term、节点编号或独立消息次序不同，还是属于根本不同的协议场景？
3. 如何机械地产生一个有限、版本化、可审查的覆盖分母？
4. 如何用确定性 Oracle 检查安全性，并在明确的部分同步与公平性假设下寻找活性反例？
5. 如何给出可重复的量化分数，同时避免把该分数错误解释成“协议正确概率”？
6. 如何让人工只提供一次最小 Protocol Charter 或选择可选 Family 扩展，之后由 Agent 自动完成 Core PSS Mapping、代码接入、覆盖规划、测试生成、失败修复和反例归纳，同时不让 Agent 进入可信执行、判定和计分路径？

### 1.2 研究主线

项目固定采用以下主线：

> **协议感知 Agent 生成并修正测试假设 + 统一共识控制层确定执行 + 语义反馈驱动探索 + 独立 Replay/Oracle 评价**

人工只提供协议实现、协议文档/形式化模型、核心安全与活性目标、故障模型、有界测试范围和薄 Adapter/Binding 所需的最小事实。Protocol/Hypothesis Agent 负责理解协议并提出风险假设，Explorer Agent 把假设转化为有界 episode 目标、调用可信搜索工具并依据机械反馈修正计划。是否拆出独立 Critic 或 Onboarding Agent 由消融实验决定，不再预建完整角色体系。所有 Agent 产物只能由确定性 Coordinator、Runtime、Evidence Matcher、Replay、Conformance 和 Oracle 接受。

系统的总原则是：

> **扩大 Agent 的测试提议权和搜索权，保持 Agent 的事实权与最终判定权为零。**

可信内核继续产生 `EnabledActionSet`，但 Agent 不再与 Random/DFS/DPOR 平级。Agent 可以提出协议级
测试假设、选择语义目标、对全局候选 WorkItem 排序、调用冻结的局部搜索算子，并根据
PSS/Risk/义务/不可达 finding 修正下一轮计划；Random、DFS、best-first 和未来 DPOR 是其可调用或
可对照的搜索工具。Agent 不生成不存在的消息，不自行判定 enabled、PSS 真值、状态等价或缺陷结论。

固定分母的义务覆盖率评价最终测试成果；无固定分母的 PSS 状态发现曲线评价搜索效率。PSS 是语义归一化和证据定位机制，不能单独成为最终完成百分比。

ESOT/测试义务目录继续保留，但定位为高风险补充层，而不是唯一覆盖分母。

### 1.3 最终结果的准确表述

系统输出的分数只能表述为：

> **在指定 Contract/PSS/Profile、故障边界、Driver 能力和 Oracle 集合内，对冻结语义测试义务获得强证据的比例。**

不得表述为：

- 协议被测试了多少百分比；
- 协议正确的概率；
- 剩余缺陷概率；
- 任意网络、任意规模或任意拜占庭行为下的全面性证明；
- 有限测试对异步协议全局活性的证明。

### 1.4 外部效果优先于内部指标

Coverage Obligation 和 PSS 都是系统自己定义、并提供给 Agent 的内部反馈。如果
系统让 Agent 优化这些指标，再用指标提高证明 Agent 有效，会形成循环论证。正式
研究必须把评价分为三层：

1. **主要外部结果**：在相同完整执行预算下，对 Agent 隐藏的历史真实缺陷和协议级
   语义 mutant 的独立根因检出率、首次检出成本、复现率与正确版本误报率；
2. **内部解释指标**：固定义务覆盖、PSS 状态/转换发现曲线、Oracle 激活和成本，
   用于解释方法为什么有效或无效；
3. **额外案例**：在官方当前版本发现并由上游确认的新缺陷，这是最强案例，但不是
   项目成立的预设条件。

义务/PSS 只有在未参与设计的 holdout 缺陷上能够预测或提高缺陷检出时，才能成为
论文的主要解释变量。没有发现新漏洞不构成失败；在隐藏缺陷上也不优于简单基线时，
不得通过增加指标或 Agent 数量维持原有主张。

### 1.5 与 Agora/Themis 的研究边界

`LLM`、“多 Agent”或“面向分布式系统”本身都不是 ConsensusAtlas 的贡献。
Themis 以 RPC 可达共享状态的冲突访问为 bug model，使用静态分析生成 race target，
再由 LLM 构造状态准备/公开接口调用序列，directed fuzzing 调参并探索并发交错。
ConsensusAtlas 不以共享变量 race 为必要前置，而以 epoch/quorum/proposal/decision/
persistence/recovery 等共识语义关系为 Risk、PSS 和 Oracle 的对象。Themis 在已知问题
benchmark 中排除 state-machine-related bugs 不应被扩大解读为“绝不可能检出共识
实现中的 RPC race”；两者的稳定区分是 bug model、控制面和判定层，而不是项目名称。

Agora 与本项目都面向共识语义问题，因此“protocol-aware Agent”也不足以区分。
Agora 的主要搜索产物是 `bug hypothesis -> attack scenario -> repository-specific unit test`；
Strategy/TestGen 可以根据仓库知识生成、执行并修复测试代码。ConsensusAtlas 的长期区分必须是：

```text
protocol/code/knowledge
          |
          v
Protocol/Hypothesis Agent -- TestHypothesis --> Explorer Agent
          ^                                      |
          |                                      v
mechanical EpisodeReport <--- trusted search tools / Runtime
          |                                      |
          +--------------- repair loop ----------+
                                                 |
                                                 v
                             independent Replay / Oracle / evaluator
```

因此 Agent 不生成 enabled set，不伪造消息/状态，不修改当前 Oracle，也不根据自己的文本解释确认缺陷；
但它必须真实决定“测什么、优先探索哪里、如何根据反馈修正”，而不是只排列一个局部 frontier。
提出测试的组件与判定测试成败的组件仍然分离。

M5.22 `CrossTargetPlannerView` 的 target-blind 输入只作为已冻结的
portability/authority 校准与消融记录；该生产组合已在 M5.23R2 从 HEAD 退休。
正式 Agent effectiveness 实验让不同方法共享相同 Runtime、root、预算和外部评价，同时仍隐藏
candidate/control、root cause、patch 和私有 Oracle verdict。单 Agent 是必须保留的对照；主方法从
Protocol/Hypothesis 与 Explorer 两个职责开始，并在相同总 token/调用预算下做角色合并消融。

### 1.6 2026-08 Agentic 研究主线重置

M5.23g 已用真实模型证明“只排列当前 frontier”的 Agent 在三个公开 root 上退化为 canonical。该结果
不证明 LLM 无法改善共识测试，却足以否定它作为最终 Agent 权限。自本版本起：

- M5.23R4b 的 Runtime、Adapter、exact-prefix、Replay、Oracle、Campaign 和 WorkLedger 冻结为底座；
- 不再以新增 digest、恢复状态、资格 schema 或 evaluator 层次作为默认进展；
- Agent-v1 保留为最小权限消融，不再扩展；
- 新主线先完成公开的“假设—episode—执行—语义反馈—修正—重放”闭环，再进行隐藏评测；
- 允许 Agent 假设和计划出错，由可信层拒绝并反馈；不要求提议层在执行前机械证明全部语义；
- 正式结论仍必须来自独立 Oracle、可重放真实轨迹、公平预算和 candidate/control 隔离。

---

## 2. 总体设计原则

以下原则优先级高于局部实现便利。

### 2.1 可信性优先于搜索速度

错误合并两个不等价场景会系统性漏掉缺陷；保守地多执行一些场景只会降低效率。因此，等价与独立关系不确定时一律不合并。

### 2.2 覆盖率与正确性分开

某个场景触发 Oracle `FAIL`，仍然说明该覆盖原子被真实到达、观察和检查。Coverage 负责回答“检查了什么”，Oracle 负责回答“检查结果是什么”。

### 2.3 固定分母后再运行

一次正式评测开始前必须冻结：

- PSS 版本；
- Profile 版本及边界；
- 覆盖原子集合；
- Driver 和能力清单；
- Oracle 集合；
- 规范化规则；
- 实现版本、编译选项与随机种子策略。

Agent 在运行中发现的新风险只能进入下一版候选目录，不能修改当次评测分母。

### 2.4 原始系统默认不修改

优先直接使用目标系统公开接口、官方库或真实进程。只有在无法控制关键非确定性或无法观测必要证据时，才允许维护可选的 instrumented build；其结果必须与官方构建分开报告。

### 2.5 接入代码要薄，协议语义不能假装不存在

每个系统都不可避免地需要少量接入信息，因为创建节点、传递消息、读取输出、恢复存储和观察提交的 API 不同。但不应要求每个系统重写调度器、网络、存储故障、轨迹或覆盖逻辑。

长期目标是让每个新系统只增加边际接入产物：

```text
通用 Runtime + 共享 Adapter kit
              + 薄 Execution Binding
              + 固定 Core PSS IR + Semantic Mapping
```

而不是：

```text
每个协议一个包含完整生命周期、调度、语义和评分的厚重 Adapter
```

`control.Adapter` 可以继续作为 Runtime 的严格完整契约，但 Manifest、Check、Yield、Collect、
identity、digest、finding 和 conformance 组装不应由每个目标重复手写。共享 Adapter kit 只能是具体
Adapter 内部的复用库，不是第二套 Runtime backend。目标专用 Binding 只调用官方 lifecycle、input、
transport、clock 和 observation 接口；协议理解只存在于独立 Semantic Mapping。

PSS 也不能以“接口返回 `state any`”代替真正复用。所有协议先映射到固定 Core PSS IR；Family/协议
特有细节只能作为可选 Extended PSS。换协议允许增加映射规则，不允许修改通用 ledger、canonicalizer
或把 term/ballot/QC 字段加入 Runtime。

### 2.6 消息是异步一等对象

“协议产生消息”“消息取得网络可见性”“消息被投递”是三个不同事件。任何把输出消息立即传给目标节点的实现都不满足本项目要求。

### 2.7 能力不足必须显式暴露

不支持某类超时、崩溃切点、持久化边界或状态观察时，应在 Capability Manifest 和覆盖结果中显示 `Unsupported`，不能悄悄近似成已支持。

### 2.8 Agent 不进入可信路径

LLM/Agent 可以生成实现 Binding、Driver 草稿、可执行见证、场景目标和反例解释，但不能：

- 决定一次事件是否真正发生；
- 修改当次覆盖分母；
- 宣布两个场景等价；
- 代替确定性 Oracle；
- 单独确认一个协议缺陷；
- 让不可重放执行获得覆盖证据。
- 修改当次 Protocol Knowledge Contract，或在 Binding 中自定义更弱的验收断言。

Agent 可以生成语义草案、覆盖义务草案、Driver、Binding、测试计划、调度目标、修复提案和反例解释；但 `proposed` 只有经过 schema、digest、构建、fixture、执行、Oracle、Replay、Conformance 和 Evidence Matcher 后才能进入 `validated`。多个 Agent 相互同意只能提高候选质量，不能替代机械证据。

### 2.9 接入必须形成自动闭环

人工输入的 Contract 是协议语义真值边界。Agent 产出只允许包含 Contract ID 到 Runtime ID 的映射、Driver 代码与可执行见证。可信验证器必须机械检查：

- Contract digest 与目标系统身份；
- 能力和操作映射完整性；
- Driver 编译、Manifest 和 conformance；
- 消息、存储、crash/restart 等边界；
- 见证是否命中 Contract 固定 label 和 monitor；
- 两次重放指纹、Oracle 与轨迹一致性。

可修复问题返回结构化 finding 给 Agent 下一轮。目标实现真实缺失的能力自动标记 `Unsupported`，但相应义务仍在分母中。旧 Integration Pack/Scout 标签流程及人工 Gate 已从主仓库删除。

### 2.10 测试生成也必须形成自动闭环

接入成功并不等于完成测试。测试阶段以 Agent 假设与 episode 反馈形成闭环；Coverage Ledger 是其中一种
可信解释输入，不是唯一目标：

```text
Protocol Knowledge -> TestHypothesis -> EpisodePlan -> Trusted Search/Runtime
       ^                                                       |
       |                                                       v
EpisodeReport + Coverage/PSS/Risk <- Oracle/Replay <- Runtime Trace
```

Protocol/Hypothesis Agent 负责“测什么”，Explorer Agent 负责“优先探索哪里以及如何根据反馈修正”。
它们只能通过 Runtime 暴露的 enabled events 和冻结搜索工具影响执行。Agent 不能直接写入 Ledger，
不能自报 observation 取得覆盖，也不能在当前 campaign 中增加、删除或降权义务。

### 2.11 复杂度必须由外部收益支付

项目设置显式“复杂度预算”：

- 在 A2 single Agent-v2 公开闭环完成前，不继续增加通用时序逻辑、复杂复合义务或第三个 Agent；
- 没有具体漏检根因时，不增加新的义务表达能力；
- 现有 Matcher/DSL 能表达时，不新建抽象层；
- 新义务、PSS 维度或 Agent 角色若不能改善 holdout 缺陷检出、降低成本或减少误报，
  不进入正式路径；
- 一个 Agent 能可靠完成的职责，不因架构对称性拆成多个 Agent；
- 第二实现不能复用的机制，只能声明为 etcd/raft 实例经验。

评价预算必须覆盖所有影响 SUT 的工作，不能只统计 Explorer measurement decision。
每次重复 setup/prepare、drain 中执行的 Runtime event、故障动作、measurement event、
run 数以及 replay 验证开销均须分别记录。正式方法比较至少固定 primary runtime
work；模型 token、墙钟时间与 replay 开销独立报告。

### 2.12 精确执行状态与语义新颖度必须分开

系统同时维护四种不同的身份：

1. `ExecutionFingerprint`：SUT/Driver/Runtime 精确执行与严格重放身份；
2. `StateRef`：指向可从初始状态或可信 checkpoint 恢复的确定性前缀；
3. `StructuralKey`：仅做节点/事件 ID 等已证安全的结构归一；
4. `PSSSemanticKey`：用于状态发现和搜索新颖度的协议语义键。

PSS 键相同只能影响搜索优先级和发现计数，不得直接用于 visited-state 剪枝。
只有未来 enabled 集合、属性监控状态和后继观测在已证等价关系下保持一致时，
才允许 DPOR/状态工作队列合并。

---

## 3. 总体架构

正式执行之前先建立确定性知识、自动接入和冻结覆盖分母：

```text
Minimal Protocol Charter + optional Family extension
               |
               v
 Knowledge/Obligation Agent mapping proposals
               |
 schema/source/fixture/differential validation
               v
  Frozen Contract/Core PSS Mapping/Profile obligations
               |
代码 + Contract + 上轮 findings
               |
               v
          Onboarding Agent
               |
     Binding + Driver + witnesses
               |
               v
 Deterministic Validator/Coordinator
       | invalid             | validated/unsupported
       +---- findings -------+----> Profile + Driver + Oracle Set
```

```text
                 ┌──────────────────────────────────────┐
                 │ 冻结输入                              │
                 │ Core/Extended PSS + Profile + Binding│
                 │ Oracle Set + Version Manifest        │
                 └──────────────────┬───────────────────┘
                                    │
                       Profile/PSS Compiler
                                    │
                     固定覆盖原子与候选场景约束
                                    │
            ┌───────────────────────┴───────────────────────┐
            │                                               │
   Systematic / Combinatorial Queue              Exploratory Queue
   DPOR、语义切点、ordered t-way                  Agent、greybox、历史模式
            │                                               │
            └───────────────────────┬───────────────────────┘
                                    │ Plan
                                    v
                 ┌──────────────────────────────────────┐
                 │ Deterministic Test Runtime           │
                 │ 时钟、事件队列、网络邮箱、故障、存储 │
                 └──────────────────┬───────────────────┘
                                    │ 标准动作/输出批次
                 ┌──────────────────v───────────────────┐
                 │ Shared Adapter Kit + Execution Binding│
                 │ 公共生命周期 + 原生 API/编码转换     │
                 └──────────────────┬───────────────────┘
                                    │
                          Official SUT / Process
                                    │
                                    v
                    原始执行轨迹 + Decision Log
                                    │
              ┌─────────────────────┼──────────────────────┐
              v                     v                      v
       Mapping to Core PSS IR  Deterministic Oracle   Replay Verifier
              │                     │                      │
              └─────────────────────┼──────────────────────┘
                                    v
                    Causal Graph / Canonical Key
                                    │
                                    v
                     Coverage Ledger + Score Vector
                           │                 │
                  Coverage Debt             v
                           │       Counterexample Analysis Agent
                           v
              Planner/Scenario/Critic Agents
                           │
                           v
                Deterministic Concretizer
                           │
                           └────────> 下一轮 Runtime
```

### 3.1 三个逻辑平面

#### 执行平面

负责确定性地改变系统状态：

- 逻辑时间；
- 事件启用条件；
- 网络邮箱；
- 事件投递、丢弃、复制和延迟；
- 分区和恢复；
- 持久化阶段；
- 宕机和重启；
- Decision Log；
- 原始轨迹。

执行平面使用 Go 实现，不依赖 Python 或 Agent。

#### 语义与验证平面

负责：

- 实现事件到 PSS 事件的抽象；
- 因果图构造；
- 对称性和偏序规范化；
- 确定性安全/活性 Oracle；
- 覆盖账本和评分。

该平面也属于可信路径。

#### Agent 平面

Agent 平面按产物和权限拆分，而不是用一个模型同时生成、执行和批准自己的结果。第一条可运行主线只包含：

1. `Protocol/Hypothesis Agent`：从协议文档、源码、能力清单和经验证语义投影中提出带来源的
   `TestHypothesis`，说明风险、前置语义、目标现象和允许的故障边界；
2. `Explorer Agent`：把假设转成有界 `EpisodePlan`，对全局候选状态和冻结搜索算子给出优先级，读取
   `EpisodeReport` 后保留、修正或放弃计划；
3. `Critic/Repair Agent`（条件加入）：只有两角色闭环的机械失败记录表明独立反思能改善缺陷检出或
   降低 invalid proposal 时才拆出；
4. `Onboarding Agent`（独立研究轨）：只评价接入成本，不与搜索效果实验同时扩张。

Obligation、Coverage Planner、Scenario、Search 和 Failure Analyst 先作为上述角色内部的逻辑职责，
不为了“多 Agent”数量预建独立进程。多 Agent 的贡献必须由与单 Agent 相同总模型预算的消融支持。

Coordinator、Runtime、Evidence Matcher、Coverage Ledger、Replay、Conformance 和 Oracle 都是确定性程序，不是投票 Agent。Agent 可以读取共享 Blackboard 中的 `proposed/validated/rejected/superseded` 产物，只有 Coordinator 能写入 `validated`。

---

## 4. 通用 Runtime 与薄 Driver

术语约束：本节的 `ProtocolDriver`、`OutputBatch` 是架构层概念名；当前生产代码中的唯一执行契约是
`control.Adapter`，冻结输出单位是 `ProducedItem`。后续文档不得把概念接口误写成另一套待恢复的生产 API，
也不得同时维护 Driver Runtime 与 Adapter Runtime 两条路径。目标专用代码统一称 Execution Binding；只有
具体 Go 类型或历史工件才使用其冻结名称。

### 4.1 为什么不能做到真正的“零接入”

不同系统至少在以下方面不同：

- 节点创建与配置；
- 消息编码；
- `Step/Tick/Propose` 等输入 API；
- 输出批次及其持久化契约；
- snapshot、成员变更和应用状态机接口；
- commit/finalize 的可观测证据；
- restart 时需要恢复的持久状态。

因此完全零接入只能获得粗粒度进程级 chaos 测试，无法可靠控制消息、持久化和重启边界。

Agora 式“直接调用原系统 API”仍然会生成目标仓库专用的测试代码。适配工作只是由 Agent 写进了测试文件，并没有消失。ConsensusAtlas 可以借鉴这种自动生成能力，但生成结果必须经过契约测试。

### 4.2 Runtime 的固定职责

通用 Runtime 不理解 Raft term、HotStuff QC 或 Tendermint lock。它只理解以下协议无关概念：

- 节点和节点 epoch；
- 输入动作；
- 输出批次 token；
- host operation 及依赖 DAG；
- wire message；
- logical timeout；
- visible write、durable sync 和 applied state；
- crash/restart；
- observation；
- capability；
- event identity、causation 和 replay decision。

### 4.3 Runtime 契约与目标 Binding 的职责

下面的完整接口描述 Runtime 与 Adapter/Driver 的可信边界，不代表每个目标都应逐方法手写。M5.7 起，
共享 Adapter kit 负责与协议无关的生命周期、identity、digest 和冻结输出；目标专用 Execution Binding
只提供 Boot/Stop/Invoke、transport/clock 接缝和稳定 observation。不能形成可靠边界的能力直接标记
`Unsupported`。

建议的概念接口如下，具体 Go API 可在实现时细化：

```go
type ProtocolDriver interface {
    Identity() DriverIdentity
    Capabilities() CapabilityManifest

    Boot(node NodeID, durable DurableImage) error
    Stop(node NodeID, mode CrashMode) error
    Invoke(node NodeID, input Input) error

    Poll(node NodeID) (*OutputBatch, error)
    ExecuteHostOp(node NodeID, batch BatchToken, op HostOpToken) ([]Observation, error)
    Acknowledge(node NodeID, batch BatchToken) error

    Inspect(node NodeID) (ImplementationState, error)
    CheckConformance() []ConformanceFinding
}
```

关键约束：

- `Invoke` 不能自行选择调度；
- `Poll` 只能读取并冻结一个输出批次，不能立即发送其中消息；
- Driver 返回 host operation DAG，由 Runtime 调度；
- Driver 只执行 Runtime 已选择的 operation；
- 同一节点不能无约束地存在多个相互覆盖的 outstanding batch；
- Driver 不读取墙上时间，不自行 sleep，不使用未记录的随机选择；
- `Inspect` 只提供观察，不能改变目标状态。

目标 Binding 不包含 scheduler、Replay、Oracle、PSS ledger、Coverage Matcher 或协议算法。共享代码
只有在至少两个真实目标消费时才进入 Adapter kit；否则留在目标目录或删除。接入成本按目标专用人工
LOC、配置/映射条目、首次可执行时间、conformance 修复轮数和映射错误数报告，不能只用最终 capability
通过率表示。

### 4.4 OutputBatch

OutputBatch 应允许系统声明宿主集成契约，而不是让 Runtime 硬编码 Raft 的 `Ready`：

```text
OutputBatch
  token
  operations:
    - visible_write
    - durable_sync
    - release_message
    - apply_application
    - acknowledge
  dependencies:
    - op_a must happen before op_b
  observations:
    - optional implementation evidence
```

每个 operation 的具体 payload 可以由 Driver 解释，但其种类、依赖、目标节点和生命周期由 Runtime 记录。

### 4.5 三种接入模式

#### A. Embedded Native Driver

直接链接官方库并调用原生接口。

优点：

- 消息和持久化切点控制最精确；
- 轨迹信息完整；
- 执行速度高；
- 适合 etcd/raft、Rust raft-rs 等库型实现。

代价：需要一个薄 Driver。

#### B. Process/Proxy Driver

启动真实节点进程，通过统一网络代理截获网络可见消息，通过进程控制实现 crash/restart。

优点：

- 接近生产部署；
- 不需要链接目标代码；
- 很多网络控制逻辑可跨系统复用。

限制：

- 通常只能观察“已经发到 socket”的消息；
- write/fsync/commit/apply 切点可能不可见；
- TLS、连接复用、批处理和自定义传输会增加接入成本；
- 能力不足必须在 Manifest 中标记。

#### C. Agent-Generated Driver

Agent 阅读代码和接口后生成 A 或 B 模式的 Driver 草稿。

生成代码只有通过以下检查后才能进入正式评测：

- 编译和 API 版本检查；
- 输入/输出不丢失检查；
- 消息序列化确定性检查；
- 持久化顺序契约测试；
- crash/restart 只恢复 durable state；
- 相同 Decision Log 的稳定重放；
- 与一组 Contract 派生的 smoke/witness test 的差分检查；
- 每个可支持义务的固定 label、monitor、replay 和 conformance 验收。

---

## 5. 消息模型：产生、释放与处理必须分离

### 5.1 消息状态机

```text
Produced in SUT
      │ Driver.Poll 冻结完整字节
      v
Held in OutputBatch (volatile)
      │ release barrier 满足
      v
Released to Runtime Mailbox
      │
      ├── Deliver ──> target Driver.Invoke(message)
      ├── Drop ─────> terminal + optional sender feedback
      ├── Duplicate -> 新消息 ID，保留 CloneOf
      └── Delay/Partition -> 保持 pending
```

定义：

- `Produced`：协议内部已经产生输出，但尚未对网络可见；
- `Released`：宿主持久化条件满足，消息完整副本进入 Runtime 网络邮箱；
- `Delivered`：目标 Driver 已处理消息；
- `Dropped`：该消息副本被显式终止；
- `Duplicated`：生成一个具有新 ID 和新链路序号的独立副本；
- `Blocked`：受分区、目标宕机、延迟或前置条件影响，仍保留在邮箱中。

`emit/release` 绝不等价于 `deliver`。

### 5.2 WireMessage 必备字段

```go
type WireMessage struct {
    ID             string
    From           NodeID
    To             NodeID
    SenderEpoch    uint64
    LinkSequence   uint64
    CausationID    string
    CloneOf        string
    TypeHint       string
    Payload        []byte
    PayloadDigest  string
    Metadata       map[string]string
}
```

要求：

- Payload 必须进入 trace/可重放工件，不能只保存内存指针；
- digest 使用固定算法和确定性序列化；
- 每个 `(From, To, SenderEpoch)` 维护单调链路序号；
- duplicate 产生新 ID，不能复用原 ID；
- 调度选择使用稳定 ID 和完整 selector，不使用“当前队列第 N 条”；
- replay 时字段不匹配必须失败，禁止 clamp 到另一条消息；
- `TypeHint` 只用于选择和可读性，真实语义来自 PSS 映射。

### 5.3 crash 与消息所有权

| crash 时刻                        | 预期语义                                                    |
| --------------------------------- | ----------------------------------------------------------- |
| 消息 Produced、尚未 Released      | 属于节点 volatile output batch；崩溃取消                    |
| 状态已 durable、消息尚未 Released | 持久状态保留，消息仍未进入网络                              |
| 消息 Released、尚未 Delivered     | Runtime 已取得所有权；源节点崩溃不自动删除                  |
| 目标节点宕机                      | 消息保持 pending，除非场景显式 drop 或 Profile 定义传输过期 |
| 消息 Delivered                    | 已产生不可撤销的目标输入；后续只能影响目标产生的新输出      |

### 5.4 drop feedback

某些协议提供 `ReportUnreachable`、`ReportSnapshot` 等发送反馈。它们是单独的输入事件：

- 普通消息 drop 是否反馈由 Driver capability/协议契约决定；
- snapshot 成功或失败必须按原系统 API 契约报告；
- feedback 也可能产生新 OutputBatch，必须经过相同生命周期；
- feedback 不能由 Agent 隐式补充。

---

## 6. 存储、宕机与恢复模型

### 6.1 四类状态不得混淆

```text
Protocol Volatile State
        │ host write
        v
Visible Storage State
        │ fsync/sync
        v
Durable Storage Image
        │ commit/apply
        v
Application Applied State
```

网络邮箱是第五类独立状态，不属于节点 durable storage。

### 6.2 基本语义

- `visible_write`：存储接口已经能读到，但在 power loss 下可能丢失；
- `durable_sync`：对应数据进入 durable image；
- `apply`：应用状态机消费已提交条目；
- `crash_process`：可以按 Profile 选择是否保留 OS page cache；
- `crash_power`：只保留 durable image；
- `restart`：必须依据 crash mode 从允许的状态恢复，不能复制整个内存对象；
- snapshot、hard state、log、vote、lock、QC 等具体含义由 Driver/PSS 描述。

第一版若只实现简化的 power-loss 模型，应明确标记，不得把内存存储直接称作 durable。

### 6.3 故障切点

Runtime 必须允许 crash 插入以下通用边界：

- 输入处理前/后；
- output batch 产生后；
- visible write 前/后；
- durable sync 前/后；
- release message 前/后；
- commit 可见后、application apply 前；
- acknowledge 前/后；
- snapshot 安装和配置切换阶段。

并非每个 Driver 都能支持全部切点。未支持的切点保留在 Profile 分母或能力报告中，以便诚实反映接入深度。

---

## 7. 时间、超时和部分同步

### 7.1 逻辑时间

可信执行路径不得依赖 wall-clock sleep。Runtime 维护单调全局逻辑时间，并接收 Adapter 在稳定
yield 暴露的三类 `TemporalItem`：

- `OneShotTimer`：明确注册的一次性 timer；
- `PeriodicPulse`：etcd/raft `Tick()` 等周期性宿主时钟脉冲；
- `SleepWakeup`：sleep/wait 的确定性唤醒事件。

令全部有效时间项的最早 deadline 为 `T`。Runtime 只把 `[T,T+E]` 内的时间项枚举为
`FireTemporalEvent(TemporalID)`；第一版冻结 `E=0`，即只能选择最早时间，同 deadline 项可以分支。
普通消息、effect、crash 等 action 仍与该集合竞争，表达消息先到或超时先到。Agent 不能提交任意
`AdvanceTime(to)`、delta 或提前 timeout。

`FireTemporalEvent` 内部原子执行：推进 `now` 到所选 deadline、记录
`ClockAdvanced(from,to,reason,item)`、执行 callback/tick/wakeup、运行至下一 yield 并收集输出。
sleep 快进不得越过更早时间项；没有其他分支且 wakeup 唯一最早时可以作为有记录、有成本的机械
转换。对于 tick-based Raft，每节点每周期只调用一次 `Tick()`，由协议内部 elapsed counter 自然
产生 election/heartbeat；未证明中间无输出、竞争动作和 timer reset 前禁止批量 Tick。

没有可控 temporal 边界的 Adapter 仍可运行，但自然 timeout 和 strict virtual-time replay 必须
标为 Unsupported。局部时钟漂移、租约误差、GST 和 `E>0` 由后续 Liveness Profile 显式冻结。

### 7.2 受控随机性

协议、workload、scheduler 和 Agent 不能共享一个受调用顺序影响的全局随机流。Campaign 主种子
必须派生互相隔离的种子域；SUT 再按 `SUT identity + NodeID + Incarnation + DomainID` 派生稳定
流。第一版冻结版本化生成算法，并为每次抽样记录：

```text
RandomDrawRecord
  = DomainID + NodeID + Incarnation + Ordinal
  + Operation + Arguments + Result
```

Fresh run 由分域 `EntropySource` 计算结果并写入 tape；strict replay 逐次验证请求身份、操作、参数和
结果。只记录原生随机结果却不能在 replay 中重新提供，不足以声明严格重放。Adapter 的接入优先级
是：原生 per-instance RNG 接口、隔离测试进程中的 entropy API 截获、最后诚实声明
`strict_entropy_replay=false`；禁止通过未审计私有状态写入冒充 unmodified 接入。

第一版 RandomDraw 不是在线 Action。Random、DFS、Agent 和专家计划必须使用相同的预冻结 SUT
seed 集合；以后只允许 run 前的有界 `EntropyPlan` 或经 continuation 暴露的有限 ChoiceRequest，
不能让 Agent 提供任意抽样值。

官方 etcd/raft v3.6.0 没有 per-node `Config.Rand`，其随机 election timeout 使用进程级
`crypto/rand.Reader`。保持官方模块不变的 Legacy Adapter 只有在隔离、串行测试边界中按
node/incarnation/domain 分流该 Reader，并通过域外读取、并发读取和 replay conformance 后，才能
声明自然选举严格重放。桌面 fork 的 deterministic hook 只能作为工程对照，不能表述为官方
v3.6.0 原生能力。

不能因为连续运行两次“碰巧相同”就声明自然 timeout 可确定重放。

### 7.3 安全性与活性评分分开

第一版安全覆盖分数不混入活性分数。活性实验必须额外冻结：

- GST；
- GST 后消息延迟上界；
- 调度公平性；
- 最大故障数；
- timeout 与延迟的关系；
- proposer/leader 公平性；
- 随机选择重放策略；
- 进展度量与 lasso 判定规则。

活性结论只能表述为：

> 在指定 Profile 的部分同步、公平性和故障边界内，发现或未发现活性反例。

---

## 8. Protocol Semantic Skeleton（PSS）

### 8.1 定位

PSS 是轻量、可版本化的协议语义层。它不是完整可执行规范，也不能替代 TLA+/Ivy 模型。M5.7 起，
PSS 分成固定 Core IR 与可选 Extended PSS：Core 提供跨协议状态发现、规范化和基础义务所需的最小
共同结构；Extended 只增加某家族的测试深度。Agent 可以提出映射草案，但正式运行只接受经过 schema、
fixture、差分/突变校准和 digest 冻结的映射。

### 8.2 Core PSS IR

Core 状态固定组合两部分：

```text
Core PSS State = Runtime Control Context + Consensus Semantic Graph
```

Runtime Control Context 由可信执行层直接投影，不经过目标 Mapping：

```text
participant lifecycle + relative incarnation
link connectivity / partition relation
pending message/temporal/effect kind + route + conservative causal shape
relative earliest temporal frontier
```

它排除随机 Action/Item ID、绝对时间、opaque payload 和 persist/sync/emit/apply 等 host microstep，避免
实现细节虚增状态，同时保留 Crash、Partition、pending message 和 timeout 竞争这些根本场景。

Consensus Semantic Graph 使用封闭词汇：

```text
entities:
  Participant, Epoch, DecisionUnit, Value, Evidence

relations:
  belongs-to, proposes, supports,
  depends-on, conflicts-with, precedes,
  decides, persists, applies

stages:
  unknown, proposed, supported, accepted, decided, applied

participant modes:
  inactive, passive, contending, coordinating
```

Participant mode 只表达协议无关的执行姿态，不能把 `leader/follower/candidate` 等目标枚举写进 Core；
无 leader 协议可以保持 passive，并用 `proposes/supports` 等关系描述具体 decision unit 的协调事实。

Core canonicalization 组合 Control Context 与 Semantic Graph，并只执行版本化、保守的
participant/epoch/value/decision-unit 重命名和图关系规范化。
绝对 term、ballot、view、index 或节点名称本身不构成新状态。Core PSS 发现曲线没有完备分母，不得
解释为协议测试百分比。

### 8.3 Semantic Mapping 与 Extended PSS

每个协议只增加 `ImplementationEvidence -> Core PSS IR` 映射。例如：

| Core 概念 | Raft | EPaxos | HotStuff |
|---|---|---|---|
| DecisionUnit | log entry | `(replica, instance)` | block |
| Epoch | term | ballot | view |
| supports | vote/replication evidence | PreAccept/Accept reply | vote/QC evidence |
| depends-on/precedes | log prefix/order | dependency set/sequence | parent relation |
| decides | committed entry | committed instance | committed block |
| applies | state-machine apply | command execution | block execution |

Raft log conflict、EPaxos fast/slow path、HotStuff lock/QC shape 等进入可选 Extended PSS。Family Pack
可以复用这些扩展映射、Oracle 和历史风险模式，但不能替代 Core IR，也不能让通用统计器按 family
分支。Core/Extended 状态发现和覆盖结果分别报告；某协议不存在的扩展不能算作未覆盖。

### 8.4 映射可信边界

Mapping 可以读取协议字段，但不能自行宣布等价、覆盖或缺陷。可信验证至少包含：正常路径 fixture、
ID/epoch/value 重命名 metamorphic test、边界状态差分、已知语义 mutant 和重放稳定性。不能稳定观察
的实体/关系不进入 Core key，并在 capability 中显式降级。已有形式化模型可生成额外校准见证，但
不是所有新系统的强制输入。

---

## 9. Profile、测试义务与覆盖分母

### 9.1 Profile 固定测试宇宙

Profile 至少包括：

```yaml
cluster:
  configurations: [minimal-quorum, minimal-quorum-plus-one]

bounds:
  epochs: 3
  proposals: 2
  log_depth: 3
  crashes: 1
  restarts: 1
  partitions: 1

network:
  pre_gst: arbitrary
  post_gst_delay: bounded
  duplication: false
  corruption: false

storage:
  crash_modes: [power-loss]
  visible_write: true
  durable_sync: true

bft_actions:
  - leader_equivocation
  - double_vote
```

Profile 必须带唯一 ID、语义版本、创建证据和变更记录。

### 9.2 机械产生的覆盖原子

PSS + Profile 编译器产生五类原子：

1. `Transition`：协议语义状态转换；
2. `Ordering`：有依赖的二元/三元操作次序；
3. `Fault`：语义故障与恢复切点；
4. `Boundary`：quorum、epoch、log/config、GST、故障数量边界；
5. `Property`：Oracle 前置条件实际被激活。

### 9.3 ESOT/测试义务的位置

ESOT 作为补充层，用于表达：

- 历史 bug 模式；
- 协议专家特别关心的集成错误；
- 较难由 PSS 机械产生的故障切点；
- 安全/活性属性的复杂触发条件；
- 跨多个 phase 的高风险序列。

每个义务必须被拆解或映射到一个或多个覆盖原子，避免用粒度差异巨大的自然语言条目直接等权计分。

### 9.4 结构化 Coverage Obligation

正式分母不能只依赖自由文本或单个 `witness_label`。每个 Coverage Obligation 至少包含：

```text
identity:     ID, category, description, risk, weight
reach:        必须到达的状态/轨迹前置条件
observe:      必须真实出现的事件和 observation 证据
ordering:     关键 before/after 或二元、三元约束
activation:   必须执行的 Oracle/monitor
requires:     Driver capability
status:       Supported / Unsupported
```

Agent 可以选择 obligation ID 并生成使其成立的计划，但不能提供 `covered=true`。Evidence Matcher 必须从 Runtime Trace、可信 observation、Oracle、Replay 和 Conformance 中重建强证据，并将 trace digest、步骤和 artifact 引用写入 Ledger。

### 9.5 Coverage Ledger 与状态管理

- `Uncovered`：尚无有效尝试或证据；
- `Attempted`：Agent 已以它为目标，但强证据不完整；
- `Covered`：至少一个执行具有 Reach + Observe + Check + Replay + Conform；
- `Unsupported`：能力不足，仍在分母但不再作为可调度 Coverage Debt；
- `Invalid`：Profile/Binding/计划/证据未通过机械验收，不得写入 Ledger；
- 新发现的协议义务只能进入下一版 Contract，不修改当次分母。

Ledger 跨多条测试轨迹累积覆盖，保存每项的首次强 witness、最近失败证据、尝试次数和 Profile digest。Coverage Debt 是 Ledger 对 Planner Agent 提供的只读视图，默认按风险和历史尝试成本排序。

---

## 10. 场景、调度与严格重放

### 10.1 Plan 与 Decision Log 分离

场景规划器输出高层约束：

```yaml
targets:
  - crash_after_vote_persist

constraints:
  - node_2_becomes_candidate
  - node_2_vote_is_durable
  - crash_before_vote_message_delivery
  - restart_with_old_messages_pending
```

Runtime 将约束实例化成具体事件 ID、消息 ID、timeout 样本和故障位置，并写入 Decision Log。

### 10.2 具体动作

第一阶段通用 DSL 至少支持：

- inject campaign/propose/client input；
- advance logical time / fire timeout；
- deliver/drop/duplicate message；
- partition/heal；
- crash/restart；
- execute visible write/sync/release/apply/ack；
- wait for semantic predicate；
- assert enabled/pending/observation；
- checkpoint and replay。

### 10.3 严格选择

消息和事件 selector 至少可包含：

- event/message ID；
- source/target；
- sender epoch；
- per-link sequence；
- type hint；
- payload digest；
- causation ID；
- batch/group token；
- semantic metadata。

正式 replay 必须逐字段校验。找不到目标时立即失败，不允许根据当前队列位置自动改选。

### 10.4 确定性要求

相同的：

```text
SUT build + Driver + PSS + Profile + TestSpec + Decision Log
```

必须产生相同的：

- 原始事件序列；
- 消息 payload digest；
- before/after observation；
- Oracle 结果；
- execution fingerprint。

语义 canonical key 相同不等于 byte-for-byte replay 相同，两者必须分别报告。

---

## 11. 偏序场景与规范化

### 11.1 三层轨迹

#### L0：Raw Execution Trace

保留完整事件、字节、状态证据、ID、时间和决策，用于重放和诊断。

#### L1：Structural Trace

只做确定安全的结构变换，例如按首次出现重命名节点、事件 ID 规范化，不做协议推断。

#### L2：Semantic Causal Graph

依据已确认 PSS 将实现事件抽象成语义事件，并建立：

- 节点程序顺序；
- produced/released/delivered；
- persist/sync/visibility；
- timeout 因果；
- crash/restart；
- commit/apply；
- 明确的 must-happen-before。

### 11.2 规范化键

```text
K(trace) = Canon(Graph(alpha(trace)), Symmetry, Independence)
```

其中：

- `alpha` 只能使用已确认 PSS 映射；
- `Symmetry` 处理节点、值和 epoch 重命名；
- `Independence` 只包含保守证明或经验证的交换关系；
- `Canon` 输出稳定图表示和哈希。

### 11.3 独立关系的准入条件

某对事件只有在满足以下条件后才能声明独立：

- 不访问同一节点的相关状态；
- 不改变彼此的 enabled 集合；
- 不存在消息、持久化、quorum、配置或 timeout 因果；
- 交换次序后的抽象状态和 observation 等价；
- 通过交换一致性测试；
- 规则由人工或形式化证据确认。

Agent 提出的独立关系默认为 `candidate`。

---

## 12. 测试生成与探索队列

### 12.1 系统化队列

用于正式覆盖分数的主要证据：

- DPOR；
- SAMC 风格语义约简；
- 有界故障调度；
- 稳定代表执行；
- 保守独立关系。

### 12.2 组合覆盖队列

当完整偏序探索过大时使用：

- ordered pairwise；
- 高风险 ordered triples；
- quorum/epoch/log 边界组合；
- 语义故障切点组合。

### 12.3 探索队列

Agent 或 greybox fuzzer依据：

- 未覆盖原子；
- 新 causal graph；
- 新抽象状态；
- 历史 bug pattern；
- 热状态和长期无进展状态；
- 接近激活的 Oracle；
- Driver/PSS conformance 异常。

探索新颖度用于发现候选测试，不直接作为固定覆盖分母。

### 12.4 最小化

反例最小化必须保持：

- Oracle 仍失败；
- replay 仍稳定；
- 必要因果边仍存在；
- payload 和持久化证据未被错误替换；
- semantic key 的变化被显式报告。

最小化顺序建议为：删除无关动作、缩短时间、减少故障、删除消息副本、缩减节点/epoch/value，再做因果图级归约。

### 12.5 Agentic Model Checking 搜索内核

在现有 Test Plan/Campaign 之下增加一个可选的有限状态—动作工作队列：

```text
WorkItem = <StateRef, ActionRef, PathMetadata>
EnabledActionSet = KernelEnumerate(ExactState)
Next = SearchPolicy.Rank(WorkItems)
```

严格边界如下：

- Runtime 产生稳定 `ActionRef`、验证 enabled 并执行；
- replay/checkpoint 只能恢复 exact state，不能用 PSS 键构造实现状态；
- Random、DFS、Bounded DPOR 和 Agent 共享同一个 WorkItem/ActionRef 边界；
- Agent 只返回 ActionRef 排序和有界理由，不提交任意代码或原始消息；
- 未能证明完整枚举受控边界内所有非确定动作、也未能证明剪枝保守时，
  结果称为“Agent-guided implementation-level state-space exploration”，
  不声称完整实现级 model checking。

产品默认运行可以混合 baseline 与 Agent 配额以降低盲区；正式方法对比必须将
Random/DFS/DPOR/宏观 Agent/微观 Agent/完整闭环分成独立等预算实验组，不用混合配额
自证 Agent 优势。

---

## 13. Oracle 设计

### 13.1 协议无关 Oracle

通用层可可靠检查：

- trace/Decision Log 完整性；
- event ID 与依赖一致性；
- 未 release 的消息不得 deliver；
- 同一消息副本最多 terminal 一次；
- duplicate lineage 有效；
- durable 前置关系；
- crash 后 volatile batch 被取消；
- restart 没有读取不允许的 volatile state；
- apply 不得先于已声明的 commit/release barrier；
- replay fingerprint 稳定性。

### 13.2 Family/Protocol Oracle

以下属性不能完全协议无关：

- Raft 的多数匹配和当前任期提交；
- Paxos chosen value；
- HotStuff QC 链、lock rule 和 three-chain；
- Tendermint prevote/precommit/lock/validValue；
- 成员变更期间 quorum 解释。

这些 Oracle 由可选 Extended PSS/Family Pack 提供骨架，并从同一冻结 Core/Extended Mapping 读取证据。

### 13.3 安全性

优先实现：

- agreement；
- committed prefix/finality；
- vote/lock/accepted 持久性；
- 不冲突的决定证据；
- apply-after-commit；
- crash/restart durability；
- membership transition safety。

### 13.4 活性

后续独立模块检查：

- progress measure；
- temperature；
- abstract state lasso；
- 持续 enabled 但未执行的公平事件；
- GST 后仍不收敛的循环。

有限执行只寻找反例，不声称证明全局活性。

### 13.5 缺陷确认流程

```text
Oracle failure
  -> strict replay
  -> trace integrity/conformance audit
  -> counterexample minimization
  -> official build reproduction
  -> distinguish SUT / Driver / PSS / Oracle bug
  -> human confirmation
```

Agent 分析只能帮助最后几步，不能跳过它们。

---

## 14. 覆盖评分

### 14.1 五维覆盖向量

```text
C = (C_T, C_O, C_F, C_B, C_P)
```

- `C_T`：语义转换覆盖；
- `C_O`：依赖次序与关键三元组覆盖；
- `C_F`：故障/恢复切点覆盖；
- `C_B`：quorum、epoch、日志、配置、GST 等边界覆盖；
- `C_P`：属性前置条件和非空洞检查覆盖。

### 14.2 强覆盖证据

一个原子只有同时满足以下条件才算覆盖：

```text
Reach   到达了定义的语义前置条件
Observe 所需证据完整且可信
Check   对应确定性 Oracle 已执行
Replay  相同输入和 Decision Log 可稳定重放
Conform PSS 映射、Driver 与目标版本通过一致性检查
```

### 14.3 默认综合分数

```text
SCS = 100 × (
    0.25 × C_T +
    0.25 × C_O +
    0.20 × C_F +
    0.15 × C_B +
    0.15 × C_P)
```

默认“高覆盖”门槛：

```text
SCS >= 85
每个核心维度 >= 70%
Capability 支持率 >= 90%
```

SCS 的分母是冻结的结构化 Coverage Obligation，不是运行时发现的 PSS 状态数。义务内部默认等权，显式非零权重只能来自冻结 Profile；类别权重和阈值必须版本化。正式结果必须将 SCS 完整称为“指定有界 Profile 下的强义务覆盖率”。SCS 是测试行为的内部解释指标，不是 Agent 有效性的最终外部判据。

正式报告必须同时展示：

- 总分；
- 五维向量；
- 每类分子/分母；
- Unsupported/Invalid/Candidate；
- Capability 支持率；
- Oracle failure 数量；
- replay/conformance 失败；
- Profile/PSS/Driver/SUT 版本。

### 14.4 防止空洞高分

- Oracle 仅“运行过”不构成属性覆盖，必须激活前置条件；
- 早期 crash 导致后续行为都未发生时，不能覆盖 commit/agreement 原子；
- 缺少 observation 时不得用角色猜测替代；
- Unsupported 保留在分母；
- FAIL 与 PASS 都可覆盖，但必须分开展示；
- 探索次数、代码覆盖和状态数量可作为辅助指标，不能替代 SCS。
- 一个 Agent 计划声明 target 只增加 `Attempted` 次数，不产生任何覆盖分子；
- Profile digest 不同的证据不得写入同一 Ledger；
- 单条测试可以意外覆盖非目标义务，但必须由 Evidence Matcher 从真实轨迹重建证据。

### 14.5 无固定分母的协议状态发现

固定 Profile 覆盖用于评价最终测试结果，但不适合单独衡量搜索器在开放状态空间中的探索能力。系统同时报告基于 PSS 的协议状态发现曲线：

```text
D_m(b) = |在预算 b 内由方法 m 发现的不同 PSS 状态键并集|
```

它用于比较 Random、DPOR、greybox 和 Agent 方法在相同预算下发现根本不同协议状态的速度。该指标没有预先声称完整的状态分母，不得转换为“完成百分比”。

公平评测必须固定 SUT/Driver/PSS/Profile/初始状态/工作负载，定义统一 measurement window，并以包含重复 setup/prepare 的 primary Runtime work 为主预算。scheduler decision、setup work、run、replay、时间、模型 token 和计算成本分别报告。所有方法共有且只执行一次的 bootstrap 前缀可以排除；每个 run 重复执行的 bootstrap/setup 不能免费。PSS 必须消除节点名、绝对 term/index 和 value 等无关重命名，并避免在 persist/sync/emit/apply 等 host microstep 上重复采样。

最终论文报告采用双轴：

- `PSD curve`：搜索方法的发现效率；
- `SCS/Profile coverage`：最终测试套件对冻结义务的完成程度。

Oracle failure 独立报告。状态发现数不能替代偏序路径、故障切点、属性激活和固定覆盖分母。

### 14.6 分数校准

权重和阈值必须通过以下实验校准，而不是主观固定：

- 历史缺陷；
- 隐藏缺陷；
- 高质量语义突变体；
- 训练/校准/隐藏数据隔离；
- 与代码覆盖、随机调度数、状态数的相关性比较；
- 分数与缺陷检出率的单调性和稳定性。

### 14.7 隐藏缺陷外部评价

正式 benchmark 同时使用：

- 可复现的历史真实缺陷版本；
- 修改 SUT 而不修改 Runtime/Driver/Oracle/Profile 的协议级语义 mutants；
- 无缺陷 control 版本，用于计算误报率。

具体缺陷位置、修复 patch、触发测试、root-cause identity 和 mutant 参数对 Agent
隐藏。开发、校准和 holdout 缺陷严格分离；同一根因的多个表象按一个 root cause
统计。主要结果至少报告 root-cause kill rate、time/work-to-first-kill、稳定复现率、
false-positive rate 和总成本。Agent 发现的新官方缺陷只作为额外 case study，不能
替代上述预先冻结的外部评价。

---

## 15. Agent 系统

### 15.1 人工与 Agent 的稳定边界

人工的正常输入缩小为 Minimal Protocol Charter：仓库/实现身份、协议家族或新家族声明、论文/文档/形式化模型、核心安全与活性目标、故障模型和有界测试范围。已有家族由 Agent 生成协议差异、接入和义务草案，并通过机械验证自动冻结；完全新家族仍需设计者一次性确认无法从实现本身推出的核心不变式和环境假设。

冻结后的 Contract/PSS/Profile 是当次运行唯一语义真值边界。Agent 可以为下一版本提出候选事实和义务，但不能修改当前 digest、覆盖分母、等价关系、Oracle 或权重。系统不设置逐事实、逐测试的人工 Gate。

### 15.2 Onboarding Agent（独立后续研究轨）

Onboarding 不作为 A1–A5 搜索闭环的前置条件，也不与 Agent-v2 同阶段扩张。只有统一控制和 Agent 搜索
主问题取得可解释结果后，才用以下边界单独评价自动接入成本。

输入：

```text
repository/source identity
Protocol Knowledge Contract + digest
available build/test tools
previous Binding + deterministic findings + complete witness report
```

输出：

```text
Binding: contract capability/operation ID -> runtime ID
Thin Driver or process/proxy configuration
Executable witness scenarios -> contract obligation IDs
build metadata and generated artifact digests
```

Binding 中不允许 Agent 提供自定义 assertion、`confirmed` 状态或覆盖权重。预期 label、monitor、replay、conformance 和 Oracle 要求全部来自 Contract 与验证器。当前接入 Agent 只生成 Binding/witness 引用；Driver 源码生成在有第二实现需求和独立验收设计前不进入正式路径。

### 15.3 确定性 Coordinator 闭环

Coordinator 不使用 LLM 裁决，按以下状态机运行：

```text
Generate proposal
  -> schema/digest/identity check
  -> compile + capability/operation check
  -> execute every required witness twice
  -> conformance + replay + Oracle + contract-label check
       -> actionable failure: exact findings -> next Agent attempt
       -> runtime limitation: Unsupported -> keep obligation in denominator
       -> all actionable checks pass: emit validated Profile
```

缺失映射、伪造 capability、错误 event kind、路径逃逸、见证失败、非确定重放和 conformance/Oracle 失败均为 actionable，不能产生可运行 Profile。Driver Manifest 明确报告且无法由 Binding 修复的缺失能力为非 actionable Unsupported。

模型进程通过版本化 JSON stdin/stdout 边界运行，不继承父进程的任意凭据。Provider key 仅以受限文件路径传入专用客户端，并且不得进入 prompt、Binding、报告或错误文本。每轮记录 provider/model、API 参数、prompt/request/response digest、token、响应 identity、墙钟时间和机械验证结果。模型输出可以不稳定，但只有确定性验证器接受的 Profile 才能进入运行路径。

### 15.4 Protocol/Hypothesis 与 Explorer Agent

`Protocol/Hypothesis Agent` 读取源码/文档摘要、冻结 Protocol Knowledge、validated capabilities、
历史公开机械反馈和覆盖债务，输出 `TestHypothesis`。假设可以是错误或暂不可达的，但必须引用冻结的
risk/obligation/capability ID，并声明预期语义阶段和可观察结果；自由文本只用于解释。

`Explorer Agent` 读取假设和可信生成的 `SemanticEpisodeView`，其中可以包含当前 prefix 绑定的 Core PSS、
RiskWitness 进度、workload phase、fault usage、候选 WorkItem 和剩余预算。它可以：

- 选择当前 episode 的语义目标；
- 对全局 WorkItem 或可信分桶排序，而不只排列当前 frontier；
- 选择冻结目录中的 DFS、best-first、mutation 等局部搜索算子；
- 请求执行一个当前 enabled ActionID，或提交由可信 concretizer 解析的 selector；
- 根据 `EpisodeReport` 中的不可达、near-miss、PSS/义务变化和 Oracle 激活情况修正下一轮计划。

它不能构造 Action、改变 enabled set、跨过更早 timer、修改 root corpus/总预算、写 PSS/Coverage/Defect
Ledger，或读取 candidate/control 身份、补丁、根因、已知触发轨迹和私有 verdict。Runtime 机械拒绝
非法提议并形成结构化 finding；提议失败是闭环输入，不自动使整个 trial invalid。

模型输出不要求位级确定；正式实验通过冻结模型/提示/tool policy、重复运行和完整成本报告处理波动。
被接受的具体执行仍必须形成 exact trace 并可 fresh Replay。普通公开探索只需保存请求、响应、digest、
token 和执行引用，不再要求每个中间 Agent 思考步骤都具备生产级跨进程事务恢复。

与 Random、DFS 和确定性 semantic best-first 比较时固定完整 Runtime work 预算，同时报告模型调用、
token、墙钟时间和费用。Agent-v1 frontier permutation 保留为权限消融；PSS/义务仅解释搜索行为，
外部缺陷检出、correct-control 误报和 work-to-kill 决定主要结论。

### 15.5 Failure Analyst（可选）

负责语义化解释、提议最小化、生成时间线和辅助区分 SUT/Driver/PSS/Runtime/Oracle 问题；不能确认真实协议缺陷。

### 15.6 旧接入路径退场

Repo/API Scout、Source Catalog、required/acceptable/additional 标签、旧 Integration Pack 及其人工 Gate 已从主仓库删除。如需进行历史对照实验，应从 Git 历史在独立分支恢复，不与正常接入主线并存。

### 15.7 Agent 记忆与评价

长期记忆只存储经验证的 Contract/Binding/Driver identity、Coverage Ledger、机械 finding、稳定反例和版本变更。未通过验证的 Agent 解释不得成为下轮事实。

Onboarding Agent 的主要指标是：从 Contract 到首个 validated Profile 的时间、自动重试次数、目标专用
人工 LOC/人工决策数、见证通过率、错误 Binding/Mapping 拦截率、共享 kit/Core Mapping 复用比例，以及
与专家实现的差分运行结果。正式模型实验必须固定 Contract/prompt/tool-policy digest，记录模型版本、
采样参数、token、工具调用、墙钟时间、失败/重试和费用。holdout 实现是跨实现泛化的必要证据。

---

## 16. 有限 BFT 范围

第一阶段不开放任意拜占庭代码。后续采用受控算子：

- leader equivocation；
- double vote；
- selective withholding；
- forget lock/accepted state；
- 同一身份的两个 Twin 实例。

规则：

- 双投等是注入动作，不自动构成协议缺陷；
- Oracle 检查它是否使正确节点违反安全属性；
- Profile 明确正确/拜占庭身份和最大行为次数；
- 报告称为“有限攻击算子下的覆盖”，不称为任意 BFT 覆盖。

---

## 17. etcd/raft 第一实现路线

### 17.1 定位

etcd/raft 是第一个真实系统，用于验证 Runtime/Driver/PSS 分层，而不是让通用层变成 Raft Harness。

### 17.2 依赖策略

- 使用官方 `go.etcd.io/raft/v3` 固定版本；
- `go.mod` 不通过 `replace` 指向本地修改版；
- build manifest 记录 module version、commit、Go 版本和配置；
- 第一阶段不修改 raft 原始实现；
- 若后续为了自然 timeout 注入 RNG，作为独立 instrumented variant 报告。

### 17.3 Thin Driver 映射

```text
Boot           -> NewRawNode + durable Storage
Campaign       -> RawNode.Campaign
Propose        -> RawNode.Propose
Deliver        -> RawNode.Step
Timeout/Tick   -> RawNode.Tick（仅在能力可重放时）
Poll           -> HasReady + Ready
Host writes    -> HardState / Entries / Snapshot storage operations
Apply          -> CommittedEntries / ConfChange / application state
Acknowledge    -> RawNode.Advance
Inspect        -> Status + storage/application frontier
```

Driver 必须保证一个 `Ready` 只冻结一次，protobuf 消息以确定方式深拷贝，不能在后续 raft 状态变化后再序列化旧引用。

### 17.4 Ready 生命周期

```text
Input
  -> RawNode state transition
  -> freeze Ready batch
  -> visible write
  -> durable barrier
  -> release eligible messages
  -> apply committed entries
  -> Advance
  -> poll next Ready
```

注意：官方 etcd/raft 对 HardState、前一 Ready 的 Entries 和当前 Ready 消息之间有具体发送顺序契约。第一版可以采用更保守的“当前批次完全 durable 后再 release”策略以保证合法性，但这会减少合法并发次序，必须：

- 在 Capability 中说明；
- 不把相应 ordering atoms 宣称为已覆盖；
- 后续根据官方契约实现精确 barrier DAG。

### 17.5 必须支持的第一批场景

1. 显式 campaign 完成选举；
2. 投票消息延迟、丢弃和复制；
3. 分区与恢复；
4. proposal replication 与 commit/apply；
5. Ready 产生后、persist 前 crash；
6. visible write 后、sync 前 crash；
7. sync 后、message release 前 crash；
8. release 后、deliver 前源节点 crash；
9. commit 后、apply 前 crash；
10. restart 只从 durable image 恢复；
11. snapshot 发送成功/失败反馈；
12. 配置变更的基础路径；
13. 相同 Decision Log 的严格重放。

### 17.6 第一版明确不做

- 修改官方 raft RNG；
- 宣称自然选举 timeout 已确定性覆盖；
- DPOR 全量实现；
- 完整成员变更状态空间；
- 磁盘扇区级损坏；
- 性能和吞吐评测；
- BFT 行为；
- 把 MemoryStorage 默认等同于真实 durable disk；
- 仅凭 etcd 内部状态断言跨协议通用性质。

### 17.7 第一版退出条件

- 通用 Runtime 不 import `go.etcd.io/raft/v3`；
- Raft protobuf、Ready、HardState 等只出现在 etcd/raft Driver 和 Raft Family Pack；
- 第二个独立实现无需修改 Runtime；
- 产生、释放、投递、丢弃是独立 trace 事件；
- 至少覆盖一次 persist/sync/release/apply/ack crash cutpoint；
- restart 不复制 volatile 内存状态；
- trace 自包含且两次严格 replay fingerprint 相同；
- agreement、prefix、durability、apply-after-commit Oracle 可运行；
- Capability/Unsupported 能诚实反映自然 timeout 等缺口；
- 官方 raft 源码保持未修改。

---

## 18. 建议仓库结构

```text
cmd/
  adapter-qualify/           Adapter 资格组合入口
  control-experiment/        唯一 v2 实验/Bundle composition
  defect-eval/               control/candidate 评测
  sut-build/                 source-bound 构建

internal/
  control/                   Action/Item/Manifest/opaque envelope
  controlruntime/            enabled、状态机、trace/replay
  controlentropy/            分域随机与 tape
  conformance/               外部见证与 Qualification
  controlexperiment/         admission/workload/policy/ExecutionBundle
  psscore/                   固定 Core PSS IR
  protocolstate/             discovery/aggregate
  semantic/                  decision observation
  oracle/                    TraceIntegrity/Agreement
  defectbench/               最小 v2 evaluator
  sutbuild/                  source/module/binary audit

adapters/
  etcdraftv2/                官方 etcd/raft strict Binding/Mapping
  hashicorpraftv2/           第二实现的部分资格 Binding
  fixture/                   公共契约 fixture

qualifications/              真实 Adapter 资格 composition
benchmarks/                  小型冻结工件与历史 archive
docs/
artifacts/
                             被 Git 忽略的可再生成完整 Bundle/二进制
```

M5.16R 已删除 v1 `Engine/Host/Driver`、Raft Family、Coverage/Campaign、onboarding、旧 Agent
与 migration 可编译源码。历史文档/JSON 可继续引用旧路径，当前生产依赖只允许：

```text
cmd/qualifications -> adapters + trusted internal packages
adapters           -> official SUT + internal/control*
oracle/evaluator   -> generic bundle + semantic observation
internal/control*  -X-> adapters or any specific consensus package
```

---

## 19. 阶段路线图

### M0：方向冻结与核心契约

交付：

- 本总体规划；
- Runtime/Driver/Batch/Message/Capability ADR；
- trace 与 Decision Log schema；
- 代码依赖边界测试。

退出条件：团队能明确回答“哪些逻辑属于 Runtime，哪些属于 Driver，哪些属于 PSS”。

### M1：通用确定性 Runtime

交付：

- 逻辑时间和 enabled event queue；
- produced/released/delivered mailbox；
- strict selectors；
- drop/duplicate/delay/partition/heal；
- host operation DAG；
- crash/restart 和 visible/durable 分层；
- replay fingerprint；
- 通用 trace-integrity Oracle。

### M2：官方 etcd/raft Driver

交付：

- 官方依赖；
- RawNode 薄 Driver；
- Ready staged lifecycle；
- durable restart；
- snapshot/conf change 基础处理；
- etcd/raft capability manifest；
- 第一批确定性场景和 conformance tests。

### M3：Raft PSS、规范化与安全覆盖

交付：

- Raft Family Pack；
- Raft PSS v1；
- 协议无关状态发现账本、PSS 状态键和首见 witness；
- 单轨迹发现曲线与固定 Profile 覆盖分离；
- transition/ordering/fault/boundary/property atoms；
- relative term/log relation normalization；
- agreement/prefix/durability/apply Oracle；
- SCS v1；
- 历史 bug 和突变体初步校准。

达到 M3 后，才可称为“可研究评估的单协议原型”。

### M4：系统化探索与组合覆盖

交付：

- causal graph；
- 保守 independence rules；
- DPOR/SAMC 原型；
- ordered pairwise/triples；
- counterexample minimizer；
- 跨运行 discovery experiment harness、measurement window 和等预算聚合；
- 与随机/chaos 基线比较。

当前进度：已实现显式 measurement window、跨运行 PSS 状态并集、严格 scheduler-decision 总预算，以及可重放 Random/无状态克隆 DFS 基线。尚未完成 DPOR、independence、状态约简、组合覆盖和多 seed 统计，因此 M4 尚未退出。

### M4.5：Protocol Contract 与自动接入验证

交付：

- versioned Protocol Knowledge Contract 与 Binding schema；
- Contract 到 Profile 的确定性编译器；
- capability/operation/identity 交叉一致性；
- 固定 label/monitor、双重 replay、Oracle 和 conformance 见证验证；
- Unsupported 义务保留分母；
- Coordinator finding 反馈环与正反例 fixtures。

当前进度：etcd/raft v1 Contract（12 义务）、Binding、见证、确定编译、机械验证与 Coordinator 接口已落地。当前自动确认 7/9 capabilities 和 10/12 obligations，其余两项保留 Unsupported。DeepSeek V4 Flash 的真实接入实验已在第 2 轮根据机械 witness 报告自动收敛，其输出 Profile 与 checked-in 静态 Binding 基线逐字节等价。

退出条件：不调用 LLM，也能机械拦截伪造 digest/capability、错误 operation、缺失见证、非确定重放与 conformance/Oracle 失败，并且只输出验证过的 Profile。

### M4.6：Coverage Kernel 与测试债务闭环

交付：

- Profile v2 结构化 Coverage Obligation；
- Profile/Driver Manifest digest 固定运行身份；
- 协议无关 Trace Predicate 和 strict ordering Evidence Matcher；
- 跨测试 Coverage Ledger、强证据引用和风险优先 Coverage Debt；
- Test Plan DSL、确定性 concretizer 和 Campaign Coordinator；
- Planner/Scenario/Critic Agent 围绕机械 finding 的自动修复闭环；
- 固定义务覆盖百分比与 PSD 搜索曲线分离报告。

当前进度：Coverage Kernel v2、55 项有界 Raft Campaign 分母、Test Plan DSL、
确定性 concretizer、私有增量 Campaign Session、hash-chained Blackboard 和首个
DeepSeek Planner Campaign 的 Blind 请求、拒绝、执行、反馈与重放闭环均已实现。可信闭环
已成立，但尚未证明模型能够优于简单基线，因此 M4.6 仍未退出。

退出条件：先使用可信专家 etcd/raft Driver，在无人修改 Ledger 的情况下，至少一个 Agent 能根据 Coverage Debt 生成计划、接收机械失败、修复计划并使固定覆盖率真实上升。

### M4.7：外部缺陷 Benchmark 与完整成本

交付：

- 完整 primary execution cost：重复 setup/prepare、Runtime events、measurement
  decisions 和 runs；replay/模型成本单列；
- versioned Defect Benchmark Manifest、digest 和对 Agent 隐藏的 trial identity；
- 历史缺陷、协议级语义 mutant 和正确 control 的统一结果账本；
- 只接受 replay-stable、conformant、可信 Oracle failure 的 defect kill 证据；
- 按独立 root cause 聚合 kill rate，并报告 false positive 与首次检出成本；
- Random、DFS、专家、无反馈 Agent、单 Agent 的同预算 pilot。

第一阶段只实现协议无关 schema/ledger、完整成本和隐藏 fixture mutant 正反例；真实
etcd/raft 历史缺陷与 mutant 集随后独立冻结。此阶段完成前暂停扩展 Scenario/Critic
和通用复合义务语言。

当前进度（2026-08-06）：第一阶段已完成。Campaign report v2 对 fresh SUT、重复
setup/prepare、setup Runtime event 和 measurement event 计入 primary work，并将 replay
开销单列；私有 Defect Manifest、canonical digest、opaque blind trial、预算/身份检查、
可信 Oracle 重算以及 root-cause/false-positive ledger 已落地。fixture control 与两个
同根因 mutant 获得相同的 100 Coverage 分，evaluator 只 kill mutant 且按一个根因
统计，证明内部覆盖分不会直接产生外部 kill credit。M4.9 已补入一个真实 etcd/raft
历史回归 candidate/control；M4.11 又补入一个公开历史语义重构 candidate/control。M4.7
的可信评测基础由此完成，但外部方法效果仍需多个对 Agent 隐藏的独立样本，不能由这些公开
pilot 替代。

退出条件：在 Agent 不接触 defect identity/patch/trigger 的情况下，可信 evaluator
可以机械区分 killed、survived、false-positive 和 invalid trial，并证明内部
Coverage/PSS 分数不会直接写入 defect kill 结论。

### M4.8：候选资格、受控构建与公开校准

交付：

- Candidate 只声明六类 typed requirements；可信 CapabilitySnapshot 分开记录
  controllable input、observable event、Driver capability、trusted monitor、execution
  outcome 和 Profile bound；
- QualificationReport 只通过集合包含关系机械产生 qualified/deferred 和稳定 reason code；
- module path/version、source transformation/digest、command allowlist、binary/toolchain
  identity 完整审计的只读依赖构建；
- 同一 Profile、Driver、Runtime、Test Plan、Replay 和 Oracle 下的正确 control 与公开
  calibration variant；
- opaque trial、Campaign v2、可信 Oracle 重算和 root-cause/false-positive 账本的端到端报告。

阶段关闭时（2026-08-06）：四个官方 etcd/raft 候选已进入公开 Catalog，均由当时的
CapabilitySnapshot 机械判为 deferred；没有为增加样本数扩展 Driver。公开命令数据分歧
calibration 使用相同 96-decision/200-primary-work 上限，correct control 与 calibration
实际都执行 41 decisions、127 primary/127 replay work，并得到相同 21/55、38.67 Coverage。
evaluator 从保存 trace 重算 Agreement 后得到 control-pass/killed，一个 calibration
root cause、零 false positive。该结果只验证评测管线，不进入正式 holdout 结果。

退出条件：构建与评测链能够在不改 module cache、Driver、Runtime、Profile、计划和
Oracle 条件的前提下机械区分公开 calibration/control。后续能力扩展必须在新的阶段由
Candidate requirement、CapabilitySnapshot 和独立 control 重新验收。

### M4.8.1：证据链与资格判定加固

状态：已完成。公开复现实验与负例见 `stage-m4.8.1-trust-chain-hardening.md` 和
`benchmarks/pilots/etcdraft-calibration-v2/`。

M4.8 的公开 calibration 证明功能路径连通，但不将“功能连通”等同于“正式评测
证据链闭合”。在 ReadIndex 和首个官方历史候选之前增加这一强制阶段。

交付：

- canonical persisted trace 身份：Runtime、Campaign、Coverage Ledger 和 evaluator 对同一
  setup + measurement trace 得到相同 digest；
- evaluator 重建并校验 Ledger run witness，拒绝 trace/Ledger/replay/conformance/cost 内部不一致；
- trial submission 绑定 BuildAudit、binary digest、source identity、Driver Manifest 和 Campaign Report，
  Agent 不能用自报 SUT identity 取得正式结果；
- Candidate Catalog 声明官方回归测试所需的完整前置条件，而不是只声明当前缺失项；
- qualification 强制 protocol/family/profile 作用域、canonical set digest 和与编译 Profile
  一致的有向 bound capacity；
- 伪造 protocol/bounds、替换 binary/audit、修改 trace/Ledger/cost 和非目标源文件的
  负例必须机械变为 deferred/invalid。

退出条件：公开 control/calibration 保持 control-pass/killed；同一持久化轨迹只有一个
canonical digest；工件替换负例全部 invalid；错误协议、Family 或伪造 bounds 不能使候选
qualified。

### M4.9：ReadIndex 历史回归复现

状态：已完成。阶段记录见 `stage-m4.9-etcdraft-readindex.md`，最终工件见
`benchmarks/pilots/etcdraft-readindex-v1/`。

交付：

- 受限 `ReadIndex` input、`Ready.ReadStates` typed observation 和独立
  `linearizable-read` monitor；
- Runtime-owned message `capture_message`/`execute_ref` 与只用于 host output 的
  `execute_optional`，没有开放 event ID 或消息注入；
- 未修改 official v3.6.0 candidate（module audit v4）和 `63903dd` 精确修复 control
  （多文件 audit v3）的 source/binary/report identity binding；
- private manifest、opaque trial、trusted rerun 和 root-cause ledger；
- Coverage 浮点累加按 category 排序的确定性修复及回归测试。

实际结果：`63903dd` 是四个公开候选中唯一机械 `qualified` 的样本。冻结计划下 candidate
被 `linearizable-read` kill，control pass；每侧均为 1 run/1 decision、218 primary/218 replay
work、26/55（45.67）Coverage，合计 1/1 root cause killed、0 false positive、0 invalid。
可信 evaluator 首次重跑还捕获 Coverage 浮点最低位造成的 report digest 差异；修复、重建和
再次重跑之后才冻结最终结果。

该交付是公开历史回归复现，不能支持 Random/DFS/专家/Agent 的效果主张：触发计划由 curator
冻结，样本数为一，且当前 RawNode Driver 尚无自然 timer queue。它只解除“尚无真实历史样本”的
管线风险；后续方法比较必须使用多个彼此独立、对 Agent 隐藏的样本。

退出条件：资格、构建、candidate/control、可信重跑与 root-cause 账本均可复现；M4.9 trace
在后续 Runtime 时间模型变化后保持不变。

### M4.10：可验证虚拟时间边界

状态：已完成。阶段记录见 `stage-m4.10-virtual-time.md`。

交付：

- 可选、只声明的通用 `TimerSource`：Driver 声明 timer，Engine 管理 queue；
- 带逻辑时间与 timer queue evidence 的 `clock-advance` trace record；deadline 到达只开放
  scheduler choice，绝不强制执行；
- stable timer ID 的取消、re-arm、重复/陈旧声明拒绝和完整单元测试；
- etcd/raft Driver integration fixture，确认虚拟时钟不会制造未经认证的 native timeout；
- M4.9 frozen candidate 计划的 setup、full execution、measurement fingerprint 回归比较。

实际验证：通用 fixture 证明未到期 timer 不启用、到期后仍须由 scheduler 显式执行、执行后只能
由 Driver remove/re-arm；malformed declaration 被拒绝。官方 etcd/raft v3.6 Driver 未实现
`TimerSource`，`natural-election-timeout-replay` 仍为 Unsupported。用当前 Runtime 重跑 M4.9
冻结计划，三个 fingerprint 分别仍为
`4a70dcbe7b6728dbfa650963bfa797d396d2c217b813bc4a8f1eb6f405855237`、
`59b74e04eec1af04b7d7d98cd0f41c4f958c3be0de82e12c9ec9563208fa71df`、
`69e8ed5889f703974c3c0cb39e2030dae129a06476e4704337ddc35bbf766a2a`；Coverage、成本和
monitor finding 也相同。

该阶段没有实现 etcd 的 `RawNode.Tick`、随机选举 timeout 重放、GST、调度公平性或活性结论。
后续 Driver 只有在原生随机性可注入、完整捕获重放或明确分离 instrumented variant 时，才能
声明对应 timer capability。

退出条件：有 timer 的通用接入与无 timer 的历史 Driver 都可重放；时间进展不能静默执行协议，
也不能污染不使用虚拟时间的冻结 trace。

### M4.11：Ready.MustSync 历史语义重构

状态：已完成。阶段记录见 `stage-m4.11-etcdraft-ready-must-sync.md`，工件见
`benchmarks/pilots/etcdraft-ready-must-sync-v1/`。

交付：

- Profile digest 绑定的 `runtime_profile=etcdraft-ready-must-sync-v1`，只在 opt-in 模式启用
  `conditional-ready-sync` 和 `ready-must-sync-observation`；
- 默认 etcd/raft Driver 保持保守 sync，不暴露 `ready-must-sync` campaign monitor；
- `families/raft.ReadyMustSync` 独立 monitor，从 typed observation 检查
  message-only Ready 且 empty HardState 时 `MustSync=true` 的语义差异；
- 当前 v3.6 module 上一处精确反向源码转换的 digest-bound candidate，以及未修改官方 v3.6
  control；
- private manifest、blind submission、trusted rerun 和 root-cause ledger。

实际结果：`etcdraft-0675f3d-ready-must-sync` 成为本 Profile 下唯一机械 `qualified` 候选。
candidate 在 1 run/1 decision、105 primary/105 replay work 下被 `ready-must-sync` kill；
control 在 100/100 work 下 pass。两侧 Coverage 都是 2/3（75.00），最终为 1/1 semantic root
cause killed、0 false positive、0 invalid。

M4.11.1 可信边界加固：Candidate requirements 不再把 `unchanged HardState` 这种必须由时序
达到的语义前置状态伪装成 controllable input，而是只声明 `read-index` 这一真实输入；
malformed typed monitor evidence 统一成为 invalid/conformance，不得产生 kill credit；
`first_kill_primary_work` 通过 `detection_granularity=plan-end` 明确当前批量 Oracle 的检测边界；
fresh-clone verifier 在固定 Go/toolchain 和 readonly module cache 下重建 binary 并逐字节比对
qualification、Campaign 与 evaluator 工件。加固后 candidate/control 结果保持不变。

边界：这是公开历史语义重构，不是完整历史 checkout 复现；它证明第二条 Ready/持久化策略
评测链闭合，但不能支持 Agent、Coverage 或 PSS 的方法效果结论。M4.9 仍是当前唯一精确
历史回归复现。

退出条件：opt-in Ready monitor 不污染默认 Driver 路径；资格、构建、campaign 和 evaluator
均可复现；静态 capability 不夸大动态可达性；malformed Driver observation 不能产生 SUT kill；
文档明确区分 semantic reconstruction 与 historical reproduction。

### M4.12：Blind Planner Agent v1

状态：开发期闭环完成，阶段记录见 `stage-m4.12-blind-planner-v1.md`；尚未运行模型或形成
方法效果结论。

交付：Planner 只看到 opaque trial ID、Profile identity/node 投影、Driver 声明的受限输入形状、
supported capability 名称、opaque Coverage Debt ref、受限
DSL、预算与机械 finding；不看到 Driver/SUT/build identity、真实 obligation ID、描述、evidence、
monitor、trace、Oracle 文本或 Ledger。可信 `CoordinateBlind` 在 Runtime 前才将 opaque ref 解析
为真实目标，并将私有 Campaign Report 排除在可持久化 Planner transcript 之外。真实 ID/未知 ref
在 Runtime 前拒绝，fixture 已验证拒绝后有效 ref 仍可取得 Ledger 强证据。

边界：opaque ref 不能替代正式 holdout 的私有 manifest/identity 映射，也不能证明模型没有从
公开协议知识或机械反馈中推断语义；M4.9/M4.11 仍仅可作闭环调试。下一步是冻结多条独立、对
Planner 不公开 candidate identity/trigger 的 qualified trial/control，再在共同 decision/token/
clock 预算下比较方法。

### M4.13：Blind Benchmark 预检与 Exposure Audit

状态：工具链完成，阶段记录见 `stage-m4.13-blind-benchmark-preflight.md`；尚未创建正式 private
holdout 或运行模型比较。

交付：private `Manifest` 现在拒绝将 private `variant.id` 复用为 opaque `trial_id`；
`cmd/blind-audit` 重算 `Manifest.Blind()` 并审计 Blind Manifest、Planner request/transcript、
submission 等 public JSON。它拒绝 private variant ID、root cause、category、source/SUT/build digest
和 private kill monitor 的直接 JSON 字符串泄露；输出只保留公共工件 digest 和稳定 finding code，
不回显 private 值。

边界：该审计只检查已枚举 private Manifest 字符串的直接泄露，不能证明模型不能从公开协议知识、
Profile/Capability 名称或交互结果推断语义，也不替代 runner 的进程/文件系统隔离。它是冻结样本
之前的必要 preflight，不是正式方法效果证据。

### M4.14：Blind Trial 的可信 Replay Bundle

状态：运行/复验链接完成，阶段记录见 `stage-m4.14-blind-trial-replay.md`；没有正式 sample 或
模型结果。

交付：Blind Coordinator 在 opaque target ref 被可信解析和执行后，私有保存真实 Test Plan；
public `BlindReport` 不含这些计划。`TrustedReplayBundle` 绑定 Config、scope、Profile/capability
digest 与接受的计划；`cmd/blind-replay` 不调用模型，以相同 Coordinator identity 重建 Campaign，
并适配 trusted evaluator 的统一 flags。正式 submission 的 `plans` 应指向 private bundle，
`binary` 应为同一 candidate/control 构建的 blind-replay binary。

边界：这只保证已产生 Plan 的可重放性，不解决 candidate/control 的选择、Planner feedback 的
信息泄露或多样本方法效果。任何正式 trial 仍须通过 M4.13 exposure audit。

### M5：Onboarding Agent 与 Strategy Agent

交付：

- model-backed `autoonboard.Generator`；
- 仓库读取、Binding/Driver/witness 生成和 finding-driven 自动修复；
- 完整审计日志与构建产物 digest；
- Knowledge/Onboarding Agent 的协议理解与分阶段 Driver/Binding/witness 生成；
- Coverage Planner/Scenario/Search/Critic Agent 复用 M4.6 的计划和验证边界；
- 与人工驱动接入、单次 Agent、无反馈 Agent 的接入成本/成功率实验；
- 与 Random/DFS/DPOR 的等预算状态发现曲线实验。

Failure Analyst 在 M5 后半阶段实现，不作为自动接入闭环的前置条件。

当前进度：第一个 model-backed Generator 已完成 Binding-only 阶段，使用隔离子进程、私有 key 文件、固定 JSON schema、温度 0、关闭 thinking、完整反馈和逐轮 digest/token 审计。Driver/witness 自动接入尚未在第二实现上验证，因此 M5 不退出。

### M6：非 Raft 实现与边际接入验证

先以 OmniPaxos 的 Sequence Paxos + BLE 作为首个非 Raft 目标，验证控制层和 Core PSS 能否跨协议；
其后仍需用 leaderless/dependency-graph 实现（优先 EPaxos）验证更强差异：

- Runtime、Action 和 Core PSS schema 零协议修改；
- 只新增 Execution Binding、Semantic Mapping、fixture 和 composition；
- 共享 Adapter kit 的真实复用比例；
- 目标专用人工 LOC、首次可执行时间和修复轮数；
- Core/Extended PSS 映射错误率及基础指标可比性；
- 缺失持久恢复、clock 或 yield 时能否诚实保持 Unsupported。

### M7：HotStuff/Tendermint、有限 BFT 与活性

在 CFT 路线稳定后加入：

- QC/lock/round PSS；
- Twins 风格有限攻击；
- GST/fairness Profile；
- temperature/lasso 活性检查；
- 独立的 Liveness Coverage 报告。

整体预期：单个 Raft 可研究原型约 4–6 个月；跨 CFT/BFT 家族并完成严谨论文实验，按 9–15 个月规划更现实。

---

## 20. 论文评价计划

### 20.1 核心研究问题

论文主问题只保留三个：

1. **统一共识控制（RQ1）**：不同共识协议能否通过薄 Binding 暴露消息、自然超时、生命周期、
   持久化和输入，而不为每个协议重写 Agent、搜索器、Replay 与 evaluator？
2. **Agent 测试效果（RQ2）**：在相同完整 Runtime work 预算下，协议感知 Agent 是否比 seeded random、
   DFS 和确定性 semantic best-first 检出更多独立隐藏根因，或获得更低 work-to-kill？
3. **多 Agent 必要性（RQ3）**：在相同模型、token、调用和 Runtime work 预算下，Protocol/Hypothesis 与
   Explorer 的角色分解是否比单 Agent 减少无效计划、提高跨协议迁移或提高缺陷检出？

以下问题保留为解释性或后续研究，不再与主系统同时扩张：PSS/义务与缺陷检出的相关性、规范化稳定性、
保守 DPOR、自动接入成本、版本升级稳定性、有限活性测试和 limited-BFT 扩展。

### 20.2 基线

- 随机/chaos scheduler；
- 代码覆盖引导 fuzzing；
- ordered t-way；
- DPOR/SAMC；
- 手工 DSL/Netrix 风格；
- Agent 自由探索；
- 有模型时的 ModelFuzz/Mocket/SandTable。

### 20.3 数据隔离

- 历史缺陷与 Pattern Pack 分开；
- 训练、校准和隐藏突变体分开；
- Agent 不接触隐藏缺陷描述；
- 同一缺陷的多个表象按根因归并；
- Harness/Driver 缺陷单独统计；
- 失败必须在官方构建上重放。

---

## 21. 风险与应对

| 风险                         | 后果                    | 应对                                      |
| ---------------------------- | ----------------------- | ----------------------------------------- |
| Contract/PSS Mapping 错误    | 错误规范化或错误 Oracle | Core IR、版本审查、差分/突变校准          |
| Binding/Adapter 过厚         | 跨协议复用失败          | 共享 kit、依赖门和边际接入账本            |
| Runtime 硬编码 Raft          | 第二实现无法接入        | M6 前持续进行协议 import audit            |
| 消息立即投递                 | 丢失大量调度和故障场景  | 强制 produced/released/delivered 状态机   |
| MemoryStorage 被当作 durable | crash/restart 结果失真  | visible/durable image 分层                |
| 非确定 RNG/时间              | replay 证据不可信       | 注入、记录重放或 Unsupported              |
| selector 按队列位置          | replay 投递错误消息     | 稳定 ID + 严格字段校验                    |
| 错误 independence            | 系统性漏 bug            | 保守规则、交换测试、候选不生效            |
| Agent 动态改分母             | 分数不可比较            | Contract digest 与确定性 Profile 编译     |
| Oracle 未被真正激活          | 空洞高覆盖              | Property activation atoms                 |
| 权重主观                     | 分数缺少科学依据        | 隐藏突变体和历史缺陷校准                  |
| 只在 Raft 家族上有效         | 贡献退化为专用 Harness  | 先以 OmniPaxos、再以 leaderless EPaxos 分层硬验收 |
| 把活性当固定超时             | 大量误报                | GST、公平性、temperature/lasso 独立模块   |
| Agent 误报被当成缺陷         | 结果不可信              | Oracle、重放、最小化、官方构建、人工确认  |
| 自定义指标循环论证           | 系统复杂但缺少外部价值  | 隐藏历史缺陷/mutant 主评价，义务/PSS 只作解释 |
| prepare/setup 被当作免费     | Agent 刷分且预算不公平  | 统计并限制完整 primary Runtime work       |
| 为架构完整盲目增加 Agent/DSL | 工程复杂度掩盖负结果    | holdout 收益门槛和显式停止/删除条件       |

---

## 22. 每次设计/代码评审的偏航检查表

加入重要功能前必须回答：

- [ ] 这段逻辑属于 Runtime、Driver、PSS、Oracle 还是 Agent？
- [ ] 通用层是否新增了 Raft/HotStuff/Tendermint 专有类型或分支？
- [ ] 新系统是否要复制调度、网络、持久化或覆盖代码？若是，抽象层可能错误。
- [ ] 消息是否先冻结完整内容，再经历 release，最后才 deliver/drop？
- [ ] crash 后是否错误保留了 volatile output 或丢掉了已 release 消息？
- [ ] restart 是否只读取 Profile 允许的 durable 状态？
- [ ] 新的随机性和时间是否进入 Decision Log？
- [ ] selector 找不到目标时是否严格失败？
- [ ] trace 是否自包含，能否脱离进程内对象重放？
- [ ] 新 observation 是否有明确语义和实现证据？
- [ ] Oracle 是否独立于 Agent？
- [ ] 覆盖原子是否在运行前冻结？
- [ ] Unsupported 是否被诚实保留？
- [ ] 新规范化规则是否可能合并不等价执行？
- [ ] 是否有第二独立实现测试证明通用性？
- [ ] 报告是否同时给出分数向量、能力和失败，而非单一数字？
- [ ] 主要结论是否来自 Agent 看不到的外部缺陷，而不是它正在优化的 Coverage/PSS？
- [ ] setup/prepare/drain 是否进入完整执行成本，而非被当作免费前缀？
- [ ] 新增抽象或 Agent 角色是否有预先声明的 holdout 收益与删除条件？

任一关键项不能回答时，功能不应直接进入正式评测路径。

---

## 23. 当前立即执行顺序

截至 2026-08-08 的落地状态：

1. [X] 定义 Protocol Knowledge Contract v1、严格 Go 模型、canonical digest 和 JSON schema。
2. [X] 实现 Contract 到 Profile 的确定性编译；实现支持状态不影响 atom 分母。
3. [X] 定义 Agent Binding v1；Agent 只能映射 capability/operation 和引用 witness，不能提供自定义验收断言。
4. [X] 实现自动验证器：digest/identity、Manifest、operation kind、contract label/monitor、双重 replay、conformance 和 Oracle。
5. [X] 实现确定性 Coordinator/Generator 接口和 finding 反馈环。
6. [X] 为 etcd/raft 编写 12 项 Contract 和实现 Binding；机械确认 7/9 capabilities、10/12 obligations，两个 Unsupported atom 仍保留分母。
7. [X] 添加伪造 Contract digest、虚构 runtime capability 和缺失 witness 负例，证明 Agent 无法绕过机械验收。
8. [X] 将 `make run-raft` 和 Explorer 的正常路径切换到自动验证产生的 Profile。
9. [X] 实现第一个 model-backed `autoonboard.Generator`，先只生成 Binding/witness；DeepSeek V4 Flash 在真实 API smoke test 中经两轮机械反馈通过，产物与静态基线 Profile 一致。
11. [X] 实现 Coverage Kernel v2：结构化 Coverage Obligation、Profile/Manifest digest、Evidence Matcher、跨运行 Coverage Ledger、风险优先 Coverage Debt、私有可信状态和可下钻强证据；现有 etcd/raft 基线已迁移且仍为 10/12、85.42。
12. [X] 由 Raft Family Pack + 有界 Campaign Spec 机械展开 55 项 etcd/raft 转换、顺序、故障切点、边界和属性激活分母；12 项 Profile 仅保留为接入质量指标。Family matcher 使用冻结角色/日志/分区证据，当前 53 项 Supported、2 项 Unsupported，单场景审计基线为 25/55、44.41。
13. [X] 定义受限 Test Plan DSL、Profile digest/全局预算约束和确定性 concretizer；实现多运行 Campaign，使手写计划围绕 Coverage Debt 调用 Random/DFS，每条 decision log 强制重放后由 Oracle/Conformance/Evidence Matcher 自动更新 Ledger。etcd/raft 专家基线为 39/55、66.38，剩余 14 项可行动债务，33 runs/1624 decisions 全部重放稳定且无执行错误/Oracle 违规。
14. [X] 实现私有增量 Campaign Session、hash-chained Agent Blackboard 与确定性 Coordinator，加入有界重试、run/decision/token/no-progress 预算、ID/协议因果结构重复检测和只追加审计；脚本化负例证明 Agent 不能直接写 Ledger 或绕过拒绝路径。
15. [X] 实现 M4.7 外部缺陷评价基础：Campaign report v2 记录完整 setup/prepare Runtime 成本并分离 replay；Defect Benchmark 具备私有 Manifest/digest、opaque trial、预算与 SUT identity 检查、可信 Oracle 重算、root-cause/false-positive 账本。隐藏 fixture control/mutant 在相同 100 Coverage 下得到 control-pass/killed，两个同根因 mutant 只计一个 root cause。
16. [X] 实现 M4.8 候选资格与公开 calibration pilot：Candidate 无人工状态，六类 CapabilitySnapshot 做纯集合判断；四个官方候选全部 deferred。离线只读构建与 digest-bound SUT identity 完成，正确 control/calibration 在相同 127 primary work、相同 38.67 Coverage 下得到 control-pass/killed，零 false positive；该结果不计正式 holdout。
17. [X] 完成 M4.8.1：统一 persisted trace digest；evaluator 重建 Ledger witness 并亲自重跑 digest-bound binary；BuildAudit 绑定完整 module tree、binary/source/report；qualification 强制 protocol/family/Profile bounds；公开 v2 calibration 保持 control-pass/killed，工件与报告替换负例均被拒绝。
18. [X] 按官方 `9b9d6ee + 63903dd` 回归场景补全 Candidate requirements，实现 ReadIndex input/read-state observation 和检查 stale-leader read-index 下界的独立 monitor；`63903dd` 已机械 qualified，digest-bound candidate/control 由 trusted evaluator 区分为 killed/control-pass。
19. [X] 实现 M4.10 可验证虚拟时间/timer queue 边界：可选 TimerSource、Engine-owned queue、显式 `clock-advance` trace、到期仅 enable、不合法声明拒绝和无 timer 历史 trace 兼容。etcd/raft 仍未声明自然 timeout capability。
20. [X] 按官方 `0675f3d` Ready.MustSync 语义构造 digest-bound semantic reconstruction candidate/control，实现 opt-in conditional-ready-sync、Ready.MustSync observation、独立 monitor 和 trusted evaluator 闭环；明确它不是完整历史 checkout 复现。
21. [ ] private Manifest → Blind Manifest → exposure audit → trusted replay bundle 的预检/复验工具链已完成；下一步在仓库外冻结一批彼此独立、对 Agent 不暴露的 qualified etcd/raft 历史差异样本和正确 controls。当前公开 M4.9 回归与 M4.11 语义重构只作管线/评测链验证，不能进入方法比较；样本就绪并通过 audit 后运行 Random、DFS、专家与 Blind Planner，检查 Coverage/PSS 与缺陷检出的关系。
22. [ ] 只有在正式 pilot 暴露具体漏检根因后，增加少量可由现有 Matcher 扩展支持的复合时序义务；暂不开放任意 LTL 或笛卡尔义务生成。
23. [ ] 若单 Planner 在 holdout 缺陷上的主要失败来自角色混合，再依次接入 Scenario 和 Critic Agent；若失败来自完整计划频繁不可达，先增加 `StateRef/ActionRef/EnabledActionSet` 微观排序实验，并与宏观 Agent、无反馈 Agent 做独立消融。
24. [ ] 增加统一 Campaign/Benchmark 报告：外部 root-cause kill 为主结果，义务覆盖、PSS、Oracle、完整成本和方法对比独立展示。
25. [ ] 将自动接入拆成 Driver-only fixtures 闭环和固定 Driver 后的 Binding/witness 闭环，加入隐藏专家 Driver 差分行为见证。
26. [ ] 接入 Knowledge/Obligation Agent；同家族协议尽量自动冻结差异 Contract/Profile，新家族只保留一次核心语义确认。
27. [ ] 在一个开发期未见的非 Raft 实现上验证 Core PSS IR、薄 Binding/Mapping、自动接入和测试闭环成功率。
28. [ ] 完成 DPOR/causal graph、多 seed、holdout 缺陷和完整 Agent 消融实验。
29. [X] 完成 Control Runtime v2 M5.1—M5.2.4：协议无关 Action/Item、自然时间、分域 entropy、
    官方 etcd/raft N 节点消息、durable lifecycle、opaque proposal、application durable image、
    committed/rejected client result、三个声明能力的外部 Conformance 和 strict replay；公共核心
    不 import Raft 或 v1 Runtime。
30. [X] M5.2.5 完成第一轮 v1/v2 冻结对照：正常提交、消息 drop/duplicate 后提交、committed
    follower crash/restart 三项 passed、零 mismatch；自然换主因 v1 不支持自然 timeout replay
    deferred，suite 为 `qualified=false`。`go list` 删除门确认 PSS/Coverage/Agent/benchmark 仍直接
    消费 v1，因此本轮不删除 legacy control plane，并已冻结保留/迁移清单。
31. [X] M5.3 提供 Adapter 模板、Manifest/Profile/Report schema、枚举 Unsupported 和机械
    Qualification；etcd/raft v2 对公共 Profile 得到 8/8 required validated，optional process
    isolation 保持 Unsupported，fresh bundle 可按 digest 复验。
32. [X] M5.4 已完成 HashiCorp Raft 真实 RPC、deliver/drop、opaque invoke/FSM.Apply、store
    保留、crash/restart、incarnation 和部分 Qualification；相同 released-message lifecycle intent
    已在两个官方实现上通过。当前 HashiCorp 在 `portable-cft-control-v2` 下 3 项
    validated、6 项 Unsupported，仍为 `qualified=false`。M5.4d 已用 module/source-bound 机械审计
    冻结 clock、entropy 和 strict replay Unsupported；M5.4e 从两个 fresh QualificationReport 冻结
    双实现能力矩阵，共同 validated 交集为 3 项，并将旧探针降为 test-only，生产 Go 净减
    191 行。本项表示“第二实现实验已收口”，不表示原定完整 strict replay 退出条件已满足。
33. [X] M5.5a 将通用场景表面与确定性测试保证拆开：external input、message、lifecycle、
    temporal 和 durability 分别记录 presence、declared control 与 validated control；stable yield、
    pure enabled、strict replay、audited entropy 和 process isolation 独立记录。presence 不产生
    control credit，声明不能自行取得 scheduler-owned。机械报告显示两个 Raft 的 5/5 表面
    都存在；HashiCorp 为 3/5 validated scheduler control 和 0/4 required deterministic guarantee。
34. [X] M5.5b 实现最小 Level-1 黑盒 Target Envelope：独立进程、显式 readiness endpoint、
    数据目录和 opaque 客户端调用。真实子进程已验证 freeze/deliver/drop、kill/restart、
    incarnation、数据保留与 pending call 跨重启。该边界仅为 observable/interceptable，不宣称
    peer message control 或 strict replay。
35. [X] M5.5c 已用两个真实独立子进程验证 connection-level gateway：开放时 opaque byte forwarding、
    partition 时拒绝新连接且接收端状态不变、heal 后恢复；冻结计数为 accepted=3、forwarded=2、
    rejected=1，连续 20 次复验通过。该能力要求目标可配置 peer endpoint，只取得连接级控制，不识别
    framing/message ID，也不提供 strict replay。通用黑盒网络机制在此停止扩张。
36. [X] M5.6a 没有新增 backend selector，而是在唯一 `control.Adapter` 上加入 Runtime-owned Action
    actuation 接缝。官方 etcd/raft mailbox 与 test-only Gateway wrapper 对 `Partition([n1],[n2])` 和
    `Heal` 生成逐字段完全相同的 Action；Gateway 路径的一次选择同时更新 Runtime 与外部 gate，连续
    20 次通过；失败负例不会提交 Runtime partition。生产 Go 净增 31 行，没有新增 Action/Profile/Schema；该结果只证明分发路径可行，
    不构成生产黑盒接入或 scheduler-owned 资格。
37. [X] M5.6b 已将 private partition JSON 提升为公共 typed parameters，并相对于显式 node/directed
    link inventory 确定性解析 crossing gateways。三节点 cut、组内链路排除、左右对称、稳定 ID、伪造
    参数、重复 ID/edge/gateway、未知节点和无 crossing link 负例均通过；删除重复 Runtime 逻辑后生产
    Go 净增 145 行，没有新增 Action/Profile/Schema/backend。完整性只相对于输入 inventory，尚无原子
    多 Gateway actuation、重叠 partition 引用计数或正式资格。
38. [X] M5.6c 已增加 Runtime-owned Action 选择期 eligibility、重叠 partition 引用计数和
    多 Gateway 失败回滚。可控负例分别验证 partition/heal 中途失败；三个独立子进程与两条
    Unix Gateway 又验证实际流量、共享引用与最后恢复，阶段审计连续 20 次通过。生产 Go
    净增 165 行，未新增 Action/Profile/Schema/CLI/backend selector。该回滚仅保证 controller gate
    state；多 gate 依然顺序切换，不能恢复已关闭 connection，因此不提升 scheduler-owned message 资格。
39. [X] M5.6d 已保留五个既有 surface，在 ControlGrade 中插入 `scheduler-actuated`，并让
    grade 只由 witness facts 推导。三路径冻结矩阵机械记录：etcd/raft 为 message/scheduler-owned/
    stable-item/strict-replay；HashiCorp Raft 为 message/scheduler-owned/stable-item/non-strict-replay；
    Gateway 为 connection/scheduler-actuated/controller-atomic/non-external-atomic/non-strict-replay。连续
    20 次复验稳定，生产 Go 净增恰好 100 行；Runtime、Adapter、Action、Profile、Gateway 功能零变化。
    Gateway 仍只是 path witness，未获得完整 Adapter Qualification。
40. [X] M5.7 已冻结接入与 PSS 收敛目标：新系统只增加 Execution Binding 与 Semantic Mapping；
    完整 Adapter 生命周期由共享 kit 复用；PSS 统一映射到固定 Core IR，Family/协议细节退到可选
    Extended PSS。本项只有文档决策，没有实现、迁移或新 capability 结果。
41. [X] M5.7a 已实现 Core PSS IR + etcd/raft Mapping，并盘点两个现有 Adapter 的重复职责。可信
    Runtime 直接投影 lifecycle、partition、pending item 与最早 temporal frontier；公开 Evidence
    只映射相对 epoch/decision 和封闭 participant mode，聚合 ApplicationDigest 不冒充单项 Value。真实三节点自然选主、重复投影与 native
    identity/absolute time/payload 变形测试通过，Runtime/Action/Adapter schema 零修改。两个 Adapter
    的 Check、yield、Evidence 与 entropy 语义差异明显，故没有创建共享 kit；旧 PSS 与 Core 指标分版。
42. [X] M5.8a 已对固定 commit 的 `efficient/epaxos` 完成接入前 feasibility。生产包构建和三节点
    单命令 smoke 成功，外部输入、peer TCP 与语义字段表面存在；stable message item/yield 缺失，
    clock、持久恢复和 strict replay 保持 Unsupported。冻结报告只机械推导 blocker/下一步，不把
    人工源码审阅伪装为 Qualification；Action、Runtime、Core PSS 与生产依赖零修改。
43. [X] M5.8b 已用官方 `SendMsg` 与 `epaxosproto.Commit` 完成 test-only message-port worker witness。
    55 字节帧为一次底层 Write，5,139 字节帧为两次，机械否证 chunk-as-message；codec-aware assembler
    恢复完整原始 bytes 和稳定 ID，release 5,139/drop 0 字节，race 与冻结结果复验通过。新增生产 Go
    为 0、test-only Go 212 行。该结果只授权减负后的最小 Binding，Runtime integration、完整 Adapter、
    Qualification、yield、clock、restart 与 strict replay 均未获得。
44. [X] M5.9a 已重算并冻结 28 条 legacy execution 生产 import 边，默认 `make test` 禁止 consumer
    集合回流。两个实现专用 qualification CLI 从 92 行合并为 60 行统一入口，并删除一条死赋值，
    生产 Go 净减 33 行；两个原 Make target 和资格工件字节保持。当前没有整包达到删除资格。
45. [X] M5.9b 已将单轨迹 `protocolstate.Discover` 改为通用 `step/key/state` sample。Raft v1 在
    Family 层保持 step 1/4/5 的精确曲线；真实三节点 etcd/raft v2 Core PSS 直接复用账本，得到
    3 samples/2 states。`internal/protocolstate` 生产代码仍为 201 行，Action/Runtime/Agent/Coverage/
    schema 零修改，28 条 legacy execution 边不变。
46. [X] M5.9c 已实现 100 行协议无关 `OnlineSampler`，在初始 step 0 和每个已应用
    v2 Action 后机械校验并配对 Snapshot/Evidence。真实三节点 etcd/raft 自然选举与
    follower crash/restart 得到 29 decisions/30 samples/20 states；严格 Runtime replay 和全新
    Runtime sample replay 逐值一致。legacy experiment 原样隔离，Runtime/Action/Agent/Coverage 零修改。
47. [X] M5.9d 已将跨运行 Aggregate 泛化为 shared initial sample + measured decision/optional
    sample；未采样 decision 仍计费。v1 compatibility 保持旧 discovery 子树 SHA-256。两个
    etcd/raft v2 run 各 32 decisions/23 states，union 45、prefix area 1454，两条 trace 严格 replay。
    根 Aggregate 117 行 + compatibility 69 行，相比旧实现生产 Go 净增 45 行。
48. [X] M5.10 已实现最小协议无关 v2 实验执行器和 etcd/raft composition command。两个固定策略
    各执行 32 decisions，并分别以全新 Adapter/Runtime 严格 replay；结果为 23/23 states、union 45、
    prefix area 1454，primary/replay 各 2 setup + 64 decisions = 66 work units。193,818 字节的公开
    `measurement-complete` 报告保存 config/manifest/trace/sample/report identity 和 state witnesses，
    不保存重复的完整 trace/sample body，不输出 Oracle/Coverage pass。通用层 516 行、composition
    root 102 行，并已加入禁止具体协议/legacy import 的 architecture gate。
49. [X] M5.11 已在现有执行器上增加 `uniform-random-policy/v1`：public policy seed、decision 与
    canonical enabled-set digest 经拒绝采样选择 Action，不接触 Runtime entropy。相同 2×32 primary/
    replay 预算下，公开 seed 1 得到 29/26 states、union 53、prefix area 1843，严格 replay 2/2；同
    seed report/trace identity 一致，不同 seed report 不同，而 Runtime config/seed digest 不变。M5.10
    fixed 工件 SHA-256 保持不变。本阶段生产 Go 净增 84 行，没有第二套执行路径或 schema。
50. [X] M5.12 已冻结受限 Planner Proposal → Policy 的机械编译边界。可信 Scope 独占 PSS/Runtime/
    run set/order/budget/replay，Proposal 结构只允许 priority/rules/public seed；strict decoder 拒绝
    unknown/trailing JSON。非法 run set/policy 得到计费的 `proposal-rejected`，运行期不可达 rule 得到
    带 partial work ledger 的 `execution-failed`。0-call deterministic stub 复用唯一 Execute/Replay，
    以 2×32 decisions 得到 union 45、prefix area 1454；M5.10/M5.11 工件逐字节保持不变。本阶段没有
    新建 package、Action、Adapter、执行/Replay 算法、PSS 或 Coverage 路径。
51. [X] M5.13 已在 M5.12 边界上完成一次真实 `deepseek-v4-flash` Planner call。模型只收到 Scope
    digest/run/budget/replay、公共 nodes/Action kinds、目标描述和 Proposal schema；调用为 temperature 0、
    thinking disabled、1,800 max tokens、5 分钟 deadline、无 retry。工件记录 request/response/prompt
    digest、模型/response identity、provider usage 和 duration。Proposal 编译 accepted；run 1 完成 32
    primary + 32 replay，run 2 在 decision 3 的 exact `drop-message/node=n1` rule 不可达，得到计费的
    `execution-failed`：1 call/744 tokens、34 primary decisions、32 replay decisions。没有 PSS summary、
    Coverage/Oracle 输入、免费修复或方法优势结论。
52. [X] M5.14 已完成审计后的减负与实验准入：删除 4 个未使用公共枚举和 1 个不可达
    native Drop 分支；生产 Qualification 统一到 `portable-cft-control-v2`，历史 v1 工件仅由显式
    legacy 入口逐字节复验。新增 digest-bound `ExecutionAdmission`，把所需 capability 子集与完整
    Qualification/Adapter/Implementation/Build/Configuration/Manifest identity 绑定，并在实际 Runtime
    初始化后复核 Manifest。etcd/raft 当前 8/8 required validated；HashiCorp 严格准入被机械拒绝。
53. [X] M5.15 已在现有 v2 Experiment composition root 完成确定性 Workload/Fault envelope，没有
    新建 Campaign 或执行器。Provider 通过 Semantic Mapping 等待唯一 `coordinating` participant，再
    调用 `OfferInvoke`；Policy 仍只从 Runtime enabled set 选择。FaultEnvelope 限制 crash/drop/duplicate/
    partition 总量及 crash/partition 并发数。公开 etcd/raft calibration 以 1 prepare + 96 decisions
    完成 1 次 committed write、applied PSS witness 和 96/96 strict replay；primary/replay 各 98 work units。
54. [X] M5.16 已定义最小 `ExecutionBundle`，绑定完整 Action trace、prepare state transition、
    Evidence/client history、Core PSS、Qualification、Replay 和成本。target-owned
    `DecisionProjector` 将 etcd/raft Evidence 投影为 position/value digest；通用 TraceIntegrity/
    Agreement 不解析 Raft。公开 control/candidate 在共同 96 decisions/98 work 下得到
    `control-pass/killed`，1/1 root cause、0 false positive、0 invalid；PSS/Coverage 不参与 verdict。
55. [X] M5.16R 已在冻结 M5.15/M5.16 identity 后删除 v1 实现锥体：旧
    Engine/Host/Driver、Raft Family、Coverage/Campaign、onboarding、DefectBench、Python Agent、migration
    和对应 CLI 不再进入编译。production/test 由 29,931/12,559 行降为
    14,564/6,299 行，legacy production import edge 由 28 归零。历史文档/JSON 保留为 archive。
56. [X] M5.17a 已在共同 workload/FaultEnvelope/96 decisions/98 work 上实现 action-class random：
    先均匀选 ActionKind，再选该 class 成员；旧 uniform Action random 身份不变。选择器只在
    Runtime enabled 中不超过冻结 envelope 的子集采样，不修改 enabled/ActionID。seed 1 的
    control 发现 83 个 Core PSS states 且严格重放；同一公开 candidate 在此策略下
    `survived`，而 M5.16 fixed 只发现 56 states 却 `killed`。这一负结果已固化，不筛 seed，
    证明 PSS discovery 不能代替根因检出。
57. [X] M5.17b 已实现协议无关 trace mutation operator：精确 source prefix、相邻
    Action swap 和 digest-bound priority suffix。公共规则按 source 顺序取首对相邻
    message deliveries，不尝试多个 pair 后筛选。不可执行引用产生稳定 reason code
    和 partial work，不静默 fallback。source/mutation 各 98/98，方法完整成本为
    196 primary / 196 replay；失败校准的 35 primary 另计。本阶段只有正确
    control，没有 candidate verdict。
58. [X] M5.17bR2 已删除悬空的 M5.10–M5.13 在线兼容路径：旧 fixed/random CLI、Planner
    Proposal/Attempt、DeepSeek transport、`ExecuteLegacy` 和无消费者的 model command runner。历史
    Markdown/JSON 继续冻结，不为重放历史摘要保留失去准入边界的可执行代码；当前 qualified workload、
    bundle、Oracle、evaluator 和两个强基线的身份保持不变。production/test 由 15,136/6,659 行降为
    14,068/6,232 行；`audit-no-retired-experiment` 阻止旧符号回流。
59. [X] M5.17c0 已加固 Experiment 语义。所有策略只面对同一份
    `qualified Runtime enabled -> fault-filtered admissible` frontier；workload 路由从 PSS Mapper
    拆到 target-owned `WorkloadRouter`；pending workload 和 target quiescence 能形成合法、严格可重放的
    运行结果，ExpectedStatus 不再兼任 Oracle。框架错误、trace/qualification/projection 失败仍只能记为
    invalid，不能包装成正常终止。v2 分开记录 runtime/admissible digest 与选中 ActionID，
    fresh replay 重算终止、fault usage 和每次 Invoke 的 Router 唯一目标。etcd/raft 在 96 的上限下
    于 42 decisions 完成并 configured-stop；1-decision 运行保留 1 个 pending/no-candidate。
60. [X] M5.17c1 已完成 corpus 的可信前提。有序 `MutationSourceCorpus` 绑定任意
    qualified source bundle 的 report/config/trace/manifest/PSS/policy/qualification 身份，而不再
    假设 source 必须是单一固定策略；顺序参与 digest。mutation v2 以
    `(ActionID, occurrence)` 消解状态循环中的重复 ID，v1 冻结身份不变。`PSSFeedback`
    必须从 Trace/最终 Snapshot/Evidence 重构并重新调用绑定 Mapper，不接受外部 state key。
    `MethodLedger` 强制 source/proposal/execution/失败的引用关系和完整成本。真实 etcd/raft
    见证为 96 decisions、97 samples/56 states，source+execution 各 98/98，方法总成本
    196 primary / 196 replay；本阶段无 candidate verdict。
61. [X] M5.17c2 已实现最小 batch PSS-guided corpus 和通过相同
    qualification/admissible frontier 的 uniform random。uniform 三个 seeds 在 294/294 ceiling 内完成
    3 条 strict replay bundle，累计 291 samples/238 states。PSS-guided 从共同 seeds 1/2 的可信
    feedback 中选中 source 2、decision 65 的稀有状态附近 mutation，但在 decision 66 因精确
    ActionID 不再 enabled 而失败；方法完整记录 263 primary/196 replay、无 fallback。这一负结果
    证明 PSS 稀有度不包含动作因果/可交换性，因此继续只作为 batch coarse feedback，不用于
    visited-state pruning、Oracle 或综合总分。两方没有 candidate verdict，不做方法优劣结论。
62. [X] M5.18a 已建立最小正式方法评价前提：ExecutionBundle v3 绑定完整
    WorkloadPlan 和可从 Trace/ClientHistory 重建的 invoke/return OperationHistory；
    `MethodSpec/v1` 绑定 strategy、seed、预算、PSS/projector、非 SUT Config projection 和
    evidence schema；manifest 另行绑定 build-audit/binary digest。evaluator 从已验证 binary
    bytes 创建隔离副本并亲自 fresh execution，不接受 submitted bundle 作为 kill authority。
    公开 fixed calibration 在共同 96 decisions/98+98 work 下得到 control-pass/killed、0
    false positive、0 invalid。两侧 OperationHistory 相同，差异由已有 applied-prefix projector +
    Agreement 检出；未在没有实际缺口时增加 durability monitor。该轮仍是单 execution
    公开 calibration，不是多 attempt 方法比较或 private holdout。
63. [ ] M5.18b 实现 `ProtocolKnowledgePack -> AgentSemanticView -> Guarded TestIntent -> deterministic
    compiler/local search`。Agent 读取协议知识和批次级机械反馈，不读取隐藏缺陷身份；hard constraint 与
    preference 分开，preference miss 计费后确定性 fallback。先做单 Agent 和 one-shot/feedback 消融，
    不增加多 Agent。M5.18b0 已完成不调用模型的宏观边界：人工冻结的
    `ProtocolKnowledgePack` 和可信 backend catalog 经真实 Qualification 生成不含 build/candidate/
    root-cause/Oracle 身份的 `AgentSemanticView`；严格 JSON proposal 不允许未知字段、尾随 JSON 或
    提交者自算 digest。compiler 从已准入 backend 中机械选择，hard capability/action/fault/
    budget 不满足直接拒绝，preference miss 记录 compiler work 并使用冻结 fallback。
    etcd/raft 真实见证复用 M5.17a 的 action-class executor 并保持原 report/bundle identity。
    M5.18b1 已增加单次 `deepseek-v4-flash` JSON transport、秘钥文件边界、渐进式
    `AgentInvocationAudit` 和成功/失败离线见证；唯一真实调用进入现有 qualified executor，
    并分开记录 1 call/1779 tokens、3 compiler work、98/98 primary/replay。调用前还修复
    Agent view 缺少 backend `max_fault_envelope` 的可操作性问题。但模型回复精确复制了
    prompt 中的具体有效示例，因此该结果只是 public transport calibration，不是 Agent
    规划证据。活跃 prompt 已改为 fictional 纯结构示例，但本阶段没有第二次调用。
    M5.18b2 已增加 `AgentBatchFeedbackView/v1`：每个 backend 的 MethodObservation 必须用完整
    bundles 和绑定 Mapper 重算，Agent 只看到 common ceiling、完成度、成本和 coarse PSS
    discovery，不看 bundle/trace/build/candidate/root-cause/Oracle/state key。action-class 和 uniform
    使用预先固定的 seeds 1/2/3：前者 2 complete/1 failed、294/196 work、165 states，
    后者 3 complete、294/294 work、238 states。失败 attempt 保留计费且不替换 seed；`238 > 165`
    不是缺陷效果或覆盖完整度结论。后续 M5.18b3 复核发现，v1 中 action-class 使用
    严格 workload failure 语义，uniform 使用 Experiment v2 pending 语义，所以 `2/1` 与
    `3/0` 不可比。历史 identity 保留，但不再用于选择。M5.18b3 已增加
    `AgentBatchFeedbackView/v2` 和 `AgentFollowUpSpec/v1`：两个 source 方法统一
    Experiment v2，均为 3 execution complete、2/3 workload complete 和 294/294 work；分别
    发现 241/238 states，不做效果排名。确定性规则冻结 source seeds 1/2/3 与
    unseen seed 4，source 实际 588/588，per-arm ceiling 686/686。seed 4 通过 strict replay
    但 workload pending 且 hard ActionKind 不完整，以计费的 `execution-failed` 保留，不替换
    backend/seed；实际合计 685/685，0 model calls。尚缺冻结后的 no-feedback/
    feedback 两次真实调用，因此本项保持未完成。
    M5.18b4 调用前插入一个不调用模型的 pre 阶段：历史 b0/b1/b2/b3 identity 不回写，
    新 b4 Agent view 只声明 Experiment 当前能实际产生并交给 backend 选择的 Action；
    在 FaultProvider 实现前关闭 Partition/Heal 与 partition envelope。新执行结果将
    execution validity、intent reachability 和 Oracle outcome 分开；hard ActionKind 未全部出现
    记为 `intent-not-reached`，不再记为 Runtime/replay execution failure。新 compiler plan
    不再写入实际 policy seed，由 digest-bound execution instance 绑定 plan + seed + 单执行预算。
    M5.18b4-pre 已完成：新 catalog/view 在 backend surface 关闭当前无 FaultProvider 的
    Partition/Heal，新 plan v2 不含 seed，execution instance 独立绑定 seed 4 和 98/98
    ceiling。真实 seed-4 轨迹通过 qualified execution 和 strict replay，消耗 97/97 work、
    发现 79 states；因 Trace 缺少 `invoke`，新 `IntentOutcome` 记为 execution `valid`、intent
    `not-reached`、Oracle `not-evaluated`，模型调用为 0。这仅是 public trust-boundary calibration。
    M5.18b4 request-freeze 已完成：两臂共用同一 semantic view、risk、must、
    source seeds 1/2/3、unseen seed 4、98/98 execution ceiling 和 transport 上限。两份
    request 由同一 builder 生成，system prompt 逐字节相同，实验信息差异只是
    `agent_batch_feedback` 为 `null` 或可重算 feedback v2。精确 prompt/request digest、
    byte length、每臂 1 call/0 retry 和完整 source 计费已由
    `AgentPreferenceAblationFreeze/v1` 绑定。新增单份 proposal 对冻结 baseline 的
    机械校验，避免两份回复同时非法修改 hard 字段却通过 pairwise 比较。
    已知 b3/b4-pre outcome 身份与结果词未进入 request；本阶段仍为 0 model calls。
64. [X] M5.18b4R 只重构验证门禁，不修改 Runtime、Adapter、Action、PSS、Oracle、Agent request、
    freeze identity 或实验语义。`cmd/control-experiment` 的全部顶层测试必须由版本化清单机械枚举，
    与 `go test -list` 精确对账并恰好归入一个 race shard；每个 shard 使用独立 test binary、独立
    20 分钟 ceiling 和 `-count=1`。`test-race-core` 只提供开发期并发回归信号，不能替代穷尽清单的
    `test-race-full`。分片按共享 fixture 组织，不为拆分 seed 1 而增加原本不存在的 SUT run。
65. [X] M5.18b4 request consumer 只消费 freeze 已绑定的 prepared request，不重新生成 prompt 或接受
    新 request bytes。单臂依次执行一次 transport、response audit、strict parse、baseline validation、
    plan v2、seed-4 instance、已有 qualified executor 和 IntentOutcome；每个失败阶段保留稳定分类和
    已发生成本，无 retry、回复修复、backend/seed 替换或 hard constraint fallback。
66. [X] M5.18b4 pair orchestration 固定按 no-feedback/with-feedback 各调用一次现有 consumer，单臂
    失败不阻断另一臂。唯一 pair ledger 绑定共同 freeze、两份 invocation audit、可选 instance/outcome
    和每臂完整 source/post-freeze/model 成本；大型 report/bundle 独立持久化，不进入 ledger body。
67. [X] M5.18b4 显式 opt-in pair 入口必须先完成并验证 freeze，再读取用户显式指定的 key
    文件。入口只调用现有 pair consumer；任一 arm 失败时仍必须先把完整 pair ledger
    持久化到全新目录，再向调用方返回失败。不得自动重试、替换 response/backend/seed 或
    覆盖旧工件；增加入口本身不授权真实模型调用。
68. [X] M5.19 Campaign Coordinator v1 的协议无关数据基础先冻结配置、停止与检查点语义。
    wall-clock 只是运维停止上限，不是方法效果的可比预算；权威账本仍使用 primary/replay
    work、decisions、attempts 和 model calls/tokens。terminal attempt 必须引用 artifact digest 并携带
    完整 WorkLedger；checkpoint 以 one-record + cumulative totals + previous digest 构成 O(n) 增量链，
    resume 重新校验 Campaign/config/target/spec identity。当前没有文件 persistence 或执行循环。
69. [X] M5.19a 实现 crash-safe checkpoint 文件布局。只有 artifact 文件 durable 且 digest 对账后
    才能提交 terminal record；恢复读取完整链并拒绝缺项、篡改、identity 漂移和已停止后续写。
    artifact-only 中断点作为 orphan 明确报告但不计入 Campaign，临时文件也不能影响可信 head。
70. [X] M5.19b 增加 digest-bound remaining-budget allowance、协议无关 terminal attempt provider 和
    持有可信恢复会话的最小 Coordinator。provider 不能写 checkpoint/totals/stop reason；普通 error
    不重试，超 allowance 在 artifact 落盘前拒绝。先用 deterministic provider 验证多 attempt/恢复，
    再以同一接口连接现有 qualified executor，不新增 Runtime 或目标专用 Campaign。
71. [X] M5.19c 将现有 etcd/raft qualified executor 包装为第一个真实 Campaign provider，并冻结
    experiment spec、seed/policy/workload 与 artifact schema。provider 必须把任何已发生成本的失败返回为
    terminal result；同时补 durable coordinator failure marker，避免 artifact-less error 跨进程后被
    当作同一 ordinal 重试。marker 只保存 config/head/request 身份和稳定分类，不保存私有
    错误文本；现有 `ExecutionFailure.Work` 必须转成带 artifact 的 terminal failed attempt。接入不能
    复制 Runtime、replay、PSS 或 Oracle 路径，也不在通用包中引入 etcd/raft 类型。
72. [X] M5.19d 先增加协议无关 Campaign summary/reader，只输出已校验 checkpoint 的
    attempt/outcome/work/artifact 索引和停止/失败状态，不复制大型 artifact。然后增加显式 opt-in
    etcd/raft 离线 runner，只组装冻结 spec、Campaign 目录和现有 provider；不在 CLI 内新建执行、
    PSS 或 Oracle 逻辑。Summary 的 `running/stopped/failed` 不能被表述为 pass/fail verdict；reader 只能
    按 committed ordinal 读取内容寻址 artifact，不暴露任意路径或 orphan。
73. [X] M5.20 从 reader 返回的已校验 artifact 机械投影跨 attempt Campaign Observation。设计已冻结：
    target-owned projector 严格复核 artifact/request/spec/target identity，通用层只聚合与 Summary
    attempt 一一绑定的协议无关 projection。第一版分开报告 terminal outcomes/cost、复用
    `protocolstate.Aggregate` 的 Core PSS 并集/发现曲线、fault/workload 统计和 monitor 触发索引；
    不复制大型 bundle/trace，不把各栏拼成自定义综合分数。详见
    `docs/stage-m5.20-campaign-observation.md`。
74. [~] M5.21 冻结 Campaign Feedback/Planner 输入边界。M5.21a 已完成：复用
    `AgentSemanticView`、`CampaignObservation`、`CampaignAttemptRequest` 和
    `GuardedTestIntent`，Planner 只能改 `Prefer`，不能选择 Runtime enabled Action、
    修改 target/spec/monitor/PSS 身份或将指标声称为 verdict。实际 Go 净增 563 行，
    已实现最小视图、确定性 plumbing planner 和既有 compiler 的越权拒绝；全量
    test/vet 与定向 race 通过，未新增 CLI、执行器、持久化 schema 或模型调用。
    M5.20 尚缺每个 attempt 的可信 prior intent/backend 归因；M5.21b 必须由 target-owned
    artifact projector 机械补全该绑定，才能接入增量 checkpoint/恢复/账本。详见
    `docs/stage-m5.21a-campaign-planner-view.md`。
75. [X] M5.21b prior-choice 可信归因已完成。新增的协议无关
    `CampaignExecutionChoice` 只能从既有 intent/compiled plan/execution instance 机械构造；
    etcd/raft target artifact 必须重算该绑定并将 spec+semantic+intent+plan 纳入
    experiment identity。Observation/Planner 只看 choice/intent/plan digest、backend 与 strategy，
    不暴露 seed/instance/trace/witness。实际 Go 净增 408 行，全量 test/vet 与定向
    race 通过；真实 2x8-decision 验收的两个 attempt 已各自将 PSS 增量绑定到
    `admissible-uniform` choice。未新增执行器、CLI、Planner 算法或模型调用。
    详见 `docs/stage-m5.21b-prior-choice-attribution.md`。
76. [X] M5.21c 已将每次 planner view/proposal/compiled plan/execution instance 与 model work 机械绑定到
    exact attempt request 和 durable artifact/checkpoint 恢复链。先使用 zero-model planner 验证中断恢复后
    不重算、不重复收费、不漂移。设计已冻结为 `plans/N.json` no-replace envelope：
    先落盘 plan 再执行，checkpoint record/artifact 均绑定 envelope digest，恢复最多允许一个
    `head+1` pending plan。实际 Go 净增 519 行，未改 Runtime/scheduler/executor，也未接真实模型。
    恢复测试证明 durable plan 存在时 planner 不会再调用；计划替换、篡改、future ordinal 与
    已提交计划缺失均 fail closed。真实 3x8-decision 验收得到 24 decisions、27/27 work 和
    17 个唯一 PSS 状态，三个 checkpoint 均绑定各自 planned-attempt digest。远程调用在
    “已收费但响应未落盘”时存在本地无法消除的歧义，
    后续必须使用 durable call-intent + ambiguous terminal 或服务端幂等键。详见
    `docs/stage-m5.21c-durable-planned-attempt.md`。
77. [X] M5.21d 在首次真实远程 Planner 前增加 durable call-intent 和 ambiguous terminal。
    只有在调用前落盘 exact request/idempotency identity，并在无法证明未收费时拒绝自动重试，
    才允许设置非零 model allowance。d1 已完成通用 intent/dispatch/result 三文件状态机：
    `CampaignConfig/v3` 冻结 planner mode，dispatch-only 恢复必须成为不可重试的 ambiguous terminal，
    且仅原进程私有活跃令牌能提交 result。实际 Go 净增 593 行，全量 test/vet 与通用
    Campaign race 通过；未读取 key 或调用模型。d2 已在 etcd/raft composition 接入 injected
    offline transport：result-before-plan 恢复不重调，dispatch-only/failed 均不重试，模型成本只在
    Campaign artifact/record/totals 附加一次，report/bundle 身份不变。d2 实际 Go 净增 444 行，
    全量 test/vet、定向 race 与 exact-partition audit 通过。该阶段未改 Runtime、target executor 或
    CLI，也未读取 key/访问 HTTP。详见 `docs/stage-m5.21d1-durable-model-call.md` 和
    `docs/stage-m5.21d2-etcdraft-offline-model-planner.md`。
78. [X] M5.21d3 已增加显式 opt-in durable-call runner。每个 attempt 先 no-replace 冻结 exact
    intent，只有 prepared 状态才读 key，单次 Coordinator Step 后立即清空；completed result、
    durable plan、ambiguous 和 failed 恢复均不读 key、不重调。离线 2-attempt 见证得到 2 key reads、
    2 calls/14 tokens；key failure 保留 prepared intent，resume 后唯一调用；三类 terminal 恢复的
    key/transport 都为 0。实际 Go 净增 443 行，全量 test/vet、160.600 秒定向 race 和审计通过。
    pre-plan failed result 的成本虽已 durable，但尚未进入 Summary totals；M5.21d4 必须先补可信
    terminal accounting，不能伪造 plan，也不能据 summary 的 0 model work 声称没有调用。
    详见 `docs/stage-m5.21d3-opt-in-durable-runner.md`。
79. [X] M5.21d4 已关闭 pre-plan terminal accounting gap。`CampaignFailureMarker/v2` 中非空
    work 只能由 store 中同一 next ordinal 的 durable model-call result 机械投影，恢复会重验
    result digest、work 与 allowance-overrun 标志。`CampaignSummary/v2` 将 terminal work 加入
    totals，但不增加 Sequence、Attempts、checkpoint、artifact 或 PSS evidence。离线见证覆盖
    completed、ambiguous、failed、invalid proposal 和 over-budget；五者恢复都是 0 key reads/
    0 transport calls。实际 Go 净增 225 行，全量 test/vet、通用 Campaign race（53.313 秒）、
    runner race（137.293 秒）和审计通过；外部模型调用仍为 0。详见
    `docs/stage-m5.21d4-pre-plan-terminal-accounting.md`。
80. [X] M5.21d5 已执行一次 durable Campaign 外部连通性校准。冻结 official etcd/raft v2、
    1 attempt、8 decisions、seed 151、180000 ms wall ceiling 和 1 call/8192 tokens allowance；
    exact intent durable 后才读 key，实际 1 call 消耗 2417 tokens。proposal 通过可信编译，使用
    action-class-random 提交 1 个 8-decision attempt，primary/replay 均为 9 work units，得到
    9 个 Core PSS states。workload 未在该有界轨迹内送入，monitor 零触发。exact-config 恢复读取
    与保存 Summary/Observation 字节一致，没有 retry 或第二次调用。该阶段只证明真实
    transport 与 trusted pipeline 连通，不作 Agent 效果或方法优势结论。
    详见 `docs/stage-m5.21d5-single-call-connectivity.md`。
81. [X] M5.21e 已执行预注册的 matched Agent/zero-model public pilot。两臂共享 official
    etcd/raft v2、2 attempts、32-decision ceilings 和 seeds 161/162；只将 primary/replay 执行 ceiling
    匹配，Agent 额外 model work 单独报告。Zero-model 完成 2 attempts/64 decisions，得到
    46 states。Agent 第一前缀的 choice/plan/bundle/PSS curve 与基线完全一致；第二次调用
    把 `backend_ids` 错写为 `backend_id/strategy`，被 strict parser 拒绝。Agent 以 1 committed
    attempt、2 calls/5211 tokens 和 durable provider failure 结束，其中未执行的第二次 2780 tokens
    由 result-bound terminal accounting 准确记录。这个负结果说明当前 Agent 未改变首个搜索前缀，
    且下一步必须机械提供精确输出 template/schema，不能放宽 parser 或重试本 pilot。
    详见 `docs/stage-m5.21e-matched-agent-zero-pilot.md`。
82. [X] M5.21f 完成 Planner usefulness gate 第一阶段。可信代码将精确 proposal template、
    `prefer.backend_ids`/`prefer.actions` 数组和当前 allowed/eligible 枚举冻结进 prompt；strict parser
    与 0 retry 保持不变。新确定性自适应基线只读取同一 PlannerView、只修改同一 Prefer：先尝试每个
    backend，之后在零 PSS 新颖度或 workload 持续停滞时轮换。M5.21e frozen view 证明它与 zero-model
    可产生不同 backend 和执行输入。一次新契约下的 2-call public pilot 得到 2/2 valid proposals、
    2 committed attempts、66/66 primary/replay work 和 6027 tokens；Agent 两轮都选择
    `action-class-random`，第二轮与 zero-model 相同、与选择 `admissible-uniform` 的自适应基线不同。
    Pilot 同时发现完整 plan/instance digest 会被当前无执行作用的 `Prefer.Actions` 改变，因而后续
    behavior delta 必须由 trusted effective-execution identity 判定，不能把审计元数据差异计为搜索增益。
    详见 `docs/stage-m5.21f-planner-usefulness-gate.md`。
83. [X] M5.21g 实现协议无关 `CampaignEffectiveExecution/v1` 与首个三方法 proposal-only gate。
    identity 绑定 target/environment、strategy、exact Policy/seed、decisions、FaultEnvelope 和 work budget，
    排除 proposal/compiler/plan/instance 中未进入执行器的元数据。etcd/raft composition 绑定固定 Runtime、
    workload/router、target manifest 与 Policy，并用 M5.21f 归档 Report.Config 反向核对。两个 frozen views
    上，attempt 1 Agent/zero/adaptive 折叠为一个有效执行；attempt 2 Agent/zero 折叠为 action-class，
    adaptive 产生唯一 uniform identity。因此 Agent 对 zero behavior delta 为 0/2，adaptive 为 1/2；
    本阶段新增 model calls=0、SUT executions=0，不作效果优势结论。
    详见 `docs/stage-m5.21g-effective-execution-gate.md`。
84. [X] M5.21h 只补跑 gate 中唯一没有证据的 attempt-2 adaptive uniform identity。official
    etcd/raft v2 在 seed 172、32 decisions、34/34 ceiling 下实际消耗 33/33 primary/replay work，Replay
    稳定，Agreement/Trace Integrity 零 finding。与同边界归档 ActionClass 相比，单次 PSS states 为
    23 对 17，集合交集 8、并集 32、Jaccard 0.25；两边 workload 都是 pending。这证明 identity delta
    对应真实轨迹差异，但不能从单 seed 状态数推出方法优势。CLI 只补两个既有 B4 strategy 的 qualified
    bundle allowlist，没有新增执行器。下一步仅复用三个已有 bundle 聚合两条等预算两步 method corpus，
    不调用模型或运行新 SUT。详见 `docs/stage-m5.21h-missing-effective-execution.md`。
85. [X] M5.21i 从三个可信 bundle 重投影并按顺序聚合两条等成本 method corpus。ActionClass→
    ActionClass 与 ActionClass→Uniform 都计 2 attempts、64 decisions、66/66 primary/replay work；前者
    得到 32 states/1325 prefix area，后者为 40/1444，最终集合交集 26、并集 46。Agent/zero 指向
    相同 32-state corpus，但 Agent 另计 2 calls/6027 tokens；本阶段新增 model calls/SUT executions
    均为 0。结果只说明当前两尝试中 Agent 无 behavior delta、简单 adaptive 轮换产生 discovery 增量，
    不构成总体方法优势。下一步不扩大 LLM pilot，先实现最小可信 RiskWitness 并重判现有证据。
    详见 `docs/stage-m5.21i-ordered-method-corpus.md`。
86. [X] M5.21j 实现最小可信 RiskWitness。协议无关 `RiskWitnessSpec/v1`/`Result/v1` 机械验证
    family/risk/milestone identity、冻结 DAG、target/execution/projector binding、step order 和 digest；
    Raft Family Pack 固定 `workload invoked at coordinator < coordinator changed while inflight < old
    coordinator restarted after change`，etcd/raft composition 只从真实 Invoke、role/term/incarnation、
    client return step 和 node transition 投影。M5.21f/h 三份归档 bundle 均为 workload offered=0，故
    0/3 witness reached、三份第一缺口均为 workload invoke；新增 model calls/SUT executions 都为 0。
    该结果只区分 execution validity 与 risk reachability，不参与 Oracle 或方法优势判定。下一步先做
    holdout readiness audit，并保留第二 strict CFT 实现迁移门，不立即扩大 semantic planner。
    详见 `docs/stage-m5.21j-risk-witness.md`。
87. [X] M5.21k 完成 current-repository Formal Holdout Readiness Gap。回归机械扫描七份公开
    evaluator manifest：当前可验证 3 pair/1 个不同根因，旧格式归档 4 pair/3 个根因，
    但 formal private pair/root cause/control 均为 0。当前 contract 拒绝 formal classification，fresh CLI
    只接受一组 pair，M4 blind/preflight 路径没有可编译源码；HashiCorp Raft strict qualification
    为 required 3/8。结果 `ready=false`，公开样本不进入正式分母。本阶段没有模型调用、新 SUT
    execution 或生产组件；下一步只实现最小 `FormalBenchmarkContract/v1`。
    详见 `docs/stage-m5.21k-holdout-readiness-gap.md`。
88. [X] M5.21l 实现最小 `FormalBenchmarkContract/v1` foundation。private contract 用显式
    `FormalPair{Control,Candidate}` 机械要求至少 3 pair/3 个不同 root labels，并绑定 Profile、
    MethodSpec、bundle schema、共同预算、build evidence 和 composition identity。`FormalOpaqueView/v1`
    只公开 commitment、Family/Profile/Method identity、预算和 opaque trial ID；pair/kind/root/build/
    projector/monitor/nonce 均不公开。通用 resolver 按 exact ID 选择 `DecisionProjector` 与
    `BundleMonitor`，不导入 target package。公开 synthetic fixture 只证明 contract plumbing；exposure audit、
    formal fresh evaluator、真实 private pair 与第二 strict CFT 仍缺失，因此 `formal_ready=false`。
    本阶段 model calls/SUT executions 均为 0。详见 `docs/stage-m5.21l-formal-benchmark-contract.md`。
89. [X] M5.21m 实现 `FormalExposureAudit/v1`。curator-side 通用代码重算 exact opaque
    projection，对每份即将越过边界的 JSON bytes 计算 SHA-256，并递归检查 object key/string
    value 是否直接复制 nonce、pair/root、variant/build 或 projector/monitor atom。报告只保存
    artifact index/digest 与稳定 no-echo finding；clean synthetic request 通过，direct leak、malformed
    JSON、wrong opaque view 和 report tamper 被拒绝。它只防 exact atom 直接复制，不证明无法语义
    推断或替代 runner 隔离。formal fresh evaluator 仍未接线，`formal_ready=false`；model calls/
    SUT executions 均为 0。详见 `docs/stage-m5.21m-formal-exposure-audit.md`。
90. [X] M5.21n 实现 `FormalFreshEvaluation/v1`。private entry 必须接收 sealed contract、
    passed exposure audit、MethodSpec、精确 trial→fresh evidence map 和注册的 projector/monitor；
    通用代码按 explicit pair 映射评价 3+ matching pairs，并复用既有 build/method/bundle/
    projection/budget/Replay 校验。公开 evaluator wrapper 继续固定 Agreement，formal composition
    则按 exact registry identity 选择 trusted monitors 且始终先运行 TraceIntegrity。private ledger
    绑定 contract/exposure/method digest，保存 pair/root/result 和 FP/kill/invalid 机械汇总。
    public synthetic 集成将同一 correct bundle 复用为 3 pair/6 trial，结果是 3 control-pass、
    3 candidate-survived、0 FP/invalid；它只验证 plumbing，不是 binary provenance、holdout 或
    方法效果。多 pair CLI、真实 private pair 与第二 strict CFT 仍缺失，故
    `formal_ready=false`；model calls/新增独立 SUT executions 均为 0。下一阶段只增加
    strict CLI artifact I/O 并调用该唯一 evaluator，不新建 Runtime 或报告 schema。
    详见 `docs/stage-m5.21n-formal-fresh-evaluator.md`。
91. [X] M5.21o 将 formal multi-pair 评测接入现有 `cmd/defect-eval`。private
    `FormalFreshInputs/v1` 只包含 opaque trial ID 及 BuildAudit/binary path，路径不进入评测账本。
    formal flags 与 public pair flags 完全互斥；CLI 在任何 SUT execution 前完成 contract/
    exposure/method/composition admission、exact 2N trial set 和全部 audit/binary digest 检查，再复用
    既有 `runFreshBundle` 和 M5.21n evaluator。artifact directory 与 final output 必须为新路径。
    public synthetic 集成新运行 1 次 32-decision official etcd/raft correct execution，再用 injected
    runner 复用为 3 pair/6 trial：3 control-pass、3 candidate-survived、0 FP/invalid。篡改最后
    binary 在 runner calls=0 时被拒绝，但该 fixture 不是真实 binary provenance 或 holdout。
    model calls=0；真实 private pair 和第二 strict CFT 仍缺失，故 `formal_ready=false`。
    下一步停止增加 evaluator schema，在仓库外准备至少 3 组人工复核的 private
    matching pair/BuildAudit/binary curator pack；如数据不存在就明确报告 input gap。
    详见 `docs/stage-m5.21o-formal-multi-pair-cli.md`。
92. [X] M5.21p 完成首个真实 Runtime RiskWitness 正向可达性校准。公开、固定的
    etcd/raft strategy 只通过现有 qualified executor 从 enabled Action 选择：64 decisions 中实际
    `Invoke(n1)@28 -> coordinator n2@53 -> Restart(n1)@54`，中间的新主由自然 temporal/
    message/effect 推进产生，没有强制 term/role/timeout。workload 保持 `offered=1, completed=0,
    pending=1`，primary/replay 均为 66 work units，Replay 稳定，RiskWitness 三个 milestone
    全部由真实 Trace/Evidence/NodeTransition 投影并得到 `reached`。这只证明冻结 risk
    可由当前控制面实现、projector 不是恒空谓词；该人工固定校准不进入方法排名、
    Oracle verdict、formal holdout 或单次测试质量评分。model calls=0，新增独立
    SUT execution=1。下一步不再扩 evaluator；只设计 Agent 与强确定性自适应基线可共用的
    RiskWitness progress/current-frontier 信息面，并用无模型 gate 证明新权限能改变
    effective execution。在 formal holdout readiness 与第二 strict CFT 迁移门完成前，
    不将 witness feedback 交给 Agent，也不调用模型。详见 `docs/stage-m5.21p-risk-witness-reachability.md`。
93. [X] M5.21q 完成 no-model Risk Frontier exact-authority gate。通用层从 M5.21p 真实
    trace 的 28/53 decision 前缀，以 fresh Adapter 严格 Replay 后重建 Runtime-enabled 与
    envelope-admissible ActionRef；可信 progress 只保留已满足 milestone、首个缺失项和 source
    digest。消费者选择必须逐字段引用当前 view 成员，并编译为 exact ActionID rule。
    `Crash(n1)@29 + Restart(n1)@54` 重现 reached 风险轨迹；同一前缀选择普通
    `CompleteEffect(n1)@29` 则完成 workload、仅满足 1/3 milestone，policy/config/trace identity
    均不同。两臂各为 66/66 primary/replay work，前缀重建 30/55 work 另行记账；model calls=0，
    新增 SUT executions=2。该结果只证明精确选择具有真实执行权限，不是 Agent 效果或方法排名。
    下一步优先将该通用边界迁移到第二 strict deterministic CFT target；不继续增加 etcd/raft
    专用目标来回避跨实现门禁。详见 `docs/stage-m5.21q-risk-frontier-authority.md`。
94. [X] M5.21r 完成第二 strict target 候选门。HashiCorp v1.7.3 因官方 clock/RNG/goroutine
    边界不作为 strict 迁移目标，efficient/epaxos 因只证明 framing 而保持 deferred。固定
    Cargo.lock 的 raft-rs 0.7.0 probe 在两组 fresh 三节点 RawNode 上，仅通过 5 次 logical tick、
    显式 message step 与 Ready/advance 自然选出 n1 并提交 index 1；两份 28-record trace digest
    相同。该结果只允许进入 bounded Binding spike，不授予 Adapter/Runtime replay/Qualification，
    也不产生 PSS/RiskWitness/Agent/formal credit。M5.21s 新 target-owned 生产代码软上限 900 行，
    不得修改公共 Action/Runtime/PSS 或 raft-rs 算法源码。详见
    `docs/stage-m5.21r-second-target-candidate-gate.md`。

当前主线已完成 v2 的第一个真实测试闭环、v1 实现锥体删除、action-class random、trace mutation、
qualified uniform 和 batch PSS-guided 基线，以及 Experiment/corpus/feedback/MethodLedger 可信数据面。
M5.17c2 保留了 PSS-guided proposal 不可执行的负结果，不为获得成功 trace 临时改启发式。
M5.18b4R 已完成：24 个顶层 experiment 测试与 `go test -list` 机械 exact-once 对账，并按共享
fixture 分入三个独立 race test binary。method/execution/agent 分别以 353.374/128.527/88.508 秒
通过，其余 package race 也全部通过；所有调用均使用 `-count=1`，没有 data-race 报告。该阶段没有
优化 Snapshot 深拷贝、拆分 execution API 或改变 SUT run 数，验证了门禁拓扑调整本身足以消除
单一进程累计成本造成的超时。
M5.18b4 frozen request consumer 已完成：单臂只能选择 freeze 中的 exact prepared bytes，client
配置必须与 transport commitment 相等，随后唯一经过一次 invoke、既有 response audit、strict
parser、单份 baseline 校验、plan v2、seed-4 instance、qualified executor、projection 和
IntentOutcome。离线成功、hard-baseline 越权、transport failure 和 request tamper 回归通过；没有新增
CLI、临时 summary schema、执行器或真实模型调用。当前 exact-once 清单为 25 项，受影响 agent shard
12 项以 361.838 秒通过 race。
M5.18b4 pair orchestration/persistence 已完成：固定顺序各消费一次 exact frozen request，
第一臂失败仍执行第二臂。唯一 ledger 绑定共同 freeze、两份 audit、可选 instance/outcome
digest 和每臂完整成本；大型工件独立写入全新目录。离线回归已拒绝 ledger 篡改、目录覆盖、
key 落盘和 transport 私有诊断泄漏。当前 exact-once 清单为 26 项，受影响的 agent shard 13 项
以 652.141 秒通过 race；未读取 key，真实模型调用为 0。
M5.18b4 显式 opt-in pair runner 已完成：CLI 只接受显式 pair strategy、key file 和全新
artifact directory，依次执行目录预检、freeze construction/full validation、key read、两臂
consumer 与 persistence。离线见证中第一臂 transport failure、第二臂 baseline rejection，两份 audit
均先落盘再返回失败；私有诊断、key 和无效 freeze 均未越过边界。最终普通全量测试的
composition 耗时 99.492 秒，受影响 pair race 以 426.731 秒通过；真实模型调用为 0。
M5.19 Campaign 数据基础已完成：config、四类 terminal attempt、逻辑/墙钟停止和增量 hash-chain
checkpoint 均可 canonical seal/revalidate。定向回归覆盖 resume identity、预算、顺序、缺链和篡改；
普通全量、vet、受影响包 race 与旧路径审计通过。该阶段没有运行 SUT 或模型，也没有文件恢复。
M5.19a crash-safe persistence 已完成：全新目录以 artifact-first、checkpoint-second 的 fsync/no-replace
顺序提交；完整恢复重验 config、链和 artifact，并报告不计入账本的 orphan/pending。只有带私有校验
令牌的恢复状态可以续写，正常连续提交与存储均为 O(n)。普通全量、vet、受影响包 race 与旧路径审计
通过；没有运行 SUT 或模型。随后 M5.19b 在该层上实现 remaining-budget allowance 和 deterministic
provider，不再延伸 b4 micro-ablation。只有用户明确执行 opt-in 命令时才会读取 key 并进行真实两臂调用。
M5.19b deterministic Coordinator 已完成：request 绑定 config/head/ordinal 和剩余 attempts/decisions/
work/model allowance；provider 只能返回 terminal outcome、WorkLedger 和 opaque artifact。确定性见证在
attempt 1 后恢复并继续至 attempt 3，保留一次 terminal failed 成本，最终以 15 decisions、18/18 work
到达 attempt limit；logical 和 wall-clock stop 也独立通过。非法 provider result 不落盘且当前
Coordinator 不重试。M5.19c 已补齐跨进程 durable failure marker，并在 etcd/raft composition root 中
冻结第一个真实 provider。两个 16-decision attempts 以 seeds 41/42 通过现有 qualified executor，
在中途完整恢复后到达 attempt limit；两个 artifact 均含 replay-stable bundle/Core PSS，总账本为
32 decisions 和 36/36 primary/replay work。现有 `ExecutionFailure.Work` 已保留为 terminal failed artifact，
普通 provider/result error 则以不含私有诊断的 `failure.json` 封住 exact next request。本阶段未新增
Runtime、Replay、PSS 或 Oracle 路径，真实模型调用为 0。
M5.19d 已增加协议无关 `CampaignSummary/v1` 和 committed-ordinal artifact reader；Summary 只保存
小型 record/checkpoint 索引、totals 与 `running/stopped/failed` 状态，不复制 report/bundle。
`campaign-etcdraft-v1` runner 现可按 attempts/decisions/first seed/wall ceiling 启动新 Campaign，或以
显式 flag 恢复 exact config。真实 2-attempt 见证以 seeds 61/62 得到 16 decisions 和 18/18 work；
独立见证从 seed 71 的 head 恢复完成 seed 72。artifact-less error 在返回前保存 failed Summary，
私有诊断未落盘。当前 Summary 不是 verdict，也没有跨 attempt 的 PSS/Coverage/Oracle 聚合。
M5.20 引入的 `CampaignObservation/v1` 已由 M5.21b 升级为 `CampaignObservation/v2`。通用层只聚合与 Summary record 绑定的
projection；etcd/raft composition 从 committed reader 取回工件并重验 artifact、decision projector
和 Core PSS mapper，再运行 trace-integrity/agreement。v2 另将每个 attempt 的 PSS 增量归因到
已重验的 choice/intent/plan/backend，不暴露 seed 或 instance。seeds 91/92 的 2x8-decision 验收得到
18 samples/15 unique Core PSS states、2 crashes、2 个 pending workload 和两个 monitor 各 2 次零触发。
这些栏保持独立，没有综合分数；零触发不构成正确性证明。
M5.18b4-pre 曾以 590.400 秒通过 full race；加入 request-freeze 回归后，本阶段两次 full race
均在 20 分钟 ceiling 超时，分别运行到既有 adjacent trace mutation 和 M5.17c2 method 回归，
但未报告 data race。按 timebox 停止，不抬高 timeout，也不将未见告警记为通过；当前完整 race
门禁明确保持未通过。普通全量测试通过并由 fixture 复用保持在 90.785 秒，workload/trace-mutation
定向 race 以 84.420 秒通过。后续阶段不得无界复制真实轨迹测试。
M5.18b4R 保留上述历史失败记录，并用 exact-once 分片重新建立完整门禁：三个
`cmd/control-experiment` shard 和排除该 package 的其余仓库 race 均通过。分片总计执行原有 24 个
顶层测试，不依赖 `testing.Short`，也没有把 core gate 冒充 full gate。
Agent 仍不读取 private variant、build transform、root cause 或 Oracle 私有结果。Control
Runtime、统一 Action、消息所有权、自然时间和严格 replay 核心继续冻结。
Coverage Kernel、首个 Planner、M4.7 评价基础、M4.8 公开校准、M4.8.1 可信链、M4.9
历史回归复现、M4.10 虚拟时间边界和经 M4.11.1 加固的 Ready.MustSync 语义重构均已冻结。
`Blind Planner Agent v1` 的无模型受限闭环已冻结：Agent 只读取 opaque trial ID、冻结 Profile
投影、CapabilitySnapshot、opaque Coverage Debt、受限 Test Plan schema 与机械 finding，不接触
defect identity、patch、trigger、真实 obligation ID、Oracle 私有输出或 Ledger。M4.13 已加入
private Manifest 到 public JSON 的直接泄露审计。M4.9/M4.11 只用于该闭环的开发调试；下一步是
在仓库外冻结未泄露的独立历史样本/controls、通过 exposure audit，并在共同预算下进行首次盲化
方法比较。多样本 holdout 出现前，不增加复合义务或额外 Agent 角色。发现官方新缺陷是额外案例，
不是阶段退出条件。

M4.15 已把协议输入收紧为 Driver Manifest 声明的 `protocol-input` 形状：通用层保存但不解释
operation，具体 etcd/raft Driver 才映射到官方 API；早期 toy、Contract-only 与旧全信息 Planner
路径已删除。该整理只降低接入耦合，不构成第二实现复用或 Agent 方法效果的证据。

M4.16 将新的正式 benchmark Manifest 升级为 v2，并冻结 `pss_id`：trusted evaluator 从
Manifest 而非独立 CLI 参数选择 Family monitor，Campaign report PSS identity 不一致即为
invalid。公开 v1 pilot 仅保留显式 legacy 复验入口；该身份收紧不等于已经拥有正式 holdout 样本。

M4.17 新增 private readiness gate：在 Blind view 生成前机械要求 Manifest v2、足够的 distinct
root-cause label/control、historical provenance 和每个 trial 的 build artifact binding。报告只含
计数与稳定代码；label 不等于因果独立，curator 的私有来源/时间切分审查仍不可省略。

当前已暂停直接执行上述 holdout/多 Agent 待办，先完成 Control Runtime v2 与接入普适性校正。
M5.1 已实现协议无关 Action/ProducedItem Runtime、最早 temporal event、periodic pulse、分域
EntropySource、稳定 yield、严格 replay、fixture 和外部 Conformance Suite；v1 可信执行和实验工件
继续冻结。M5.2.5 已用独立 migration model 对 v1/v2 的三个外部子集绑定固定 command/
applied-node/safety/replay/witness expectation，结果全部 passed；自然换主 capability gap 明确
deferred，没有用显式 Campaign 改写输入语义。报告保留 v1 fresh fingerprint 与 v2 strict decision
replay 的强度差异，suite 为 `qualified=false`。M5.3 又将 Manifest、外部 case 和 Unsupported
机械合并为资格报告；etcd/raft v2 已通过 8 项 required 公共控制能力，process isolation 仍明确
Unsupported。依赖审计确认 PSS/Coverage/Agent/benchmark 仍消费 v1，本轮没有删除 legacy path。
M5.4c 已使用同一公共 Runtime 在 HashiCorp Raft 上完成消息、应用和生命周期闭环，
并用同一协议无关 Conformance intent 复验两个官方实现。这支持“控制语义可复用”，
但 HashiCorp 的自然墙钟、包级随机和操作性时间戳仍不可严格重放，所以尚不宣称
通用控制层具备完整 strict replay 能力。M5.4d 已确认薄 Adapter 无法接管官方库内部 `time.After/time.Now`
和进程级全局随机调用顺序，因此机械冻结对应 Unsupported。M5.4e 已机械冻结双实现
capability matrix 与 validated-only 消费规则，并删除被正式 Adapter/Qualification 取代的生产探针。
M5.5a 又将“语义存在”、“控制等级”和“确定性保证”拆分，防止把 Qualification 比例误当为
协议功能或覆盖率。M5.5b 已以真实独立进程验证最小黑盒 Target Envelope，并保留 wall-clock、
单 connection、直接子进程边界。M5.5c 进一步验证 connection-level gateway，但冻结了可配置 peer
endpoint、无 framing 和无 strict replay 的能力上限。因此不再扩张通用黑盒网络功能。M5.6a 已证明
现有 Adapter 可以直接承担 Runtime-owned Action 的外部 actuation，不需要另建 backend selector；
M5.6b 又补齐公共 typed partition 参数和显式拓扑 binding。M5.6c 已在 200 行停止线内补齐
选择期 eligibility、重叠引用和 controller gate 失败回滚，并用三进程双 Gateway 真实流量
验证。但多 gate 顺序切换仍会暴露瞬时 partial cut，回滚也无法恢复旧 connection；因此
黑盒路径保持 connection-level scheduler-actuated/interceptable，不宣称 scheduler-owned message
或 strict replay。通用黑盒网络代码在此再次停止扩张。后续默认路径冻结为“统一 Action +
分级能力 + 最小非侵入式灰盒”：优先使用官方 step/transport/clock/storage 接口，其次替换外围
依赖，协议核心修改是最后手段。M5.6d 已机械证明 grade 与 guarantee 可独立表达三条当前路径。
Control Port 只能作为目标薄 Adapter 内部构件，不得演化为第二套 Runtime backend。M5.7 又将目标专用
接入面收敛为 Execution Binding + Semantic Mapping，并冻结固定 Core PSS IR。共享 Adapter kit 只有被
现有目标和第三目标共同消费时才能保留。M5.7a 现已完成首个 Core IR 与 etcd/raft 映射：它只产生状态发现 digest，不参与 Oracle、Coverage
分母或搜索剪枝；M5.9c 后由可信在线 sampler 配对逐步 Runtime Snapshot/Evidence，M5.9d/M5.10 已接通
跨 run ledger 和可保存报告，但 summary 不是完整 replay bundle。现有 Adapter
审计没有支持提前抽取 kit。M5.8a 对固定 EPaxos 版本的生产构建、三节点 smoke 和接口表面检查已完成，
M5.8b 又证明 codec-aware Binding 能在 test-only worker 中冻结跨多个 byte Write 的完整真实帧；
opaque Gateway 不能自行提供消息边界。该结果仍没有取得 validated capability。当前进入 M5.9 v1
消费者迁移/删除门。M5.9a 已冻结当前 28 条执行 import 边并完成首批净减 33 行；删除门会随迁移显式
收缩，不能新增边。M5.9b 已去除单轨迹 discovery ledger 对 v1 Trace 的依赖，同时保持旧 Raft 曲线并
加入 Core PSS 复用见证；M5.9c 又形成可信 v2 在线 sample 序列，并将 legacy experiment 隔离为
明确子包。M5.9d 又完成通用跨运行 Aggregate 与等预算 v2 union 见证。M5.10 已形成可保存的
v2 非 Agent `measurement-complete` 报告并冻结唯一执行路径。M5.11 已在其上加入独立策略 entropy
的确定性 Random 基线。M5.12 又冻结受限 Planner Proposal 编译、strict decode、显式失败和部分工作
计费，并用 0-call stub 贯通。M5.13 已完成第一次真实单调用 LLM transport，并诚实保存运行期不可达
结果；该结果机械否证了“协议盲、无 workload 的绝对步号策略”作为正式 Agent 接口。原 M5.14 一次
repair 已退出主线；当前依次推进 admission、workload/fault envelope、v2 Oracle/DefectBench、强基线
和 Guarded TestIntent。EPaxos 最小 Binding 在形成首个 v2 测试闭环后再按停止线恢复。

---

## 24. 当前冻结的关键决策

除非 ADR 明确修改，以下决策视为冻结：

1. 项目名称为 ConsensusAtlas。
2. 核心 Runtime 和可信执行链使用 Go。
3. Python 主要用于 Agent/实验编排，不进入执行、Oracle 和评分可信路径。
4. 覆盖率相对于版本化有限 Profile，不宣称绝对全面性。
5. 固定 Core PSS IR 是默认语义中间表示；已有形式化模型和 Family/Extended PSS 是增强插件，不是强制前提。
6. ESOT 是高风险补充层，不是唯一覆盖分母。
7. 场景等价基于保守规范化偏序因果图。
8. Agent 不能修改当次分母、Oracle 或等价关系。
9. 接入采用通用 Runtime + 共享 Adapter kit + Thin Execution Binding/Semantic Mapping，避免厚重的协议专用 Adapter。
10. 目标系统默认使用官方未修改实现。
11. 消息必须区分 Produced、Released 和 Delivered/Dropped。
12. visible、durable、applied 和 network mailbox 状态不得混淆。
13. 安全覆盖与活性覆盖分别定义和报告。
14. etcd/raft 是第一实现，但第二实现是通用性主张的必要验收。
15. 覆盖 FAIL 场景仍算覆盖，覆盖和正确性分别报告。
16. Minimal Protocol Charter、Core PSS schema 和核心不变式构成最小人工信任根；Family/Extended PSS 可选。Agent 可以生成 Contract/Mapping/Profile/Binding/测试草案，冻结后只有 digest 标识的 validated 产物能成为当次语义真值。
17. 正常接入不设置逐事实人工 Gate；是否通过只由 digest、编译、conformance、重放、Oracle 和 Contract 见证机械决定。
18. Unsupported 不会从 Contract/Profile 分母中删除。
19. Coordinator 是确定性程序，不是有权修改结论的 Agent。
20. 扩大 Agent 的测试提议权和搜索权，保持其事实权与最终判定权为零；Agent 只能提交 proposal，可信内核才能写入 validated。
21. 最终主百分比来自冻结 Coverage Obligation 分母；PSS 状态发现只用于搜索效率比较。
22. Agent 测试经 `TestHypothesis -> SemanticEpisodeView -> EpisodePlan -> EpisodeReport` 形成反馈闭环；可信 concretizer、Runtime、Replay 和 Evidence/Oracle 决定真实执行与结果。
23. 自动接入与覆盖驱动测试先分开验证，再组成端到端系统。
24. Agent 方法的主要效果由隐藏历史缺陷和语义 mutant 外部评价；Coverage/PSS 不能自证有效。
25. 发现官方未知缺陷是 bonus，不是预设成功条件。
26. 完整执行预算必须计入每个 run 重复的 setup/prepare；scheduler decision 不能代表全部成本。
27. 新义务、PSS 维度和第三个及以后 Agent 角色必须通过公开校准中的机械失败或正式消融收益支付复杂度，否则删除或降级为实验路径。
28. Candidate 不保存人工资格状态；只有 typed requirements 与可信 CapabilitySnapshot 的纯集合判断可以产生 QualificationReport。
29. 公开 calibration 与正式 holdout 严格分离；calibration 只能验证构建和评测管线，不能支持方法效果主张。
30. Agent 是测试假设、语义目标和反馈修正的主体；系统搜索是 Agent 可调用的可信工具和对照基线，不是 Agent 的同级替代品。
31. `ExecutionFingerprint/StateRef/StructuralKey/PSSSemanticKey` 是不同身份；PSS 新颖度键不得未经证明地用于状态剪枝。
32. 在线 Agent 可以提出 `TestHypothesis/EpisodePlan`、排序全局 WorkItem、选择冻结搜索算子并引用当前 `ActionRef`；enabled 判定、具体化、动作执行、语义真值和 Frontier 身份仍属于确定性内核。
33. 未控制模型边界内全部非确定性、未完整枚举 enabled actions 或未证明剪枝保守时，不宣称完整 implementation-level model checking。
34. 在线控制面不提供任意 `AdvanceTime(to)`；搜索器只能选择最早窗口内的 `TemporalID`，时钟推进是 `FireTemporalEvent` 的受审计子步骤。
35. tick-based 协议把每次宿主 Tick 映射为一个 `PeriodicPulse`，未证明中间步骤不可观察前不批量快进。
36. SUT、workload、scheduler 和 Agent 使用隔离随机域；SUT 按 node/incarnation/domain 派生并记录 RandomDraw tape，第一版随机结果不是在线 Action。
37. enabled 只表示当前控制状态下机械可执行，不预测 leader/term/view 等协议语义或动作成功。
38. 外部 Invoke 不在 trace 外排队；当前不合格的 offer 不改变状态，Agent 必须在后续 frontier 重新
    提交。协议自身的接受、拒绝或忽略必须形成 Result/Observation，不能伪装成 Runtime 故障。
39. M5.4 起实行代码增长门：第二实现产生真实消息证据前，通用 Runtime/Conformance 目标净增为
    零；HashiCorp 探针超过 350 行仍无真实消息即停止，不能用新增基础设施替代功能进展。
40. 基础控制 witness 与 strict replay witness 分开计证；前者不因墙钟不可重放而被抹去，
    也不能替代后者获得完整 Qualification。
41. 统一 Action 只统一上层行为语义，不承诺不同目标获得同一控制强度。
42. 能力必须拆成 Control Surface、Control Grade 和 Deterministic Guarantees，不得压缩为单一分数。
43. 默认接入路径为最小、非侵入式灰盒：官方 API 优先，外围依赖注入次之，协议核心修改最后。
44. Control Port 是目标薄 Adapter 的内部构件，Runtime 仍只依赖唯一 `control.Adapter`。
45. candidate 和 control 必须使用相同的灰盒接入层；接入差异、工具链和二进制纳入 digest identity。
46. 跨实现同时报告共同 validated 能力结果和各自最大 validated 能力结果，Unsupported 不得隐藏。
47. 新目标的边际产物固定为 Execution Binding、Semantic Mapping、fixture 和 composition；不得复制调度、重放、PSS ledger 或评分。
48. 完整 `control.Adapter` 契约由共享 kit 复用；kit 不是 Runtime backend，且只有两个真实消费者共同需要时才进入共享包。
49. 所有协议先映射到固定 Core PSS IR；Family/协议特有字段只能进入分开报告的 Extended PSS。
50. 非 Raft 目标若要求修改 Runtime、Action 或 Core PSS schema，视为抽象失败并停止，而不是增加特例。
51. 非 Raft 验收分两层：先用公开同步控制面较清晰的 OmniPaxos 验证控制层跨协议，再用 EPaxos 类目标
    验证无稳定 leader/dependency graph；任何已有论文采用都不构成降低 conformance 的理由。
52. feasibility 的人工源码事实、机械决策和外部 Qualification 必须分层报告；build/smoke 成功不等于
    stable item、strict replay、完整 Adapter 或目标正确性。
53. connection byte chunk 不能当作 Message Item；无原生 message API 的目标必须在薄 Binding 中提供
    source-bound codec/framing，Runtime 只接收完整 opaque bytes 和稳定身份，不导入协议 decoder。
54. legacy execution 的生产 consumer 集合先由 `go list` 冻结并逐步收缩；M5.16R 在当前消费者归零、
    M5.15/M5.16 identity 冻结后删除实现锥体。历史工件不要求主分支在线重放，但必须保留摘要和文档。
55. Control Runtime 只有一套执行语义；exploratory 与 strict 的差异由 Experiment 声明的 capability
    requirements 和 digest-bound QualificationReport 机械准入，不由 Runtime 类型分支或 Adapter 自报决定。
56. Workload/Fault Provider 只能调用 Runtime 的公开 Offer 入口；`EnabledActions` 始终是唯一可选择
    动作集合，Provider、Agent 和搜索器不能在外部伪造 ActionID。
57. Agent 默认是 protocol-aware、defect-blind：可读取冻结协议知识、Family semantic view 和批次级
    机械反馈，不可读取隐藏候选身份、补丁、根因、已知触发轨迹或私有 Oracle 结论。
58. Agent-v1 的 frontier permutation/旧 Guarded TestIntent 只保留为历史与权限消融；Agent-v2 输出
    TestHypothesis/EpisodePlan 而非未来绝对 decision rule，hard constraint 不可降级，拒绝与 miss 必须
    记录、计费并进入可修正的 EpisodeReport。
59. Core PSS 只用于 coarse feedback/discovery；具体因果身份被压缩时不得用于状态等价或剪枝，Family
    风险信息进入分开版本的 Extended PSS/semantic view。
60. 不按目录蓝图预建 `evidence/search/intent/campaignv2` 等包；只有第一个真实消费者出现并证明重复
    机制后才提升公共抽象。
61. `ExecutionBundle` 是 trusted evaluator 的完整证据边界；普通 measurement report 不得被当成可独立重算
    Oracle 的 bundle。
62. Runtime Offer/prepare 等 scheduler 决策外的状态变化必须有显式 state-digest transition；不得通过放宽
    TraceIntegrity 隐藏证据链缺口。
63. 通用 Oracle 只消费最小 semantic observation；协议 Evidence 解码属于 target-owned trusted projector，
    projector identity 和重算结果必须进入 bundle digest。
64. TraceIntegrity/Qualification/Projection 失败的 trial 只能记为 `invalid`，不得记 candidate kill 或
    control false positive；PSS/Coverage 不参与 defect verdict。
65. 强基线只能在 Runtime 枚举的 enabled Action 中选择；FaultEnvelope 可以约束某一策略的
    selectable 子集，但不得改写 Runtime enabled、ActionID 或协议 Adapter 语义。
66. control/candidate 方法比较可显式绑定每个 variant 的完整 config digest；替换 policy、
    workload、FaultEnvelope、admission 或 SUT identity 的 trial 只能记为 `invalid`。
67. PSS state count/prefix area 是 coarse discovery 指标，不是最终测试质量分数；即使状态
    数增加，也必须分开报告 root-cause kill、correct-control false positive 和完整成本。
68. Runtime enabled 与 Experiment admissible 必须分层：Qualification/Profile 先决定该运行能否准入；
    Runtime enabled 只表达当前控制状态下机械可执行；FaultEnvelope 再得到每决策的
    admissible frontier，预算只决定是否继续。所有被比较策略只能看到同一 canonical admissible
    frontier；Runtime-enabled digest、admissible digest 和最终选择必须分别留痕。
69. Workload 声明测试输入和期望观察，不决定协议正确性。`ExpectedStatus` 不得让 pending、拒绝或停滞
    轨迹在 Oracle 之前消失；planned/offered/completed/pending/actual response 必须进入可重放产物。
70. 合法终止至少区分 `budget-exhausted`、`quiescent` 和显式配置停止；框架执行错误、trace 损坏、资格
    或投影失败不属于合法目标终止，只能进入无效实验/方法账本。
71. PSS Mapper 只负责语义状态投影。外部输入目标由 target-owned、确定性的 WorkloadRouter 解析；零个或
    多个候选目标是 guard/result observation，不得直接抹掉潜在的无主或多协调者现象。
72. 已持久化的 PSS feedback 只有在可信内核能从绑定 Runtime/Evidence/Mapper 身份的 bundle 重算时才能
    进入 corpus；Agent 或外部 JSON 自报的 state key 不具有搜索反馈资格。
73. 第一版 PSS-guided corpus 保持单信号基线。其他 novelty/near-miss 维度独立报告，只有 private
    holdout 消融证明具体遗漏后才能进入组合策略。
74. 旧 Planner/DeepSeek transport 的历史结果只证明一次受限调用及其失败语义，不是 Guarded TestIntent
    的可执行基础；失去当前 admission/workload/fault/bundle 边界的在线入口应删除，未来 Agent 按新接口
    重新建立最小 transport。
75. 方法定义必须以 digest-bound MethodSpec 冻结 strategy、seed、预算、超时、PSS/projector
    和非 SUT Config projection；candidate/control 的 build/qualification identity 另行绑定，不得藏在方法配置差异中。
76. 正式 evaluator 不信任 submitted bundle 的决定权。它必须验证 build audit 和 binary digest，
    从已验证 bytes 亲自 fresh execution，再重算 bundle、operation history、projection、Oracle 和成本。
77. OperationHistory 必须从冻结 WorkloadPlan、Trace Invoke 和 ClientHistory 重建，不得由 Agent/
    Adapter 自报。client history 只是 Oracle 输入之一；它不能替代 applied-prefix、durability 或其他按实际缺口选择的 monitor。
78. AgentSemanticView 必须从冻结 KnowledgePack/catalog 与真实 Manifest/Qualification 重算，
    不含 build/candidate/root-cause/Oracle 身份。Guarded TestIntent 的 hard ActionKind 不仅要有已准入
    selector，还必须在真实 Trace 中出现；未出现时整次 intent 失败，不得当作 preference miss 回退。
79. Agent 被要求提交的每个有界字段，必须在语义视图中有机械可验证的上限或候选值；
    不得让模型猜测 decision range、fault envelope 或 backend capability，再用 compiler 大量拒绝补救。
80. 每次外部模型调用必须单独记录 request/response digest、provider metadata、token、时长和
    progressive failure status，不保存秘钥。模型复制 prompt 具体示例的 trial 只能归类 transport
    calibration，不得通过重试或人工 fallback 改写为 Agent 规划成功。
81. Agent batch feedback 必须从完整 ExecutionBundle 和绑定 PSS Mapper 重算方法观测；
    只能暴露共同预算、完成度、成本、粗粒度失败类和 discovery curve，不得暴露
    bundle/trace/build/candidate/root-cause/Oracle/state identity。
82. feedback ablation 不得改变 semantic view、risk、decision budget、hard capability/action 或
    FaultEnvelope，只能改变 preference。source batch 与 unseen follow-up 必须使用预先冻结的不重叠
    seed/身份，source construction 成本同样纳入各消融臂的总成本。
83. 框架 execution complete 与 workload completed/pending 必须分开记录；只有终止语义、
    attempt 定义和成本投影相同的方法反馈才能直接比较，历史不可比 identity 不回写。
84. unseen follow-up 的 source seeds、follow-up seed、选择规则和每个 arm 总成本必须在
    结果前冻结。失败 arm 保留并计费，不替换 backend/seed，已知 deterministic outcome 不得泄露给后续 Agent。
85. capability 必须区分 Runtime-supported、Experiment-producible 和 backend-selectable。
    Manifest 能执行某 Action 不等于当前 Experiment 能将它 Offer 到 frontier；后两层不成立时
    Agent catalog 必须明确关闭，不得让 compiler 接受必然不可达的 hard Action。
86. execution validity、intent reachability 和 protocol correctness 是三个独立结论。
    report/bundle/replay 有效但 hard Action 或后续 RiskWitness 未到达时，记 `intent-not-reached`，
    不得改写为 execution invalid，也不得产生 Oracle violation。
87. 宏观 CompiledIntentPlan 只绑定 risk、backend strategy、hard constraints 和 compiler 工作；
    实际 policy seed 属于独立、digest-bound 的 execution instance。同一 plan 可在多个预先冻结
    seed 上执行，但 Agent 不能提交或覆盖这些 seed。
88. b4 后优先增加小型可信 RiskWitness，不建立开放 temporal DSL。target-owned projector
    从 Trace/Evidence 投影 semantic milestones，family-owned 冻结 witness 偏序，通用层只验证
    identity、step ordering 和 digest。Agent 只选 risk_id，不提交 milestone 或 reached 结果。
89. b4 的研究定位固定为 backend-preference feedback micro-ablation。它不能证明 Agent
    构造了协议时序场景；只有 RiskWitness 和多个证据驱动 risk 进入后才评价协议语义规划。
90. 当前已验证对外范围为 leader-based CFT/Raft。Control Runtime 可保留 limited-BFT
    扩展目标，但在拜占庭 Action、witness、Oracle 和第二 strict 实现证据出现前不声称 BFT 适用性。
91. RiskWitness 和 target-side backend definition 完成后，在大量增加 Raft 专用 risk/
    workload/Oracle 前必须插入第二 strict CFT 实现迁移门，避免 Agent/Experiment 层重新与 etcd/raft 绑死。
92. feedback ablation 的精确 prompt/request bytes 必须在读取 key 和首次调用前冻结。
    两臂使用同一 prompt builder、semantic view、hard baseline 和 transport 限制；唯一实验信息
    差异是结构化 feedback 为 `null` 或可重算对象。精确 bytes 可保存在 ignored artifacts，
    但 digest、length、seed、budget、call/retry 上限必须进入可提交的小型承诺工件。
93. preference-only 权限必须对每份 proposal 分别与调用前冻结 baseline 机械比较，
    不能只比较两臂回复彼此相同。两臂同时改动 risk/must/budget 仍是越权，必须拒绝并计费。
94. race correctness gate 与完整研究回归必须分开命名。快速 core gate 只提供局部信号；完整 gate
    可以用多个独立 test binary 分片，但必须由机器验证全部顶层测试恰好执行一次、不得依赖
    `testing.Short` 静默缩减、不得复用 Go test cache，并为每个分片保留明确 timeout。测试拓扑变化
    不得改变 SUT 执行次数、随机 seed、实验 fixture、冻结 identity 或可信结论。
95. 冻结请求的 consumer 只能引用 freeze 中的 arm commitment 和 exact prepared bytes；transport
    配置、follow-up seed 与 execution budget 必须从 freeze 机械取得。response audit、strict parse、
    baseline validation、compile、execution 和 intent reachability 必须分阶段记账；任何阶段失败都
    不得触发 retry、人工修复、backend fallback 或替换 seed。
96. 双臂 orchestration 必须保留失败 arm 并继续另一 arm，不得以第一臂失败为由提前结束或替换输入。
    source 物理复用不改变每臂逻辑计费；pair ledger 只绑定现有 audit/instance/outcome identity，不复制
    transport、compiler、executor 或大型 bundle。持久化目标必须是全新目录，禁止覆盖已有实验工件。
97. 正式 pair 入口必须遵守 freeze-before-key；key 读取和 transport 不得影响已冻结的输入。
    pair consumer 返回可持久化结果和独立失败，因此调用方必须先落盘完整 ledger，再向外返回
    arm failure；不得因非零错误丢弃已发生的模型成本或失败 audit。
98. Campaign 的 wall-clock ceiling 只能作为运维安全上限；因机器性能不同而完成的不同
    work 不能直接用于方法排名。正式统计必须同时报告 wall time 和确定的 logical/primary
    work；方法比较使用共同 work ceiling 或共同前缀，不使用“同样跑 N 分钟”代替等预算。
99. Campaign checkpoint 必须按 terminal attempt 增量提交，不在每个 checkpoint 重复完整历史。
    每项只保存当前 record、累计 WorkLedger 和 previous digest；完整链验证负责重算累计值和恢复
    identity。artifact digest 只有经持久化层与 durable 文件对账后，才能成为可恢复的已完成证据。
100. Campaign 持久化必须先同步 content-addressed artifact，再以 no-replace 操作提交 checkpoint。
     完整恢复产生不可由外部伪造的续写状态；普通 checkpoint 值不能单独授权提交。artifact-only
     中断残留可以报告并复用，但在 checkpoint 引用前不得计入可信 totals 或实验结论。durability
     声明必须绑定实际文件系统语义；当前只承诺支持 fsync 和 hard link 的本地 POSIX 文件系统。
101. Campaign provider 只能消费 digest-bound request 和 remaining allowance，不能自行设置 attempt
     identity、artifact digest、checkpoint、totals 或 stop reason。terminal failed/invalid 必须保留 artifact
     与已知成本；普通 error 只能表示无法形成可信 terminal result，不能自动重试或进入正式比较。
102. Agent Campaign 调用必须先 durable result、再解析和编译计划。completed result 恢复只能复用
     已保存回复，dispatch-only 必须以 ambiguous 终止，failed result 也不得重试。模型工作只在 Campaign
     artifact/record/totals 外层附加一次，不能污染 target report/bundle；planner identity 必须进入
     experiment composition digest，防止 zero-model 与 durable-call provider 在恢复时互换。
103. Durable-call runner 必须按 attempt 执行 `exact intent durable -> key read -> one Step -> key clear`，
     不能在 Campaign 开始时读取一次 key 后跨 attempt 复用。只有 intent-only prepared 状态允许读 key；
     completed result、durable plan、ambiguous 和 failed 恢复均不得读 key 或调用 transport。pre-plan
     failure 的模型成本以 durable result 为准；在引入可信 terminal accounting 前，不得伪造 plan 将其
     填入 checkpoint totals，也不得把 summary 中的零 model work 解释为没有发生调用。
104. Pre-plan terminal accounting 只能引用同一 next ordinal 已持久化并由 store 重验的 model-call
     result，不能接受 provider 自报 WorkLedger。failure marker 保存 result evidence digest、机械附加的
     work 和 allowance-overrun 标志；failed Summary totals 等于 committed record totals 加 terminal
     work，但 Sequence/Attempts/head checkpoint 不增加。普通失败和 dispatch-only ambiguous 保持空 work，
     不得产生 plan、artifact、PSS 或 execution evidence。
105. 真实 Agent Campaign 首次连通必须是公开固定输入、单 attempt、单 call、0 retry 的
     calibration。运行结果无论 stopped 还是 failed 都必须保存 durable result 和实际 work；
     dispatch-only interruption 必须停在 ambiguous，不允许为获得成功样本而 resume 重调。连通成功只证明
     transport/parser/compiler/executor 链路可达，不证明规划质量或测试效果。
106. Agent/zero-model 初次对照只能称为 matched execution-ceiling pilot，不得称为相同总成本实验。
     共同 attempts、decision ceilings、seeds 和 executor 与 Agent 独有 model calls/tokens 必须同时报告。
     PSS 数量、workload completion 或 monitor 触发都只是分量，不允许事后合成一个分数、换 seed
     或补跑以获得有利结果。
107. Preference-only Agent 的输出形状必须由可信代码逐请求生成，并作为 exact prompt/request bytes
     的一部分持久化。空 `backend_ids`/`actions` 也必须显式编码为数组；允许值由冻结 risk、eligible
     backend 和 backend action surface 机械求交。可靠性修复不得通过放宽 unknown-field parser、
     自动重试或修改 hard baseline 实现。
108. Planner behavior delta 不能只比较 intent、plan 或 `IntentExecutionInstance` digest。这些审计
     identity 正确绑定 proposal 和 compiler work，但可能被不影响 backend/strategy 的偏好元数据改变。
     决定是否需要重复 SUT 执行时，必须另行比较由可信组合层生成的 effective execution identity，至少
     绑定 target/runtime composition、实际 strategy/Policy、seed、decisions、FaultEnvelope 和实际 work
     budget；LLM 与非 LLM 方法使用完全相同规则。该 identity 只用于执行去重和行为差异归因，不取代
     完整审计 identity、Replay fingerprint 或 PSS semantic key。
109. Effective execution identity 的通用层只能接收 target/environment 与 Policy 的 opaque digest，
     不得导入协议类型。目标组合层必须从真实 Adapter/Runtime/workload/router/policy 构造输入，并用已保存
     execution config 反向核对；backend 描述 ID、Agent proposal 和 compiler 统计若不进入执行器，就不能
     单独改变该 identity。方法比较只执行尚无可信 evidence 的唯一 effective identity，已有相同 identity
     的 bundle 可共享，但共享关系不能跨 target build/config、Runtime composition、seed 或预算。
110. 多 attempt 方法结果必须从该方法按顺序引用的有效 bundle 重新聚合，不能相加每次的 unique PSS
     states、prefix area 或 monitor 计数。物理共享一个相同 effective identity 的 bundle 不免除各方法臂的
     逻辑 execution work 记账；实际新增 SUT execution 与每臂逻辑成本必须分别报告。单次 identity 对照
     只证明行为差异，只有共同预算的完整 method corpus 才能形成方法级 discovery 诊断；外部缺陷检出与
     control 误报仍是独立的主要效果指标。
111. Method view、ordered execution corpus 和物理 evidence object 必须分层。多个方法可以引用同一
     corpus，多个 corpus 可以引用同一 bundle；去重只减少物理 SUT 执行，不改变每个方法的 attempts、
     primary/replay logical work 或独有 model work。Agent 与 zero-model 若映射到同一 corpus，就没有
     behavior delta；Agent 的额外 token 不能被 PSS metadata 差异或共享执行掩盖。
112. RiskWitness 是 frozen risk 到可信 trace 的有序可达性见证，不是 Oracle、Coverage 分母或综合评分。
     family 只拥有 milestone ID 与固定偏序，target projector 只拥有 opaque evidence 到 milestone 的映射，
     通用层重算 reached/missing/order/digest；Agent 不得提交 milestone、step、reached 或当轮新增 risk。
     在将 witness feedback 交给 Agent 前，必须先完成 holdout readiness 与第二 strict CFT 实现迁移门；
     semantic objective 以后只有在可信编译后改变 effective-execution identity 才算实际规划权限。
113. 公开 calibration、历史回归和语义重构无论数量都不得转换为 formal holdout credit。
     正式 readiness 必须分别验证 private dataset、opaque exposure boundary、multi-pair fresh evaluator
     和第二 strict CFT target；任一项缺失都不得声称 ready。M4 已删除的 blind/preflight/
     submission 实现不是当前功能，未来只能在 v2 数据面上建立最小
     `FormalBenchmarkContract/v1`，不恢复旧 runner、第二 Runtime 或大型候选目录。
114. Formal contract 必须用显式 pair 表达 matching control，不允许 evaluator 依赖位置、文件名或
     Agent 自报重建配对。Agent-facing view 只允许公开 trial ID 和冻结共同方法/预算 identity；
     projector、monitor、root cause、kind、pair 和 build identity 保持 private。composition 通用层只按
     ID 解析 `DecisionProjector`/`BundleMonitor`，不导入 target Adapter。contract/view 存在本身不等于
     holdout ready；exposure audit、fresh multi-pair evaluation、private dataset 和第二 strict target 仍是独立门禁。
115. Exposure audit 必须比较 exact supplied bytes 与 sealed contract 枚举的 private atom，同时重算
     opaque projection。持久化报告只包含 artifact index/digest 和稳定 code，不得回显 matched value、
     命中数量或 curator 本地路径。它只是 direct-atom leakage 的必要门禁，不得声称防止
     协议推断、编码/改写泄露，也不得替代进程、网络和文件系统隔离。审计通过不产生
     defect credit，只允许工件进入后续 formal evaluator 阶段。
116. Formal holdout 回答的是方法在隐藏 matching candidate/control 上的外部效果，不是
     单条 Campaign 的绝对测试质量分数。对一次测试的产出必须继续并列报告
     obligation/profile coverage、Core PSS discovery、RiskWitness reached/missing、Oracle findings
     和完整成本；任何一项都不得被 holdout kill rate 或复合总分替代。
117. 默认研究对象是一个物理 Planner Agent。Scenario/Critic/Onboarding 只有在预注册消融证明
     其解决了单 Planner 的明确失败类后才增加。每个 formal trial 必须使用新鲜上下文、
     无跨 trial memory，且 Agent、Random/自适应基线和专家方法只能读取预注册的同等
     public/opaque 信息面；model/provider/version、calls/tokens 和全部执行成本必须独立记账。
118. M5.21o formal CLI 当前只能 fresh 执行一个 CLI-compatible `MethodSpec`，且该
     MethodSpec 的 execution attempt 上限为 1。在 durable multi-attempt Planner provider、per-trial reset、
     同等信息面和完整 model-work 记账接入同一 fresh runner 前，不得把该 CLI 称为
     formal Agent Campaign evaluator，也不得用手工预生成 bundle 替代 fresh method execution。
119. 一个 RiskWitness 在作为 Planner 目标或反馈前，必须先通过公开真实 Runtime 正向可达
     校准和明确负例。正向校准只证明 control surface/projector 可用，不进入方法效果
     分母。Agent 只能读取可重算的已满足/首个缺失进度与当前 enabled ActionRef，
     不能自报 milestone evidence、step 或 reached。
120. 第二个 strict deterministic CFT target 是控制层、Core PSS、RiskWitness 和 formal runner
     的独立迁移门，不以 Agent 公开 pilot 是否取得正结果为前提。无论 Agent 当前强弱，
     都不得用继续增加 etcd/raft 专用 backend/risk/monitor 来回避该迁移门。
121. RiskWitness progress 进入任何 Planner 前必须由可信 target projection 绑定严格 Replay 的 trace
     prefix，只暴露已满足里程碑、首个缺失项和 source digest；当前 frontier 必须由 fresh Runtime
     重建并同时冻结 enabled/admissible digest。消费者只能选择视图中的 exact ActionID/Action digest，
     不得以同 kind/node 的相似动作替换；prefix 重建成本必须单独计入 ledger。该 authority gate 通过
     只证明选择可改变真实执行，不得称为 Agent 效果或方法排名。
122. 第二 strict target 必须先通过隔离的 deterministic-core candidate probe，再进入有 LOC 停止线的
     Binding spike。candidate probe 的 fresh equality 不等于 Control Runtime Replay 或 Qualification；
     harness 未调用墙钟/线程/显式 campaign 也不能外推成 SUT source audit。部分资格目标不得通过外围
     包装升级为 strict；若新目标需要协议 Action、Runtime type switch、第二 scheduler 或修改算法源码，
     必须停止并重新选择目标。
123. “实现 `control.Adapter`”只表示 interface compatibility，不表示 strict controllability。
     官方未修改的 HashiCorp v1.7.3 保持 native-partial；未来 instrumented build 必须只修改环境边界，
     以独立 BuildID/patch digest 与官方构建分开报告，不得改协议转换规则。M5.21s raft-rs Binding
     必须保持公共 control/runtime/experiment 生产 Core Churn=0，fresh worker-process Replay 稳定，
     并把新增 LOC 分成 target-native、language bridge、boilerplate 与 conformance。两个 strict Raft
     只验证跨实现 Binding，不得作为跨协议普适性证明。
124. M5.21s 已证明 raft-rs 0.7.0 的最小 `Temporal → Ready effect → Message` 路径可在共享 Runtime
     上 fresh-worker-process strict Replay，公共 control/runtime/experiment 生产 Core Churn 为 0；但
     1,027 行 target-owned 生产 Binding 超过 900 行软目标。下一阶段必须先做 M5.21sR contraction，
     不得同时扩展 workload、durable restart、PSS、RiskWitness、Agent 或 Qualification。当前 Ready
     只被建模为合并的 persisted/applied/advanced effect，不能冒充双阶段持久化或 power-loss 证据。
125. M5.21sR 将 target-owned 生产 Binding 从 1,027 行收缩到 981 行，并增加 unsupported-operation
     rejection 与 abnormal-worker-exit conformance；公共 Core Churn 仍为 0。900 行只是软目标，禁止
     通过排版压缩、删除可信检查或把单一消费者代码重分类为共享包来达标。981 行是当前暂定基线，
     不是理论最小值。M5.21t 只允许增加 opaque Propose→commit→ClientResult 纵向切片，不得同时加入
     restart、PSS、RiskWitness、Agent、新 Action 或 Qualification。
126. M5.21t 已以 28 个 Runtime decision 完成唯一 opaque Invoke→commit→ClientResult，并在 fresh
     worker 中严格 Replay；共享 Core Churn 与 raft-rs 算法修改仍为 0。但该切片增加 237 行生产代码，
     target-owned 总量达到 1,218 行，超过原 minimal-binding 1,200 行观察线。下一阶段必须先做
     M5.21tA 边际接入成本审计，不写新 capability；只有两个真实消费者共同需要的代码才可提取为共享
     helper，禁止通过搬文件、改 LOC 口径或弱化身份/eligibility/Evidence 来宣称 Adapter 变薄。
127. M5.21tA 只提取被 Runtime、fixture 与三个共识 Adapter 实际消费的 command wire contract；审计
     生产面净减少 81 行，raft-rs target-owned Binding 从 1,218 降至 1,185，原 28-decision trace
     digest 不变。proposal、持久化/应用、ClientResult、单消费者 process bridge 与 PSS/Oracle 不得因
     字段相似而进入共享 Core。下一步固定为非 Raft construction/yield probe：只验证原始公开接口能否
     暴露确定性多节点构造、消息与自然时间边界，不先写完整 Adapter；probe 期间若必须修改公共
     Action、Runtime 或 Core PSS，必须记录为抽象缺口并停止自动扩张。
128. M5.21u 固定 `omnipaxos 0.2.2` 的三节点 probe 只用 logical tick、外部 pending-message FIFO、
     explicit delivery、non-leader append 与 decided suffix 得到两次相同 80-event trace；公共
     Action/Runtime/Core PSS 和上游源码 churn 为 0，因此只获得 non-Raft candidate-core go。下一阶段
     M5.21v 仅做 `Temporal + Message` 最小 Binding 和 fresh-worker-process Replay，target-owned 生产
     Binding 软目标 850 行、硬停止线 1,000 行；不得同时加入 workload、restart、durable storage、
     PSS、RiskWitness、Agent 或 Qualification，也不得把该 leader-based 结果冒充 EPaxos 验证。
129. M5.21v 已用未修改的 OmniPaxos worker/Go Adapter 接入唯一 Runtime：真实 51-decision 轨迹包含
     natural temporal、message duplicate/drop/deliver，并在第二个 fresh worker process 精确重放；公共
     Action/Runtime/Core PSS churn 为 0，target-owned 生产 Binding 为 770 行，低于 850/1,000 行界限。
     该结果只证明 leader-based Sequence Paxos 最小控制数据面。M5.21w 只允许增加 non-leader opaque
     append→decided→ClientResult，边际软目标 180 行、总量硬停止线 1,000 行；不得同时加入 restart、
     durable HostEffect、PSS、RiskWitness、Agent 或 Qualification。
130. M5.21w 在 n1 自然选主后从 n2 完成唯一 opaque append→decided→ClientResult；35-decision 轨迹在
     fresh worker process 精确重放，结果只由声明 origin=n2 的 exact decided entry 产生一次。公共
     Action/Runtime/Core PSS churn 和上游修改均为 0；生产 Binding 经 15 行收缩为 948 行，相对基线
     增加 178 行，低于 180/1,000 行界限。下一阶段 M5.21x 只允许从稳定 Evidence 映射现有 Core PSS，
     不修改 schema/Runtime/worker，不虚构 contending 或 durable state；mapping 软目标 180 行、硬停止
     240 行，仍不得加入 Agent、Coverage 总分、Oracle 或 Qualification。
131. M5.21x 以 154 行 target Mapper 将现有 OmniPaxos Evidence 保守映射到固定 Core PSS；35-decision
     workload 得到 36 samples、35 complete Core states、9 semantic graphs，fresh sample/Discovery/
     trace 完全相同。绝对 ballot/index/priority 变化不改变相对 Mapping，且不生成 contending、Value、
     applied 或 durable 语义。35/9 均不得作为质量分数。下一阶段 M5.21y 只增加 exact decided-prefix
     digest 与 target DecisionProjector，复用现有 Agreement；公共 Oracle/Runtime 不改，target 增量软目标
     140 行、硬停止 200 行，不加入 restart、Agent、Coverage 总分、Qualification 或真实缺陷声明。
132. M5.21y 从 OmniPaxos `read_decided_suffix(0)` 的完整有序前缀生成 domain-separated cumulative
     SHA-256，并由 51 行 target DecisionProjector 投影 exact participant/position/digest。真实 35-decision
     control 的两个同位置节点观察一致，fresh worker Replay 的 trace/projection 相同；只替换一个 Evidence
     digest 的显式 calibration 触发现有 Agreement 1 次。calibration 不是协议执行或真实缺陷，control 的
     0 violation 也不证明正确。target 生产增量 89 行，公共 Runtime/Oracle/Core PSS 与上游改动为 0。
     下一阶段 M5.21z 只做机械 Qualification 差距审计：保留完整 Portable CFT Profile 的 partial/
     unqualified 结果，并检查 executor 是否错误地把全能力资格作为所有子集实验的统一门槛；不得为通过
     而缩小 requirement，也不得同时加入 restart、Agent、新评分或 formal 效果声明。
133. M5.21z 保留 `portable-cft-control-v2` 的 8-required 完整分母，机械得到 OmniPaxos
     `validated=3, unsupported=3, unvalidated=3, failed=0, qualified=false`。Temporal/Message/Replay 已有
     target gate 证据，但 v2 Profile 的 witness 与 Crash/Restart lifecycle 组合，不能直接获得独立资格
     信用；不得人工改写状态。现有 ExecutionAdmission 能绑定 partial report 的命名 validated subset，并
     拒绝完整 Portable set，但 requirement 仍由调用方声明，尚未从 Policy/Workload/FaultEnvelope/Replay
     机械推导下界。因此 meaningful OmniPaxos experiment 仍不可进入可信评测。M5.21zR 固定为 M5.21 最后
     阶段：新增版本化解耦 witness/profile、在 SUT 执行前拒绝少报，并只运行无 crash/durability 的 admitted
     smoke；历史 v2 identity 不改，不加入 Agent、Campaign、评分或真实缺陷声明。
134. M5.21zR 新增 `portable-cft-control-v3`，不改写 v2 identity/digest；通用独立 witness
     在 OmniPaxos 和官方 etcd/raft 上验证 Temporal/Message/Replay。OmniPaxos 机械结果为
     `validated=6, unsupported=3, unvalidated=0, failed=0, qualified=false`。Config capability 下界在
     Adapter factory/SUT 前由 Policy/Workload/FaultEnvelope/Replay 精确或保守推导，少报不得进入执行。
     六能力 admitted smoke 完成 29 decisions、28 Core states、1 workload 且 fresh Replay 稳定；该结果
     不含 crash/durable/Agent/方法优势。M5.21 已收口；M5.22 只用已认证的 etcd/raft 与 OmniPaxos
     执行面做共同预算 cross-target Campaign/Planner，新 capability 必须由真实失败证据驱动。
135. M5.22a 不放宽旧开放 stochastic policy 的保守完整能力门禁。新增版本化 bounded uniform/action-class
     policy，将 canonical `selectable_actions` 纳入 Policy digest，并只允许进入 Experiment v2；Runtime-enabled 动作先经过 FaultEnvelope，
     再与冻结选择面求交，admission 从同一集合机械推导 capability 下界。Priority 越界、workload 无 Invoke、
     Trace 实际动作越界均失败关闭；Runtime 仍有动作但选择面为空时记为 `policy-surface-exhausted`，不冒充
     quiescent。未知 Effect/Callback 与尚无独立 witness 的 Duplicate/Partition/Heal 仍保守要求完整 required
     set。旧 v1 policy identity/序列化不变。OmniPaxos 六能力正向校准只允许 Invoke/DeliverMessage/
     FireTemporal，以 29 decisions 完成 1 workload、发现 28 个 Core states 且 fresh Replay 稳定；旧开放
     action-class 在 Adapter factory 前机械拒绝。该结果只证明 partial target 可使用受限 stochastic backend，
     不证明 Agent、跨目标 Campaign、方法优势、缺陷检出或绝对覆盖质量。M5.22b 只实现不含协议字段的共同
     semantic view/bounded intent 跨 etcd/raft 与 OmniPaxos composition，PSS/coverage 保持 target-local。
136. M5.22b 新增只读 `CrossTargetPlannerView/v1`：source AgentSemanticView 必须共享 KnowledgePack/Catalog，
     对 validated capability、Manifest Action 和 backend-selectable surface 做机械交集，对 Agent 只公开 opaque
     source digest。同一父 GuardedTestIntent 只替换 target view identity 后重新 seal，两个 child plan 的 risk、
     backend、strategy、预算、FaultEnvelope、required capabilities/actions、preference 和 compiler work 必须
     相同，再进入已有 target compiler 与唯一 executor。共同面固定 Invoke/DeliverMessage/FireTemporal。
     etcd/raft Ready 的 CompleteEffect 是显式 target-local deterministic plumbing，不是 Agent 搜索动作；由于
     尚无独立 effect capability，它保守绑定完整 8-cap admission，移除后真实执行在 0 decision 以
     `policy-surface-exhausted`/not-reached 停止。OmniPaxos 使用六能力 admission。正向见证中 etcd/raft 为
     42 decisions、44/44 work、31 target-local states，OmniPaxos 为 29 decisions、31/31 work、28
     target-local states，两侧 workload 完成且 Replay stable；PSS 数量不得跨 mapping 相加或排名。该阶段
     model calls=0，不证明 Agent/方法优势、缺陷检出或 durable multi-target Campaign。M5.22c 只在现有
     single-target Campaign 之上增加引用 committed artifacts 的 composition ledger，不改历史 identity、
     不复制 bundle、不增加第二个 Coordinator。
137. M5.22c 保持 `CampaignConfig`、Coordinator 和 artifact store 的单目标语义：两个 child plan 在任何
     target 执行前分别通过原有 `PreparePlannedAttempt` 持久化，执行后将各自 report/bundle/outcome
     提交为内容寻址 artifact，再从两个独立 Campaign 目录恢复。`CrossTargetCampaignLedger/v1`
     只保存 config/head/record/plan/artifact digest、target-local PSS identity 和 WorkLedger；它不复制
     artifact、不合并 PSS、不产生 Action/判定，也不是第二个 Campaign。真实恢复得到
     42/29 decisions、75/75 合计 work；修改任一内容寻址 artifact 后恢复失败关闭。下一阶段
     只允许在已有 durable planner-call/request 边界上比较一次受限共同 proposal 与 deterministic
     baseline；若不产生可比较新行为，应停止增加跨目标结构并转入 holdout/mutant 评测。
138. M5.22d 复用原 durable model-call，由 target-a Campaign 仅担任单次模型成本所有者；模型看到的
     request bytes 只含共同 view 与 preference-only contract，不含 target/Campaign identity。离线 blind
     mock 改变 parent 和两个 target plan digest，但 etcd/raft 与 OmniPaxos 的 report、bundle、trace 都与
     deterministic baseline 逐字节相同；mock 只增加 1 call/7 tokens 和 compiler checks。这是负证据：
     当前共同 Catalog 仅有一个 executable backend，Agent preference 没有实际执行权限，因而不得
     声称 Agent 已控制测试或优于 baseline。下一 gate 只允许复用已 admission 的现有策略，
     建立至少两个在两目标上都可执行且真实 trace 不同的共同 backend。若该 gate 失败，
     停止 preference-only Agent 路线；若通过，也只证明受限 preference 具有执行影响，不证明
     Agent 推理有效。下一主线先在 v2 `exact prefix -> reconstructed frontier -> WorkItem`
     边界上实现不做状态合并的 bounded stateless DFS，然后才接入 protocol-aware Search Agent
     并进行强非 LLM baseline/hidden-root-cause 评价。target-blind cross-target Agent 保留为
     portability/authority 消融，不作正式效果方法。
139. M5.22e 不新增 Runtime、Action、schema、target 或搜索算法，只将已有 bounded action-class 与
     bounded uniform 纳入新的双 backend 共同 Catalog。两个 backend 共享 6 项 validated capability、
     Invoke/DeliverMessage/FireTemporal、96-decision ceiling、opaque workload 和预先固定 seeds 1/2/3；
     etcd/raft 的 CompleteEffect 保持 target-local plumbing。12 次 primary 均完成 workload/Intent 并
     stable Replay，6/6 个同 target/seed backend pair 的 trace 不同，M5.22b-d 冻结 identity 不漂移。
     因此 preference 已有真实执行影响，但 224 与 323 的实际 decision 合计不能在没有 discovery/缺陷
     结果时解释为方法优劣，更不能证明 Agent。M5.23a 固定为最小 bounded stateless DFS：WorkItem 只含
     exact replay prefix 与重建 frontier，先不做状态合并、DPOR 或 LLM；通过确定性、Replay 和完整成本
     门禁后，protocol-aware Search Agent 才能对同一批可信 WorkItem 排序。
140. M5.23a 新增不含 RiskWitness/PSS 的协议无关 `ActionFrontierView` 和 bounded stateless DFS：每个
     WorkItem 只绑定 exact prefix StateRef、fresh 重建的 admissible ActionRef、父 ordinal/depth 和 child
     prefix；按 canonical ActionID 做 depth-first 顺序。每个分支分别计费父前缀重建、child 物化和独立
     child Replay，不克隆 Adapter 状态、不用 PSS 恢复、不做状态合并/DPOR。etcd/raft 公开校准固定在
     28-decision root、depth=2、items=6、work ceiling=1,000，实际扩展 2 个前缀并用 443 search work；
     两次搜索逐字段一致，6/6 child Replay，叶路径经 exact Policy 进入原 qualified executor 后前 30 步
     digest 一致且完整 64/64 primary/replay stable。该结果不证明完整枚举、DFS 方法优势、缺陷检出或
     Agent 效果。M5.23b 必须保持通用 DFS API 不变，迁移至 OmniPaxos；隐藏协议耦合应报告为失败，
     不得用 target 条件污染通用层。
141. M5.23b 在 `internal/controlexperiment` DFS API 和实现零修改的前提下，复用 M5.22b 已资格化
     的 OmniPaxos six-capability workload、Runtime seed、FaultEnvelope 和 qualified executor。从冻结真实
     bundle 的 1-decision prefix 出发，depth=2、items=6、work ceiling=1,000 实际扩展 2 个前缀、
     使用 51 search work；两次搜索逐字段一致，6/6 child fresh Replay，叶路径经原 qualified
     executor 精确复现前 3 个 decisions 并完成 29/29 primary/replay stable。worker 子进程只由
     target composition 管理生命周期，没有进入 Action/DFS 抽象。该门禁证明同一搜索 API 跨
     embedded Go 与 process-backed Rust Adapter 可执行，不证明 DFS 方法优势。etcd/raft 的 28-decision
     root 与 OmniPaxos 的 1-decision root 不匹配，443 与 51 不得横向排名。M5.23c 必须先建立 target
     内 matched-root、matched-budget 的 no-model DFS/bounded-uniform 校准；如果 stochastic policy 不能从 exact
     prefix 启动，应记录方法接口缺口，不得用不同起点冒充 matched comparison。
142. M5.23c 确认现有 `BoundedUniformPolicyVersion` 与 exact decision rules 互斥，因此不能直接与
     root-prefix DFS 做 matched comparison。本阶段不放宽 scheduler Policy，而是新增最小的 versioned
     `StatelessTraversalMethod`/`Result`：方法只能对 Runtime 已提供的 trusted `FrontierActionRef`
     排序，不能生成、删除或改写 Action。canonical 方法与旧 M5.23a DFS 逐字段一致；
     seeded-uniform 用 seed、prefix digest 和 admissible digest 对 canonical frontier 做确定性 permutation。
     etcd/raft 公开校准固定同一 28-decision root、depth=2、items=6、work ceiling=1,000；
     canonical 和 uniform seeds 01/02/03 均扩展 2 个前缀、实际使用 443 work，4/4 order digest
     不同且各自重复运行逐字段一致。uniform 叶路径返回原 qualified executor 后 exact prefix
     复现且 strict Replay stable。该结果只资格化同起点、同实际成本的比较装置，尚未定义
     discovery quality，不证明任何方法或 Agent 更优。M5.23d 必须先冻结只读、可重算、
     不反向影响搜索的 discovery projection，再扩展到多 roots/seeds。
143. M5.23d 固定 `StatelessTraversalDiscovery` 为搜索后只读投影。由于 WorkItem 不保存完整
     trace/PSS，每个 WorkItem 必须编译为 exact Policy，经原 qualified executor 执行至 child decision 并
     strict Replay 后才能进入 discovery。通用 projector 验证 WorkItem/bundle/trace/manifest/final state，
     并调用已注册 SemanticMapper 从 trace/evidence 重投影 Core PSS，逐项核对 bundle 样本；不相信
     bundle 自报或搜索内部状态。M5.23c 单 root 校准中，四种方法 search work 均为 443；各自
     6 个 discovery bundles 另需 191 primary + 191 replay，所以每方法可信 evidence 总成本为 825。
     common root 含 19 个 PSS states，canonical/uniform 01/02/03 在 root 之后分别新增 6/5/6/6，
     联合增量为 22。该数据只证明投影可重算、成本匹配且不反向影响搜索，不是 Coverage、
     正确性、缺陷或方法排名。M5.23e 必须事先冻结小型 strict-Replay root corpus，不得根据方法
     结果反选 root，然后才可报告跨 root 增量集。
144. M5.23e 新增方法执行前冻结的 `StatelessRootCorpus`：它将 M5.21p 同一
     strict-Replay source bundle 的 0/28/54 decision anchors 绑定为 exact prefix identity，分别对应
     source 初态、workload-invoked 和 old-coordinator-restarted 已冻结 milestone；phase 标签无执行
     权限。通用 `StatelessCorpusDiscovery` 复用 M5.23d projection，并从各 root local incremental
     union 中扣除全部 corpus root baseline，得到 corpus-novel PSS。四种方法的 corpus-novel
     为 15/13/15/14，联合 39；该结果不是排名。同一 root/depth/items/work ceiling 下，
     exact-prefix 长度使边际可信成本为 2,395–2,397，证明不能用 item 数冒充完全等成本；
     系统保留实际差异，不通过空操作填充。M5.23f 只冻结 Search Agent 的 frontier permutation/
     preference 权限与 validator，先做 no-model mock，不宣称 Agent 效果。
145. M5.23f 冻结 `StatelessFrontierOrderRequest/Proposal/Record` 和
     `ExploreBoundedStatelessDFSWithAgent`。可信 request 绑定 ProtocolKnowledgePack digest、当前
     `ActionFrontierView` 和只含已完成结果的 history；proposal 唯一可变字段是 `action_ids`，
     必须是当前 frontier ActionID 的完整 permutation。validator 映射回原 ActionRef，并拒绝
     遗漏、额外、重复、虚构 Action 和未知 JSON 字段；离线验证还要求 WorkItem 顺序是已接受
     proposal 的前缀。etcd/raft no-model reverse-frontier 校准中，planner 2 次调用/2 次接受，
     6-item 顺序与 canonical 不同，search work 仍为 443，叶路径返回原 qualified executor 后
     strict Replay stable。模型调用为 0，因而只证明权限链路可执行，不证明 Agent 效果。
     M5.23g 在 M5.23e 预声明 corpus 上运行一次 opt-in 真实 Agent pilot，与 canonical/
     fixed uniform seeds 按完整实际成本并列；完成后关闭 M5.23。
146. M5.23g 在 M5.23e 预声明的 0/28/54-decision root corpus 上完成一次真实
     DeepSeek v4 Flash pilot。每次调用先持久化 exact prompt/request intent，再写 dispatch，最后
     写唯一 terminal result；调用预算为 6、retries 为 0。6/6 proposal 均通过当前 frontier
     ActionID 完整 permutation 验证，三 root 共 18 个 WorkItem 返回原 qualified executor，全部
     strict Replay 并只读重投影 PSS。实际模型成本为 24,876 input + 3,459 output = 28,335 tokens；
     Agent corpus-novel PSS 为 15，与 canonical 的集合及三 root Action 顺序完全相同，fixed
     uniform 01/02/03 为 13/15/14。这是真实负结果：Agent 闭环可执行且权限/计费可复核，
     但当前 knowledge/prompt 在该小 corpus 上没有超越 canonical 的新增价值。M5.23 至此关闭；
     M5.24 必须先冻结重复试验和非公开 candidate/control，不得通过事后调 prompt 或反选 root
     消除该负结果。
147. M5.23R 是 M5.24 前的有界收缩阶段，不改写 M5.23g 方法或实验结论。已推送提交
     `3ec9237` 是 M5.23g 代码与必要冻结工件的不变检查点；本阶段不重新调用模型。HEAD 只继续
     维护 `Stateless Search + restricted frontier-order Agent + Campaign durability + formal evaluation`
     主线以及正式基线；Git history、阶段文档和冻结 summary 负责历史实验复现。M5.23R1
     先提取当前 Search Agent/Campaign 仍需的 protocol-neutral transport freeze、provider request
     和 key-file 读取原语，再退休 M5.18 one-shot、feedback/follow-up 和 B4 的生产文件、
     CLI strategy 与仅服务该路径的测试。独有的 credential、exact-byte request、tamper、Replay
     和 authority 负例必须迁移到保留原语的测试中，不得因删除阶段组合而丢失。
     M5.22 Cross-target Planner、旧 Campaign Planner 与 macro Intent 本轮不删：先由 M5.24 方法契约
     决定是否存在正式消费者。`campaign_model_call` 与 `stateless_agent_call` 也不先抽象；
     若退休 Campaign Planner 后只剩一个正式消费者，应直接删除旧数据模型而非为死路径
     增加公共抽象。本阶段的 identity 门禁是：所有保留主路径的 schema、digest、Trace、
     strict Replay 和 M5.23g 冻结结果不变；历史 CLI 不再可执行是有意的界面收缩，不属于
     identity 回归。M5.23R 不优化 exact-prefix reconstruction；M5.24 必须先报告
     work-to-kill、wall time、explored branches 和 reconstruction work，证明其为主要瓶颈后
     才能增加 checkpoint/snapshot 机制。
148. M5.23R2 冻结 protocol-neutral `StatelessCampaignSpec/v1` 与
     `StatelessCampaignAttempt/v1`：一个 Campaign attempt 精确绑定一个预声明
     `StatelessTraversalMethod`，canonical 可重复，seeded-uniform 的 seed 序列须在运行前冻结，
     Agent 使用相同 attempt 权限而不启用旧 Campaign Planner mode。Spec 同时绑定
     target/source/corpus identity、root/depth/item/search ceiling、strict Replay、read-only
     discovery 和 per-attempt 预算；source bundle 构造成本对每个 attempt 全额计费。
     target composition 只能返回 sealed discovery 或 typed failure，search/qualified execution/model
     work 分栏记录，execution evidence 不得夹带 model cost。Campaign 存储和恢复不引入
     新 Coordinator，attempt artifact 必须重验 request/spec/method/discovery/work digest。
     M5.23g 冻结 Agent evidence 已在零新 model call 下逐字段迁移到该契约。
     机械引用审计证明 M5.22 Cross-target 生产组合没有当前 CLI、Stateless Search、
     Campaign durable core 或 formal evaluator 消费者，因而其生产/测试闭包已从 HEAD
     退休；历史阶段文档、冻结工件与 Git checkpoint 不改。M5.23b OmniPaxos 迁移
     门禁改用 target-local fixture 并保持冻结 identity。旧 Campaign Planner 仍被两个现行
     CLI 消费，须等 Stateless target runner 替换后再删除，不得先破坏可恢复 Campaign。
149. M5.23R3 实现首个真实 target-local Stateless Campaign runner：输入必须显式给出
     M5.23e 冻结 root corpus，canonical repeats 或 seeded-uniform seed sequence 在 Campaign 运行前
     完整预声明。每个 attempt 在 0/28/54-decision 三个 root 上执行 depth=2、每 root
     6 items 的 exact-prefix Search，18 条路径均进入原 etcd/raft qualified executor 并执行
     primary + fresh strict Replay。target composition 强验证 discovery 后才通过 R2 通用
     provider 提交 sealed artifact；Campaign 每次 terminal attempt 持久化 checkpoint，恢复后按原
     request/spec/method 重新严格解码。新 `StatelessCampaignObservation/v1` 只报告方法
     identity、PSS 集合、qualified execution 计数与 source/search/primary/replay/model 完整
     WorkLedger，不生成 Coverage 百分比、完备度或 correctness verdict；strict reader 拒绝虚构
     `coverage_percent` 字段。canonical 恢复集成和 uniform seed 01 真实执行均完成
     18 次 qualified execution，无模型调用。预算由 source work、root 最大 decision、
     depth/items/search ceiling 机械推导，不按结果反向调整。M5.23R4 先迁移并审计
     `campaign-etcdraft-v1`/`campaign-etcdraft-agent-v1` 的真实消费者；只有旧入口无消费者后，
     才能删除 planned-attempt/macro Intent/Campaign Planner 闭包。
150. M5.23R4a 先完成旧消费者迁移的可独立验证切片。机械审计确认旧无模型
     `campaign-etcdraft-v1` 的 create/resume/summary 能力已被 R3 canonical/uniform Stateless
     runner 完整替代，因此退休其 CLI、Make 目标和专用 runner 测试。旧 Agent Campaign 仍保存
     不可丢失的调用生命周期语义，故先将其迁到 `statelessAgentCallJournal`：journal 可在不读取
     key 时创建并持久化 exact-byte intent；prepared 恢复后只有显式激活 key 才能 dispatch 一次；
     completed 只重放已保存 content/work；intent+dispatch 无 result 永久按 ambiguous 终止，禁止
     自动重试；rejected/failed 也保持 terminal。所有恢复均拒绝 symlink、未知文件、ordinal/root/
     request drift 并重验 digest。该能力当前只是 call-level sidecar，不得冒充 attempt-level 恢复。
     M5.23R4b 必须把 Agent method/knowledge/model budget、6 次 durable frontier call、18 次 qualified
     execution、discovery 和 model work 封入 R2/R3 的共同 Campaign attempt artifact；只有 mock
     create/resume/ambiguous 门禁通过后，才退休旧 Agent CLI 与 planned-attempt/macro Intent/Planner
     闭包。该迁移不得读取真实 key、产生新模型调用或提前开始 M5.24 holdout。
151. M5.23R4b 将 restricted Agent 实现为第三种 Stateless Campaign method，不再使用旧
     Campaign Planner mode。一个 attempt 同时封存 Agent method/knowledge/model allowance、6 次
     frontier call audit、18 次 qualified primary + strict Replay、read-only discovery 与完整
     WorkLedger。exact request 在凭证读取前写 intent；prepared 不推进 Campaign head
     也不占用终态输出路径；completed 只重放保存结果；dispatch-only 按 ambiguous
     terminal failure 记录且禁止重试。已提交 artifact 在产生 observation 前重读 sidecar
     exact intent/dispatch/result 并逐项比对 secret-free audit。旧 v3 非模型 Campaign 的空
     legacy 目录可恢复，但存在旧 Planner 内容时 fail closed。离线 mock 恢复和篡改门禁
     通过后，HEAD 删除 GuardedTestIntent、macro compiler、planned-attempt、旧 Campaign
     model-call、feedback/follow-up/B4、无消费者 Cross-target Planner 和 M5.23g 在线 pilot
     入口；冻结 M5.23g 工件只读验证保留。两个核心 composition 包 Go LOC 从 33,832
     降至 21,294，净减 12,538（37.1%）。本阶段不读真实 key、不调用模型。
     M5.24a 先冻结 private candidate/control curator pack、blind exposure、canonical/uniform/Agent
     三臂 spec、重复次数、work-to-kill 与 invalid-trial 规则。deferred/restart 的运维开销在
     可跨进程完整计费前不得进入单位成本排名；PSS/义务只作解释性诊断。

---

## 25. 可行性自审查

### 高可行

- 官方 etcd/raft 上的 Embedded Driver；
- 逻辑时钟、网络邮箱和严格消息调度；
- Ready 生命周期的显式 host operation；
- crash/restart durable model；
- 原始轨迹和严格重放；
- 固定 Core PSS IR、有限 Raft Mapping 和安全 Oracle；
- Profile 相对覆盖分数。

### 中等可行

- Core PSS IR 跨 CFT 实现复用；
- 共享 Adapter kit 降低第三目标边际接入成本；
- 保守 DPOR/SAMC 语义约简；
- Process/Proxy Driver；
- Agent 基于固定 Contract 自动生成高质量 Binding/Driver/见证；
- HotStuff/Tendermint 共用元模型；
- 有限 Twins BFT 行为。

### 低可行或不可作为承诺

- 对全新协议零配置自动接入；
- 自动推断正确 commit/quorum/lock 语义；
- 完全协议无关的 safety Oracle；
- 用有限测试证明异步协议全局活性；
- 用一个数字证明“测试全面”；
- 让 Agent 自动确认真实协议 bug；
- 在没有控制随机性、网络和存储边界时仍声称严格确定性。

总体判断：确定性控制、opaque input/result、保守 Core PSS、Agreement 和 partial capability admission
已在 Raft 与 leader-based Sequence Paxos 上成立；Runtime/Adapter/Replay/Oracle 足以支撑下一阶段研究，
不再是默认扩建对象。M5.23 的 exact-prefix、共同成本和只读 discovery 提供了可靠搜索底座，但真实
Agent-v1 在 6 次调用和 28,335 tokens 后退化为 canonical，没有产生新的 Action 顺序或 PSS 集合。

这说明项目没有偏离“共识测试”，却已经阶段性偏离“以 Agent 为主体的共识测试”。下一个主要风险不是
再增加一种工件身份或恢复状态，而是 Agent 是否能够提出测试假设、控制有意义的 episode 目标并根据
真实语义反馈修正。跨 CFT/BFT 的深层安全语义仍需要 Extended PSS 和协议 Oracle，但不应重写控制层。
真正的论文创新集中在：

1. 协议感知 Agent 如何把源码/知识转成可执行、可修正的测试假设；
2. Agent 如何经统一控制层指导真实共识执行，而不取得事实或 verdict 权限；
3. PSS/Risk/义务反馈是否帮助 Agent 更快到达隐藏缺陷，而不是形成自我评价；
4. 相同 Agent episode 接口能否在 etcd/raft 与 OmniPaxos 上复用；
5. 两 Agent 职责分解是否在相同模型总预算下真正优于单 Agent。

---

## 26. 参考研究

- [MODIST: Model Checking for Distributed Systems](https://www.usenix.org/legacy/events/nsdi09/tech/full_papers/yang/yang_html/index.html)
- [SAMC: Semantic-Aware Model Checking](https://www.usenix.org/conference/osdi14/technical-sessions/presentation/leesatapornwongsa)
- [NIST Ordered t-way Testing](https://www.nist.gov/publications/ordered-t-way-combinations-testing-state-based-systems)
- [Netrix](https://arxiv.org/abs/2303.05893)
- [Mallory](https://arxiv.org/abs/2305.02601)
- [ModelFuzz](https://arxiv.org/abs/2410.02307)
- [iMocket](https://conf.researchr.org/details/issta-2025/issta-2025-papers/14/Model-Checking-Guided-Incremental-Testing-for-Distributed-Systems)
- [SandTable](https://doi.org/10.1145/3627703.3650077)
- [Twins](https://arxiv.org/abs/2004.10617)
- [Liveness Checking of the HotStuff Protocol Family](https://arxiv.org/abs/2310.09006)
- [Agora](https://arxiv.org/html/2605.29910v1)
- [Themis: RPC-Driven Race-Directed Test Generation and Fuzzing](https://www.usenix.org/conference/nsdi26/presentation/cao)

---

## 27. 一句话方向检查

如果未来无法用下面这句话准确描述 ConsensusAtlas，项目就可能已经偏航：

> ConsensusAtlas 让协议感知 Agent 形成并修正测试假设、选择语义 episode 目标并调用统一搜索工具，
> 由共识 Control Runtime 将提议具体化为真实 enabled Action 并确定执行，由独立 Replay、Oracle 与
> Ledger 决定事实和结果，最终用跨协议、Agent 看不到的 candidate 根因检出、正确 control 误报和
> 完整成本评价单 Agent 与多 Agent 的测试效果。
