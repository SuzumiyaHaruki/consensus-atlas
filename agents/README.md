# Agent boundary

ConsensusAtlas 的主路径只保留三个 Agent 角色：

- `Onboarding Agent`：基于不可修改的 Protocol Knowledge Contract 生成 Binding、薄 Driver 和可执行见证；
- `Strategy Agent`：基于已验证 Profile 和覆盖债务生成声明式语义目标；
- `Failure Analyst`（可选）：解释和最小化反例，但不能确认缺陷。

Coordinator 是确定性 Go 程序，不是负责投票或确认结论的 Agent。Agent 无权修改 Contract、覆盖分母、Oracle、见证标签、重放结果或 conformance 结论。

## Onboarding Agent

模型接入点是 `internal/autoonboard.Generator`。每次请求包含固定 Contract、digest、Driver Manifest、允许场景、尝试次数以及上一轮 Binding/完整机械报告；当前响应只能产生下一版 Binding 和已有见证引用。Driver 源码生成属于后续独立 patch/build 沙箱阶段。

```bash
make auto-onboard-etcdraft
```

当前同时提供静态 proposal 和 DeepSeek V4 Flash model-backed Generator，二者共用同一 Coordinator/Validator：

```bash
make auto-onboard-etcdraft-llm
```

DeepSeek 客户端使用 JSON Output、温度 0 和非思考 Flash 模式。模型只看到 Contract、Driver Manifest、允许场景、上一轮 Binding 和完整机械验证报告；不读取参考 Binding。每轮记录 model/fingerprint、生成参数、token/duration 和 prompt/request/response digest，不记录 API key。key 文件以 no-follow 方式读取，凭据只能发送到官方 DeepSeek endpoint。

## Contract-only Onboarding Agent

`deepseek_contractonly.py` 承担更严格的从零实验：输入不含具体系统的参考 Driver、Binding 和场景，输出是受限路径下的完整 Go integration、witness JSON 与 Binding。`internal/contractonly` 检查路径、大小、Go AST/import、Contract identity 和 source digest，再在无网络 bubblewrap 中构建。可信 evaluator 过滤生成 Driver 自报的 Contract labels，并从 Raft PSS 前后状态和实际 Host 事件重建 transition、timeout、commit 与 host evidence。

```bash
make contract-only-etcdraft
```

当前实验表明单个 Flash 调用适合修复 schema/编译错误，但在 Driver 与 witness 同时生成时不能稳定定位语义失败。下一版应先用 Contract 派生的固定 lifecycle/API fixtures 验收 Driver，再让 witness Agent 针对已经通过的 Driver 生成场景。

## Test Plan Planner v0.1

`deepseek_planner.py` 是第一个真实 Strategy Agent。它通过隔离的版本化 JSON
子进程接收冻结 Profile 身份、当前 Coverage Debt、Driver Manifest、剩余预算、
历史 proposals 和机械 findings，每轮只能返回一个受限 Test Plan Proposal。

```bash
make agent-campaign-etcdraft
```

Coordinator 会严格验证目标、DSL、ID、预算和协议因果结构，再把计划交给可信
Campaign Session。Planner 无法调用 Runtime、修改 Ledger、产生 observation、
定义 evidence/Oracle 或输出分数。无效 proposal 已消耗的模型 token 仍会进入
Blackboard 审计。

v0.1 live run 证明 Agent 能取得真实覆盖，也证明单 Planner 不能稳定修复低层
Scenario 错误。下一阶段将目标选择、计划构造和失败修复拆成 Planner、Scenario、
Critic 三个窄角色，而不扩大其权限。完整结果见
`docs/stage-v0.1-single-planner.md`。
