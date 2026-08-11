# M5.21o：Formal multi-pair CLI

日期：2026-08-11
状态：strict private artifact CLI 已接通；`formal_ready=false`

## 目标

M5.21n 只有 Go library entry。本阶段让 curator 能通过当前 `cmd/defect-eval` 调用它，同时保持公开
single-pair calibration 模式不变。CLI 只负责严格 I/O、全量前置校验、调用既有 fresh subprocess runner
和保存 private ledger；不新增 Runtime、Adapter、Oracle、Agent schema 或评价指标。

## 输入、处理、输出

```text
输入（curator private environment）
  FormalBenchmarkContract + passed FormalExposureAudit
  + MethodSpec + FormalFreshInputs(trial -> audit/binary path)
                         |
                         v
处理（任何 SUT execution 前）
  strict JSON/mode separation + exact composition admission
  + exact 2N trial set + all BuildAudit/binary digest checks
                         |
                         v
  existing evaluator-owned subprocess runner per opaque trial
  + M5.21n FormalFreshEvaluation
                         |
                         v
输出
  new artifact directory/{trial}/report.json,bundle.json
  + new private FormalFreshEvaluation ledger
```

## CLI

```bash
go run ./cmd/defect-eval \
  -formal-contract /private/contract.json \
  -formal-exposure-audit /private/exposure.json \
  -formal-inputs /private/inputs.json \
  -method-spec /private/method-spec.json \
  -fresh-artifacts /private/run-001 \
  -out /private/evaluation-001.json
```

`FormalFreshInputs/v1` 是 curator-side path manifest，每项只含 opaque `trial_id`、BuildAudit path 和 binary
path。相对路径以 input manifest 所在目录为基准；这些路径不会进入 formal evaluation 或 Agent-facing
artifact。

formal flags 必须完整出现，并且不能与 public `-manifest`、submitted bundle 或 single-pair audit/binary
flags 混用。当前 composition root 只注册 official etcd/raft DecisionProjector 与通用 Agreement monitor；
未知 projector/缺失 monitor 在任何 SUT 执行前拒绝。扩展第二 target 只应增加注册项，不得修改
`internal/defectbench`。

CLI 会先读取并验证全部 audit/binary，再开始第一个 trial，避免前五个 trial 已运行后才发现第六个文件
不匹配。artifact directory 和 final output 必须不存在且不得相互嵌套；已有路径不会被覆盖。实际 per-trial execution
继续使用既有 `runFreshBundle`、MethodSpec timeout 和 `ExecutionBundle/v3`，没有第二套 runner。

## Synthetic 集成结果

回归用 official etcd/raft Adapter 运行一次 32-decision correct execution，再由 injected runner 将该同一
report/bundle 复用于 3 pair/6 trial，完整经过 path manifest、build evidence、formal evaluator 和 output
ledger：

- 3/3 control 为 `control-pass`；
- 3/3 synthetic candidate 为 `survived`；
- 0 false positive、0 invalid；
- 六个 runner 调用均发生在全量 preflight 之后；
- 篡改最后一个 binary 时 runner calls 为 0；
- 重用 artifact/output path 被拒绝。

摘要位于 `benchmarks/experiments/formal-multi-pair-cli-m5.21o/summary.json`。本阶段新增一次 in-process
official SUT integration execution，没有模型调用；fixture 没有启动六个 subprocess。

## 不能证明

六个 trial 仍引用同一份正确 bundle，build binaries 也是 synthetic private bytes。因此它不能证明：

- production subprocess 对六个真实 binary 的 provenance；
- 任何 candidate defect 被检出；
- private holdout 已经存在；
- Agent、Random 或其他方法的效果；
- 第二个共识实现已复用该 CLI；
- PSS/Coverage 对 defect detection 具有预测性。

真实 private pair 和第二 strict CFT 仍缺失，所以 `formal_ready=false`。

## 下一阶段

停止继续增加 evaluator/CLI 模型。下一步应在仓库外建立 curator pack：至少 3 个由人工复核为不同根因的
private candidate/control pair，生成真实 BuildAudit/binary 和 exposure audit，再用本阶段 CLI 做第一次
正式 dry-run。如果尚无这组数据，应把它报告为实验输入缺口，而不是再用公开或 synthetic pair 凑分母。
跨实现结论仍需随后让第二个 strict deterministic CFT target 复用同一 contract/CLI。
