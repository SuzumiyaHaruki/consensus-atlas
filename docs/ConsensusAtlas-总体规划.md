# ConsensusAtlas 总体规划

> 文档性质：项目方向约束、总体架构和阶段验收基线
> 状态：Draft v1.1
> 日期：2026-08-05
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
6. 如何让人工只提供一次最小 Protocol Charter 或选择已有 Family Pack，之后由 Agent 自动完成语义提取、代码接入、覆盖规划、测试生成、失败修复和反例归纳，同时不让 Agent 进入可信执行、判定和计分路径？

### 1.2 研究主线

项目固定采用以下主线：

> **最小协议知识 + Agent 自动接入 + 固定语义覆盖义务 + 覆盖债务驱动的多 Agent 测试闭环 + 确定性执行与 Oracle**

人工只提供协议实现、协议文档/形式化模型、核心安全与活性目标、故障模型和有界测试范围，或者直接选择已有 Family Pack。Knowledge/Obligation Agent 负责生成协议差异和结构化覆盖义务；Onboarding Agent 负责生成薄 Driver、Binding 和基础见证；Planner/Scenario/Search/Critic Agent 围绕 Coverage Ledger 中的未覆盖债务持续产生和修复测试。所有 Agent 产物只能由确定性 Coordinator、Runtime、Evidence Matcher、Replay、Conformance 和 Oracle 接受。

系统的总原则是：

> **最大化 Agent 的工作量，最小化 Agent 的判定权。**

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

长期目标是：

```text
通用 Runtime + 薄 Protocol Driver + 可复用 Family Pack/PSS
```

而不是：

```text
每个协议一个包含全部逻辑的厚重 Adapter
```

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

接入成功并不等于完成测试。测试阶段必须维护只追加的 Coverage Ledger：

```text
Frozen Profile -> Coverage Debt -> Agent Plan -> Deterministic Concretizer
       ^                                                |
       |                                                v
Score + Evidence <- Oracle/Replay/Conformance <- Runtime Trace
```

Planner Agent 只能从冻结分母选择目标；Scenario Agent 只能输出受限 Test Plan DSL；Search Agent 只能通过 Runtime 暴露的 enabled events 调用 Random/DFS/DPOR/约束搜索；Critic Agent 只能依据机械 finding 提交新计划。Agent 不能直接写入 Ledger，不能自报 observation 取得覆盖，也不能在当前 campaign 中增加、删除或降权义务。

---

## 3. 总体架构

正式执行之前先建立确定性知识、自动接入和冻结覆盖分母：

```text
Minimal Protocol Charter + Family Pack
               |
               v
 Knowledge/Obligation Agent proposals
               |
 schema/source/fixture/differential validation
               v
  Frozen Contract/PSS/Profile obligations
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
                 │ PSS + Profile + System Binding       │
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
                 │ Thin Protocol Driver                 │
                 │ 原生 API 调用、编码转换、状态观察    │
                 └──────────────────┬───────────────────┘
                                    │
                          Official SUT / Process
                                    │
                                    v
                    原始执行轨迹 + Decision Log
                                    │
              ┌─────────────────────┼──────────────────────┐
              v                     v                      v
       Semantic Abstraction    Deterministic Oracle   Replay Verifier
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

Agent 平面按产物和权限拆分，而不是用一个模型同时生成、执行和批准自己的结果：

1. `Knowledge Agent`：从论文、文档、代码和形式化模型提取带来源的协议事实与差异草案；
2. `Onboarding Agent`：生成薄 Driver、Binding、状态映射和基础见证；
3. `Obligation Agent`：基于 Family Pack、协议差异和有界 Profile 生成结构化覆盖义务草案；
4. `Coverage Planner Agent`：读取 Coverage Debt，选择下一批高价值目标；
5. `Scenario Agent`：将目标细化为受限 Test Plan DSL；
6. `Search Agent`：调用确定性 concretizer 及 Random/DFS/DPOR 等后端实现计划；
7. `Critic/Repair Agent`：根据机械 finding 修复计划、场景或接入代码；
8. `Failure Analyst`：最小化和解释反例，无权确认真实缺陷。

Coordinator、Runtime、Evidence Matcher、Coverage Ledger、Replay、Conformance 和 Oracle 都是确定性程序，不是投票 Agent。Agent 可以读取共享 Blackboard 中的 `proposed/validated/rejected/superseded` 产物，只有 Coordinator 能写入 `validated`。

---

## 4. 通用 Runtime 与薄 Driver

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

### 4.3 Driver 的最小职责

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

可信执行路径不得依赖 wall-clock sleep。Runtime 维护逻辑时间，显式调度：

- node-local tick；
- election/campaign timeout；
- heartbeat timeout；
- view/round timeout；
- message delay boundary；
- GST；
- post-GST delivery deadline。

### 7.2 随机超时

如果原实现内部随机选择 timeout，正式确定性测试必须满足至少一种方式：

1. 原生 API 可注入 RNG/timeout；
2. 所有随机选择被捕获进 Decision Log，并可强制重放；
3. 使用官方支持的确定性配置；
4. Profile 将自然 timeout 标为 Unsupported，改用显式 campaign/timeout 输入完成第一阶段测试。

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

PSS 是轻量、可版本化的协议语义层。它不是完整可执行规范，也不能替代 TLA+/Ivy 模型；其目标是提供覆盖分母、抽象、规范化和 Oracle 所需的最低语义。正常流程中它由人工选择的 Family Pack 与最小差异知识组成 Protocol Knowledge Contract，而不是由 Agent 自由提出后再逐项审批。

### 8.2 必需内容

```text
identity:
  nodes, clients, configurations, faulty identities

