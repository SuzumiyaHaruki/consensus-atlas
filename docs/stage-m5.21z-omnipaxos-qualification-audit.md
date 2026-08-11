# M5.21z：OmniPaxos 机械 Qualification 与 admission 差距审计

日期：2026-08-11

## 结论

M5.21z 的机械审计完成，但 M5.21 尚不能在这里收口。OmniPaxos 使用未缩小的
`portable-cft-control-v2` 得到稳定 partial Qualification：8 个 required capability 中 3 个 validated、
3 个 unsupported、3 个 unvalidated，`qualified=false`。结果不是人工标签，完整 Profile、Manifest、
conformance cases、unsupported declaration 和 findings 都保存在可重验 QualificationBundle 中。

现有 executor 确实允许从 partial report 绑定“全部 validated 的命名子集”，并拒绝完整 Portable set。
但是它只验证调用方提交的 `RequiredCapabilities`，尚未从 Config 的 Policy ActionKinds、Workload 和
FaultEnvelope 机械反推最低需求。调用方因而可能少报 capability。当前不能把 subset binding 成功直接
解释为 meaningful OmniPaxos experiment 已获得可信 admission。

## 机械资格结果

| capability | 状态 | 直接原因 |
|---|---|---|
| strict-yield-evidence | validated | collect-idempotent 通过 |
| pure-enabled-check | validated | enabled-check-pure 通过 |
| opaque-invoke-boundary | validated | opaque-invoke-accepted 通过 |
| natural-temporal-progress | unvalidated | 缺 natural-released-message-recovery |
| runtime-owned-message | unvalidated | 缺 released-message-lifecycle |
| strict-decision-replay | unvalidated | 缺两个 crash/lifecycle witness；opaque replay 已通过 |
| crash-restart-incarnation | unsupported | Manifest 无 crash/restart/power-loss |
| audited-entropy-replay | unsupported | SUT exercised core 无可审计 RNG API，empty tape 不获信用 |
| formal-process-isolation | unsupported | 当前资格流程没有独立进程隔离 witness |

完整 summary 为 `total=9, required=8, validated=3, unsupported=3, unvalidated=3, failed=0`。

## 为什么已通过的 Temporal/Message/Replay 没有被计入资格

M5.21v 至 M5.21y 已经用真实 worker 验证 natural tick、消息 retain/drop/duplicate/deliver、fresh Replay、
Invoke、Core PSS 和 Agreement。然而 Portable v2 Profile 的三个相关 capability 依赖以下 witness：

- natural temporal 依赖 `natural-released-message-recovery`；
- runtime-owned message 依赖 `released-message-lifecycle`；
- strict replay 同时依赖 crash-cancel、released-message-recovery 和 opaque-invoke replay。

前两个 witness 都包含 Crash/Restart。对没有生命周期 Action 的 Adapter，它们无法单独给 Temporal 或
Message 信用；strict replay 也被同一组合阻塞。这说明问题位于 Qualification witness 粒度，不是统一
Action/Runtime 无法控制 OmniPaxos。M5.21z 没有为了让结果好看而删除 requirement 或手写 validated。

## admission 审计

当前 `ExecutionAdmission` 的正面性质：

- 不要求 report 全局 `Qualified=true`；
- 只接受 report 中确实为 `validated` 的 capability；
- 绑定精确 Profile、Qualification、Adapter、Build、Configuration 和 Manifest digest；
- 完整 Portable set 在 OmniPaxos 上会被机械拒绝。

仍存在的可信边界缺口：

```text
caller RequiredCapabilities ──> BindExecutionAdmission ──> ExecuteQualified
          ^
          └── 尚无 Config(Policy/Workload/FaultEnvelope) 的机械下界校验
```

因此，调用方可以只声明 `strict-yield-evidence`，同时在 Policy 中列出消息、时间甚至生命周期 Action；
admission 当前不会发现这种少报。etcd/raft composition 一直请求完整 required set，所以历史严格实验不受
影响；但新的 partial target 不能依赖调用方自律建立论文级可信性。

## 代码与工件

- 新 target qualification composition：83 行；测试：122 行；
- CLI 只增加第三 target 与 worker path 参数，没有复制写文件逻辑；
- Adapter、Runtime、Conformance core、Experiment admission、上游 OmniPaxos 修改：全部 0；
- frozen bundle：446 行，它是完整可验证证据，不是阅读摘要；
- reader summary：`benchmarks/qualifications/omnipaxos-v2-m5.21z/summary.json`；
- bundle digest：`43f9e40340d7f8f76ab8d795bce779195a6635818597b9cedc00b3c106bb370c`。

不同 Rust toolchain 可能产生不同 BuildID，因此 `make adapter-qualify-omnipaxosv2` 将 current bundle 写入
ignored `artifacts/`，不会覆盖历史冻结工件。

验证通过：fresh qualification 两次 bundle equality、冻结 bundle 内部验证、完整 OmniPaxos binding gate、
全仓 `go test ./...`、`go vet ./...`、一次 `go test -race ./qualifications/omnipaxosv2`（8.989 秒）、
JSON 语法、`git diff --check` 和两份总体规划字节一致性检查。

## 下一阶段：M5.21zR

M5.21zR 是 M5.21 的最后一个修复复核阶段，范围必须保持有限：

1. 新建版本化、协议无关的解耦 witness/profile；保留 `portable-cft-control-v2` 及历史 digest 不变；
2. 从 Config 的 Policy、Workload、FaultEnvelope 和 Replay 要求机械推导/验证 capability 下界；
3. 少报必须在 SUT 执行前拒绝，不能依赖 Agent、Adapter 或 composition root 自律；
4. 重新运行 OmniPaxos Qualification，证明 Temporal/Message/Replay 可以独立获信，但 crash/entropy 仍不通过；
5. 只运行一个无 crash/durability 的 admitted smoke，不加入 Agent、Campaign、评分或真实缺陷声明。

如果该修复无法保持小范围，应停止 M5.21 并把问题明确列为架构阻塞，不能继续堆 target 专用代码。
