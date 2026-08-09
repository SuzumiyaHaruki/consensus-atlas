# M5.19：Campaign 配置与 checkpoint 基础

日期：2026-08-09

状态：完成

## 目的

本阶段不运行 etcd/raft、Agent 或模型，也不实现按墙钟循环。它先在协议无关层冻结 Campaign 的配置、
attempt 终态和 append-only checkpoint，使后续 Coordinator 不能边运行边解释预算或覆盖失败成本。

```text
CampaignConfig
  target identity + experiment identity
  logical work ceiling + operational wall-clock ceiling
                         |
                         v
CampaignAttemptRecord
  terminal outcome + durable artifact digest + complete WorkLedger
                         |
                         v
CampaignCheckpoint sequence N
  previous digest + record[N] + cumulative totals + mechanical stop reason
```

## 核心语义

1. `wall_clock_ceiling_ms` 是运维停止上限，不属于方法效果的可比预算。
2. 权威逻辑预算固定为 attempts、primary scheduler decisions、primary/replay work、model calls 和
   model tokens；任一项超过 ceiling 时 append 失败，不能先写后修。
3. attempt 只有 terminal record 才能进入 checkpoint：completed、rejected、failed 或 invalid。
4. 每条 attempt 必须引用 artifact digest；下一阶段的持久化层负责证明 digest 对应工件已经 durable。
   failure 同样不能没有证据工件。
5. checkpoint 0 绑定配置但没有 attempt。后续 checkpoint 每次只保存新的一条 record、累计 totals 和
   previous digest；完整链占用 O(n) 而不是反复复制历史前缀的 O(n²)，旧对象不可被原地修改。
6. append 只允许发生在 `running` checkpoint；达到 attempt/logical/wall-clock ceiling 后机械停止。
7. resume 必须重新提供并验证完整 CampaignConfig；target 或 experiment identity 改变即拒绝。
8. elapsed wall time 必须单调，但不能增加或减少逻辑 `WorkLedger` totals。

## 最小对象

### CampaignConfig

- target ID 与 opaque target identity digest；
- experiment spec digest；
- `CampaignLogicalBudget`；
- wall-clock ceiling；
- 固定 `checkpoint-after-each-terminal-attempt` policy；
- canonical digest。

### CampaignAttemptRecord

- ordinal、attempt ID、input/artifact digest；
- terminal outcome 与可选稳定 failure；
- 完整 `WorkLedger`；
- canonical digest。

### CampaignCheckpoint

- config/target/experiment identity；
- budget 与 wall-clock ceiling 副本；
- sequence、previous digest 和当前 terminal record；
- mechanically summed totals、elapsed milliseconds、stop reason；
- canonical digest。

## Stop reason 优先级

1. attempt 数到达上限：`attempt-limit`；
2. 任一逻辑 work ceiling 精确到达：`logical-budget`；
3. elapsed 到达墙钟上限：`wall-clock-ceiling`；
4. 否则：`running`。

超过任何逻辑 ceiling 是 invalid append，不是 stop reason。相同 attempt/work 在不同机器上可以得到不同
wall-clock stop 点，但正式比较必须截断到共同逻辑 work 前缀。

## 验收

- [x] config、attempt 和 checkpoint 都能 canonical seal/revalidate；
- [x] checkpoint 0 不消耗 work，第一次 append 产生 sequence 1 和 previous digest；
- [x] rejected/failed/invalid attempt 的 artifact 与成本不会消失；
- [x] append 不修改旧 checkpoint，ordinal/elapsed 只能单调增加；
- [x] 超预算、已停止后 append、记录篡改和断链均被拒绝；
- [x] target/experiment/config identity 改变时 resume 被拒绝；
- [x] wall-clock stop 不改写逻辑 totals；
- [x] 现有 MethodLedger identity 回归保持不变；
- [x] 普通全量测试、vet、race 和旧路径审计通过。

## 实现结果

协议无关实现位于 `internal/controlexperiment/campaign.go`。它没有导入 Adapter、Raft、SUT 或模型
transport。checkpoint 使用增量链：sequence 0 只绑定配置，后续每项只包含当前 terminal record、
累计逻辑账本和 previous digest。`ValidateCampaignCheckpointChain` 逐项重算链关系及累计 totals，
因此 N 个 attempt 的持久化数据为 O(n)，不会在每个 checkpoint 中重复完整历史形成 O(n²) JSON。

三项定向测试覆盖 canonical JSON round trip、四类终态、顺序/墙钟单调性、预算停止、超预算拒绝、
目标身份漂移、record/digest/totals 篡改和缺失链项。现有 `MethodLedger` 的求和逻辑只抽取为同包 helper，
已有 identity 由全量回归继续约束。

验证结果：

- `go test -count=1 ./...`：通过；`cmd/control-experiment` 94.706 秒；
- `go vet ./...`：通过；
- `go test -race -count=1 ./internal/controlexperiment`：通过，52.594 秒；
- `make audit-race-shards`、`make audit-no-v1`、`make audit-no-retired-experiment`：通过；
- 两份总体规划字节一致，`git diff --check` 通过。

## 当前限制

本阶段只定义内存对象和验证规则。artifact digest 尚未与磁盘文件机械对账，checkpoint 尚未使用
crash-safe 文件布局，进程中断恢复和 attempt provider 也尚未实现。因此它不能单独执行“配置一段时间
自动测试”的流程，也没有产生新的 SUT、PSS、Oracle 或 Agent 实验结果。

本阶段完成后仍没有真正的 Campaign 执行循环。下一小阶段才实现 checkpoint 的文件布局与 crash-safe
append，然后再连接一个不调用模型的确定性 attempt provider。