relative_time:
  epoch/view/term, phase/round, timeout classes, GST relation

semantic_state:
  role, leader relation, proposal/log relation,
  accepted/voted/locked relation, certificate/quorum,
  commit/finalize frontier, durable frontier, applied frontier

semantic_events:
  propose, vote, certificate, receive, release, deliver,
  timeout, view-change, persist, sync, crash, restart,
  commit, apply, membership change

properties:
  agreement, prefix/finality, vote/lock invariants,
  durability, apply-after-commit, progress conditions

equivalence:
  node permutation, epoch translation, value renaming,
  log/QC shape, conservative independence
```

### 8.3 Family Pack

PSS 应支持家族继承：

- `raft-family`；
- `paxos-family`；
- `hotstuff-family`；
- `tendermint-family`。

Family Pack 提供确定的字段、关系、常见 Oracle、切点和历史风险模式；具体协议 Contract 只覆盖家族差异。Contract 一旦进入正式运行就只有版本身份，没有逐映射 `candidate`。实现可观察性与支持状态由自动接入验证器生成，不回写 Contract。

### 8.4 Raft 抽象示例

原始 term 数值应抽象为相对关系：

```text
stale / current / future
```

日志应抽象为形状：

```text
same
prefix
extension
conflict-before-commit
conflict-after-commit
```

节点 ID、提案值、term 绝对数值改变，但上述关系和因果图不变时，应获得相同语义场景键。

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

这些 Oracle 由 Family Pack 提供骨架，由具体 PSS 冻结证据映射。

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

SCS 的分母是冻结的结构化 Coverage Obligation，不是运行时发现的 PSS 状态数。义务内部默认等权，显式非零权重只能来自冻结 Profile；类别权重和阈值必须版本化。正式结果必须将 SCS 完整称为“指定有界 Profile 下的强义务覆盖率”。

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

公平评测必须固定 SUT/Driver/PSS/Profile/初始状态/工作负载，定义统一 measurement window，以 scheduler decision 为主预算，并同时报告时间/计算成本。所有方法共有的 bootstrap 前缀应排除或单独显示。PSS 必须消除节点名、绝对 term/index 和 value 等无关重命名，并避免在 persist/sync/emit/apply 等 host microstep 上重复采样。

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

---

## 15. Agent 系统

### 15.1 人工与 Agent 的稳定边界

人工的正常输入缩小为 Minimal Protocol Charter：仓库/实现身份、协议家族或新家族声明、论文/文档/形式化模型、核心安全与活性目标、故障模型和有界测试范围。已有家族由 Agent 生成协议差异、接入和义务草案，并通过机械验证自动冻结；完全新家族仍需设计者一次性确认无法从实现本身推出的核心不变式和环境假设。

冻结后的 Contract/PSS/Profile 是当次运行唯一语义真值边界。Agent 可以为下一版本提出候选事实和义务，但不能修改当前 digest、覆盖分母、等价关系、Oracle 或权重。系统不设置逐事实、逐测试的人工 Gate。

### 15.2 Onboarding Agent

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

Binding 中不允许 Agent 提供自定义 assertion、`confirmed` 状态或覆盖权重。预期 label、monitor、replay、conformance 和 Oracle 要求全部来自 Contract 与验证器。Binding-only 阶段只允许模型从受限的 Driver Manifest 和候选场景集合中生成 Binding/witness 引用；Contract-only 阶段的 Driver 源码只能进入独立 patch/build 沙箱，并先于 witness 生成通过单独的 API/PSS fixtures。

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

### 15.4 Strategy Agent

输入已验证 Profile、覆盖债务、PSS 发现记录和历史失败，输出声明式语义目标/假设。确定性 DFS/DPOR/combinatorial concretizer 负责在 enabled events 上实现目标。Agent 不直接修改 Runtime 状态，也不单独决定覆盖成立。

与 Random/DFS/DPOR 比较时固定 scheduler-decision 预算与 measurement window，同时报告模型调用、token、墙钟时间和费用。无固定分母的 PSS 状态发现曲线评价搜索效率，固定 Contract/Profile 分数评价最终测试债务；两者不混合。

Strategy Agent 在实现上拆成 Planner、Scenario、Search 和 Critic 四个角色。Planner 读取风险优先 Coverage Debt；Scenario 只能生成 schema 约束的 Test Plan；Search 通过确定性 concretizer 调用现有搜索器；Critic 只依据 `PRECONDITION_UNREACHABLE`、`BUDGET_EXHAUSTED`、`ORACLE_NOT_ACTIVATED`、`REPLAY_MISMATCH` 等机械 finding 生成完整替代计划。任何 Agent 都不能直接执行 SUT API 或写 Coverage Ledger。

### 15.5 Failure Analyst（可选）

负责语义化解释、提议最小化、生成时间线和辅助区分 SUT/Driver/PSS/Runtime/Oracle 问题；不能确认真实协议缺陷。

### 15.6 旧接入路径退场

Repo/API Scout、Source Catalog、required/acceptable/additional 标签、旧 Integration Pack 及其人工 Gate 已从主仓库删除。如需进行历史对照实验，应从 Git 历史在独立分支恢复，不与正常接入主线并存。

### 15.7 Agent 记忆与评价

长期记忆只存储经验证的 Contract/Binding/Driver identity、Coverage Ledger、机械 finding、稳定反例和版本变更。未通过验证的 Agent 解释不得成为下轮事实。

Onboarding Agent 的主要指标是：从 Contract 到首个 validated Profile 的时间、自动重试次数、人工修改 LOC/人工决策数、见证通过率、错误 Binding 拦截率、Family Pack 复用比例，以及与专家实现的差分运行结果。正式模型实验必须固定 Contract/prompt/tool-policy digest，记录模型版本、采样参数、token、工具调用、墙钟时间、失败/重试和费用。holdout 实现是跨实现泛化的必要证据。

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
- toy Driver 与 etcd/raft Driver 使用相同 Runtime；
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
  atlas/                     CLI

internal/
  runtime/                   确定性事件、逻辑时钟、网络、故障
  mailbox/                   message lifecycle 与 link sequence
  hostops/                   batch、persist/sync/release/apply/ack
  replay/                    Decision Log 与严格校验
  trace/                     raw trace 与工件格式
  scenario/                  声明式 DSL
  canonical/                 structural + semantic graph
  oracle/                    通用 Oracle
  coverage/                  ledger、profile compiler、score

drivers/
  toy/
  etcdraft/                  唯一允许 import 官方 etcd/raft 的实现
  process/                   通用进程/代理接入基础设施

families/
  raft/
    pss/
    oracles/
    patterns/
  hotstuff/
  tendermint/

profiles/
scenarios/
agents/
docs/
  adr/
artifacts/
```

