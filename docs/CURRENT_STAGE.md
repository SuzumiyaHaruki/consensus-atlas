# 当前阶段

日期：2026-08-14

分支：`feature/agentic-consensus-testing`

阶段：A8 前架构收敛与减负

## 一句话状态

ConsensusAtlas 已让 etcd/raft 与 OmniPaxos 复用同一受限 Scenario Agent、确定性 Runtime、Campaign、Replay、
PSS/Risk 和 Oracle 闭环；当前代码路径已经收敛，下一步是设计并执行 A8 方法效果实验。

## 输入什么

一个目标接入当前需要：

- thin Adapter：把官方实现的节点、消息、时间、生命周期和 host effect 映射为公共 Action/Item；
- qualification：机械声明并验证该 Adapter 实际拥有的控制能力；
- protocol knowledge 与 TestHypothesis：人工可编辑 JSON，描述协议风险而不包含 verdict；
- workload 与实验预算：同一 JSON 中声明目标操作、Runtime、fault envelope、episode/token/work 上限；
- Risk/Semantic projector、PSS mapper 和独立 monitor；
- 显式 opt-in 的 OpenRouter 模型与 key 文件，仅在真实 Agent 运行时提供。

当前活动输入位于：

- `plans/agent/etcdraft-leader-change-inflight-v1.json`；
- `plans/agent/omnipaxos-message-loss-before-decision-v1.json`；
- `benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e/root-corpus.json`；
- `adapters/<target>/` 与 `qualifications/<target>/`。

## 如何处理

```text
knowledge/hypothesis/workload + qualified Adapter
                         |
                         v
            Explorer 或 Scenario Agent
                         |
               当前可信 Frontier
                         |
                         v
        Control Runtime 具体化并执行 Action
                         |
          +--------------+--------------+
          |                             |
          v                             v
     fresh Replay                PSS / Risk / Oracle
          |                             |
          +--------------+--------------+
                         v
             Campaign artifact/summary
```

职责边界：

1. Agent 可以排列可信候选、选择一个当前 Action，并根据机械 feedback 修正计划；
2. Agent 不能设置 root、预算、fault envelope、Oracle、PSS 等价关系或最终 verdict；
3. Runtime 只执行当前 enabled Action，消息在 release 后由 Runtime 持有；
4. 自然时间只通过到期 temporal Action 前进，不提供任意强制超时；
5. completed episode 进入唯一 qualified executor，生成完整 ExecutionBundle；
6. fresh Replay、PSS/Risk projector 与独立 Oracle 从真实 Trace 重算结果；
7. Campaign 负责 episode/work/token/time 预算、checkpoint、终止原因和恢复；
8. 已终止 Campaign 可只依赖 config/checkpoint/artifact 恢复，不需要 SUT、provider 或 key。

## 得到什么

一次 Session 当前输出：

- durable provider 请求/响应与 transport attempt 审计；
- 每步 Action 选择、机械反馈和自然推进记录；
- 完整 Trace、Snapshot、OperationHistory 与 ExecutionBundle；
- primary/replay/model work 和 wall-clock 统计；
- fresh Replay 是否稳定；
- Core PSS samples、跨 episode 唯一状态并集；
- RiskWitness reached/not-reached 与满足的 milestones；
- target-local 和通用 Oracle violations；
- completed、stopped、failed 与明确 stop reason。

PSS/Risk 是解释性指标，不直接形成 defect kill。正式 finding 必须由 replay-stable、conformant 的独立
Oracle/evaluator 产生。

## 当前实现能力

| 目标 | 控制与执行 | Agent Session | 结果验证 |
|---|---|---|---|
| etcd/raft | strict scheduler-owned 消息、自然时间、crash/restart、持久化 effect | 可恢复多 episode | Replay、Core PSS、Risk、TraceIntegrity、Agreement、target-local monitors |
| OmniPaxos | 外部 worker、消息丢弃、自然时间、workload | 可恢复多 episode | fresh worker Replay、Core PSS、Risk、TraceIntegrity、Agreement |
| HashiCorp Raft | 部分 interceptable 黑盒能力 | 未进入正式 Agent Session | 用于暴露黑盒适配上限 |

公共 `internal/` 生产代码没有导入具体共识实现。目标专用 projector、monitor 和 composition 留在 Adapter、
Qualification 或 `cmd/control-experiment` 边界。

## 当前实验证据

