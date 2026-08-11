# M5.19c：首个真实 Campaign Provider

日期：2026-08-09

状态：完成

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

- [x] artifact-less provider error 写入可完整校验的 durable marker；重启后同一 ordinal 不会重试；
- [x] marker 篡改、identity 漂移、marker 后追加和 stopped-head marker 均被拒绝；
- [x] etcd/raft provider 的 spec/seed/target/request/artifact/work 身份可机械重验；
- [x] 两个真实 attempt 经现有 qualified executor 完成，Campaign 在中途恢复后继续并机械停止；
- [x] 保存的每个成功 artifact 均含 replay-stable bundle 与 Core PSS，不新增计算旁路；
- [x] 普通全量、vet、受影响包 race、v1/retired audit 和 `git diff --check` 通过；
- [x] 不生成需要提交的大型 benchmark JSON，不读取 key，真实模型调用为 0。

## 明确不在本阶段完成

- 通用 Campaign CLI、任意时长运行与进程级强制终止；
- Agent 根据反馈生成下一 attempt；
- 跨 attempt 的统一 Coverage/Oracle/缺陷汇总报告；
- 第二个协议的 provider；
- 对 etcd/raft 正确性、覆盖完备性或方法优势作结论。

## 实现与验证结果

`etcdraftCampaignProvider` 位于 `cmd/control-experiment` composition root，生产代码 322 行。它的
executor field 在构造时唯一绑定现有 `etcdraftExecution`；测试替换该 field 只用于证明 typed
`ExecutionFailure` 被转换为 terminal failed artifact，不是另一个生产执行器。通用 Campaign 包
没有导入 etcd/raft 类型。

真实见证使用 2 attempts、16 decisions/attempt 和 seeds 41/42。attempt 1 durable 后关闭并完整
恢复目录，attempt 2 继续完成；最终 stop reason 为 `attempt-limit`。两项都是 replay-stable
completed artifact，各含 17 个 Core PSS samples。累计账本为 32 primary decisions、36 primary
work 和 36 replay work，与两个 artifact 逐项求和相等。

验证结果：

- `go test -count=1 -timeout 20m ./...`：通过；`cmd/control-experiment` 98.423 秒；
- `go vet ./...`：通过；
- `go test -race -count=1 ./internal/controlexperiment`：通过，43.886 秒；
- 真实 provider 定向 race：通过，15.888 秒；
- race shard exact-once、no-v1 和 no-retired-experiment audit：通过；
- 没有读取 key、调用模型或写入 checked-in benchmark JSON。

下一阶段应先增加协议无关 Campaign summary/reader，使 CLI 无需解析 checkpoint 私有布局即可输出
attempt/outcome/work/artifact 索引；随后再增加显式 etcd/raft 离线 runner。
