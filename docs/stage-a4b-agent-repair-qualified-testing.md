# A4b：Agent 修正与 qualified 测试

日期：2026-08-13

## 结论

A4b 已闭合短计划的 Agent 数据流：模型可以提交完整 ScenarioPlan，在第一次计划被机械拒绝后修正一次；成功
多步选择会编译成已有 Policy，并由原 qualified executor 生成 Bundle、PSS、Replay 和 Oracle。

```text
protocol knowledge + hypothesis + root RiskFrontier
  -> durable model call
  -> ScenarioPlan
  -> trusted concretizer
  -> no-match / ambiguous / plan-invalid feedback
  -> at most one revised model call
  -> successful ScenarioExecution
  -> exact Policy
  -> qualified Bundle / PSS / Replay / Oracle
```

## 权限与预算

- 最多两次模型调用、每次最多 8 个计划步骤，因此搜索侧执行总量有明确的 `attempts × maxSteps` 上界；
- prompt 不提供 fault、Oracle、candidate/control 身份或隐藏 verdict；
- prior feedback 只包含公开 reason、已执行 choice、Risk progress 和失败 frontier 的可用 Action；
- 第二次提议必须从同一个 root 返回完整修订计划，不能接管中间 Runtime；
- ScenarioExecution 必须与 Trace 中的 Action、decision 和 Action digest 一致，才能编译为 exact Policy；
- qualified executor、fresh Replay 和 Oracle 沿用 A3 路径。

ScenarioPlan、Agent result 和 testing result 没有增加独立摘要或新账本。模型调用继续使用已有
`statelessAgentCallJournal` 的 intent/dispatch/result、凭据单次使用和恢复语义。

## 端到端结果

本地 provider stub 的第一次响应请求在全运行集群上执行 `restart`，可信层返回 `no-match`，并在 prior
feedback 中给出失败 frontier。第二次响应修正为：

```text
exact current crash ActionID -> semantic restart(node)
```

修正计划完成后：

- qualified executor 重现 ScenarioExecution 的同一 Trace digest；
- Core PSS 产生样本；
- fresh Replay stable；
- TraceIntegrity 与 Agreement 均运行且无 violation；
- 两次响应从 journal 恢复时 provider 调用数不增加，Bundle digest 保持一致。
- 两次尝试的 prefix reconstruction/materialization/replay work 被累计报告，失败尝试不是免费工作。

测试不读取 key，也不访问外部模型。

阶段验证通过：`make test`、`go vet ./...`、`git diff --check`。未运行 race。

## 规模与边界

A4b 净增 Go 生产代码约 512 行、测试约 145 行；没有新增 package。生产增量由通用一次修正协调器、现有
journal 的薄 prompt facade、Scenario→Policy 校验和 etcd/raft qualified bridge 构成。

当前仍未证明：

- DeepSeek 实际能稳定生成或修正计划；
- Agent 比 deterministic/random/DFS 更有效；
- 消息和自然时间的多步计划已被验证；
- 该 episode 已复用于第二协议；
- PSS/Risk 能替代正式 finding。

## 下一步

A4c 只增加显式 opt-in 的真实 provider 运行入口和紧凑 summary，执行一次调用、恢复和结果审查。若真实响应
暴露 prompt 或 selector 问题，优先修改 prompt/现有 selector，不新建数据模型。
