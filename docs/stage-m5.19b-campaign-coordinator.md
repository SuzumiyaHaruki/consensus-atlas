# M5.19b：Deterministic Campaign Coordinator

日期：2026-08-09

状态：完成

## 目的

本阶段在 M5.19a 的可信恢复会话上增加最小执行循环，但不连接具体协议、SUT 或模型。Coordinator 每次
只把冻结身份、当前 head 和剩余逻辑预算交给 provider；provider 返回 terminal result，可信层验证成本、
构造 attempt record 并调用现有 crash-safe persistence。

```text
CampaignRecovery + CampaignConfig
              |
              v
mechanical remaining allowance + digest-bound AttemptRequest
              |
              v
bounded provider(ctx, request) -> terminal result + opaque artifact bytes
              |
              v
validate outcome/work <= allowance
              |
              v
CampaignRecovery.CommitAttempt -> durable checkpoint -> next request / stop
```

## 信任边界

1. provider 不能提交 ActionID、enabled set、checkpoint、totals 或 stop reason；它只返回结果数据。
2. attempt ID、ordinal、input digest 和 artifact digest 由可信层机械构造。
3. request 绑定 Campaign/config/target/spec、previous checkpoint 和 exact remaining allowance。
4. provider 返回普通 error 时，当前 Coordinator 锁死且不重试、不提交空白 attempt，Campaign 保持原
   head；该运行不能被当作完整方法结果。执行后失败必须由 provider 返回带 artifact/WorkLedger 的
   terminal failed/invalid result。
5. WorkLedger 任一维超过 allowance 时在 artifact 落盘前拒绝，不能通过超支换覆盖。
6. Coordinator 的 wall clock 是运维上限。每个 provider call 获得 remaining-time context；provider 必须
   尊重 cancellation。当前不提供强制终止不合作 goroutine/process 的能力。
7. session elapsed 从恢复 head 累加，只计算本次活跃运行，不把离线停机时间算入方法成本。
8. provider 身份和具体计划必须由 `experiment_spec_digest` 绑定；通用层不解释协议类型。
9. artifact-less provider error 的失败锁目前只存在于本次 Coordinator 内存中；新进程仍可重新恢复目录。
   因此正式 provider 必须把任何已发生成本的失败转换成 terminal result。跨进程 durable coordinator
   failure marker 留到真实 provider 接入时设计，当前不能把此类失败后的目录当作正式完整结果。

## 本阶段验收

- [x] remaining allowance 从 checkpoint totals 机械相减且不会为负；
- [x] request canonical digest 绑定 config/head/ordinal/allowance；
- [x] deterministic provider 在新目录连续提交多个 attempt 并由 attempt limit 停止；
- [x] 每次提交后的 head、totals 和恢复结果一致；
- [x] 恢复已停止 Campaign 不再次调用 provider；
- [x] provider error、超 allowance、空 artifact、非法 outcome 均不改变磁盘 head；
- [x] wall-clock 到达后由 terminal attempt checkpoint 记录，deadline 前不启动无界调用；
- [x] 没有增加第二套 Runtime、target-specific Coordinator 或真实模型入口；
- [x] 普通全量、vet、受影响包 race 和旧路径审计通过。

## 实现结果

`CampaignAttemptRequest/v1` 绑定 config/target/spec、previous checkpoint、ordinal 和六维 remaining
allowance。`CampaignCoordinator` 只持有 M5.19a 产生的可信恢复会话；每次 `Step` 给 provider 一个带
remaining-time deadline 的 context，验证 terminal result 后机械构造 record 并提交。`Run` 只是重复
同一个 Step，不包含第二套 Runtime、scheduler 或协议判断。

确定性见证执行 3 个 attempt，并在第 1 项后完整关闭/恢复目录再继续。第 2 项是带 artifact 和成本的
terminal failed result，第 3 项到达 attempt limit。最终为 15 primary decisions、18/18 primary/replay
work、elapsed 30 ms；四个 checkpoint 完整恢复。独立见证还覆盖 logical-budget stop 和 terminal
wall-clock stop。

provider error、超 allowance、空 artifact 和非法 outcome 都在落盘前失败；当前 Coordinator 随即锁死，
第二次 Step 不会再次调用 provider，磁盘仍为 sequence 0。这个锁不是跨进程 durable marker，因此此类
目录不能当作正式完整结果，限制未被隐藏。

验证结果：

- `go test -count=1 ./...`：通过；`cmd/control-experiment` 97.273 秒；
- `go vet ./...`：通过；
- `go test -race -count=1 ./internal/controlexperiment`：通过，50.812 秒；
- `make audit-race-shards`、`make audit-no-v1`、`make audit-no-retired-experiment`：通过；
- 两份总体规划字节一致，`git diff --check` 通过。

本阶段没有运行 SUT、读取 key、调用模型或生成 benchmark JSON。

本阶段完成后，系统具备协议无关的可恢复多-attempt 外壳，但 provider 仍是确定性测试实现。下一阶段将
现有 etcd/raft qualified executor 包成第一个真实 provider，并冻结它的 artifact 内容和 spec identity。
