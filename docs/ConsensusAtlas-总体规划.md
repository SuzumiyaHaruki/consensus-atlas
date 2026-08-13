# ConsensusAtlas 总体规划

> 状态：Draft v2.0（A2R 架构收敛与减负）
> 日期：2026-08-13
> 适用分支：`feature/agentic-consensus-testing`

## 0. 一句话目标

> ConsensusAtlas 是一个利用协议感知 Agent 形成并修正测试假设、利用统一控制层在真实共识实现上确定执行、
> 再由独立 Replay、语义投影和 Oracle 评价结果的分布式共识测试系统。

“Agent”“共识”“测试”缺一不可：

- 没有 Agent，会退化为普通 model checking/fuzzing；
- 没有共识语义，会退化为通用故障调度器；
- 没有真实执行与独立判定，会退化为测试代码生成 demo。

Agentic 是架构目标；多 Agent 是否优于单 Agent 必须通过同预算实验验证，不能预设。

## 1. 最终用户流程

### 1.1 输入什么

接入一个新共识时，人工提供：

1. 共识实现或可运行构建；
2. 一个薄 Adapter，把已有接口映射为统一 Action/ProducedItem；
3. 协议文档、源码或有限的 Protocol Knowledge；
4. workload 与安全/活性性质；
5. 允许的故障范围、节点规模、测试时间和资源预算；
6. target-local Evidence → PSS/Risk/Oracle 投影。

目标不是“零人工理解协议”，而是把人工工作限制在一次接入事实与性质定义，不要求人工编写大量具体测试轨迹。

### 1.2 如何处理

```text
SUT + Adapter + knowledge + workload/fault/budget
                         |
                         v
              mechanical qualification
                         |
                         v
        Protocol/Hypothesis Agent（可选）
                         |
                    TestHypothesis
                         |
                         v
                  Explorer Agent
                         |
              bounded semantic episode
                         |
                         v
           trusted search + Control Runtime
                         |
              Trace / Evidence / result
                         |
          +--------------+---------------+
          |                              |
   PSS/Risk/obligation feedback      fresh Replay
          |                              |
          +----> Agent 修正计划           v
                                     Oracle
                                        |
                                        v
                           evaluator / session summary
```

Agent 决定测试方向和候选优先级。可信层决定 enabled Action、执行事实、语义真值和正式 verdict。

### 1.3 得到什么

一次测试 session 输出：

- finding：违反性质的最小可重放前缀，或“未发现正式 violation”；
- coverage evidence：固定义务覆盖、PSS/Risk 发现与时间顺序；
- efficiency：决策、primary/replay work、模型调用、token、wall time；
- qualification report：目标实际提供了哪些控制能力；
- session summary：运行数、有效/无效 episode、重放率、finding 数和成本。

任何百分比都必须写明 Profile、故障范围和可观察能力。不得称为协议正确率或剩余缺陷概率。

## 2. 控制层

### 2.1 统一 Action

公共 Action 只表达跨实现稳定的控制意图：

- deliver/drop/duplicate message；
- crash/restart；
- fire earliest due temporal item；
- partition/heal；
- complete host/application effect；
- invoke external workload。

Action 引用 Runtime 当前给出的 ID。Agent 不能构造任意消息、时间、节点 incarnation 或 payload。

### 2.2 Runtime 所有权

Control Runtime 负责：

- 保存尚未决定命运的消息；
- 只允许投递、丢弃、复制当前待处理消息；
- 用逻辑时间触发自然超时，而不是直接调用“成为 leader”等协议事件；
- 跟踪 crash/restart 与 incarnation；
- 区分 volatile/durable/application effect；
- 记录完整 Trace 和确定性随机输入；
- fresh Replay 同一动作序列。

Adapter 负责把一次 Action 映射到目标已有接口，并返回新 ProducedItem/Evidence。Adapter 不拥有全局调度。

### 2.3 能力差异

统一 Action 不等于统一控制强度。接入结果允许 partial：

- embedded/grey-box 目标可能交出消息和时间所有权；
- black-box 目标可能只能拦截部分网络或生命周期；
- 缺失能力必须显式报告，不能用 no-op 或猜测补齐。

当前跨实现证据：

- etcd/raft：完整 strict 主路径；
- OmniPaxos：非 Raft 的同 Runtime 路径；
- HashiCorp Raft：部分资格，代表黑盒接口上限；
- raft-rs 探索未取得资格，已从 HEAD 删除，不作为现行证据。

## 3. Agent 设计

### 3.1 Protocol/Hypothesis 职责

输入协议知识、能力和公开机械反馈，输出值得调查的语义假设，例如：