- etcd/raft Session 已连续完成两个 qualified episode，并验证同 episode feedback、跨 episode root 隔离、
  PSS 状态并集、成本归属和无 provider/SUT 恢复；
- OmniPaxos 已用同一 Campaign coordinator 完成两个 qualified episode，Risk reached、fresh Replay stable、
  Oracle 0 violation，并在终止恢复时不重访 worker/provider/key；
- 真实 OpenRouter 校准证明 bounded retry、structured output、调用计费和 durable recovery 可用；
- 两协议的 artifact 都保存完整 Bundle/Risk/Oracle，并能拒绝篡改；
- deterministic canonical/uniform stateless Campaign 和 Semantic Explorer 仍可作为 A8 对照方法。

这些结果只证明闭环和证据边界可用，不证明 Agent 比 baseline 更有效，也不证明目标协议正确。

## 本轮减负结果

本轮已完成：

- 删除 etcd/raft 与 OmniPaxos 两套被 Session 覆盖的单 episode Scenario CLI；
- Session 策略改为稳定名称 `etcdraft-agent-session-v1` 与 `omnipaxos-agent-session-v1`；
- 合并公共 semantic authoring 校验与 etcd/raft Ready effect 构造；
- 删除无用目标类型别名；
- 将 durable Agent journal、Semantic Explorer 文件及 experiment/execution 文件改为职责名称；
- 将 30 个阶段编号测试改为行为名称并同步现有 race shard 清单；
- 把 `control-experiment.run()` 收敛为 flag 解析与四路显式分派；
- 删除 11 份阶段流水账，历史由 Git 保存。

当前 Go 规模：

- 生产代码：32,823 行；
- 测试代码：15,016 行；
- 合计：47,839 行。

相对本轮开始净减少 647 行 Go；CLI 主入口的最高圈复杂度热点已经消除。测试仍占约 31%，这是确定性执行、
恢复和反例验证的主要可信边界，不进行比例式删除。

## 当前边界

尚未完成：

- 非公开 candidate/control 正式效果实验；
- deterministic baseline、单 Agent、双角色 Agent 的同预算比较；
- 多次重复实验与置信区间；
- fixed Profile obligation coverage 接入当前 Session 汇总；
- PSS/义务指标与隐藏根因检出的相关性验证；
- OmniPaxos target-local 共识 monitor；
- Protocol/Hypothesis Agent 与 Scenario Agent 的正式双角色在线衔接。

另外：

- projector identity 是随实现维护的显式版本 ID，A8 必须同时记录 Git 提交；
- HashiCorp Raft 的部分能力不能被描述为 scheduler-owned；
- 旧阶段 artifact 需要 checkout 对应 Git 提交解释，不维持当前 HEAD 的兼容 CLI；
- 普通测试不会读取 key 或调用外部模型。

## A8 下一步

A8 先形成一个最小、可预注册的配对实验：

1. 准备非公开 candidate/control pair，并保持 Planner 输入对身份与 verdict 盲化；
2. 固定模型、总 model calls/tokens、primary/replay work、wall clock 与 semantic exposure；
3. 比较 deterministic baseline、单 Agent 和双角色 Agent；
4. 主要指标为独立根因检出、正确 control false positive 与有效 trial 数；
5. PSS、Risk、义务覆盖、计划修正次数和成本只作为解释性指标；
6. 每个 arm 重复多次，不以单次成功或状态数宣称优势。

在预注册前不增加第三 Agent、通用 DSL、新 PSS 维度或新的评分公式。

## 当前验证

已通过：

- `go test ./... -count=1`；
- `go vet ./...`；
- etcd/raft Adapter 全包；
- etcd/raft 与 OmniPaxos 双 episode Session；
- semantic authoring、CLI 参数边界与 qualified execution；
- `internal/controlexperiment` 与 `internal/defectbench`；
- unused production check；
- race shard manifest；
- 旧路径与旧名称引用检查；
- 两份总体规划的字节一致性检查；
- `git diff --check`。

本轮没有改变并发执行语义，因此不重复运行耗时的全仓 race；已有 race shard 清单已完成机械审计。

## 阅读顺序

1. [总体规划](ConsensusAtlas-总体规划.md)
2. [架构](architecture.md)
3. [Control Runtime](control-runtime-v2.md)
4. [指标](metrics.md)
5. [Coverage Kernel](coverage-kernel.md)
6. [Defect Benchmark](defect-benchmark.md)
