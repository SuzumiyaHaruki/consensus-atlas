# M5.21tA：边际 Binding 成本审计

日期：2026-08-11

## 结论

M5.21tA 没有增加 target capability。审计确认，三个共识实现和 protocol-free fixture 重复维护了
Runtime→Adapter 命令 envelope、Invoke/result/mode 参数结构以及解码校验。这部分是协议无关 wire
contract，满足“至少两个真实消费者”的提取门；现已由 `internal/control` 唯一定义，Runtime 负责构造，
各 Adapter 只解码协议专用内容。

提取后，审计生产面从 4,013 行降至 3,932 行，净减少 81 行。raft-rs target-owned Binding 从
1,218 行降至 1,185 行；M5.21t workload 相对 981 行基线的边际成本由 +237 降至 +204。新增的共享
helper 为 57 行，不计入 target-owned 数字，但已经被 Runtime 和四个 Adapter 路径实际使用。因此本轮
不是把 target 代码改名成 shared，也不是为了跨过 1,200 行观察线而改统计口径。

既有 raft-rs 轨迹保持为 28 decisions、一个 Invoke、一个 ClientResult，trace digest 仍为
`0dabc8204c8b174d59b25b8e1bc9ccc6ca0131c4ce7bf5bfc7a69c9dc4a56164`；worker binary digest 也未变化。

## 机械行数账本

| 审计生产面 | 提取前 | 提取后 | 净变化 |
|---|---:|---:|---:|
| `internal/controlruntime/actions.go` | 302 | 278 | -24 |
| protocol-free fixture | 580 | 559 | -21 |
| etcd/raft changed Adapter surface | 1,287 | 1,253 | -34 |
| HashiCorp Raft changed Adapter surface | 626 | 600 | -26 |
| raft-rs target-owned Binding | 1,218 | 1,185 | -33 |
| new shared command contract | 0 | 57 | +57 |
| 合计 | 4,013 | 3,932 | **-81** |

另新增 37 行 shared-contract regression test，不混入生产 Binding 数字。raft-rs 当前组成是：

| 类别 | M5.21t | M5.21tA | 变化 |
|---|---:|---:|---:|
| target-native Rust worker | 376 | 376 | 0 |
| Rust/Go protocol + client | 198 | 179 | -19 |
| target normalization/application/bookkeeping | 644 | 630 | -14 |
| target-owned production total | 1,218 | 1,185 | **-33** |

## 可以共享的边界

- command schema、logical time、parameters、frozen item 的 wire envelope；
- Invoke input、effect result 和 crash mode 的最外层参数包装；
- payload schema/encoding/digest 校验与 JSON 解码。

这些对象描述的是 Control Runtime 与 Adapter 的接口，不包含 Raft role、term、Ready、proposal 或 commit
语义。正常命令的 JSON field order 和 bytes 保持不变；共享 decoder 现在统一拒绝错误 schema/encoding，
这是 wire contract 的收紧，不改变合法轨迹。

## 不应继续共享的边界

- **输入含义**：etcd/raft 的 input operation 与 raft-rs request/value 不是同一种协议对象；
- **提交路径**：RawNode、跨进程 worker 和 HashiCorp FSM 的推进/回调所有权不同；
- **持久化语义**：etcd/raft 分离 persist/apply/advance，当前 raft-rs 只有合并 Ready effect；
- **ClientResult 产生条件**：request origin、applied node 与去重规则属于目标专用可信代码；
- **进程桥接**：目前只有 raft-rs 使用 Rust worker，尚不满足共享 process kit 的消费者门；
- **PSS/Oracle**：协议 Evidence 到语义状态或安全判定的映射不能由相似字段名推导。

继续把这些代码抽到 Core 会隐藏实际语义差异，并不会降低新协议接入工作量。

## 回归结果

- `make test-raftrs-binding`：通过；28-decision workload digest 未变化；
- `go test ./...`：通过；
- `go vet ./...`：通过；
- 受影响的 control、Runtime、fixture 和三个 Adapter 包 `go test -race`：通过；
- `git diff --check`：通过；
- raft-rs worker SHA-256：`93ebfe45dee35f0455bae2eac76a999ea4ed64514bf8dce36e4498688471d4fc`。

## 没有证明

- 1,185 行并不等于“新 Adapter 已足够薄”；workload 的 target-specific 增量仍为 204 行；
- 两个 strict Raft 实现仍不能证明跨协议普适性；
- raft-rs 仍没有 durable restart、PSS/Oracle、Qualification 或 Experiment admission；
- 本阶段没有 Agent/model call 或新增 benchmark/campaign trial；仅执行回归测试，没有 Coverage 或缺陷结论。

## 下一阶段停止线

下一阶段固定为 M5.21u 非 Raft construction/yield 可行性 probe，而不是继续给 raft-rs 堆 capability：

1. 只检查一个非 Raft CFT 实现能否以原始公开接口构造确定性多节点实例，并截获 message/natural-time
   边界；不先写完整 Adapter；
2. probe 期间不得修改公共 Action、Runtime 或 Core PSS schema；需要修改即记录抽象缺口；
3. 输出 construction surface、scheduler ownership、随机/墙钟边界、最小 Binding LOC 估计和机械
   go/no-go；
4. 只有通过 probe 才进入有明确 LOC 停止线的 Binding spike。失败结果保留，不通过挑提交或包装成
   strict 来凑第二目标。
