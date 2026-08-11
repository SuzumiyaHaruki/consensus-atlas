# M5.21x：OmniPaxos Core PSS 跨协议映射 probe

日期：2026-08-11

## 结论

M5.21x 通过。OmniPaxos 的现有稳定 Evidence 可以在不修改固定 Core PSS schema、Control Runtime、worker
或上游源码的条件下，映射为保守的 Core PSS semantic graph。新增 target-owned Mapping 为 154 行，
低于 180 行软目标和 240 行停止线。

一条真实 non-leader workload 含 35 decisions 和 36 个在线样本；第二个 fresh worker process 使用相同
ActionID 序列得到字节语义相同的 sample sequence、Discovery ledger 和 Runtime trace。trace digest 为
`2064f35a31c4839b006b0e3d9fda5d4a72f34508aaf2a91145341684b3694acf`。

完整 Core PSS 得到 35 个 unique state，但 target semantic graph 只有 9 个。前者还包含 Runtime pending
message、temporal item 和 dependency shape，因此二者都只作为可重放描述统计，不能解释为测试质量、
协议完备性或“35 种根本场景”。

## 保守映射

Mapping 只生成 Evidence 支持的事实：

- 每个运行 participant 一个 `EntityParticipant`；
- `get_current_leader()==self` 映射为 `coordinating`，其他状态映射为 `passive`；
- 完整 promise ballot `(number, priority, pid)` 的 distinct ordering 映射为相对 `Epoch e1..en`；
- 非零 decided index 的 distinct ordering 映射为相对 `DecisionUnit d1..dn`；
- participant 与相对 epoch 建立 `belongs-to`；
- 已决定 participant 与相对 decision 建立 `decides`；
- 相邻相对 epoch/decision 建立 `precedes`。

Mapping 明确不生成：

- `contending`：公开 Evidence 没有可独立确认的 Sequence Paxos phase/role；
- `Value`：node Evidence 只有 decided frontier，没有 exact per-index value digest；
- `applied/persists`：同步 MemoryStorage 不等于可观测 application/durable boundary；
- priority、absolute ballot、absolute decided index：这些数值不进入 canonical Core state。

## 相对不变量测试

构造两份结构相同的 Evidence：第二份把所有 ballot number 和 decided index 整体平移，并改变 leader
priority 的绝对值。两份 Mapping observation 完全相同。这机械验证了“不同绝对 term/index 但相对结构
一致”不会被计为不同的 target semantic graph。

该测试不证明 Mapping 是完整的协议状态等价关系；它只防止已知的绝对计数器刷状态数问题。

## 在线采样结果

| 指标 | 结果 | 可解释含义 |
|---|---:|---|
| decisions | 35 | 本次确定性执行成本 |
| samples | 36 | initial + 每个 post-decision sample |
| complete Core states | 35 | ControlContext + semantic graph 的不同组合 |
| target semantic graphs | 9 | Mapping graph 的不同组合 |
| minimum visible milestones | 3 | all-passive、one-coordinating、decided frontier |
| fresh sample equality | true | 相同 BuildID/Action 序列可重现 |
| Discovery ledger equality | true | 统计账本可重现 |

35 与 9 都不是 coverage denominator 或最终测试评分。尤其 complete Core states 接近 decision 数，说明
控制队列形状具有高区分度；未来若用于搜索反馈必须单独校准，不能用数值增长自证 Agent 更好。

## 代码与边界

- Mapping 生产代码：154 行；测试：271 行；
- Core PSS schema 改动：0；Control Runtime 改动：0；worker 改动：0；
- OmniPaxos 上游改动：0；
- fresh worker processes：2；
- value/contending/applied/durable entity：全部 0；
- 冻结报告：`benchmarks/feasibility/omnipaxos-core-pss-m5.21x/summary.json`。

## 没有证明

- 没有 Extended Sequence Paxos PSS 或协议状态等价证明；
- 没有 Agreement/safety/liveness Oracle；
- 没有证明 Core/semantic state 数与缺陷检出相关；
- 没有 restart、durable image、Agent、Campaign 或 Qualification；
- 没有 leaderless/dependency-graph 目标；
- PSS 只描述一次执行到过什么状态，不能单独评价测试是否“全面”。

## 下一阶段：M5.21y Agreement Oracle 激活

下一步优先补外部正确性证据，而不是扩张 PSS：

1. worker/Adapter Evidence 增加每个节点 exact decided prefix 的 position 和 cumulative value digest；
2. target-owned DecisionProjector 只把 participant/position/digest 投影给现有 protocol-neutral Agreement；
3. 正确 OmniPaxos control 必须 `pass`，受控 synthetic/calibration divergence 必须触发，证明 monitor 非空；
4. ClientResult 不得冒充 quorum agreement，Runtime/Oracle 不解析 Sequence Paxos payload；
5. 公共 Oracle schema 和 Runtime 不修改；target worker+projection 生产增量软目标 140 行、硬停止线 200 行；
6. 仍不加入 restart、Agent、Coverage 总分、Qualification 或真实缺陷声明。
