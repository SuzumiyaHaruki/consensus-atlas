# M5.21i Ordered Method Corpus Comparison

本目录不包含新的 SUT 或模型运行。它复用 M5.21f 的两个 ActionClass bundle 和 M5.21h 新增的
Uniform bundle，形成两条顺序固定、逻辑成本相同的两步语料：

```text
Agent / zero-model:          ActionClass(seed 171) -> ActionClass(seed 172)
deterministic adaptive:      ActionClass(seed 171) -> Uniform(seed 172)
```

每条语料都按顺序重新投影 bundle、运行 Agreement/Trace Integrity，并通过现有协议无关
`protocolstate.Aggregate` 重算完整 PSS union 和 64-decision curve；没有相加单次 unique-state 数量。

## 结果

| 指标 | ActionClass → ActionClass | ActionClass → Uniform |
|---|---:|---:|
| logical attempts | 2 | 2 |
| primary / replay work | 66 / 66 | 66 / 66 |
| PSS samples / unique | 66 / 32 | 66 / 40 |
| new states by attempt | 25, 7 | 25, 15 |
| prefix area | 1325 | 1444 |
| workload completed / pending | 0 / 2 | 0 / 2 |
| monitor checks / findings | 4 / 0 | 4 / 0 |

两个最终状态集合交集 26、并集 46，ActionClass-only 6、adaptive-only 14，Jaccard 为 0.5652。
因此 adaptive 序列在这两个公开 attempts 下得到 8 个额外 unique states；Agent 与 zero-model
共享完全相同的 32-state 执行语料，而 Agent 额外消耗 2 calls/6027 tokens。

这是一份小样本 discovery 诊断，不是缺陷效果评价、holdout、协议正确性结论或总体方法优势。
两条语料都没有完成 workload，也没有触发可信 monitor。下一阶段不扩大模型实验，转而实现最小
RiskWitness，使“是否到达目标协议时序”成为可信的独立结果。

机械数据及两条完整 curve 见 [comparison.json](comparison.json)。
