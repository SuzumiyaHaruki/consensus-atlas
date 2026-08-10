# M5.21e：Matched Agent/Zero-model Execution Pilot

## 目标

M5.21d5 只证明了真实 Agent Campaign 链路可达。M5.21e 做一个预注册的小型对照，观察当前
preference-only Agent 在相同执行 ceiling 和 seed 下，是否会实际选择不同 backend，以及这是否
产生不同轨迹/PSS 集合。

这仍是 public pilot，不是 Agent 优势证据。单个 target、两个 seeds 和两个 backends 不支持统计或
外推结论。

## 冻结的两臂

共同：

- target：official etcd/raft v2；
- hard baseline/risk/fault envelope：使用当前 Campaign composition；
- attempts：2；
- decisions ceiling：32 per attempt；
- seed rule：161、162；
- primary/replay ceiling：由现有 Campaign config 对称派生；
- 相同 trusted compiler、qualified executor、replay、PSS projector 和 monitors。

Zero-model 臂：

- strategy：`campaign-etcdraft-v1`；
- planner：`deterministic-pss-novelty-rotation-v1`；
- model work：0；
- wall ceiling：180000 ms。

Agent 臂：

- strategy：`campaign-etcdraft-agent-v1`；
- planner：`campaign-preference-only-v1`；
- model allowance：2 calls，8192 tokens per attempt；
- transport：`deepseek-v4-flash`、temperature 0、thinking disabled、1200 max output、0 retry；
- wall ceiling：240000 ms，只是运维上限，不当作共同工作预算。

## 执行顺序与停止规则

1. 先执行 zero-model 臂，确认共同 target/executor 仍可用；
2. zero-model 出现基础设施失败时，在任何付费调用前停止；
3. 再执行 Agent 臂，每个 attempt 仍遵守 intent-before-key 和 one-call/no-retry；
4. Agent 某次失败或 ambiguous 时立即结束，不 resume 重调；
5. 不因为某一臂的 PSS 数较少、workload pending 或 monitor 零触发而补跑/cherry-pick seeds。

工件固定为
`benchmarks/experiments/etcdraft-v2-agent-vs-zero-m5.21e/{zero,agent}/`，每臂包含完整
Campaign、Summary 和 Observation。根 `README.md` 保存预注册边界和实际比较。

## 比较规则

只比较可机械重算的分量：

- 每 attempt 的 backend/strategy choice 和 seed；
- actual primary/replay decisions 与 work units；
- PSS samples、unique states、state-set intersection 和双方 exclusive states；
- 共同 primary-decision prefix 上的 discovery curve；
- workload planned/offered/completed/pending；
- fault usage 与 monitor triggers；
- Agent calls/tokens。

不生成综合分数。两臂只共享执行 ceiling，Agent 额外的 model work 必须单独列出，因此不得
将本 pilot 称为“相同总成本”。若两臂选择相同 backend 且产生相同状态集，必须将其保留为
当前 Agent 没有产生可观测搜索差异的负结果。

## 代码与结论边界

- 预期复用已有两个 runner，Go 净增为 0；
- 不新增 pair orchestrator、新执行器、新分数或自动效果结论；
- 不声称 Agent 优于基线、PSS 预测缺陷、monitor 零触发代表正确，或两个 seeds 具有代表性；
- 实验后执行 exact-config recovery、JSON/artifact 校验、secret scan 和文档自审查。