- leader change 与 in-flight proposal 的组合；
- quorum 变化前后的 commit/decision continuity；
- 持久化完成与消息释放之间的关系。

它不输出预期缺陷标签、可执行 Oracle 或完整动作脚本。

### 3.2 Explorer 职责

Explorer 接收 hypothesis、当前 prefix、PSS/Risk progress、候选队列和剩余预算，输出候选完整排序。
可信 validator 检查：

- 只引用当前候选；
- 无遗漏、重复或未知 ID；
- 不改变预算、故障范围和 projector；
- 不携带 verdict 字段。

Explorer 可以在一个 episode 内根据机械反馈修正，但不能看到正式 candidate/control 身份或 terminal verdict。

### 3.3 单 Agent 与多 Agent

第一条可用闭环先允许一个 Explorer 同时消费人工/固定 hypothesis。闭环稳定后再增加独立 Hypothesis Agent。
论文比较必须保持相同总模型 token、执行 work、root 和 exposure：

- deterministic semantic best-first；
- 单 Explorer；
- Hypothesis + Explorer；
- 必要时增加 Critic 的独立消融。

如果两 Agent 不优于单 Agent，系统仍是 Agentic testing system，但不能声称多 Agent 分工有效。

## 4. 语义反馈与覆盖

### 4.1 PSS

PSS 用来归一化协议状态，忽略 term 数值、节点编号或无关独立顺序造成的表面差异。它回答“发现了多少新的
协议语义状态/转换”，适合比较搜索效率，没有固定完备分母。

PSS 不单独构成质量分数。Agent 若只追逐新 PSS，可能生成许多易达但无测试价值的轨迹。

### 4.2 测试义务

义务是 Profile 内的有限目标，适合给出覆盖率。义务必须能表达时序和有限组合，例如：

```text
precondition -> trigger -> intermediate relation -> observation
```

组合由协议/能力约束产生，不做全笛卡尔积。不可达项必须通过独立分析或执行证据标记，不能为了提高比例直接删除。

### 4.3 RiskWitness

RiskWitness 是 hypothesis 与真实 prefix 之间的机械桥梁：报告已达到的 milestone 和第一个未达到的
milestone。它不是 Oracle，也不等同于缺陷。

### 4.4 结果表达

至少分开报告：

1.义务覆盖：在冻结 Profile 内取得强证据的比例；
2. PSS/Risk discovery：随 work 增长的状态、转换和 milestone 曲线；
3. finding：隐藏根因检出和正确 control 误报；
4. cost：执行、重放与模型成本。

不合成一个含义不明的总分。只有外部 candidate/control 结果才能支持 Agent 方法效果结论。

## 5. Oracle 与评价

通用 Oracle 只消费协议无关的最小决策投影，例如 participant、decision position 和 value digest。
target-local projector 可以理解协议 Evidence，但不能改变 Oracle 规则。

正式评价使用：

- 正确 control：预期不触发性质违反；
- candidate：包含一个独立已知根因或受控语义变化；
- 相同 workload、fault、预算、source exposure 和 evaluator；
- Agent 不见 candidate/control 标签、diff、known trigger 或 terminal verdict；
- fresh Replay 后才计入有效 finding。

主要指标：

- root-cause detection rate；
- correct-control false positive rate；
- work-to-first-finding；
- replay/reproduction rate；
- invalid episode rate；
- PSS/义务作为解释性指标。

发现当前版本的新问题是强案例，但不是项目成立的前置承诺。

## 6. 当前实现状态

### 已完成

- 协议无关 Action、ProducedItem、Adapter 和唯一 Control Runtime；
- Runtime-owned message、自然时间、crash/restart、effect、entropy 和 strict Replay；
- Adapter qualification 与 partial capability；
- etcd/raft qualified execution、PSS、Risk、Agreement Oracle 和 evaluator；
- OmniPaxos 的同 Runtime 非 Raft 路径；
- bounded exact-prefix DFS 和 deterministic semantic best-first；
- `TestHypothesis`、semantic episode、单 Explorer proposal/repair；
- provider 请求持久化、无凭证恢复和显式 opt-in 模型调用；
- 一次真实 DeepSeek 公开校准。

公开校准只证明 Explorer 能改变 etcd/raft 的搜索前缀。两臂都未达到完整 RiskWitness，不能证明 Agent 优势。

### 尚未完成

- Explorer 选择结果进入完整 qualified ExecutionBundle/PSS/Oracle；
- 多 episode 自动反馈循环；
- 用户给定运行时间后的统一 session summary；
- 非公开 candidate/control 重复实验；
- 同一 Agent episode 在第二协议上的复用；
- 多 Agent 同预算消融。

## 7. A2R 架构收敛原则