实际重构可以渐进完成，但依赖方向必须保持：

```text
runtime -> core contracts
drivers -> runtime/core contracts + SUT
runtime -X-> any specific consensus package
oracle/coverage -X-> Agent
```

---

## 19. 阶段路线图

### M0：方向冻结与核心契约

交付：

- 本总体规划；
- Runtime/Driver/Batch/Message/Capability ADR；
- trace 与 Decision Log schema；
- toy conformance tests；
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
DeepSeek Planner Campaign 均已实现。v0.1 live run 在官方 etcd/raft 上取得
16/55 强覆盖，随后对同一个 leader 前置条件错误连续生成等价计划；Coordinator
正确去重并在 no-progress 上限停止。可信闭环已成立，但单 Planner 尚未证明能够
自动修复失败计划，因此 M4.6 仍未退出。

退出条件：先使用可信专家 etcd/raft Driver，在无人修改 Ledger 的情况下，至少一个 Agent 能根据 Coverage Debt 生成计划、接收机械失败、修复计划并使固定覆盖率真实上升。

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

当前进度：第一个 model-backed Generator 已完成 Binding-only 阶段，使用隔离子进程、私有 key 文件、固定 JSON schema、温度 0、关闭 thinking、完整反馈和逐轮 digest/token 审计。etcd/raft Contract-only 路径已经加入完整源码 Proposal、无网络 bubblewrap、受限 import、离线构建、Raft PSS 逐步检查与可信 evidence 重建。代表性 `v5` 实验第 2 轮完成编译和 bootstrap host operations，但因未选主即 propose 而失败，后续五轮没有产生实质源码修复；因此 Driver/witness 自动接入尚未成功，M5 不退出。

