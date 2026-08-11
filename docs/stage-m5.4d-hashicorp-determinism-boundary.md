# M5.4d HashiCorp Raft 确定性边界决策

日期：2026-08-07

## 结论

官方、未修改的 HashiCorp Raft v1.7.3 不能仅通过当前薄 Adapter 获得虚拟时钟、分域 SUT entropy
或 strict replay。本阶段机械决策为 `freeze-unsupported`，没有实现伪时钟、源码 overlay、全局随机
替换或第二套 Runtime。

该结论不撤销 M5.4c 已验证的消息、crash/restart、incarnation 和 opaque invoke 能力；基础控制能力
与严格重放继续分别计证。

## 机械审计输入与输出

审计绑定以下身份：

- module：`github.com/hashicorp/raft@v1.7.3`；
- revision：`c0dc6a0b2c7e889f31e5ab2f7ed90ceb159acffe`；
- module sum：`h1:DxpEqZJysHN0wK+fviai5mFcSYsCkNpFUl1xpAW8Rbo=`；
- 六个协议关键源码文件的独立 SHA-256；
- 官方 `Config` 的 19 个公开字段；
- 公开顶层 Clock/Timer/Random/Entropy 注入标识符扫描结果。

测试从 module cache 的真实依赖重新读取源码并生成审计结构，再与冻结工件比较 canonical digest。
工件为 [M5.4d report](../benchmarks/probes/hashicorp-raft-v1.7.3-m5.4d/report.json)，digest 为
`44eaacc59d995ec451ed8f5d6e18db39c645317cdcd1046ea261fede3d3f66aa`。

## 证据

| 文件 | 墙钟调用 | `randomTimeout` 调用 | entropy 调用 |
|---|---:|---:|---:|
| `config.go` | 0 | 0 | 0 |
| `log.go` | 2 | 0 | 0 |
| `raft.go` | 22 | 5 | 0 |
| `replication.go` | 6 | 3 | 0 |
| `snapshot.go` | 5 | 1 | 0 |
| `util.go` | 1 | 0 | 4 |

核心事实：

1. `Config` 可设置 timeout 数值，但没有 Clock/Timer 对象；设置时长不等于控制时间推进。
2. `randomTimeout` 直接使用包级 `math/rand.Int63()` 和 `time.After()`。
3. 选举、复制和快照路径共有 9 个 `randomTimeout` 调用，另有直接 `time.Now/After/Since`。
4. `Log.AppendedAt` 来自 `time.Now()`，会进入被 Transport 冻结的真实 payload。
5. HashiCorp 包没有 Adapter 可提供的 RNG/Entropy 接口。

## 为什么不使用全局 `rand.Seed`

调用方可以在包初始化后重置标准库全局 `math/rand`，但这不是合格的 SUT entropy 注入：

- 三个节点共享同一进程级随机流；
- 并发 goroutine 的调用先后会改变后续结果；
- 无法按 node/incarnation/domain 形成 RandomDraw tape；
- 会污染进程内其他使用 `math/rand` 的代码。

因此它最多是脆弱的实验技巧，不能声明 `audited-entropy-replay`。

## 为什么本阶段不增加进程 worker

每节点独立进程可以加强真正的进程退出和全局随机流隔离，但无法通过 Transport 或普通进程边界
接管 Go runtime 内部的 monotonic clock、`time.After` 和 `time.Now`。若再使用源码 overlay、替换
依赖或非官方 fork，SUT identity 和“官方未修改实现”前提会改变。

worker 仍可能作为未来 process-isolation 能力单独研究，但不能为当前版本支付 virtual-clock 或
strict-replay 的资格债务，所以 M5.4 不实现它。

## 代码账本

| 范围 | M5.4c 收口值 | M5.4d | 变化 |
|---|---:|---:|---:|
| HashiCorp 生产 Go | 923 | 923 | 0 |
| test-only 机械审计 | 0 | 200 | +200 |

M5.4c 文档中的 921 行是最后一项 stale-target eligibility guard 加入前的计数；实际收口值为 923。
M5.4d 没有新增生产行为，因此遵守“无新行为则生产代码净增不大于零”的门槛。

## 最终能力决策

| capability | M5.4d 决策 | 稳定原因 |
|---|---|---|
| virtual-clock | Unsupported | `HASHICORP_RAFT_CLOCK_NOT_INJECTABLE_V1_7_3` |
| audited SUT entropy | Unsupported | `HASHICORP_RAFT_PACKAGE_RANDOM_NOT_INJECTABLE_V1_7_3` |
| strict replay | Unsupported | `WALL_CLOCK_AND_OPERATIONAL_TIMESTAMP_NOT_NORMALIZED` |

“Unsupported”只针对这个固定版本与当前官方未修改接入方式。未来版本若增加公开 hook，必须以新的
module identity、source audit、Conformance 和 Qualification 重新判断，不能人工改标签。

## 下一阶段

HashiCorp 主线只剩 M5.4e 收口：冻结双实现能力矩阵、修正文档计数、删除无价值辅助代码，明确
PSS/Coverage/Agent 只能消费 validated capability。M5.4e 不再追求把 3/9 人工补成完整资格。

## 复验

```bash
make audit-hashicorp-determinism
```
