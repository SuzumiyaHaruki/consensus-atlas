# M5.21i：Ordered Method Corpus 聚合

日期：2026-08-11

## 目标与实现边界

M5.21h 只比较了 attempt-2 的两个有效执行。M5.21i 按冻结规则将共享 attempt-1 与两种 attempt-2
证据分别组成完整方法序列，避免把单次 PSS states 直接相加或把物理证据复用误当成免费逻辑成本。

本阶段没有增加生产聚合器、Runtime、Adapter、PSS 维度、Oracle 或 Agent。checked experiment test
读取三个保存的 bundle，对每个 bundle 执行 Validate、DecisionProjection 和 `NewPSSFeedback` 重新投影，
运行 Agreement/Trace Integrity，再把有序 Core PSS samples 交给既有协议无关
`protocolstate.Aggregate`。`comparison.json` 保存完整 64-decision state curve，并由测试逐字段重算。

## 等预算结果

| 指标 | Agent/zero execution corpus | deterministic adaptive corpus |
|---|---:|---:|
| sequence | ActionClass → ActionClass | ActionClass → Uniform |
| logical attempts | 2 | 2 |
| primary / replay decisions | 64 / 64 | 64 / 64 |
| primary / replay work | 66 / 66 | 66 / 66 |
| PSS samples / unique | 66 / 32 | 66 / 40 |
| new states by attempt | 25, 7 | 25, 15 |
| prefix area | 1325 | 1444 |
| self-normalized area | 0.6470 | 0.5641 |
| workload completed / pending | 0 / 2 | 0 / 2 |
| monitor checks / findings | 4 / 0 | 4 / 0 |

最终集合交集 26、并集 46，ActionClass-only 6、adaptive-only 14，Jaccard 0.5652。三个物理执行证据
服务六个 method-attempt views，但每个方法仍计 2 attempts、66/66 logical work。M5.21i 自身新增
model calls=0、SUT executions=0。

Agent 与 zero-model 的有效执行序列相同，因而都指向 32-state corpus；Agent 另有 2 calls/6027 tokens。
Adaptive 在本小样本获得 40 states，但它只是一个针对两 backend 的简单确定性轮换。这个结果说明
当前 Agent 没有产生行为增益，也说明 effective identity 去重后仍能公平重建方法级曲线；它不能证明
adaptive 总体优于 Agent、zero-model 或其他搜索方法。

## 证明、未证明与下一步

本阶段证明：有序 bundle 能在不重复执行 SUT 的情况下形成可验证的等预算 method corpus；物理证据
共享和每臂逻辑成本已被分开；不能相加单次 state counts。它没有发现协议故障，没有 workload
completion，没有 holdout，也没有提供 PSS 完备分母。

不应立刻扩大 backend-preference LLM pilot：当前搜索面只有两个 backend，简单轮换已经产生可见增量，
继续增加 calls 很可能只放大成本而不增加协议语义能力。下一阶段 M5.21j 回到冻结主线，建立一个最小
RiskWitness：target-owned milestone projection、family-owned 固定偏序、generic digest/order validator；
先对现有 bundle 给出 reached/not-reached 结果，不运行新 SUT、不调用模型，也不引入开放 temporal DSL。

完整机械工件见
[`benchmarks/experiments/etcdraft-v2-method-corpus-m5.21i/`](../benchmarks/experiments/etcdraft-v2-method-corpus-m5.21i/README.md)。
