# M5.4a HashiCorp Raft 控制表面探针

日期：2026-08-07

## 结论

第二个真实实现值得继续：在不修改官方 HashiCorp Raft、不修改公共 Runtime/Conformance、且新增
254 行生产 Go 的条件下，探针截获了同一次自然选举中从 `n1` 发往 `n2`、`n3` 的真实
`RequestVote`，并冻结为已有 `control.ProducedItem{Kind: ItemMessage}`。两次 fresh run 与冻结工件
得到同一 canonical digest。

这只通过 M5.4a 的继续门，不是完整 Adapter。v1.7.3 的自然计时器和随机抖动没有注入边界，因此
当前 `qualified=false`、`strict_replay=false`。

## 输入、处理、输出

```text
official github.com/hashicorp/raft v1.7.3
+ fixed n1/n2/n3 voter configuration
                 |
       natural wall-clock election
                 |
       custom Transport.RequestVote
                 |
       freeze + validate + target sort
                 v
2 Runtime ItemMessage + capability findings + canonical report
```

固定 SUT 身份：

- module：`github.com/hashicorp/raft`；
- version：`v1.7.3`；
- revision：`c0dc6a0b2c7e889f31e5ab2f7ed90ceb159acffe`；
- Go module sum：`h1:DxpEqZJysHN0wK+fviai5mFcSYsCkNpFUl1xpAW8Rbo=`；
- 三 voter、protocol version 3、10ms heartbeat/election 配置均由 digest 绑定。

## 实验结果

| 检查项 | 结果 | 边界 |
|---|---|---|
| 官方模块与固定配置 | observed | 依赖版本、revision、module sum 与配置摘要已记录 |
| outbound RPC | observed | 捕获 `n1→n2`、`n1→n3` 两个 term 2 `RequestVote` |
| Runtime message freeze | observed | 两个 item 均通过现有 `ProducedItem.Validate` |
| virtual clock | unsupported | `HASHICORP_RAFT_CLOCK_NOT_INJECTABLE_V1_7_3` |
| entropy control | unsupported | `HASHICORP_RAFT_PACKAGE_RANDOM_NOT_INJECTABLE_V1_7_3` |
| delivery/replay | not tested | `M5_4A_CAPTURE_ONLY` |

冻结工件：
[`report.json`](../benchmarks/probes/hashicorp-raft-v1.7.3-m5.4a/report.json)。报告摘要为
`d31033a603185019dd5d35a8942463d31af07b89ec574d849dec59a60ad498b7`。

探针没有使用 `sleep` 或 polling。`context` deadline 只作为实验失败上限，不能使 SUT 的自然选举
成为虚拟时间，也没有用于 strict replay 声明。RPC goroutine 的到达顺序不进入身份：结果按目标排序。

## 代码增长审计

| 范围 | 新增生产 Go |
|---|---:|
| `adapters/hashicorpraftv2/probe.go` | 227 |
| `cmd/hashicorpraft-probe/main.go` | 27 |
| **合计** | **254** |
| 公共 `internal/control*` / `internal/conformance` | **0** |

注：最终行数以阶段完成时 `wc -l` 为准；350 行 M5.4a 停止线未触发。测试和冻结工件不计入生产
Go 预算。新增依赖导致 `go.sum` 增长，但不属于可执行框架抽象。

## 已证明与未证明

已证明：HashiCorp Raft 的 `Transport` 是可复用的消息截获边界；同一公共 `ItemMessage` 模型无需协议
分支即可承载 etcd/raft 与 HashiCorp Raft 的真实出站消息。

未证明：Runtime 尚未拥有 RPC 的 release/deliver/drop，三个 HashiCorp 节点尚未互通；没有
`FSM.Apply`、crash/restart、持久状态恢复、严格 replay、Conformance 或资格结果；更没有 Agent、PSS、
Coverage 或缺陷检出结论。

## 下一最小阶段

M5.4b 只扩展这一纵向切片：让三个官方节点的 outbound RPC 由 Runtime 持有并选择 deliver/drop，
再提交一个 opaque command，取得同一轨迹中的 `FSM.Apply` 证据。自然 wall clock 与包级随机仍明确
Unsupported，不得为它们修改公共 Action/Item。累计生产 Go 不超过 800 行；若必须复制第二套 Runtime
或依赖到达顺序才能完成，立即停止。

## 验证

- `go test ./...`、`go vet ./...`、`go test -race ./...`；
- Python Agent 7 项单测；
- 全仓 JSON 语法、冻结报告 self-digest 与 fresh artifact 字节一致；
- 连续 5 轮 probe digest 一致；
- generic `internal/` 禁止导入 etcd/raft 与 HashiCorp Raft 的耦合测试；
- `git diff --check`，以及两份总体规划字节一致。
