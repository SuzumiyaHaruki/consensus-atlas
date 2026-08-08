# M5.18b3 Unseen Follow-up Baseline

日期：2026-08-08

## 结论

本阶段完成了 `source seeds 1/2/3 -> defect-blind feedback -> unseen seed 4` 的确定性闭环，
但没有调用模型，也没有得到新的缺陷结论。它同时纠正了 M5.18b2 暴露的一处方法比较语义问题：
旧 action-class batch 使用严格 workload 终止语义，而 uniform 使用 Experiment v2 的 pending
语义，所以旧反馈中的 `2 complete / 1 failed` 与 `3 complete / 0 failed` 不能直接比较。

M5.18b2 的已发布 digest 与工件保持不变，作为发现该问题的历史校准证据；M5.18b3 新增
`AgentBatchFeedbackView/v2`，显式区分框架 execution status 和 workload
`planned/completed/pending`，并让两个 source 方法都使用 Experiment v2。

## 输入、处理、输出

```text
输入
  same qualification + same semantic view
  + action-class-v2 / admissible-uniform
  + frozen source seeds 1/2/3
                          |
                          v
处理
  validate full bundles and bound Mapper
  -> reproject comparable feedback v2
  -> freeze source cost and unseen seed 4
  -> deterministic completion-first/canonical rule
  -> existing qualified executor + strict replay
                          |
                          v
输出
  AgentFollowUpSpec/v1
  + charged deterministic follow-up result
  + small recomputable feedback/spec/summary artifacts
```

Agent 仍然不能提交 ActionID、enabled set、PSS state key、Oracle verdict 或执行结果。source bundle
可以在测试进程中物理复用，但 `AgentFollowUpSpec` 要求每个待评价 arm 在逻辑账本中承担完整 source
成本；失败 follow-up 不更换 backend 或 seed。

## 可比较的 source 结果

两个方法都使用 seeds 1/2/3、3 attempts、294 primary / 294 replay ceiling，并得到 291 个
PSS samples：

| backend | execution | workload completed/planned | actual work | unique states | new states |
|---|---:|---:|---:|---:|---|
| action-class random v2 | 3 complete | 2/3 | 294/294 | 241 | 83, 82, 76 |
| admissible uniform | 3 complete | 2/3 | 294/294 | 238 | 82, 75, 81 |

两个方法都各有一个 workload 在预算内保持 pending。`241 > 238` 只是这三个公开 source seed 上的
coarse discovery 差异，不是覆盖完整度、缺陷检出率或方法优越性证明。

## 未见 seed 的确定性 baseline

冻结规则 `workload-completion-then-execution-canonical-v2` 先比较 workload 完成比例，再比较框架
execution 状态；完全相同时按 backend ID 选择。因此该批次机械选择 `action-class-random`，并在
预先冻结且不与 source 重叠的 seed 4 上执行。

seed 4 的 primary/replay 都消耗 97 work，strict replay 稳定，产生 97 个 PSS samples 和 79 个
unique states；但 workload 为 pending，且真实 Trace 没有覆盖 intent 要求的所有 hard ActionKind。
结果以 `EXPERIMENT_COMPILED_INTENT_HARD_ACTION_MISSING` 保存为计费的 `execution-failed`，没有替换
backend 或 seed。source 实际成本 588/588，加 follow-up 后每个 arm 实际计费 685/685，低于冻结
ceiling 686/686；模型调用数为 0。

这不是缺陷 verdict。strict replay 通过只证明执行可复现，不会把未满足测试意图自动改写为成功。

## 身份与工件

- corrected semantic view：`1910d179...30de4d`；
- corrected feedback source：`c1de0afc...96fb2a5`；
- corrected feedback：`f9ce5f2d...6aeec9e`；
- follow-up spec：`bd8fbc93...eab0be7`；
- intent：`145f05a2...e3c8cbe`；plan：`963cd25f...d9dc13`；
- report：`1ce8ae13...ef8588a`；bundle：`31916d15...12b8f6d`；
- summary：`e0bda99f...e60677e`。

仓库只保存 [feedback](../benchmarks/experiments/etcdraft-v2-agent-follow-up-m5.18b3/feedback.json)、
[spec](../benchmarks/experiments/etcdraft-v2-agent-follow-up-m5.18b3/spec.json) 和
[summary](../benchmarks/experiments/etcdraft-v2-agent-follow-up-m5.18b3/summary.json)。完整 report/bundle
保存在 ignored `artifacts/experiments/etcdraft-v2-agent-follow-up-m5.18b3/`。

## 实现边界

- `internal/controlexperiment` 的 feedback v2 与 follow-up spec 不导入 etcd/raft 类型；
- etcd/raft composition 只负责把 backend identity 对应到真实 policy，并证明 source/follow-up seed；
- source fixture 复用只优化测试时间，不改变 MethodObservation、feedback 或历史 digest；
- M5.18b2 v1 工件不回写，文档将其降级为“发现不可比语义的历史校准”；
- 当前 follow-up spec 是实际 seed 的可信覆盖层；compiled macro plan 保留 catalog 的默认 seed，执行器
  必须再验证 report 的 frozen follow-up seed，不能由 Agent 或调用者自由覆盖。

## 当前没有证明

- 没有调用修正后 prompt，也没有 one-shot 与 feedback Agent 的结果；
- 没有 Agent 与 deterministic baseline、random 或专家方法的效果比较；
- 没有 private candidate/control holdout；
- 没有证明 PSS 状态数能预测缺陷检出；
- 没有证明两个方法在更多 seed、workload 或协议上等价或存在稳定排序。

## 验证

- `make test-fast`、`make test`、`go vet ./...`：通过；
- `make test-race-full`：通过，`cmd/control-experiment` 耗时 983.930 秒，低于 20 分钟 package ceiling；
- 137 个非 `artifacts` JSON、23 个 schema JSON、4 个既有 build input/audit schema 实例：通过；
- 133 个 Markdown 本地链接、两份总体规划逐字节一致、`git diff --check`：通过；
- `internal/controlexperiment` dependency closure 不包含 etcd、Raft 或 HashiCorp 包；
- M5.17c1/c2、M5.18b0/b1/b2 历史 identity 回归保持不变；checked-in feedback/spec/summary 与
  ignored 完整工件逐字节一致。

## 下一阶段

M5.18b4 只做两次冻结的一次性调用：无 feedback 与有 feedback 使用同一 semantic view、risk、hard
constraints、预算和 seed 4，反馈只能改变 preference。调用前必须冻结两份 prompt/request bytes 和
预期账本边界；M5.18b3 的 deterministic outcome 不得进入任何模型视图。每个 arm 最多 1 call、无重试，
即使 parse、compile、执行或 hard-action 验证失败也原样计费并停止。只有完整 race 门恢复通过后才允许调用。
