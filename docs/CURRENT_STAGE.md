# 当前阶段

日期：2026-08-15

分支：`feature/agentic-consensus-testing`

阶段：A9d6 real dual-target Agentic Episode calibration

## 一句话状态

ConsensusAtlas 已经在同一 `agentic-episode-v1` 入口下，用真实 OpenRouter 模型跑通 OmniPaxos 的
Risk Agent -> 机械资格 -> Scenario Agent -> 真实执行 -> Replay/PSS/Oracle 闭环。etcd/raft 的运行
暴露了输出约束和 provider 稳定性问题，已修复可在本地解决的约束缺口并停止盲目重试。

## 输入什么

一次 Agentic Episode 使用：

- `-target etcdraft-v2|omnipaxos-v2`：选择目标 Target Pack；
- `plans/agent/*.json`：协议知识、已知 Risk、workload、可编辑的执行/模型预算；
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

## 下一阶段

A9e 只做最小配置时间 session，不扩展 episode 契约：

1. 将现有 `agentic-episode-v1` 作为唯一执行单位；
2. 在 wall-time、episode、model 和 Runtime 预算内使用多个独立 root；
3. 累积 new/accepted/reached Risk、PSS 增量、Replay 成功率、Oracle finding 和完整成本；
4. 一个 episode/provider 失败应当形成可计数失败并继续后续预算，不让单次外部不稳定终止整个 session；
5. 先用 fixture 和小型公开运行验证编排，不在本阶段宣称长时间全面性或 Agent 优势。

这个阶段的用户可见终点是：给定目标、知识、workload 和预算，系统能持续运行多个独立测试单元，
最后输出可重放 finding（如有）、Risk/PSS 统计、失败分类和总成本。
