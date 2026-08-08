# M5.17bR2 Experiment 在线旧路径删除

日期：2026-08-08

## 结论

M5.17bR2 已删除 M5.10–M5.13 遗留的 pre-admission 在线路径，同时保留全部历史 Markdown/JSON。
当前 `cmd/control-experiment` 只接受 qualified workload、action-class random 和 trace mutation；所有
成功执行都必须绑定 Qualification，且最终进入现有唯一 `execute/executeRun` 与 strict replay。

这一步不改变 Control Runtime、Action、Adapter、PSS、ExecutionBundle、Oracle 或 evaluator 语义。它的
作用是让下一阶段只面对一个可信实验入口，避免在 M5.17c0 修正终止和 admissible frontier 时继续维护
两套互相冲突的准入语义。

## 删除内容

- `internal/controlexperiment/planner.go` 及测试：旧 PlannerScope/Proposal/Attempt 和对
  `ExecuteLegacy` 的调用；
- `cmd/control-experiment/deepseek.go` 及测试：引用已删除 Python 脚本的 M5.13 transport；
- `internal/modelcommand`：删除唯一消费者后不再保留的进程/密钥文件 runner；
- `ExecuteLegacy`，以及 `fixed`、`random`、`stub-planner`、`deepseek-planner` CLI composition；
- 对应 Make targets、flags 和在线工件复验测试。

通用 `PolicyVersion`、`RandomPolicyVersion` 和 `ModelWork` 账本字段没有删除：前两者仍是当前/下一阶段
qualified policy 的公共机制，后者属于 bundle schema 中的通用成本字段。删除的是绕过当前准入的
composition，不是把未来合格随机基线或模型成本表示一并删掉。

## 保留内容

- M5.10 fixed、M5.11 random、M5.12 stub 和 M5.13 DeepSeek 的阶段文档与 JSON；
- M5.15 workload、M5.16 bundle/calibration、M5.17a action-class 和 M5.17b mutation 的在线路径；
- 当前公开 artifact digest、build audit 和 evaluator report；
- 单一 `ExecuteQualified` / `ExecuteQualifiedBundle` 执行边界。

历史工件说明“当时运行过什么”，不要求主分支永久保存已失去当前信任边界的 decoder/transport。未来
Agent 将以 `ProtocolKnowledgePack -> AgentSemanticView -> Guarded TestIntent` 重建最小入口，不恢复旧
绝对 decision rule Planner。

## 体积结果

以 M5.17b 的 15,136 行 production、6,659 行测试为基线，本阶段完成后为：

- production Go：14,068 行，减少 1,068 行；
- test Go：6,232 行，减少 427 行；
- 合计：20,300 行，减少 1,495 行。

`make audit-no-retired-experiment` 新增稳定守卫，禁止 `ExecuteLegacy`、旧 Planner schema identity、已删除
脚本和 `internal/modelcommand` 重新进入编译源码。历史文档和 JSON 不受该守卫影响。

## 证明与没有证明

本阶段证明：

- 当前 composition 不再提供绕过 Qualification 的在线入口；
- 当前三种 workload 策略仍使用相同 qualified executor/bundle 路径；
- M5.15–M5.17b 的冻结回归仍可从当前源码重算。

本阶段没有证明：

- 各策略已经面对相同 admissible frontier；
- pending/quiescent workload 已能形成合法 bundle；
- PSS feedback 已具备跨代 corpus 所需 provenance；
- Agent、Random 或 mutation 在 holdout 上优于其他方法。

## 下一步

进入 M5.17c0，只修改 Experiment/target composition：

1. 统一 `runtime-enabled -> admissible -> policy` 选择边界并记录两层 digest；
2. 引入可重放的 `budget-exhausted/quiescent/configured-stop`，保留 pending workload；
3. 将 target routing 从 PSS Mapper 拆到确定性 WorkloadRouter；
4. 保持框架、trace、qualification 和 projection 错误为 invalid。

Control Runtime 核心继续冻结。

## 验证

- `make test`；
- `go vet ./...`；
- `go test -race ./...`；
- `python3 -m unittest discover -s agents -p 'test_*.py'`（源码已删除，发现 0 项）；
- 两份总体规划 `cmp` 字节一致；
- `git diff --check`。

本阶段没有修改 JSON；M5.15、M5.16、M5.17a 和 M5.17b 的 checked-in digest/summary 由现有 Go 集成
测试重算通过。
