# A4a：短计划可信具体化

日期：2026-08-13

## 结论

A4a 已把 Agent 权限从“候选排序”推进到“有界多步语义意图”，同时保持执行权仍在可信层：

```text
ScenarioPlan
  -> current trusted RiskFrontier
  -> selector unique match
  -> existing FrontierChoice / stateless materializer
  -> fresh Replay + new Risk progress
  -> next step or mechanical rejection
```

## 实现边界

- 计划最多 8 步，实际可执行步数由外部 `maxSteps` 再限制；计划本身不能修改预算；
- selector 二选一：当前 exact ActionID，或有限公共字段组合；两种方式不能混用；
- 语义字段包括 ActionKind、节点、ItemKind、owner、消息源/目标、TemporalKind、effect kind 和 durability；
- 只有一个匹配时才生成已有 `FrontierChoice` 并执行；零匹配、多个匹配和预算耗尽分别返回稳定 reason；
- 每一步复用现有 prefix reconstruction、admissible frontier、materialization 和 fresh Replay；
- `ScenarioPlan/ScenarioExecution` 没有新增独立摘要或账本，也不是持久化 contract。

## 实际验证

- fixture：两步 `crash-current → restart-node` 完成；第二步只使用节点和 ActionKind，不预知未来 ActionID；
- fixture：分别验证 `no-match`、`ambiguous`、`budget-exhausted` 不会执行猜测动作；
- strict JSON：未知的 Agent `verdict` 字段和 exact/semantic 混合 selector 均被拒绝；
- etcd/raft：相同两步生命周期计划在真实 Adapter、真实 root prefix 和 FaultEnvelope 上完成。

etcd/raft 集成还发现并修复了一个目标局部问题：协调节点停止后，合法 Evidence 会省略停止节点，旧 Risk
projector 因此把合法 prefix 误报为证据缺失。现在 projector 只在 Trace 中存在受信任的 running→stopped
转换时接受该缺席；其他无依据的缺失仍然失败。

阶段验证通过：`make test`、`go vet ./...`、`git diff --check`。未运行 race，未读取 key，也未访问外部模型。

A4a 净增 Go 生产代码约 285 行、Go 测试约 153 行；其中通用 concretizer 为 217 行。没有新增 package，主要
增量是一个执行器、selector 字段与四类结果的端到端检查。

## 尚未完成

- ScenarioPlan 尚未由真实模型生成，也没有执行一次反馈后的自动修正；
- 多步最终 Trace 尚未进入 A3 qualified Bundle/PSS/Oracle 结果；
- 只验证了生命周期组合，消息与自然时间组合仍需后续场景验证；
- 本阶段不构成 Agent 搜索效果证据。

## 下一步

A4b 复用现有 provider journal，让模型在固定调用/token/step 预算内提交计划，并最多依据机械反馈修正一次；
随后把成功的多步选择编译到 existing qualified executor。若计划第一次即合法，不产生额外修正调用。
