# 当前阶段

日期：2026-08-15

分支：`feature/agentic-consensus-testing`

阶段：A9e1 Agentic input migration 已完成；下一步 A9e2 capability / Oracle adequacy pilot

## 一句话状态

ConsensusAtlas 当前是一个面向 leader-based CFT 共识库、运行在受控协议/host-order 环境中的可信 Agentic
测试原型。A9d6 已用真实模型跑通一条 OmniPaxos 双 Agent 闭环；A9e1 已把 Agent 输入从“预置 Risk/测试题”
迁移为 Primer、Property 与 Historical Issue Pattern，并恢复普通全量测试与 race manifest 绿色。完整 race 的
method/execution 分片已通过；原 agent 聚合分片触发 20 分钟编排上限，已重分片但按本轮时间选择不再重跑。
当前尚未证明 Agent 优于 baseline、达到 Agora 同等召回，也尚无新问题或 private holdout 结果。

## 输入什么

一次 Agentic Episode 使用：

- `-target etcdraft-v2|omnipaxos-v2`：选择目标 Target Pack；
- `plans/agent/*.json`：Consensus Primer、Property Catalog、Historical Issue Pattern、workload 和可编辑预算；
- `adapters/<target>/`：将官方实现映射为公共 Action/ProducedItem/Observation；
- `qualifications/<target>/`：机械声明目标实际可控、可观察的能力；
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

Agent 可以提出 Risk 和短 Scenario，但不能构造 ActionID、执行事实、PSS 命中或 finding。Runtime 只执行
当前 enabled Action；Replay、Risk/PSS 投影和 Oracle 从真实 Trace 重算。

A9e1 不再向 Risk Agent 提供预置 `Risk` 或 `TestHypothesis`。Agent 输出仍只是待编译 proposal；执行证据和
verdict 的权力边界不变。第一篇论文应围绕 proposal、evidence、verdict separation，而不是把确定性执行、
PSS 数量或多 Agent 编排本身写成效果结论。

两个活动输入都为 Risk Agent 保留最多三次提议/机械修复，并为 Scenario Agent 保留一次调用。etcd/raft
Agentic 输入也已去掉只供 legacy Semantic Explorer 使用的 depth/work-item/explorer budget；这些字段仍由旧路径
继续校验，不再成为新 Agentic 接入的前置负担。

`property_ref` 当前只提供规划上下文。candidate accepted 只证明语法、可观察性和动作资格成立，不证明
predicates 已充分刻画被引用性质，也不证明任何 Oracle 已验证该性质。

## 得到什么

每个终态 episode 输出：

- Risk candidate 是否被接受，以及稳定的机械 feedback；
- Scenario 是否完成、Risk 是否可达；
- Core PSS sample/唯一状态数；
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

- Agreement 只比较相同 frontier position 的累积前缀摘要；不同 frontier 间的冲突可能不产生 violation。
- etcd/raft log-progress 只在同一 `(node, incarnation)` 内检查单调性，跨重启持久性仍缺少可判定证据。
- SUT panic、worker 退出和 timeout 尚未统一成为可重放、可计费、可由 evaluator 分类的执行结果。
- Core PSS 当前包含 pending message/timer/effect。对 A9d6 OmniPaxos 工件诊断性去除 `control.pending` 后，
  29 个唯一状态只剩 5 个；原指标混合了协议语义状态与调度队列变化。
- 当前 A9 Risk+Scenario 方法尚未进入 formal evaluator；仓库只有公开 calibration pair，没有真实 private holdout。
- natural-progress closure 按固定优先级最多执行 24 个普通动作；Risk milestone 变化不会提前返回规划点。
  对需要连续两次战略干预、且第二次干预窗口短于该 closure 的缺陷，这是待评测的可达性风险。

## 下一阶段：A9e2 defect capability / Oracle adequacy pilot

1. 选取少量已知根因和正确 control，逐项验证 root/workload、
   Action、Observation、Oracle 与 Replay 是否共同足以检出；只有具体缺陷暴露表达缺口时才扩 Observation/Oracle。
2. 修复优先级最高的 unequal-frontier Agreement、跨 incarnation persistence 与 panic/hang 结果保留问题。
3. 再实现真正的 Investigation loop：候选 portfolio、`act | continue`、hypothesis revision、失败隔离和
   Exploration Memory；`continue` 的 checkpoint 设计必须专门评估短暂干预窗口。
4. 将当前 Risk+Scenario 方法接入 formal evaluator，确保 trial/provider 失败不终止整批，并按完整 Campaign work、
   token 与 wall time 计费。
5. 最后准备仓库外、人工复核的 private matching pairs，在相同 root、source exposure 和预算下完成方法比较。

在 pilot 证明“已知 trigger 可执行且 Oracle 必然能看见”之前，不先扩成长时间 session。公开 calibration、
synthetic plumbing 和 private holdout 结果始终分开报告。
