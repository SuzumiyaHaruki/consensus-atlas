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

## Blind Test Plan Planner v1

`deepseek_planner.py` 是第一个 Blind Strategy Agent。它通过隔离的版本化 JSON
子进程接收 opaque trial ID、Profile identity/node 投影、supported capability 名称、
opaque Coverage Debt、剩余预算、历史 proposals 和机械 findings，每轮只能返回一个受限
Test Plan Proposal。

```bash
make agent-campaign-etcdraft
```

Coordinator 会严格验证 opaque target ref、DSL、ID、预算和协议因果结构，再在可信侧把
ref 映射为真实义务并交给 Campaign Session。Planner 无法调用 Runtime、读取 Driver/SUT
identity、真实义务、trace、Oracle 或 Ledger，或修改 Ledger、产生 observation、定义
evidence/Oracle 或输出分数。无效 proposal 已消耗的模型 token 仍会进入 blind Blackboard
审计。

v1 的无模型 fixture 已证明请求、拒绝、执行、反馈和 transcript 边界；尚未证明模型效果。
公开开发 scope 不等于 holdout。Scenario/Critic 仍应等待独立 holdout 显示角色混合的明确
失败类型。完整结果见 `docs/stage-m4.12-blind-planner-v1.md`。

## Control Runtime v2 Policy Planner

`deepseek_control_planner.py` 是 M5.12 `PlannerProposal` 编译边界的真实模型客户端。它只读取
Scope digest/run/budget/replay、公共 nodes/Action kinds、目标描述和 Proposal schema，单次返回每 run
的 priority/rules 或 public random seed。Runtime seed、PSS identity、enabled set、trace、Coverage 和
Oracle 不进入请求。

```bash
make experiment-etcdraft-v2-deepseek-planner
```

该入口每次只调用一次官方 DeepSeek API，不自动 retry；模型输出仍由 Go strict decode、机械编译、
唯一 Execute/Replay 和 partial-work ledger 约束。M5.13 首次公开 smoke 以运行期 rule 不可达结束，
因此只证明 transport/审计/失败计费连通，不证明 Agent 策略有效。