### M6：第二实现与迁移性

接入另一个 Raft/Paxos 实现，验证：

- Runtime 零协议修改；
- Family PSS 可复用比例；
- Driver 人工工作量；
- 分数跨实现可比性；
- PSS 映射错误率。

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

1. SCS 是否比代码覆盖、随机调度数、状态数更能预测缺陷检出能力？
2. term 平移、节点/值重命名后，语义键是否保持不变？
3. 保守偏序约简能节省多少执行，又是否遗漏已知/隐藏缺陷？
4. Agent 是否提高覆盖增长速度和单位时间收益，而不改变最终判定？
5. 语义/接入 Agent 是否在不降低 conformance 的前提下减少首次可控测试时间、人工 LOC 和审核成本？
6. PSS 和 Family Pack 能否迁移到第二实现？
7. 一个全新协议的接入时间、人工审核量和错误率是多少？
8. 小版本升级后 Profile 分数和 canonical key 是否稳定？
9. Capability 不足对最终分数和缺陷检出率有何影响？

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
| Contract/PSS 本身错误        | 错误规范化或错误 Oracle | Family Pack 复用、版本审查、差分/突变校准 |
| Driver 过厚                  | 跨协议复用失败          | Runtime/Driver 依赖测试和代码审查清单     |
| Runtime 硬编码 Raft          | 第二实现无法接入        | M6 前持续进行协议 import audit            |
| 消息立即投递                 | 丢失大量调度和故障场景  | 强制 produced/released/delivered 状态机   |
| MemoryStorage 被当作 durable | crash/restart 结果失真  | visible/durable image 分层                |
| 非确定 RNG/时间              | replay 证据不可信       | 注入、记录重放或 Unsupported              |
| selector 按队列位置          | replay 投递错误消息     | 稳定 ID + 严格字段校验                    |
| 错误 independence            | 系统性漏 bug            | 保守规则、交换测试、候选不生效            |
| Agent 动态改分母             | 分数不可比较            | Contract digest 与确定性 Profile 编译     |
| Oracle 未被真正激活          | 空洞高覆盖              | Property activation atoms                 |
| 权重主观                     | 分数缺少科学依据        | 隐藏突变体和历史缺陷校准                  |
| 只在一个 Raft 上有效         | 贡献退化为专用 Harness  | 以第二实现作为阶段性硬验收                |
| 把活性当固定超时             | 大量误报                | GST、公平性、temperature/lasso 独立模块   |
| Agent 误报被当成缺陷         | 结果不可信              | Oracle、重放、最小化、官方构建、人工确认  |

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
- [ ] 是否有第二 Driver 或 toy Driver 测试证明通用性？
- [ ] 报告是否同时给出分数向量、能力和失败，而非单一数字？

任一关键项不能回答时，功能不应直接进入正式评测路径。

---

## 23. 当前立即执行顺序

截至 2026-08-05 的落地状态：

