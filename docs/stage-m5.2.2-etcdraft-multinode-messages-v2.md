# M5.2.2 阶段总结：配置驱动的 etcd/raft 多节点消息闭环

日期：2026-08-07

## 阶段结论

官方 `go.etcd.io/raft/v3 v3.6.0` 已通过薄 Adapter 接入 Control Runtime v2 的多节点消息控制面。
Adapter 不再按“三节点”写死状态，而是读取 `Config.Nodes` 构造任意静态 N 节点表。拓扑、Raft ID
映射和 tick 参数进入 `ConfigurationDigest`，因此改变节点或时间参数会改变 Manifest 身份，仅改变
输入顺序不会。当前完整行为切片验证了 N=1 与 N=3，另用非连续 Raft ID 的 N=4 配置验证了同一
构造器的 bootstrap/pulse 数量扩展。

三节点集成测试已经覆盖真实选举消息的冻结、持久化依赖、长期保留、复制、分区、恢复、投递和
丢弃，并在全新 Adapter/RawNode 上严格重放完整 decision log。这个结果证明第一条真实多节点控制
链路可运行，不证明任意节点规模、任意协议或完整 etcd/raft 接入已经合格。

## 输入、处理与输出

```text
Config{N nodes, RaftID mapping, tick parameters} + seed + enabled ActionID
                                  |
                                  v
                  one RawNode/storage/entropy domain per node
                                  |
                                  v
              Ready captured -> durable effect dependency
                                  |
                     effect completion releases Messages
                                  |
          Runtime mailbox: retain / duplicate / partition / heal / drop
                                  |
                                  v
                    DeliverMessage -> target RawNode.Step
                                  |
                                  v
       cluster evidence + entropy tape + v2 trace + strict replay verdict
```

## 已实现

- `Config` 统一描述静态 N 节点拓扑、逻辑 `NodeID` 到原生 Raft ID 的映射及 election/heartbeat tick；
- Adapter 内部按节点保存 RawNode、MemoryStorage、incarnation、Ready、pulse、applied 和 ConfState，
  公共 Runtime 没有增加 Raft 类型或分支；
- Reset 对所有节点执行官方 `NewRawNode + Bootstrap`，并先排空全部 bootstrap Ready，之后才暴露
  每节点 periodic pulse；
- 每个节点使用独立的 node/incarnation/domain entropy tape；
- `Ready.Messages` 在 yield 处 protobuf 深复制，payload digest、source/target、消息类型和只读元数据
  一并冻结；
- Message 依赖产生它的 Ready durable effect；effect 未完成时消息不可投递，完成后进入 Runtime
  mailbox；
- Runtime 独立提供 deliver/drop/duplicate/partition/heal，Adapter 的 deliver 映射只解码冻结消息并
  调用目标 `RawNode.Step()`；
- cluster evidence 按规范化节点顺序输出，包含每节点协议状态、storage 状态和 outstanding identity；
- Manifest 增加必填 `ConfigurationDigest`，避免同一 build 在不同拓扑或 tick 参数下身份混淆；
- 单节点 Manifest 仍不虚报 message 能力，多节点 Manifest 才声明对应 Message/Action surface。

## 三节点测试轨迹

测试不是注入伪造的 Raft 消息，而是让周期 pulse 驱动官方 RawNode 自然产生选举流量：

1. 完成三个节点的 bootstrap Ready；
2. 选择自然 pulse，直到真实 Ready message 经 durable dependency 变为可投递；
3. 复制该消息，确认副本保留 `CloneOf`；
4. 对 source/target 建立 partition，确认原件和副本都不可投递，但仍可显式 drop；
5. heal 后投递副本、丢弃原件；
6. 继续只从 enabled 集中选择 effect、deliver 或最早 pulse，直到三节点选出 leader；
7. 用相同 Config、seed 和冻结 ActionID 在全新实例严格 replay；
8. 检查 entropy tape 同时包含 n1、n2、n3 的受审计原生随机读取。

## 本阶段没有证明

- “配置驱动”不等于任意 N 已穷尽验证；N=4 只验证 bootstrap/pulse，完整消息选举只验证 N=3，
  动态成员变更也未实现；
- crash/restart、durable image 重建、旧 incarnation 消息和 crash cutpoint 尚未接入；
- 当前 `raft-ready` effect 把 persistence、committed apply、消息 release 和 `Advance` 收拢在一个宿主
  完成动作周围。这是保守的合法顺序，但没有表达 persist/send/apply/Advance 之间更细的 crash
  交错；加入 crash 前必须拆分并验证这些阶段；
- 尚无 proposal、ReadIndex、snapshot、membership change 或 snapshot status feedback 场景；
- 进程级 `crypto/rand.Reader` 仍只有 Adapter 内互斥，正式资格需要隔离进程和负向并发测试；
- etcd/raft Adapter 尚未通过其完整 capability conformance，不能称为 qualified；
- 尚未接入第二个控制表面不同的共识实现，不能宣称跨协议普适性；
- PSS、Coverage、Oracle、Agent 和 benchmark 尚未迁移到 v2，本阶段不产生方法效果结论。

## 下一阶段最小任务

M5.2.3 聚焦 crash/restart 的可信持久化边界，而不是增加更多节点数：

1. 把 Ready 宿主处理拆成可审计的 durable checkpoint 与 Advance/release 边界；
2. 定义 Adapter 可导出的版本化 durable image，不序列化 RawNode 私有内存；
3. `Crash` 丢弃节点 volatile RawNode/outstanding Ready/pulse，但保留 Runtime 已释放消息；
4. `Restart` 只从 durable image 建立下一 incarnation，并验证旧 temporal/effect 失效；
5. 三节点验证 crash-before-release、crash-after-release、目标停止期间消息保留和恢复后投递；
6. 为已声明的 etcd/raft 能力增加外部 conformance 子集和严格 replay。

如果拆分 Ready 阶段需要公共语义，只能使用现有通用 Effect/Dependency/Message 状态机，不能给
Runtime 增加 `Ready`、term 或 Raft 专用 action。

## 阅读顺序

1. `adapters/etcdraftv2/model.go`
2. `adapters/etcdraftv2/adapter.go`
3. `adapters/etcdraftv2/storage.go`
4. `adapters/etcdraftv2/entropy.go`
5. `adapters/etcdraftv2/adapter_test.go`
6. `internal/control/model.go`
7. `internal/controlruntime/actions.go` 与 `state.go`
8. [Control Runtime v2 设计](control-runtime-v2.md)
9. [M5.2.1 单节点阶段总结](stage-m5.2.1-etcdraft-single-node-v2.md)

## 验证结果

- `gofmt` 无未格式化 Go 文件；
- `go test ./...` 通过；
- `go vet ./...` 通过；
- `go test -race ./...` 通过；
- `python3 -m unittest discover -s agents -p 'test_*.py'`：7 项通过；
- `git diff --check` 通过；
- 桌面与仓库内两份总体规划字节一致；
- 本阶段未新增或修改 JSON 工件，因此没有新的 schema 实例需要校验。
