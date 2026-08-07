# M5.2.3 阶段总结：etcd/raft durable image 与生命周期闭环

日期：2026-08-07

## 阶段结论

Control Runtime v2 已能对未修改的官方 `go.etcd.io/raft/v3 v3.6.0` 执行真实 `power-loss` crash 和
restart。停止会丢弃 RawNode、live MemoryStorage、outstanding Ready 和 pulse；重启只读取经过 digest
验证的版本化 durable image，创建全新的 MemoryStorage 和 RawNode，并进入下一 incarnation。

Ready 宿主处理不再是单一的 `persisted-and-advanced` 动作，而是两个通用 Effect：

```text
Ready captured
    |
    +--> raft-ready-persist --complete--> durable image + released Messages
    |                                      |
    |                                      +--> Runtime-owned retain/drop/deliver
    |
    +--> raft-ready-advance --complete--> apply committed entries + ApplyConfChange + Advance
```

因此现在可以真实表达三种不同切点：persist 前停止、persist 后但 Advance 前停止、Advance 后停止。
三节点测试证明已释放消息跨源节点和目标节点的 crash/restart 保持，目标恢复后投递到新 incarnation；
未释放消息及其旧 Ready effect 则随旧 incarnation 取消。

## 输入、处理与输出

```text
Config + seed + enabled Crash/Restart/Effect/Message/Temporal ActionID
                              |
                              v
        Ready persist -> versioned durable image -> Runtime message ownership
                              |
           power-loss: discard RawNode/live storage/volatile items
                              |
                              v
      restore image -> fresh MemoryStorage + fresh RawNode + incarnation+1
                              |
                              v
 cluster evidence + item/node transitions + entropy tape + strict replay
```

## durable image 信任边界

`consensus-atlas/etcdraft-v2-durable-image/v1` 只保存宿主公开状态：

- HardState；
- 已应用 index 对应的 Snapshot metadata 和 ConfState；
- Snapshot 之后仍需保留的持久日志条目；
- application Applied index；
- canonical digest。

它不序列化 RawNode 私有字段、Go 指针或 live MemoryStorage。恢复前机械检查 schema、digest、Applied
与 Snapshot index、ConfState 一致性、日志连续性以及 HardState commit 范围；任何不一致都使恢复
失败。单元测试验证了公开存储状态重建和 image 篡改拒绝。

当前没有客户端 application data，因此合成 Snapshot 的 data 为空。接入 proposal 前必须增加独立、
版本化的 application durable image；不能把空 data 误称为通用状态机恢复。

## 生命周期语义

### persist 前停止

- outstanding Ready、persist effect、依赖它的未释放消息和旧 pulse 由 Runtime 取消；
- durable image 不包含该 Ready；
- 如果连 bootstrap Ready 都未持久化，restart 会对空 Storage 重新执行静态 Bootstrap；
- 新输出全部属于下一 incarnation。

### persist 后、Advance 前停止

- HardState/Entries/Snapshot 已进入 durable image；
- 依赖 persist effect 的消息已经归 Runtime，源节点停止不删除它们；
- 尚未完成的 advance effect 属于旧 incarnation，停止时取消；
- restart 从持久日志重新产生需要 apply 的 Ready，再执行 ApplyConfChange/Advance。

### Advance 后停止

- durable image 同时包含更新后的 Applied/ConfState；
- restart 以该 Applied 和 ConfState 创建 RawNode，不重复应用已完成条目；
- 旧 temporal/effect 仍保持 canceled，不会在新 incarnation 重新 enabled。

## 修复的消息所有权问题

多节点恢复测试发现，早期 Adapter 在投递时错误要求消息的 source incarnation 等于源节点当前
incarnation。这会使已经释放的旧消息在源节点重启后无法投递，违反 Runtime ownership。

现在 delivery 只验证冻结消息的逻辑 source、原生 Raft `From/To` 与配置映射；不再要求 source
仍处于原 incarnation。目标仍必须是当前 running incarnation，因为 `Step` 发生在目标当前 RawNode。

## 外部 Conformance

新增 `EvaluateNaturalLifecycle`。它只依赖公共 Action、Item、Node lifecycle 和 Trace，不导入或解码
任何协议字段。对三节点 etcd/raft 自动执行两项机械见证：

1. `natural-crash-cancels-captured`：找到自然产生的 blocked message，停止源节点，检查消息与依赖
   effect 均 canceled，并严格 replay；
2. `natural-released-message-recovery`：找到自然 released message，依次重启 source/target，检查
   Runtime 保留、停止目标不可投递但可 drop、恢复后投递到 incarnation 2，并严格 replay。

两项均通过。它们只验证声明的生命周期/消息所有权子集，不等同于完整 13 项 fixture conformance。

## 本阶段没有证明

- 尚无 `Invoke/Propose`、客户端结果或 application data 的持久恢复；
- 尚无 snapshot installation、`ReportSnapshot`、ReadIndex 或动态成员变更；
- `raft-ready-advance` 仍把 committed application、ApplyConfChange 和 RawNode.Advance 合为一个动作；
- 没有建模普通 application write/sync 的失败结果；当前只有 `power-loss`；
- 进程级 `crypto/rand.Reader` 仍未放入正式隔离进程；
- 自然生命周期 Conformance 只有两个声明能力用例，不能称为完整 qualified Adapter；
- PSS、Coverage、Oracle、Agent 和 benchmark 尚未迁移到 v2；
- 第二个异构协议 Adapter 尚未接入，不能宣称跨协议普适性。

## 下一阶段最小任务

M5.2.4 增加协议无关外部输入纵向切片：

1. Runtime `OfferInvoke` 继续只携带版本化 opaque payload；
2. etcd/raft Adapter 将其最小映射到官方 `RawNode.Propose`，不把 proposal 语义放进 Runtime；
3. 增加版本化 application durable image 和 apply 结果证据；
4. 验证 proposal、消息调度、leader 变化、commit、crash/restart 与 strict replay；
5. 扩展 declared-capability Conformance，但不声明尚未实现的 ReadIndex/snapshot 等输入；
6. 为随后 M5.2.5 的 v1/v2 冻结场景对照和第一次 v1 删除建立迁移门槛。

## 阅读顺序

1. `adapters/etcdraftv2/storage.go`
2. `adapters/etcdraftv2/adapter.go`
3. `adapters/etcdraftv2/model.go`
4. `adapters/etcdraftv2/adapter_test.go`
5. `internal/conformance/lifecycle.go`
6. `internal/controlruntime/runtime.go` 与 `state.go`
7. [M5.2.2 多节点消息阶段总结](stage-m5.2.2-etcdraft-multinode-messages-v2.md)
8. [Control Runtime v2 设计](control-runtime-v2.md)

## 验证结果

- 新增/修改 Go 文件已执行 `gofmt`；
- `go test ./...` 通过；
- `go vet ./...` 通过；
- `go test -race ./...` 通过，包括全局 entropy reader 和多次 crash/restart/replay；
- `python3 -m unittest discover -s agents -p 'test_*.py'`：7 项通过；
- `git diff --check` 通过；
- 桌面与仓库内两份总体规划字节一致；
- 本阶段未新增或修改 JSON 工件，因此没有新的 schema 实例需要校验。
