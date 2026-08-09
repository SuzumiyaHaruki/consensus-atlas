# M5.19c：首个真实 Campaign Provider

日期：2026-08-09

状态：设计冻结，实现在推进

## 目的

本阶段把现有 etcd/raft qualified executor 接入 M5.19b 的协议无关 Campaign Coordinator。它是
composition wrapper，不是新的 Runtime、scheduler、Replay、PSS 或 Oracle 实现。

```text
frozen etcd/raft CampaignSpec + target manifest identity
                         |
                         v
generic CampaignAttemptRequest(config/head/ordinal/allowance)
                         |
                         v
thin provider -> existing etcdraftExecution -> ExecuteQualifiedBundle
                         |
                         v
terminal artifact(report + bundle | typed ExecutionFailure + exact work)
                         |
                         v
generic CampaignCoordinator -> artifact-first checkpoint commit
```

## 冻结边界

1. `CampaignSpec` 只冻结 provider schema、现有 strategy、每 attempt decision bound、首个 policy seed
   和 seed 派生规则；第 `n` 个 attempt 的 seed 只能是 `first_seed+n-1`。
2. Campaign `target_identity_digest` 使用 Adapter Manifest digest；每个完成 artifact 中的 report/bundle
   manifest 必须与 request 中该 digest 相同。
3. 成功 artifact 保存完整 `Report` 与 `ExecutionBundle`，并重验各自 digest、相互引用、request/spec/seed
   和 WorkLedger。它直接来自现有 `etcdraftExecution(..., captureBundle=true)`。
4. 现有执行器返回 `ExecutionFailure` 时，provider 必须把 typed phase/code/decision、已经发生的完整
   `WorkLedger` 和 request/spec 身份保存为 terminal failed artifact；不能把这类成本降格成普通 error。
5. qualification、输入构造或 artifact sealing 等尚未产生可证明执行成本的错误可以返回普通 error。
   Coordinator 将它持久化为协议无关 failure marker；该 Campaign 不属于可比较的完整结果。
6. failure marker 绑定 config digest、当前 head、next ordinal、exact request digest 和稳定分类，不保存
   私有 error 文本。恢复看到 marker 后不得创建可执行 Coordinator 或追加 checkpoint。
7. artifact、checkpoint 和 failure marker 都使用既有 fsync/no-replace 规则。checkpoint 提交错误不猜测
   磁盘状态；调用方必须重新完整恢复，由恢复结果判断 checkpoint 或 marker 是否 durable。
8. provider 只能使用 request allowance。M5.19c 的实测配置在启动前证明单 attempt 的最大 decisions/work
   能放入冻结总预算；不得通过超支换取更多状态。
9. 本阶段不读取 key、不调用模型、不引入 Agent 策略，也不把 etcd/raft 类型放入通用 Campaign 包。

## 最小验收

- [ ] artifact-less provider error 写入可完整校验的 durable marker；重启后同一 ordinal 不会重试；
- [ ] marker 篡改、identity 漂移、marker 后追加和 stopped-head marker 均被拒绝；
- [ ] etcd/raft provider 的 spec/seed/target/request/artifact/work 身份可机械重验；
- [ ] 至少两个真实 attempt 经现有 qualified executor 完成，Campaign 在中途恢复后继续并机械停止；
- [ ] 保存的每个成功 artifact 均含 replay-stable bundle 与 Core PSS，不新增计算旁路；
- [ ] 普通全量、vet、受影响包 race、v1/retired audit 和 `git diff --check` 通过；
- [ ] 不生成需要提交的大型 benchmark JSON，不读取 key，真实模型调用为 0。

## 明确不在本阶段完成

- 通用 Campaign CLI、任意时长运行与进程级强制终止；
- Agent 根据反馈生成下一 attempt；
- 跨 attempt 的统一 Coverage/Oracle/缺陷汇总报告；
- 第二个协议的 provider；
- 对 etcd/raft 正确性、覆盖完备性或方法优势作结论。