本阶段不增加新研究对象，集中删除和平坦化：

- 删除已被 semantic episode 替代的 trace/corpus/PSS 批处理搜索；
- 删除 Agent-v1 frontier-order Campaign，保留当前 Explorer 复用的调用持久化安全边界；
- 删除未资格化的 raft-rs 路径；
- 大型 Trace/bundle 放 ignored `artifacts/`，仓库只保留必要输入和紧凑摘要；
- 历史复现依赖 Git，不在 HEAD 保留多套可执行主线；
- README、CURRENT_STAGE、总体规划和架构只描述当前系统。

默认不新增 hash、冻结 contract、baseline 或 gate。只有能明确给出具体失败场景，并说明 Git、版本号、主键、
事务、唯一约束、类型和普通测试为何不足时，才允许增加。已有认证、数据安全、重放完整性和不可逆操作保护保留。

## 8. 接下来按什么顺序实现

### A3：单 episode 真正闭环

目标：把当前 Semantic Explorer 选中的路径送入已有 qualified executor，得到 ExecutionBundle、PSS、Risk、
fresh Replay 和 Oracle 结果。

约束：

- 复用现有 Trace、ExecutionBundle、WorkLedger 和 evaluator；
- 不建立新 Ledger/contract；
- 先用 etcd/raft fixture 与公开 calibration；
- 输出清楚区分 planning result 与 testing result。

完成判据：输入一个 hypothesis 和预算，系统能输出一个可重放测试结果，而不只是一段 prefix 比较。

### A4：配置时间内的测试 session

目标：复用现有 Campaign coordinator，把多个 semantic episode 串成：

```text
plan -> execute -> mechanical feedback -> revise -> next episode
```

停止条件只来自时间/工作量/episode 数等外部预算。输出 session summary，不让 Agent 根据隐藏 verdict 提前停止。

完成判据：用户给定目标、知识、Adapter、workload 和时长后，系统自动运行并输出 finding/coverage/cost。

### A5：第二协议复用

在 OmniPaxos 上复用同一 episode/session 接口，只增加 target-local Adapter、knowledge、mapping 和 projector。
记录目标专用新增代码量、qualification 差异和实际可运行义务。

完成判据：不修改公共 Action/Runtime/episode 数据流即可完成一次非 Raft session。

### A6：效果评测

准备非公开 candidate/control，比较 deterministic baseline、单 Explorer 和多 Agent。预注册重复次数、预算、
exposure 和主要指标；PSS/义务只解释结果，不代替 finding。

### A7：多 Agent 消融

在 A4/A5 稳定后加入独立 Hypothesis Agent，并在相同总预算下评价。只有结果支持时才保留更多角色。

## 9. 开发与验证节奏

每个小批次：

- 只运行受影响包；
- 先编译，再跑最小行为测试；
- 修改及时落地，避免长时间堆积。

每个完整阶段：

- `make test`；
- `go vet ./...`；
- `git diff --check`；
- 汇报生产代码、测试、Markdown、JSON 的净变化。

完整 race 只在明确发布/里程碑检查中运行，不在每个小阶段重复。普通测试禁止读取 key 或调用模型。

新增抽象原则：

- 至少两个现实消费者才进入通用层；
- target-local 逻辑不为“未来可能复用”提前抽象；
- 新文件必须替代重复责任或完成用户可见流程；
- 阶段进展不能只用新增文件数或 schema 数衡量。

## 10. 可行性自审查

高可行：

- embedded 共识库的消息/时间/生命周期控制；
- 确定性 Trace 与 fresh Replay；
- 有界安全 Oracle；
- Agent 基于机械语义反馈修正搜索；
- 配置预算内自动 session。

中等可行：

- Process/Proxy 目标达到与 embedded 相同控制强度；
- Core PSS 跨更多 leader-based CFT 复用；
- 自动生成 Adapter 草案并由 qualification 验收；
- multi-Agent 在同预算下产生稳定增益。

不能承诺：

- 任意新协议零配置接入；
- 纯黑盒获得完整消息、时间和持久化所有权；
- 自动推断正确 quorum/lock/commit 语义；
- 有限测试证明异步协议全局活性；
- 用一个数字证明“测试全面”；
- Agent 自动确认真实协议缺陷。

## 11. 方向检查

如果后续工作不能用下面这句话描述，就应停止扩展并重新审查：

> 输入共识实现、薄 Adapter、协议知识、workload 和预算；Agent 形成并修正语义测试计划，Control Runtime
> 在真实实现上执行，系统输出可重放 finding、PSS/义务/Risk 统计和完整成本，由独立 Oracle/evaluator
> 而不是 Agent 决定测试结果。
