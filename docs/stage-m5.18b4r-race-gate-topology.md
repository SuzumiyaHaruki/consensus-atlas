# M5.18b4R：可审计的 race 门禁拓扑

日期：2026-08-09

状态：完成

## 阶段目的

M5.18b4-pre 的单一 full-race test binary 曾在 590.400 秒内通过。加入 request-freeze 回归后，
同一 package 的普通测试约 90.785 秒，但单一 race binary 两次达到 20 分钟 ceiling，分别停在不同的
既有重型回归中，且都没有报告 data race。现有证据更符合“多个真实三节点 fixture 在 race 下累积
CPU 成本超过 package ceiling”，不支持锁等待或新 Runtime 缺陷结论。

本阶段不提高 timeout，也不把“未见 race 告警”改写为通过。目标是让完整 race 回归既可完成，又能
机械证明没有少测。

## 冻结边界

本阶段只允许修改测试门禁、顶层测试分片清单和相应文档，不修改：

- Control Runtime、Action、Adapter 或调度语义；
- PSS Mapping、Oracle、Replay 或 evaluator；
- Agent prompt/request bytes、freeze identity、seed、预算或 transport；
- 真实 fixture 的 SUT run 数和共享关系；
- 已冻结的实验工件或历史阶段结论。

尤其不把 action-class seed 1 单独拆成新 fixture：M5.18b2 本来就需要完整三条 source runs，额外拆分
会让测试从三次执行膨胀到四次。

## 门禁设计

### 1. `test-race-core`

开发期并发回归信号。它覆盖协议无关 Runtime/Control、两个 Adapter 以及少量跨层代表性测试。
它使用 `-race -count=1`，但不声称覆盖全部重型实验测试，不能替代 full gate。

### 2. `test-race-full`

研究分支的穷尽 race 门禁。`cmd/control-experiment` 的顶层测试按共享 fixture 分成：

- `method`：feedback、follow-up、request freeze 和方法比较；
- `execution`：bundle、trace/corpus mutation、Experiment 语义和 OperationHistory；
- `agent`：transport、parser、compiler、key 边界和 one-shot 审计。

三个 shard 分别创建 test binary，各自使用独立 20 分钟 ceiling 和 `-count=1`。仓库其余 package 在
第四次 race 调用中执行，避免再次包含 `cmd/control-experiment`。

### 3. 机械完整性审计

版本化清单逐行保存 `shard TestName`。`audit-race-shards` 必须在执行前验证：

1. 行格式和 shard 名称合法；
2. 每个 shard 非空；
3. TestName 无重复；
4. 清单排序后的 TestName 与 `go test -list '^Test' ./cmd/control-experiment` 精确相等。

因此新增顶层测试但没有归入 shard、重复归类、拼错名称或删除测试而遗留清单都会使 full gate在运行
重型 fixture 前失败。分片只是 test binary 拓扑，不是 `testing.Short` 跳过或缩小测试集合。

## 明确暂不做

- 不再运行第三次单 binary 20 分钟诊断；它的信息增益已经很低；
- 不通过 `-p 1` 解释 package 竞争；最终超时记录中主要成本就在目标 package 本身；
- 不立即替换 `Snapshot` 的 JSON 深拷贝；这是可能的性能热点，但涉及 payload 复制语义，应先有基准和
  全 ItemKind 等价测试；
- 不为了 race 测试新增只服务测试的生产缓存 API；未来若建立正式 prepare/execute seam，再独立设计；
- 不把 `test-fast` 当成可信全量门禁；当前 `testing.Short` 覆盖范围仍需后续单独审计。

## 验收

- 总体规划两份副本逐字节一致；
- 清单与全部顶层实验测试恰好一一对应；
- `make test-race-core` 通过；
- 三个 control-experiment shard 分别通过；
- 其余 package 的 race 通过；
- `make test`、`go vet ./...` 和 `git diff --check` 通过；
- 没有 Runtime/Adapter/PSS/Oracle/Agent freeze identity 改动。

若某个独立 shard 仍达到 20 分钟 ceiling，则保留失败和具体 shard，不继续提高 timeout。此时才针对该
shard 的 fixture 或生产热点做独立 benchmark，而不是再次修改完整门禁口径。

## 实际结果

版本化清单与 `go test -list` 均得到 24 个顶层测试，重复项为 0，三个 shard 均非空。

| gate | tests / scope | 结果 | 用时 |
|---|---|---|---:|
| `test-race-core` Adapter/Control/Runtime | 两个真实 Adapter + shared control | pass | 最慢 package 35.571s |
| `test-race-core` representative experiment | workload + guarded intent | pass | 39.728s |
| method shard | 6 tests | pass | 353.374s |
| execution shard | 7 tests | pass | 128.527s |
| agent shard | 11 tests | pass | 88.508s |
| remaining packages | 除 `cmd/control-experiment` 外全部 package | pass | 最慢 package 34.626s |

三个 experiment shard 合计覆盖 24/24 个顶层测试，累计 570.409 秒，但每个 test binary 都远低于
独立 20 分钟 ceiling。其余 package 也全部通过，没有 data-race 报告。结果支持原诊断：旧失败来自
把所有重型 fixture 放在一个长期 test process 中产生的累计/非线性成本，而不是某个独立测试本身
超过 20 分钟。

为避免把同一批约 10 分钟测试无意义地再执行一次，本轮按顺序直接运行了 full target 的两个组成目标：
`test-race-control-shards` 与 `test-race-other`；`make -n test-race-full` 确认顶层目标只在完成三个审计后
依次调用这两个目标。普通 `make test`、`go vet ./...`、总体规划副本比较和 `git diff --check` 通过。

本阶段只新增 Make 门禁、24 行清单和文档，没有修改任何 Go 生产/测试源码、SUT fixture 或冻结工件。

## 后续入口

M5.18b4R 完成后回到 M5.18b4 request consumer：冻结 request bytes 依次进入 response audit、strict
parse、单份 baseline 校验、plan v2、seed-4 execution instance、已有 qualified executor 和
IntentOutcome。模型调用仍需要用户显式授权。
