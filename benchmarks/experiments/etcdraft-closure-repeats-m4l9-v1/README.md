# M4l9：etcd/raft closure 五组真实模型重复

本实验在固定公开 Risk 下比较 `public-fixed` 与 `target-local`。五组顺序在调用前写入
`run-plan.json`，采用 A/B、B/A、A/B、B/A、A/B 交替顺序。两臂共享 Target、已有
Risk、协议材料、模型、Scenario prompt 版本、温度和预算，只改变由 Target composition
机械绑定的 `closure_mode`。

## 结果

| arm | 正确干预 | Risk reached | calls | tokens | search work | qualified primary/replay |
|---|---:|---:|---:|---:|---:|---:|
| public-fixed | 5/5 | 2/5 | 15 | 395,236 | 1,022 | 212/212 |
| target-local | 5/5 | 5/5 | 12 | 265,819 | 816 | 236/236 |

两臂都在 5/5 Episode 中丢弃了语义等价的 follower→leader `MsgAppResp`：
`g03-target-local` 和 `g05-public-fixed` 为 `n3→n1`，其余为 `n2→n1`。因此本轮
差异不是 Agent 是否找到干预，而是干预后的协议因果路径能否闭合。
`target-local` 五次均发生可信 closure handoff 并到达 Risk，但只有 4/5 是干预后
由 closure 直接闭合。`g01-target-local` 在首段 closure quantum 耗尽后交还 Agent，
又执行 `Crash(n2)` 才达到目标。`public-fixed` 两次到达，三次在正确干预后
耗尽 Scenario 调用预算。

按整个方法臂计算，target-local 的 observed tokens 比 public-fixed 少 32.7%，调用数
少 3 次。这个差额属于完整方法臂结果，不应全部归因于 selector；模型输出长度、无效
proposal 和反馈轮次都包含在其中。target-local 的 qualified primary/replay work 略高，
因此不能宣称所有成本都下降。

10 个 Bundle 中封存的 fresh Replay 结果均为 stable；独立 evaluator 重新校验
Bundle/projector 并重算每个 Bundle 的四个 Target registry monitor，合计 0 violation。
evaluator 没有重新启动 Runtime 执行 Replay。这里的 Risk 是公开恢复场景，Oracle clean
表示闭合轨迹没有发现实现缺陷，不是“系统无缺陷”的证明。
为兼容既有 M4l7 `saved-bundle-oracle-audit/v1` 工件，audit JSON 继续使用字段名
`replay_stable`；它表示 Bundle 中记录的 Replay 结果，不表示 evaluator 重新执行了 Replay。

`bundles/` 保留 10 个确定性 gzip canonical Bundle，`audits/` 保留对应的
Oracle audit；不保留 provider journal。逐 Episode 的干预 step/source/target/leaf/item、
handoff step 和 handoff 后新干预由可信 Trace 提取并记入 `final-summary.json`。

## 传输事件

第一个 public-fixed 尝试在外部调用边界得到 `agent-transport-ambiguous`，本地 usage
未知，没有作为方法结果；它由独立的 `g01-public-fixed-r2` 替代。其余中断使用 durable
journal 原位恢复。有效 10 个 Episode 共 27 次模型调用、661,055 observed tokens；另有
一次未知 dispatch，均未超过预授权的 36 次调用和 1,200,000 tokens 上限。

## 结论边界

该结果支持一个窄结论：在这个固定三节点 etcd/raft Risk 中，Agent 负责选择干预、
Target-local 后端负责受限因果闭合，比让 Agent 继续猜测 Ready/message 时序更稳定。
样本只有五组，三个不一致 pair 均由 target-local 获胜，但配对精确检验的双侧
`p≈0.25`。只覆盖单 Risk 和单协议，不能据此证明统计显著性、一般优越性、缺陷
发现能力或跨协议可迁移性。下一步应在 OmniPaxos 上验证同一个 closure factory 接口，
而不是继续为 etcd/raft 增加专用场景。
