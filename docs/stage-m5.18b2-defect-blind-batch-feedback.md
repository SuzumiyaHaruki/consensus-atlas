# M5.18b2：Defect-blind batch feedback view

日期：2026-08-08

状态：可信 batch feedback 数据面与 preference-only 消融边界完成；未调用模型

## 结论

本阶段没有继续调用 LLM，而是先解决“反馈从哪里来、能包含什么、能改变什么”三个问题：

```text
real MethodObservation + full bundles + bound PSS mapper
                         |
                  trusted revalidation
                         |
                         v
            AgentBatchFeedbackView/v1
   (common budget + completion + cost + coarse PSS only)
                         |
                         v
        preference-only GuardedTestIntent ablation
```

Agent 不能提交 PSS key，也不能直接读取 MethodObservation。目标 composition 必须为每个 backend 提供完整
bundle 和 Mapper；通用构造器重新运行 `MethodObservation.ValidateBundles` 后才生成视图。仅有自洽 digest 的
外部 JSON 不能成为反馈来源。

## Agent 能看到什么

每个 backend 只暴露：

- 共同 method ceiling；
- execution attempts、completed/failed attempts 和粗粒度失败分类；
- observed runs、PSS samples、unique states 和逐 run new-state 数；
- primary/replay/model 成本。

固定解释为 `coarse-discovery-without-completeness-denominator`。视图不包含 bundle、trace、manifest、
qualification、build、candidate、root cause、Oracle、具体 failure code 或 state key。`SourceDigest` 只绑定
已经重新验证的 `(backend ID, MethodObservation digest)` 集合。

## 反馈只能改变什么

`ValidateFeedbackPreferenceAblation` 要求有反馈和无反馈 intent 的以下字段完全相同：

- semantic view；
- risk ID；
- decisions；
- required capabilities/actions；
- FaultEnvelope。

反馈只能改变 `prefer.backend_ids` 和 `prefer.actions`。模型不能通过增加 decision/fault budget、删除 hard
Action 或换一个更容易的 risk 来制造“反馈提升”。最终 proposal 仍须进入原 strict parser 和 trusted
compiler。

## 真实共同预算见证

两个 backend 使用预先固定的 seeds 1/2/3 和相同 ceiling：3 attempts、294 primary、294 replay。

| backend | attempts | completed/failed | actual primary/replay | samples/states | new states |
|---|---:|---:|---:|---:|---|
| action-class random | 3 | 2/1 | 294/196 | 194/165 | 83, 82 |
| admissible uniform | 3 | 3/0 | 294/294 | 291/238 | 82, 75, 81 |

action-class seed 3 在 decision 96 未完成 workload。该 attempt 的 98 primary work 被保留，未进行 replay，
也没有用替换 seed 凑成第三条成功轨迹。完整方法账本保存准确的 `EXPERIMENT_WORKLOAD_INVALID`，Agent view
只看到一个 `execution-failed`。

uniform 复用了 M5.17c2 冻结方法，MethodObservation digest 仍为 `90fb30c3...deff75`。action-class 新
MethodObservation 为 `a7d2f3ad...e97d34`；最终 feedback digest 为
`a7a6e7e95bcbe68fe97a099639724313afd22e4caeb410fc55e0eafe168ab7c2`。

Agent 可见工件为
[feedback.json](../benchmarks/experiments/etcdraft-v2-agent-feedback-m5.18b2/feedback.json)，文件 SHA-256 为
`cdddab6b84dd7f328ad1a357f96e5af9e186fcc778b60849d3f0c9c49d3e0016`。约 12.8 MB 的完整 report/bundle/
observation 保存在 ignored `artifacts/`，没有提交重复 Trace body。

## 本阶段证明了什么

- batch feedback 来自 bundle-backed PSS 重投影，不是 Agent 或 MethodObservation 自报；
- 两个 backend 可以在相同 ceiling 和预注册 seeds 下机械比较完成度、成本和 coarse discovery；
- 不完整方法不会因缺少 replay 而伪装成低成本成功；
- Agent-visible JSON 不暴露执行、构建、候选、Oracle 或 PSS state identity；
- 反馈消融不能改变 hard constraints 或预算；
- 整条反馈生成路径不需要模型服务。

## 本阶段没有证明什么

- `238 > 165` 不证明 uniform 更容易发现缺陷，也不是覆盖完整度；
- action-class 的一次 workload failure 不是协议缺陷或 defect verdict；
- 没有证明 LLM 能正确理解或利用反馈；
- 没有 no-feedback/with-feedback 模型结果；
- 没有 candidate/control、holdout 或新 Oracle 结果；
- 当前仍只有一个人工冻结 risk，不证明知识包普适。

## 一个必须先解决的新边界

当前反馈来自 seeds 1/2/3，而 M5.18b0 的 one-shot backend 仍固定 seed 1。如果立即让 Agent 根据反馈选择
backend，下一次执行会重复已经观察过的 source，而不是产生新的 follow-up test。下一阶段不能直接调用
模型；必须先冻结 `source batch -> feedback -> unseen follow-up seed/MethodSpec` 的身份和总成本，让有/无反馈
方法使用同一个未见 seed，并把 source construction 计入两边账本。

## 代码成本

M5.18b1 收口为 19,562 行 production / 7,731 行 tests。本阶段收口计数为 20,405/7,888，净增
843 production / 157 tests。增量包含通用 feedback artifact、action-class 失败 attempt 账本、目标 composition、
CLI 持久化和真实集成见证；没有增加 Agent 数量、Runtime、PSS 维度、Oracle 或 backend。

## 验证与新的工程约束

`make test-fast`、`make test` 和 `go vet ./...` 通过。M5.18b2 新增的六次执行反馈测试单独
race 通过，耗时 339.201 秒。JSON 语法、schema 语法/4 个 build 实例、123 个 Markdown
本地链接、总体规划镜像和通用包协议依赖审计均通过。

`make test-race-full` 没有通过：`cmd/control-experiment` 在 20 分钟 package ceiling 处超时，
当时仍在执行 M5.17c2 的旧真实方法测试。没有产生 race 报告，但超时不等于通过。
这说明新集成见证正在重复构造已验证 seeds 1/2/3；下一阶段必须先复用同一可信 source
fixture 并恢复 full-race 门，不能继续通过复制真实批次测试扩大耗时。

## 下一最小阶段

M5.18b3 先实现一个很小的 deterministic follow-up MethodSpec：source seeds 固定为 1/2/3，follow-up seed
固定为 4，source + proposal + follow-up 的成本全部入账；无反馈和有反馈只能在同一 action-class/uniform
集合中改变 preference。实现前先让 source 只构造一次并恢复 full-race 门。完成这个机械闭环后，
才进行两次有界模型调用并加入无模型 deterministic baseline。
