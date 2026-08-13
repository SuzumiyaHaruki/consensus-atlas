# A3：单 episode 测试闭环

日期：2026-08-13

## 结论

A3 已完成最小端到端闭环：Semantic Explorer 的首个可信选择不再只用于 prefix/Risk 比较，而是进入已有
qualified executor，在真实 etcd/raft Adapter 上得到同一个 prefix 对应的 ExecutionBundle、Core PSS、
RiskWitness、fresh Replay 和独立 Oracle 结果。

```text
Explorer selected candidate
  -> existing exact-prefix compiler
  -> existing qualified executor
  -> ExecutionBundle + Core PSS + RiskWitness
  -> fresh Replay + TraceIntegrity + Agreement
  -> testing_result in the existing calibration artifact
```

## 实现范围

- `semantic_testing_result_etcdraft.go` 只做目标局部组合，不建立新的通用执行层；
- stateless prefix executor 直接接收已有 `StatelessDFSResult`，Semantic Explorer 与 stateless Campaign 共用；
- 成功 artifact 从 v1 演进为 v2，并要求携带 `testing_result`；失败 artifact 不伪造执行结果；
- testing result 复用 Bundle 内的 Trace、资格、PSS、Replay 和成本，只额外呈现选择、Risk 与 Oracle 组合视图；
- 已完成 artifact 恢复时重新验证来源，但不再次调用 provider 或执行目标。

没有新增独立 Ledger、结果摘要、baseline、gate 或冻结 contract，也没有读取模型 key。

## 集成检查

集成测试在真实 etcd/raft Adapter 上模拟两次 provider 响应：第一次越权提议被拒绝，第二次合法候选排列被接受。
随后机械检查：

- testing result 的 candidate 等于 Explorer 首个 expansion；
- Bundle Trace 等于该 WorkItem 的 child prefix；
- qualified Replay 为 required 且 stable；
- Core PSS 至少产生一个样本；
- TraceIntegrity 与 Agreement monitor 均运行，测试目标上无 violation；
- 恢复 terminal artifact 时 provider 调用数不增加，Bundle identity 保持一致；
- provider 失败路径仍只生成失败 artifact，不生成 testing result。

阶段验证通过：`make test`、`go vet ./...`、`git diff --check`。未运行 race，也未访问外部模型。

本阶段净增 Go 生产代码 153 行、Go 测试 4 行；核心新组合文件为 129 行。增量主要来自把现有对象组装并
交叉验证，没有新增通用 package 或平行执行路径。

## 仍未证明

- 这不是 Agent 优于 deterministic/random/DFS 的效果证据；
- 公开校准不是非公开 candidate/control 实验；
- 单 prefix 闭环还不能表达需要多步意图修正的测试场景；
- PSS/Risk 覆盖仍不能替代正式性质 finding；
- 当前结果只在 etcd/raft composition 中接通，尚未证明 episode 编排可直接复用于第二协议。

## 下一步

A4 只实现最小短时域 `ScenarioPlan` 与可信 concretizer：让 Agent 描述少量连续语义意图，系统逐步解析为
当前 enabled Action，并返回有限机械反馈。Hypothesis 自动生成、JSON 输入外移、物理多 Agent 和 hidden
evaluation 均继续后置。
