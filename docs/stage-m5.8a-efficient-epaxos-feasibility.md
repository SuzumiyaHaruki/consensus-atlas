# M5.8a：efficient/epaxos 接入可行性检查

日期：2026-08-07

结论：`proceed-limited`。第三目标可以继续做一个 test-only message-port worker 实验，但当前不能创建
完整 Adapter，也没有新增任何 validated capability。

## 1. 本阶段要回答的问题

M5.8a 只检查一个问题：在不修改公共 Action、Control Runtime、Core PSS 和上游协议算法的前提下，
`efficient/epaxos` 是否存在足够的原生接缝，值得投入一个最小纵向实验。

本轮不实现搜索、Coverage、Agent、Oracle 或完整协议接入，也不为了通过资格而增加公共抽象。

## 2. 固定对象

| 项目 | 值 |
|---|---|
| repository | `https://github.com/efficient/epaxos.git` |
| commit | `791b115669fca472d3136f6a2eda46c00b3f8251` |
| tree | `708b8e37f6b50a4e6b6fcbb95045dc53ad7d6344` |
| source archive SHA-256 | `9ffde18a3bfb7763c0995ce20064d34c7ddfcf944510a845cbc655a94a676068` |
| local probe toolchain | `go1.25.8 linux/amd64`，legacy GOPATH mode |

所选文件及其摘要写入
[`report.json`](../benchmarks/feasibility/efficient-epaxos-m5.8a/report.json)，防止后续把另一个版本的
接口事实混入结论。

## 3. 实际观察

### 3.1 已观察到的接缝

- 生产包 `epaxos`、`genericsmr`、`server`、`master` 和 `client` 可在 legacy GOPATH 模式构建；
- 官方三节点 server 进程可建立 peer 连接，官方 client 提交一个命令后返回 `Successful: 1`；
- 外部命令可以经公开 `ProposeChan` 或 client TCP codec 进入；
- `genericsmr.SendMsg` 经公开 `PeerWriters` 写出带类型码的协议帧，接收端 listener 再把解码对象送入
  注册的协议 channel；
- `InstanceSpace`、`CommittedUpTo` 和 `ExecedUpTo` 提供了未来 Semantic Mapping 可读的候选字段。

### 3.2 不能据此认定的能力

| 能力 | 当前状态 | 原因 |
|---|---|---|
| Runtime-owned message | missing | 尚未稳定冻结、编号并选择性释放一个真实 peer frame |
| stable yield | missing | 构造器立即启动后台 goroutine，select loop 没有静止点握手 |
| natural temporal progress | Unsupported | batching、beacon、recovery、execution 使用不可注入的墙钟 sleep |
| durable restart | Unsupported | 存储路径可 append/sync，但固定源码没有稳定恢复入口 |
| strict decision replay | Unsupported | TCP、goroutine 与墙钟 smoke 不是确定性 Action replay witness |

上游 `epaxos_test.go` 与当前生产 channel 类型不一致，故上游测试构建失败被记录为
`UPSTREAM_TEST_FIXTURE_STALE`；这不改写生产包构建成功，也不能被忽略成“测试通过”。

## 4. 机械边界

`internal/architecture/epaxos_feasibility_test.go` 固定经过审阅的 source/smoke/interface facts，按集合规则
推导 blocker 和下一项允许工作，再与 checked-in report 做完整 JSON 比较。这里机械化的是
“事实到决策”，不是源码语义事实的自动发现，也不是 Adapter Qualification。

冻结决策为：

```text
status:               proceed-limited
next_allowed:         test-only-message-port-worker-spike
full_adapter_allowed: false
Action changes:       0
Runtime changes:      0
Core PSS changes:     0
```

复核命令：

```bash
make audit-efficient-epaxos-feasibility
```

## 5. 代码体积与耦合审查

- 新增内容只有 test-only 决策模型、冻结 JSON 和说明文档；
- 没有引入 EPaxos Go package 到生产依赖图；
- 没有新增 Adapter、共享 adapterkit、Action、Item、Runtime 分支或 Core PSS 字段；
- 没有修改 etcd/raft、HashiCorp Raft、Gateway 或 v1 实验路径；
- 三节点 smoke 使用真实墙钟，不进入严格重放或测试质量评价。

## 6. 下一步与停止线

M5.8b 只允许实现 test-only message-port worker：在外围替换/包裹 peer writer，证明一个真实协议帧能够：

```text
produce -> freeze(stable id) -> keep pending -> release or drop -> receive/no-receive
```

它必须同时满足：不复制协议算法、不解析协议字段决定调度、不修改上游协议核心、不修改公共 Action、
Runtime 或 Core PSS。若无法获得无数据竞争的安装边界和稳定 frame identity，则把
`runtime-owned-message` 记为 Unsupported，停止该目标，不继续堆代码。

M5.8b 的新增生产 Go 必须为 0，test-only Go 净增上限为 250 行；达到上限仍无真实 frame witness
即停止，不用辅助框架掩盖接入失败。

M5.8b 作出该决定后立即进入 M5.9 v1 消费者迁移/删除门；在此之前不扩张 Agent 和 Coverage。
