# M5.21f：Planner Usefulness Gate 第一阶段

日期：2026-08-10

## 目标

M5.21e 只证明当前 Agent 的首次选择没有产生行为差异，第二次回复又因输出字段漂移被拒绝。
M5.21f 不扩大 Agent 权限，先关闭两个更基础的问题：

1. 由可信代码把精确 proposal template、可变字段和允许枚举冻结进每次请求；
2. 建立一个看到相同 `CampaignPlannerView`、拥有相同 preference-only 权限的确定性自适应基线。

该阶段是 usefulness gate，不是方法优势实验。strict parser、one-call/no-retry、可信 compiler、
execution identity、Runtime、Replay、PSS 和 monitor 边界均不放宽。

## 冻结实现

输出契约必须显式包含：

- `prefer.backend_ids` 和 `prefer.actions`，即使为空也编码为 JSON array；
- 当前 risk 下同时 allowed 且 eligible 的 backend IDs；
- 上述 backend 实际支持的 ActionKind 并集；
- 与 hard baseline 相同的其余 proposal 字段，`digest` 固定为空。

Agent 仍只能修改两个 Prefer 数组。契约进入 exact prompt/request bytes，继续由既有 durable-call
边界绑定；parser 不接受未知字段、单值替代数组或尾随 JSON，也不因失败自动重试。

新的非 LLM 基线固定采用以下简单规则：

1. 按 risk 的 allowed backend 顺序，让每个 eligible backend 至少被选择一次；
2. 全部尝试后，最近一次没有新增 PSS，或者 workload 仍 pending 且从未 completed 时轮换；
3. 其他情况保留最近 backend；
4. 不设置 `Prefer.Actions`，不获得额外 trace、witness、seed 或 Oracle 信息。

这不是 UCB/bandit，也不宣称最优。它只是消除“只与弱 zero-model 比较”的明显评测缺口。

## 冻结视图预检

第一项实验只读取 M5.21e zero-model 第二次 attempt 中已经公开归档的 PlannerView，不运行 SUT、
不调用模型。输入 identity 固定为：

- source archive：`etcdraft-v2-agent-vs-zero-m5.21e/zero/campaign.tar.gz`；
- PlannerView digest：`7ddce1feb9b90fd947899db19ee4a5bcf87cb94602c70a96fcd860521eeb784b`；
- ordinal：2；policy seed：162；decision budget：32。

比较零模型与新自适应基线经同一 trusted compiler 产生的 backend、plan digest 和完整
execution-instance digest。只有完整 instance identity 不同才记为 behavior delta；不能只凭自然语言
策略名判断。

## 真实调用小试验预注册

输出契约与离线预检通过后，允许进行一次显式 opt-in 的两轮 public calibration：

- target：official etcd/raft v2；
- attempts：2；decisions：32 per attempt；first seed：171；
- model：`deepseek-v4-flash`，temperature 0，thinking disabled，0 retry；
- allowance：最多 2 calls，8192 tokens per attempt；
- wall ceiling：240000 ms，仅为运维停止条件；
- 不因回复无效、PSS 较少、workload pending 或 monitor 零触发而补跑或换 seed。

该调用只检查 proposal validity、valid execution yield 和行为差异。即使 Agent 完成两轮，也不能
证明其优于 deterministic baseline。后续只应对产生不同 execution identity 的方法进入等预算效果比较。

## 验收与停止条件

- contract template 可被 strict parser round-trip，M5.21e 的错误字段形状仍被拒绝；
- 同一 frozen view 下，自适应基线与 zero-model 可机械产生不同完整 execution identity；
- 新 prompt version 改变 model planner composition identity，不覆盖 M5.21e 历史工件；
- 真实调用的每个 completed result 均只消费一次，失败仍进入可信 model-work 账本；
- 本阶段不增加 raw trace/witness 反馈、不增加 LLM-only 权限、不生成综合分数。

## 完成结果

输出契约、确定性自适应基线和两项 frozen-view 机械比较均已实现。M5.21e ordinal 2 的公开
PlannerView 上：

- zero-model：`action-class-random`，plan `bed10531...abb83`，instance `dd08e6a...d929`；
- adaptive：`admissible-uniform`，plan `b09fff85...081e`，instance `6a7fd3a...a227`。

二者共享 view、seed 和预算，backend/strategy 不同，因此这是真正的执行输入差异，不只是 proposal
文本变化。

真实调用按预注册参数一次完成，没有换 seed、补跑或 retry。两次回复均通过 exact contract、strict
parser、preference-only validator 和 trusted compiler；Campaign 以 `stopped/attempt-limit` 完成
2 attempts，primary/replay 均为 66 work units，模型成本为 2 calls/6027 tokens。发现 32 个 Core PSS
states，两个 workload 均 pending，Agreement/TraceIntegrity 各检查 2 次且零触发。

Agent 两次都选择 `action-class-random`。第二轮与 zero-model 的有效 backend/strategy/seed/预算相同，
没有表现出相对 zero-model 的执行行为差异；新自适应基线在同一 view 会选择尚未尝试的
`admissible-uniform`。Agent 输出的全部 8 个 `Prefer.Actions` 对当前两个 backend 没有区分作用，
但仍改变 plan/instance digest。这是本 pilot 新发现的评测缺口：完整审计 identity 不能直接充当
behavior-delta identity。

Exact-config recovery 产生字节一致的 Summary/Observation 且没有新增模型调用。归档见
[`benchmarks/experiments/etcdraft-v2-planner-gate-m5.21f/`](../benchmarks/experiments/etcdraft-v2-planner-gate-m5.21f/README.md)。

本阶段证明了输出契约在 2 次调用中有效、强基线能够产生可执行差异，并发现 behavior identity
过敏问题。它没有证明 Agent 优于任何基线、PSS 代表测试质量、monitor 零触发代表协议正确，或
2 次调用足以估计长期 proposal validity。

下一阶段只应增加 protocol-neutral effective-execution identity 和三方法 proposal-only gate；在该
identity 不同前，不重复运行 SUT，也不扩大 Agent 反馈或权限。
