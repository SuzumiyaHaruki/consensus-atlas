# M5.16：ExecutionBundle 与首个 v2 可信评测闭环

日期：2026-08-08

## 结论

M5.16 已完成 M5.15 之后缺失的证据闭环：同一执行器可以产生自包含、digest-bound 的
`ExecutionBundle`，可信目标投影器把 opaque Evidence 转换为最小 decision observation，通用
TraceIntegrity/Agreement Oracle 再形成 control/candidate 评测账本。没有迁入旧 Coverage，也没有调用
Agent。

## ExecutionBundle 内容

schema `consensus-atlas/execution-bundle/v1` 绑定：

- Experiment/PSS/config/report/Manifest identity；
- 原 `RunReport` 与完整 v2 Runtime Trace；
- Runtime 外部 Offer 造成的 `PreparationRecord`；
- 最终 Runtime Snapshot；
- step 0 与每个 decision 后的 typed Core PSS sample；
- 从 trace transition 与最终 Snapshot 重建的 client history；
- target-owned projector 产生的 canonical decision history；
- 完整 QualificationBundle、primary/replay work ledger 和 bundle self digest。

第一版只允许一个 measured run。这样每个 bundle 的 primary/replay 成本拥有明确归属，避免把多 run
报告中的 setup 或 prepare 成本人为摊分。

## TraceIntegrity 新发现的缺口

最初 bundle 只包含 scheduler trace。真实运行立即暴露：`Runtime.OfferInvoke` 发生于 decision 之前，
它改变 offered state，却没有 scheduler record。因此初始 state digest 无法直接连接第一个 Select。

修复是显式记录 prepare transition，而不是放宽 Oracle：

```text
before snapshot --Offer--> after snapshot --Select(same ActionID)--> trace record
```

TraceIntegrity 检查 prepare 顺序、自身 digest、before/after state chain、Action ID immediate-selection、
连续 scheduler step、command/action binding、Evidence digest、entropy tape、final state 和 final Snapshot。
任一失败都把 trial 标为 `invalid`，不能获得 defect-kill credit。

## Agreement 的协议解耦

新 `semantic.DecisionObservation` 只有 step、participant、position 和 SHA-256 value digest。
`DecisionProjector` 是可信 target composition 接口；通用 executor/Oracle 不知道 term、log entry、QC、
ballot 或 Raft 类型。

etcd/raft projector 使用 applied index 作为 exact position，使用 application durable prefix digest 作为
exact value digest。投影结果 canonical sort、去重并单独计算 digest；bundle 落盘后会用同一 projector
从完整 Evidence 重算，不能只相信保存的 decision history。

## source-bound calibration

candidate 固定到 `go.etcd.io/raft/v3@v3.6.0`，对 `raft.go` 做一次 exact-match 文本转换：节点 3 收到
normal entry 时保持 proposal JSON 可解析，但改变 value 的一个 Base64 字符。构建器：

- 只接受唯一文本匹配和预期 original/transformed digest；
- 在临时 module copy 中离线只读构建；
- 不修改 module cache 或外部 raft 工作区；
- 用 linker variable 写入 opaque SUT BuildID；
- 审计 source/module/binary/command/toolchain digest。

它是主动构造、公开可见的 calibration，不是假装历史回归或非公开 holdout。

## 实验结果

| 项目 | control | candidate |
|---|---|---|
| BuildID | `go.etcd.io/raft/v3@v3.6.0` | `sut-c9811ab0ed8e2f39` |
| decisions | 96 | 96 |
| preparations | 1 | 1 |
| primary/replay work | 98 / 98 | 98 / 98 |
| Core PSS samples | 97 | 97 |
| decision observations | 8 | 8 |
| TraceIntegrity | pass | pass |
| Agreement | pass | conflict at step 55, position 5 |
| evaluator | `control-pass` | `killed` |

summary 为 1 control、0 false positive、1 calibration candidate、1 killed candidate/root cause、0 invalid。
Coverage 和 PSS discovery 没有参与 evaluator verdict。

## JSON 体积处理

完整 bundle 各约 1.9 MB，因为 96 个 record 中每一步都保存了完整 Evidence、状态转换和 entropy binding。
这对本地重算 Oracle 是必要证据，但不适合作为每次提交的报告。当前策略是：

- 完整 bundle 写入 ignored `artifacts/`；
- checked-in 目录只保存 build input/audit、benchmark manifest 和 evaluator report；
- report 通过 bundle/trace/build digest 指向完整本地证据。

后续只有跨机器分发成为真实需求时，才增加压缩/内容寻址存储；本阶段不为文件大小另建存储系统。

## 证明与未证明

已证明：执行证据链可闭合；协议专用 Evidence 解释可留在 target composition；TraceIntegrity failure
不能记 kill；同预算正确 control 与构造差异可以被首个 v2 evaluator 区分。

未证明：Agent 优势、holdout 检出、Coverage/PSS 预测力、Agreement 对任意协议的充分性，以及任意目标
实现或框架自身正确性。

## 复现

```bash
make experiment-etcdraft-v2-bundle
make experiment-etcdraft-v2-calibration
make evaluate-etcdraft-v2-calibration
go test ./internal/semantic ./internal/oracle ./internal/defectbench ./cmd/control-experiment ./cmd/defect-eval
```

该流程不调用 LLM，不读取 key 文件，也不修改外部 raft 工作区。
