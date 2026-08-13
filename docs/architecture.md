# ConsensusAtlas 架构

日期：2026-08-13

本文件描述当前代码，而不是历史阶段。系统只保留一条权威主线：Agent 提出语义探索偏好，可信内核落实为
真实 enabled Action，Replay/Oracle/evaluator 独立判定结果。

## 1. 端到端数据流

```text
SUT implementation
      +
thin Adapter -----------> Conformance / Qualification
      +                              |
protocol knowledge                   v
      +                     admitted target execution
workload/fault/budget                 |
                                     v
TestHypothesis -> Semantic Explorer -> exact-prefix search
                                          |
                                          v
                                    Control Runtime
                                   /       |       \
                              Trace     Evidence    client result
                                |          |              |
                                +----------+--------------+
                                           |
                                  PSS / Risk projection
                                   /               \
                         planning feedback       fresh Replay
                                                     |
                                                     v
                                             Oracle / evaluator
```

### 输入

- SUT：被测共识库或进程；
- Adapter：把目标已有接口映射到统一消息、时间、生命周期、effect、外部输入和 Evidence；
- protocol knowledge：Agent 可以读取的协议事实与风险描述；
- semantic mapping：可信代码把 Evidence 映射为 PSS/Risk；
- workload、fault envelope 和固定资源预算。

### 输出

- 可重放 Trace 与执行结果；
- PSS/Risk/义务统计；
- Oracle finding 或 evaluator 的 candidate/control 结果；
- primary/replay/model 成本。

## 2. 信任边界

```text
untrusted / bounded                         trusted

Agent hypothesis/explanation       Adapter qualification
candidate ordering                 enabled Action computation
search preference            ---> Control Runtime execution
                                      Evidence projection
                                      PSS/Risk truth
                                      fresh Replay
                                      Oracle/evaluator verdict
```

Agent 可以：

- 形成 `TestHypothesis`；
- 选择语义目标；
- 对当前可信候选排序；
- 根据不可达、near-miss、PSS/Risk 变化修正后续提议。

Agent 不可以：

- 创造消息、节点、时间或 enabled Action；
- 修改 fault/budget、Adapter qualification 或当前 Oracle；
- 声称某个 PSS/Risk/义务已经满足；
- 决定 candidate/control 身份或正式 verdict。

因此 Agent 可以犯规划错误，但不能把错误解释写成执行事实。

## 3. 核心模块

| 模块 | 责任 | 不应承担 |
|---|---|---|
| `internal/control` | Action、ProducedItem、Adapter 接口 | 协议语义和搜索策略 |
| `internal/controlruntime` | 消息/时间/生命周期/effect 的确定执行 | Agent 决策和 Oracle |
| `internal/conformance` | 机械验证 Adapter 声明 | 推断协议正确性 |
| `internal/controlexperiment` | qualified execution、搜索、episode、Campaign、成本 | etcd/raft 字段解析 |
| `internal/psscore` | 固定 Core PSS 投影 | 正式 defect verdict |
| `internal/semantic` | 协议族 RiskWitness 结构 | 目标实现 Evidence 解码 |
| `internal/oracle` | 独立性质检查 | 搜索指导 |
| `internal/defectbench` | candidate/control 评价 | Agent 在线反馈 |
| `adapters/*` | 目标专用接口映射与 Evidence | 通用搜索和评分 |
| `cmd/control-experiment` | etcd/raft 组合、CLI、模型传输 | 新的 Runtime |

协议耦合只能位于 Adapter、target-local projector、workload router 和组合入口。通用 Runtime、搜索、PSS
容器和 evaluator 不导入 etcd/raft/HashiCorp/OmniPaxos 类型。

## 4. Control Runtime 语义

Runtime 拥有所有待调度项：

- message：可投递、丢弃、复制或因分区暂时不可达；
- temporal item：只有最早到期的一组可以触发；选择超时会把逻辑时间推进到其 deadline；
- host/application effect：显式完成并记录结果；
- lifecycle：crash/restart 改变 incarnation，并遵守持久化模型；
- external invoke：只通过目标的 workload router 进入系统。

Adapter 在一次动作后返回新的 ProducedItem 与 Evidence；它不自行决定全局投递顺序。Runtime 对 ActionID、
item 状态和节点生命周期做机械校验，并记录足以 fresh Replay 的 Trace。

## 5. 新协议接入

最小 Adapter 只需要实现目标真正具备的能力，不要求伪造统一强度：

1. 创建/恢复节点；
2. 注入 Runtime 已选择的消息、时间或输入；
3. 把目标新产生的消息/effect/结果交还 Runtime；
4. 导出稳定 Evidence；
5. 声明可机械验证的能力。

接入结果可以是 partial。统一 Action 表示统一调用语义，不表示所有目标都能提供消息所有权、自然时间、
持久化切点等同等控制。缺失能力必须显式记录，不能通过 Adapter 内部猜测或 no-op 伪装。

当前证据：

- etcd/raft：完整 strict 主路径；
- OmniPaxos：非 Raft 的同 Runtime 严格路径；
- HashiCorp Raft：部分能力，用于暴露黑盒接口的真实上限；
- raft-rs：未取得资格的探索路径已从 HEAD 删除，不再作为当前适配证据。

## 6. Agent 执行单元

Agent 的单位不是“每一步自由选一个 Action”，而是有界 semantic episode：

1. 可信层给出当前 hypothesis、prefix、语义候选和剩余预算；
2. Explorer 返回候选完整排列；
3. validator 拒绝遗漏、重复、未知或越权候选；
4. search kernel 物化一个或多个真实 prefix；
5. 可信层返回机械反馈；
6. Explorer 可以在预算内再次修正。

当前 A2b3b 已完成 1 次真实 provider call 和结果恢复，但仍停在 prefix/Risk 比较。下一步必须把选中路径送回
完整 qualified execution、PSS 与 Oracle，才构成真正测试闭环。

## 7. 评价面

正式结果分开报告，不合成含义不明的总分：

- defect effectiveness：隐藏 candidate 根因检出、正确 control 误报、复现率；
- semantic coverage：固定义务覆盖和 PSS/Risk 发现；
- efficiency：decisions、primary/replay work、模型调用和 token；
- portability：新 Adapter 的目标专用代码量与获得的控制能力。

PSS 状态数是无固定分母的搜索反馈；义务覆盖率是在 Profile 边界内的成果指标。两者都不能证明协议正确，
也不能单独证明 Agent 优势。

## 8. 工件与代码保留规则

- Git 保存历史；HEAD 只保存当前代码、必要输入和紧凑结果；
- 完整 Trace/bundle 默认写入 ignored `artifacts/`，不提交重复展开的 JSON；
- 阶段文档记录结论和边界，不复制完整对象；
- 已被当前路径替代的 CLI、数据模型和测试一起删除；
- 默认不新增 hash、冻结 contract、baseline 或 gate。只有存在一个具体失败场景，并能说明 Git、版本号、
  类型系统、普通测试和常规存储约束为何不足时，才考虑增加；
- 新抽象至少应有两个现实消费者，否则优先留在 target-local composition；
- 每个阶段报告生产代码、测试、Markdown 和 JSON 的净变化，防止“文件增加等于进展”。

## 9. 检查节奏

开发过程中只运行受影响包和关键集成测试。一个完整阶段结束时再运行 `make test`、`go vet ./...` 和
`git diff --check`。完整 race 只用于明确的发布或里程碑检查；普通测试不访问外部模型。