1. [X] 定义 Protocol Knowledge Contract v1、严格 Go 模型、canonical digest 和 JSON schema。
2. [X] 实现 Contract 到 Profile 的确定性编译；实现支持状态不影响 atom 分母。
3. [X] 定义 Agent Binding v1；Agent 只能映射 capability/operation 和引用 witness，不能提供自定义验收断言。
4. [X] 实现自动验证器：digest/identity、Manifest、operation kind、contract label/monitor、双重 replay、conformance 和 Oracle。
5. [X] 实现确定性 Coordinator/Generator 接口和 finding 反馈环。
6. [X] 为 etcd/raft 编写 12 项 Contract 和实现 Binding；机械确认 7/9 capabilities、10/12 obligations，两个 Unsupported atom 仍保留分母。
7. [X] 添加伪造 Contract digest、虚构 runtime capability 和缺失 witness 负例，证明 Agent 无法绕过机械验收。
8. [X] 将 `make run-raft` 和 Explorer 的正常路径切换到自动验证产生的 Profile。
9. [X] 实现第一个 model-backed `autoonboard.Generator`，先只生成 Binding/witness；DeepSeek V4 Flash 在真实 API smoke test 中经两轮机械反馈通过，产物与静态基线 Profile 一致。
10. [X] Contract-only 原型已加入完整提案、路径/AST/import 限制、无网络构建沙箱、组件 digest、Raft PSS 逐步检查和可信 evidence 重建；代表性 v5 未收敛，证明单体 Driver+witness 任务需要拆分。
11. [X] 实现 Coverage Kernel v2：结构化 Coverage Obligation、Profile/Manifest digest、Evidence Matcher、跨运行 Coverage Ledger、风险优先 Coverage Debt、私有可信状态和可下钻强证据；现有 etcd/raft 基线已迁移且仍为 10/12、85.42。
12. [X] 由 Raft Family Pack + 有界 Campaign Spec 机械展开 55 项 etcd/raft 转换、顺序、故障切点、边界和属性激活分母；12 项 Profile 仅保留为接入质量指标。Family matcher 使用冻结角色/日志/分区证据，当前 53 项 Supported、2 项 Unsupported，单场景审计基线为 25/55、44.41。
13. [X] 定义受限 Test Plan DSL、Profile digest/全局预算约束和确定性 concretizer；实现多运行 Campaign，使手写计划围绕 Coverage Debt 调用 Random/DFS，每条 decision log 强制重放后由 Oracle/Conformance/Evidence Matcher 自动更新 Ledger。etcd/raft 专家基线为 39/55、66.38，剩余 14 项可行动债务，33 runs/1624 decisions 全部重放稳定且无执行错误/Oracle 违规。
14. [X] 实现私有增量 Campaign Session、hash-chained Agent Blackboard 与确定性 Coordinator，加入有界重试、run/decision/token/no-progress 预算、ID/协议因果结构重复检测和只追加审计；脚本化负例证明 Agent 不能直接写 Ledger 或绕过拒绝路径。
15. [ ] 依次接入 DeepSeek Planner、Scenario 和 Critic Agent，先使用已验证的专家 etcd/raft Driver，证明 Agent 能自动修复失败计划并提高固定覆盖率。第一个单体 DeepSeek Planner 已接入并完成 live run：16/55、31.00、128 decisions、36534 tokens，因连续重复不可执行计划在第 4 轮机械停止；Scenario/Critic 拆分和自动修复目标仍未完成。
16. [ ] 增加统一 Campaign CLI 和报告：主指标为固定义务覆盖率，PSD、Oracle failure、成本和方法对比独立展示。
17. [ ] 将自动接入拆成 Driver-only fixtures 闭环和固定 Driver 后的 Binding/witness 闭环，加入隐藏专家 Driver 差分行为见证。
18. [ ] 接入 Knowledge/Obligation Agent；同家族协议尽量自动冻结差异 Contract/Profile，新家族只保留一次核心语义确认。
19. [ ] 在一个开发期未见的第二 Raft/Paxos 实现上验证 Family Pack 复用、自动接入和测试闭环成功率。
20. [ ] 完成 DPOR/causal graph、多 seed、历史缺陷、隐藏语义突变体和 Agent 消融实验。

