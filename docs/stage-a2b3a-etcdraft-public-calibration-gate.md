# A2b3a：etcd/raft 公开校准预注册与离线门禁

日期：2026-08-13

分支：`feature/agentic-consensus-testing`

状态：真实目标 + fixture provider 闭环完成；真实 DeepSeek 调用待显式授权

## 1. 研究问题

A2b1/A2b2 只证明协议无关 fixture 上的 Explorer 契约和 provider 生命周期。A2b3a 回答更具体的问题：同一
契约能否不修改 Runtime 或 semantic core，直接作用于真实 etcd/raft prefix，并在模型调用前把实验输入和
比较边界冻结。

本阶段没有测试模型效果。它先排除 root 后选、prompt 漂移、失败重试、结果工件不完整和 target projector
越界这些会污染真实校准的因素。

## 2. 预注册输入

仓库内 `benchmarks/experiments/etcdraft-v2-semantic-explorer-a2b3/spec.json` 固定：

- M5.23e official-source bundle/corpus digest；
- `invoked` root、28 decisions、prefix digest；
- 15-action root frontier digest；
- Raft leader-change-with-inflight-proposal risk；
- public knowledge、semantic-only hypothesis；
- depth 2、16 WorkItems、12,000 search work；
- deterministic semantic baseline 与 Explorer identity；
- DeepSeek v4 Flash、temperature 0、max output 1,200、0 retry；
- 2 calls、16,000 reported tokens；
- `public-calibration-not-agent-effectiveness-holdout-or-correctness` 分类。

测试每次从 source 重新构造这些对象并与 checked-in spec 逐字段比较。root 在调用 planner 前固定，不能根据
模型输出或结果切换到 0/54 decision root。

## 3. 真实目标 projector

`etcdraftSemanticPrefixProjector` 是 target-local composition，不进入 `internal/`。它只接受冻结 Raft family
RiskWitness，从 exact prefix 的 etcd/raft Evidence 机械投影：

- workload invoked at coordinator；
- coordinator changed while proposal remained in flight；
- old coordinator restarted after change。

通用 semantic best-first 仍只看 `RiskWitnessResult`，不解释 term、role、leader 或 etcd/raft 类型。Projector
identity 改为符合通用 method-token 规则的稳定 ID，没有放宽 generic validator。

## 4. Backend 契约修正

当时的真实组合暴露了一个通用缺口：A1 构造路径仍默认 DFS，而 A2 使用 Semantic Best-first。
A4r 已删除无消费者的 A1 episode 契约，并要求唯一 `TestHypothesis` 对实际 backend 显式验证。
本阶段增加 `NewTestHypothesisForBackend`；旧构造器继续包装 DFS，旧 identity 和调用方行为不变。A2b3
hypothesis 只声明 semantic-best-first 支持，没有为了通过构造而虚假加入未使用 backend。

## 5. 可恢复 composition

显式 CLI strategy 为 `etcdraft-public-semantic-explorer-a2b3`。只有同时给出 corpus、artifact directory 和 key
file path 才能进入；没有默认 key 路径或默认联网 Make target。

新目录先写 spec，再创建复用的 provider journal：

- intent 已写、credential 暂不可用：返回 typed deferred，不写 terminal artifact；
- prepared resume：读取显式 key 后只 dispatch 一次；
- content-ready：Explorer strict parser 决定 semantic accepted/rejected；
- provider failed/ambiguous：terminal，不重发；
- artifact 已完成：重新验证 source 和 sidecar exact content 后直接返回。

Artifact 同时绑定 baseline、Explorer result/failure、provider audits、ModelWork 和机械 comparison。comparison
只包含首个扩展 candidate/prefix 是否不同、两侧 reached candidate 数和 accepted/rejected calls，不产生分数。

## 6. Fixture 结果

真实 etcd/raft baseline 在 28-decision root 上完成 16 WorkItems，并通过独立 source replay。

成功 fixture：

```text
create: intent durable -> key unavailable -> deferred -> 0 HTTP calls
resume call 1: unknown authority JSON -> rejected
resume call 2: reverse complete Candidate permutation -> accepted
artifact: completed, 2 calls, 14 fixture tokens
resume completed artifact: 0 additional HTTP calls
```

Explorer 与 deterministic baseline 的首个实际 prefix digest 不同，因此 Agent 允许的 permutation 确实进入了
真实 etcd/raft exact-prefix execution，而不只是改变报告顺序。

失败 fixture 使用 HTTP 503，形成 1 call/0 token 的 provider failed audit、planner-failed Explorer artifact，
并占用 terminal artifact path。它不会 fallback 到 baseline 冒充 Explorer 成功。

## 7. 可信边界和未完成项

- 没有读取真实 key或调用 DeepSeek；
- fixture 生成的修正不说明模型会修正；
- exact-prefix verification 不等于 qualified workload ExecutionBundle；
- 没有新增 PSS/Coverage/Oracle 结果；
- public calibration 不支持 holdout、方法优势或正确性结论；
- artifact/sidecar 是本 calibration 的 target-local composition，不是第二套通用 Campaign。

## 8. 代码增长审查

A2b3a 新增 804 行 target-local 生产代码、198 行集成测试、37 行冻结 JSON 和本阶段文档。增长对应四个现行
消费者：真实 prefix projector/预注册 spec、成功/失败 artifact、可恢复 runner 和显式 CLI。它没有新增
Runtime Action、generic semantic schema、PSS/Oracle/Ledger、第三方依赖或大 Trace 工件，也复用了 A2b2
journal。这个体积已达到本 calibration composition 的上限；A2b3b 只允许运行冻结入口和记录结果，生产代码
净增目标为零。A2c 开始前应再次检查这些 target-local 类型能否保持单一消费者，不能抽象成新框架。

## 9. 验证

- `go test ./...`：通过；`cmd/control-experiment` 263.072 秒；
- `go vet ./...`：通过；
- `make audit-race-shards`、JSON 语法和 `git diff --check`：通过；
- protocol-neutral A1–A2b2 定向 race：通过，5.562 秒；
- A2b2/A2b3 真实目标定向 race 运行约 4 分钟无输出后按既有约定人工终止；未出现 race 报告，但不能标记通过，
  且本阶段不再重跑；
- 总体规划仓库/桌面副本 SHA-256 一致；
- 没有 `agents/` Python 目录，故没有适用的 Python 单测；
- 未运行已知可能超时的 full-race。

## 10. A2b3b 唯一动作

用户显式授权后，使用冻结 Make/CLI 入口运行一次真实模型调用。不得在调用前后修改 spec、prompt、root、
knowledge、budget、baseline 或 retry。结果无论成功、全拒绝、provider failure 或与 baseline 相同都原样保留。
