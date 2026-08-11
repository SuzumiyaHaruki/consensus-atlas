# M5.21q：Risk Frontier 精确执行权限校准

日期：2026-08-11

## 结论

M5.21q 完成了接入 Planner 之前的 no-model authority gate：可信代码可以从真实
etcd/raft 轨迹前缀重建 RiskWitness 进度与下一步 admissible frontier；消费者只能选择
该视图中已有的 exact ActionID；所选引用编译到既有 Policy 后，确实改变唯一 qualified
executor 的后续真实执行。

这仍不是 Agent 效果实验。新模型调用数为 0。

## 输入、处理、输出

输入是 M5.21p 的 replay-stable ExecutionBundle、冻结的 Raft family RiskWitness spec、
FaultEnvelope、Runtime 配置和 fresh official etcd/raft Adapter factory。

处理分四步：

1. 对原轨迹前 28/53 decisions 形成 digest-bound prefix；
2. 用 fresh Adapter 严格 Replay 前缀，并重新观察 Runtime enabled 与 envelope-admissible actions；
3. 将可信 RiskWitnessResult 投影成只含已满足里程碑、首个缺失里程碑和 prefix digest 的
   `RiskWitnessProgress`，同时将 admissible Action 投影成精确 `FrontierActionRef`；
4. 将冻结选择编译成含 exact ActionID 的既有 DecisionRule，再执行、Replay 和重投影。

输出是两条 64-decision 执行：

| 策略 | decision 29 | decision 54 | workload | RiskWitness | Core PSS |
|---|---|---|---|---|---:|
| risk-directed | `Crash(n1/inc1)` | `Restart(n1/inc2)` | pending | reached（3/3） | 48 |
| progress-control | `CompleteEffect(n1)` | 无冻结选择 | committed | not-reached（1/3） | 52 |

两者 policy/config/trace identity 均不同。risk-directed 精确重现 M5.21p trace；control
轨迹不同且没有形成 coordinator-change 序列。两臂各记 66 primary + 66 replay work；两次
prefix 重建另记 30/55 work，不伪装成免费 Planner 计算。

## 信任边界

- target projector 仍负责把 opaque target evidence 映射到 family milestone；通用包不解码协议证据；
- Planner-facing progress 不包含 projector、target identity、participant 或 milestone evidence；
- ActionRef 只暴露公共控制语义，payload 与协议私有 metadata 不进入视图；
- choice 必须逐字段等于当前视图成员，并绑定 view digest、decision、ActionID 与 Action digest；
- exact rule 不允许用同 kind/node 的另一个 Action 替代；若引用不再 enabled，执行显式失败；
- FaultEnvelope 在重建前沿时继续生效，Runtime-enabled 与 admissible digest 分开保存。

## 该阶段没有证明

- 没有调用 LLM，也没有证明 Agent 能作出上述选择；
- 没有证明 Agent 优于 uniform、mutation 或专家方法；
- 没有形成 formal holdout、候选检出、Oracle verdict 或绝对质量分数；
- 没有把 RiskWitness 当成 Coverage 分母或正确性结论；
- 没有完成第二个 strict deterministic CFT target 的迁移门。

## 工件与复核

小型冻结摘要位于
`benchmarks/experiments/etcdraft-v2-risk-frontier-authority-m5.21q/summary.json`。
集成测试从真实执行重算并逐字段对账；完整 bundle/trace 不重复提交。

主要代码：

- `internal/semantic/risk_progress.go`：最小、可验证的 RiskWitness 进度投影；
- `internal/controlexperiment/risk_frontier.go`：prefix Replay、前沿视图、choice 和精确策略编译；
- `cmd/control-experiment/risk_frontier_authority_gate_test.go`：真实 etcd/raft authority gate。

## 下一步

下一阶段不直接宣称 Blind Planner 已就绪。先把同一通用进度/前沿/精确引用边界迁移到第二个
strict deterministic CFT target；如果该目标仍不具备 strict 资格，优先补最小资格缺口，而不是
继续增加 etcd/raft 专用目标或为获得正结果放宽门禁。formal private curator pack 继续在仓库外准备。
