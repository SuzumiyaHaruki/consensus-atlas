# etcd/raft Driver v1

## 目标系统

- module：`go.etcd.io/raft/v3`
- version：`v3.6.0`
- API：`RawNode`
- 原始源码修改：无
- `go.mod replace`：无
- `AsyncStorageWrites`：关闭

具体 Raft import 只存在于 `drivers/etcdraft`。

## 原生 API 映射

| 通用动作 | etcd/raft API |
|---|---|
| Boot | `NewRawNode` + 首次 `Bootstrap` |
| Start | `HasReady` + `Ready` |
| Campaign | `RawNode.Campaign` |
| Propose | `RawNode.Propose` |
| Query | `RawNode.ReadIndex` |
| Deliver | protobuf decode + `RawNode.Step` |
| Poll | `RawNode.HasReady/Ready` |
| Persist | `ApplySnapshot/SetHardState/Append` |
| Sync | visible storage 深拷贝为 durable image |
| Apply | committed entry、snapshot 和 ConfChange |
| Acknowledge | `RawNode.Advance` |
| Crash | 删除 RawNode、visible storage 和 outstanding Ready |
| Restart | durable storage + durable application image 重建 RawNode |

## Ready operation DAG

Driver 当前为每个 Ready 产生：

```text
persist
   |
sync
   +--> emit-000
   +--> emit-001
   +--> apply
           \
all emit ----> ack
```

`Ready.Messages` 在 Poll 时立即执行确定性 protobuf marshal 并深拷贝。这样后续 Ready 或 RawNode 状态变化不会修改已冻结消息。

当前 DAG 比官方允许的部分发送/写入并行关系更保守：所有消息等待当前 batch sync。这保证顺序合法，但会遗漏部分有效执行，因此 `exact-ready-send-barriers=false`。

M4.11 增加了仅由 Profile digest 绑定启用的 Ready sync 策略：

- 默认 Driver 仍然保守地 sync 当前 Ready 后再 release/apply；
- `runtime_profile=etcdraft-ready-must-sync-v1` 时，host sync 跟随 native `Ready.MustSync`；
- 同一 opt-in 模式才输出 `ready-must-sync` typed observation；
- 默认 Campaign monitor 集不包含 `ready-must-sync`，避免影响 M4.9 等冻结路径。

该模式只用于 Ready.MustSync 语义评测，不改变默认 etcd/raft 接入。

## Durable 模型

Driver 使用两个存储对象：

- visible `raft.MemoryStorage`：persist 后可被当前 RawNode 读取；
- `storageImage`：只有 sync 才更新，power-loss restart 的唯一 Raft 输入。

Application image 记录：

- applied index；
- applied ConfState；
- index 到 application value 的映射。

第一版将 apply 视为原子 durable。crash 在 commit 后、apply 前会使 restart 再次返回未应用 committed entries；crash 在 apply 后不会重复提交 application value。

## 消息校验

入站消息必须同时满足：

- Event source/target 与 WireMessage from/to 相同；
- protobuf `From/To` 映射到相同节点；
- protobuf 可以完整反序列化；
- 目标节点 running 且没有 outstanding Ready。

出站消息保存：type、term、index、commit、payload digest、sender epoch、link sequence 和 causation。

## PSS 状态证据

Driver 的只读 snapshot 额外公开当前角色/term/vote/leader、commit/applied、durable HardState、durable log 摘要、snapshot frontier 和 voter 集合。log value 只公开 digest，不把 application payload 复制到语义层。

`families/raft` 通过 JSON evidence 解码这些字段，不 import etcd/raft。它把绝对节点名、term、index 和 value digest 规范化为关系结构；`internal/protocolstate` 再计算单轨迹发现曲线。因此具体实现观察、协议家族语义和通用度量是三个独立层次。

## Timeout 决策

官方 v3.6 不公开每个节点随机 election timeout 的 RNG。第一版 Profile 不调用 `Tick`，只使用显式 campaign。`natural-election-timeout-replay` 被明确标为 Unsupported。

Runtime 已有通用、可选的虚拟 `TimerSource` queue，但该机制不会自行调用 `Tick`，也不会为未实现
`TimerSource` 的 Driver 生成 timeout。因而 etcd/raft v3.6 仍只能在公开 RNG/timeout 注入、完整随机
decision capture/replay，或独立 instrumented variant 之一成立后，才启用自然 timeout 覆盖。

后续只有在以下条件之一满足后才能启用自然 timeout 强覆盖：

1. 官方版本公开 RNG/timeout 注入；
2. 随机选择可以被完整捕获并强制重放；
3. instrumented variant 与官方 variant 分开报告。

## 场景

- `etcdraft-election-crash.json`：初始化、显式选举、提案、三节点提交、普通 crash/restart；
- `etcdraft-vote-drop-duplicate.json`：复制一条 MsgVote、丢弃另一条，随后由最小 quorum 选主；
- `etcdraft-crash-before-sync.json`：candidate Ready 已 visible persist，但 sync 前 crash，确认消息没有 release，恢复后重新选举。
- `etcdraft-partition-heal.json`：分区使选举消息保留在 mailbox，heal 后恢复投递、选主并提交。

## 已验证性质

- 同一场景两次 execution fingerprint 相同；
- candidate 可以通过真实 MsgVote/MsgVoteResp 成为 leader；
- proposal 经真实 MsgApp/MsgAppResp 在三节点 application image 提交；
- sync 前 crash 会记录并取消 batch 剩余 operation；
- emit 后消息不随源节点 crash 删除；
- restart 后 Driver conformance 通过；
- agreement 按相同 log index 比较值，不把顺序提交的不同值误报为冲突。
- 真实 etcd/raft trace 可以通过 Raft PSS 投影，并产生可审计的状态 witness。
- ReadIndex query 和 `Ready.ReadStates` observation 可用于 `linearizable-read` monitor。
- opt-in Ready.MustSync observation 可用于区分 message-only Ready 的历史语义重构。

## Unsupported

- 精确 Ready send/write overlap；
- natural election timeout replay；
- `AsyncStorageWrites`；
- `ReportSnapshot` 成功/失败反馈；
- 完整 snapshot 生成/压缩场景；
- 完整 joint-consensus 生成器；
- process crash 与 power loss 的区分；
- application write/sync 分离。