当前主线不再继续盲目增加 Contract-only 或单 Planner 模型重试。Coverage Kernel
v2、有界 Raft Campaign 分母、Test Plan 闭环、Blackboard/Coordinator 和首个
DeepSeek Planner 已完成阶段性冻结；结果证明权限边界可靠，但单体 Planner 的
failure repair 不可靠。下一步继续第 15 项，将 Coverage Planner、Scenario 和
Critic 拆成窄角色，并使用相同 DSL/可信内核做无反馈、单 Agent、多 Agent 消融。
达到可重复修复后，再补齐第 16 项统一成本报告和第 17 项分阶段自动接入。

---

## 24. 当前冻结的关键决策

除非 ADR 明确修改，以下决策视为冻结：

1. 项目名称为 ConsensusAtlas。
2. 核心 Runtime 和可信执行链使用 Go。
3. Python 主要用于 Agent/实验编排，不进入执行、Oracle 和评分可信路径。
4. 覆盖率相对于版本化有限 Profile，不宣称绝对全面性。
5. PSS 是默认语义输入；已有形式化模型作为增强插件，不是强制前提。
6. ESOT 是高风险补充层，不是唯一覆盖分母。
7. 场景等价基于保守规范化偏序因果图。
8. Agent 不能修改当次分母、Oracle 或等价关系。
9. 接入采用通用 Runtime + Thin Driver，避免厚重的协议专用 Adapter。
10. 目标系统默认使用官方未修改实现。
11. 消息必须区分 Produced、Released 和 Delivered/Dropped。
12. visible、durable、applied 和 network mailbox 状态不得混淆。
13. 安全覆盖与活性覆盖分别定义和报告。
14. etcd/raft 是第一实现，但第二实现是通用性主张的必要验收。
15. 覆盖 FAIL 场景仍算覆盖，覆盖和正确性分别报告。
16. Minimal Protocol Charter、Family Pack 和新家族核心不变式构成最小人工信任根；Agent 可以生成 Contract/PSS/Profile/Driver/测试草案，冻结后只有 digest 标识的 validated 产物能成为当次语义真值。
17. 正常接入不设置逐事实人工 Gate；是否通过只由 digest、编译、conformance、重放、Oracle 和 Contract 见证机械决定。
18. Unsupported 不会从 Contract/Profile 分母中删除。
19. Coordinator 是确定性程序，不是有权修改结论的 Agent。
20. 最大化 Agent 的工作量，最小化 Agent 的判定权；Agent 只能提交 proposal，可信内核才能写入 validated。
21. 最终主百分比来自冻结 Coverage Obligation 分母；PSS 状态发现只用于搜索效率比较。
22. Agent 测试生成以 Coverage Debt 为目标，经 Test Plan DSL、确定性 concretizer、Runtime 和 Evidence Matcher 形成闭环。
23. 自动接入与覆盖驱动测试先分开验证，再组成端到端系统。

---

## 25. 可行性自审查

### 高可行

- 官方 etcd/raft 上的 Embedded Driver；
- 逻辑时钟、网络邮箱和严格消息调度；
- Ready 生命周期的显式 host operation；
- crash/restart durable model；
- 原始轨迹和严格重放；
- 有限 Raft PSS 和安全 Oracle；
- Profile 相对覆盖分数。

### 中等可行

- Raft Family Pack 跨实现复用；
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

总体判断：该路线在一个 Raft 实现上高度可落地，跨同一家族具有合理复用前景，跨 CFT/BFT 家族需要 Family Pack 和协议 Oracle。真正的论文创新应集中在：

1. PSS 如何以较低成本提供足够可靠的语义；
2. 执行如何归约为保守、稳定、有意义的偏序场景；
3. SCS 是否经过隐藏缺陷/突变体实验验证，能够预测测试能力；
4. Onboarding Agent 是否能从固定 Contract 自动产生 validated Profile，降低陌生系统接入成本且保持与专家实现相当的差分行为；
5. 场景规划 Agent 是否只提高探索效率，而不损害可信性。

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

---

## 27. 一句话方向检查

如果未来无法用下面这句话准确描述 ConsensusAtlas，项目就可能已经偏航：

> ConsensusAtlas 以最小 Protocol Charter 和可复用 Family Pack 为信任根，让 Knowledge/Onboarding/Obligation Agent 自动完成语义、接入和固定覆盖义务，让 Planner/Scenario/Search/Critic Agent 围绕 Coverage Debt 持续生成并修复测试，最终只由确定性 Runtime、Evidence Matcher、Replay、Conformance、Oracle 和 Coverage Ledger 判定覆盖与结果。
