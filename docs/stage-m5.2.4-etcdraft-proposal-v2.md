# M5.2.4 阶段总结：opaque proposal 与 application durable state

日期：2026-08-07

## 阶段结论

Control Runtime v2 已完成第一条协议无关外部输入纵向切片。Runtime 只验证、冻结并记录版本化
`PayloadEnvelope`；只有 etcd/raft Adapter 解码 `propose`，调用未修改的官方
`RawNode.Propose`，再从 committed entries 更新应用状态。Runtime、Conformance 和通用控制模型
没有引入 Raft proposal、term、leader 或日志类型。

三节点确定性测试已贯通：自然选主、第一次提交、原主停止、剩余节点自然换主、第二次提交、旧主
从 durable image 重建并追赶。最终三个节点具有相同的两条命令和 application digest，两个客户端
结果均为 `committed`，完整决策轨迹严格重放通过。

## 输入、处理与输出

```text
版本化 opaque input + 当前稳定节点
                 |
                 v
 Runtime 即时资格检查 -> 冻结 ActionID -> Adapter 解码
                 |
                 v
       official RawNode.Propose
                 |
        Ready / Messages / Effects
                 |
                 v
 committed entry -> application image -> client result
                 |
                 v
 evidence + transitions + entropy tape + strict replay
```

输入的 operation、request ID 和 value 对 Runtime 保持不透明。当前 Adapter 只声明
`consensus-atlas/etcdraft-v2-input/v1` 的 `propose`，没有把 ReadIndex、成员变更或 snapshot 控制
伪装成已支持能力。

## application durable image

新增 `consensus-atlas/etcdraft-v2-application-image/v1`，保存：

- 最后 applied log index；
- 已提交命令的 index、term、request ID、origin incarnation 和 opaque value；
- canonical digest。

普通空日志条目只推进 applied index，不生成应用命令；非空普通条目必须能解码为本 Adapter 的
版本化 proposal。ConfChange 继续由官方 `ApplyConfChange` 处理，同时推进 application applied。
每次 Ready advance 后，应用 image 与 Raft durable image 一起重新封装；合成 Snapshot 的 data
携带相同 application image。恢复机械验证 schema、digest、applied index 和 Snapshot data 一致性，
篡改会被拒绝。

这仍是用于验证控制层的 append-only command ledger，不是任意业务状态机、事务去重表或外部数据库。

## Invoke 与 replay 边界修正

本阶段的协议无关 Conformance 暴露出一个 Runtime 问题：旧 `OfferInvoke` 可以把当前不合格的输入
留在隐藏队列，但“offer”不是 trace decision，输入若在若干决策后才 enabled，严格 replay 无法重建
早期 Runtime 状态。

v2alpha1 现改为即时语义：

1. `OfferInvoke(ctx, node, payload)` 先通过 Adapter 的纯 `Check`；
2. 不合格时返回 `ErrInvokeNotEligible`，不修改 Runtime、不生成 ActionID；
3. 合格时才冻结动作，调用方必须立即从当前 enabled 集选择；
4. 状态变化后必须重新 offer，禁止 trace 外排队。

Runtime 单元测试验证了拒绝前后 Snapshot digest 不变、无 offered item，并验证随后成功 offer 的轨迹
可重放。

`enabled` 仍不预测协议成功：在 leader 尚未形成时，稳定节点上的 proposal 可以执行；官方
`RawNode.Propose` 若拒绝，Adapter 产生状态为 `rejected` 的 client result 与
`proposal-rejected` observation，不把合法协议结果误报为 Runtime 执行错误。

## 外部 Conformance

新增 `EvaluateOpaqueInvoke`，仅使用 Manifest、opaque payload、公共 Action/Trace 和 replay：

- 检查目标节点和 `Invoke` capability 已声明；
- 在每个稳定 frontier 尝试即时 offer；
- 资格不满足时只选择通用 effect/message/temporal 进展动作；
- 选择成功后检查完整 command/yield/evidence，并从 fresh Adapter 严格 replay。

该用例验证的是 `opaque-invoke-boundary`，明确不解码协议 evidence，也不声称输入已经提交。提交与
应用恢复由 etcd/raft Adapter 集成测试验证。加上 M5.2.3 的两个自然生命周期用例，当前 etcd/raft
共有三个声明能力的协议无关外部见证；这仍不是完整 Adapter qualification。

## 已验证场景

- 单节点自然选主后提交一条 opaque value，产生 `committed` client result；
- 单节点停止并重建后 application command count/digest 不变；
- 三节点在选主前提交，得到可重放的 `rejected` result，application 不变化；
- 三节点全部应用第一条命令后停止原主；
- 两个存活节点自然选出不同主节点并提交第二条命令；
- 原主从 incarnation 2 恢复并追赶到相同 application digest；
- 上述完整轨迹使用 fresh 官方 Adapter 严格重放。

## 耦合审查

- `internal/control` 只包含 opaque payload、通用 client response 和 Action/Item；
- `internal/controlruntime` 只增加即时 offer 资格语义与通用错误，不 import 协议包；
- `internal/conformance` 不解码 etcd/raft evidence；
- proposal entry、application image、`RawNode.Propose` 和 committed-entry 解码全部位于
  `adapters/etcdraftv2`；
- v2 核心继续通过机械 import gate，未依赖 v1 Runtime、Raft Family 或具体协议。

## 本阶段没有证明

- application ledger 不是通用状态机，也没有 request 去重或线性一致客户端会话；
- 未实现 ReadIndex、snapshot installation/ReportSnapshot、动态成员变更和 proposal forwarding 的
  独立结果关联；
- client result 只由 proposal origin 节点在本地 apply 时产生；origin 在提交前永久停止时，当前没有
  独立客户端代理返回结果；
- Ready advance 仍组合 application apply、ApplyConfChange 和 RawNode.Advance；
- 进程级 `crypto/rand.Reader` 仍未正式隔离；
- PSS、Coverage、Oracle、Agent 和 benchmark 尚未迁移到 v2；
- 尚未完成 v1/v2 同场景结论对照；
- 第二个异构协议尚未接入，不能宣称跨协议普适性。

## 下一阶段最小任务

M5.2.5 不继续扩张 etcd/raft 专用输入，而是建立第一次迁移/删除门槛：

1. 冻结 v1/v2 都能表达的最小场景与比较字段；
2. 分别执行正常提交、消息延迟/丢弃、自然换主和 crash/restart；
3. 比较安全 Oracle 结论、外部可见提交结果与 replay，不要求内部 trace 或调度步数相同；
4. 形成 v1 代码/文档依赖清单，区分仍承载 PSS/Agent/benchmark 的保留路径与可删除执行路径；
5. 只有对照通过且耦合门保持，才进行第一轮 v1 删除；
6. 随后进入 M5.3 Adapter 模板/qualification，再接第二个控制表面不同的 CFT 实现。

## 阅读顺序

1. `adapters/etcdraftv2/application.go`
2. `adapters/etcdraftv2/storage.go`
3. `adapters/etcdraftv2/adapter.go`
4. `adapters/etcdraftv2/adapter_test.go`
5. `internal/controlruntime/actions.go`
6. `internal/conformance/opaque_invoke.go`
7. `internal/conformance/etcdraft_invoke_test.go`
8. [M5.2.3 生命周期阶段总结](stage-m5.2.3-etcdraft-lifecycle-v2.md)
9. [Control Runtime v2 设计](control-runtime-v2.md)

## 验证结果

- 新增/修改 Go 文件已执行 `gofmt`；
- `go test ./...` 通过；
- `go vet ./...` 通过；
- `go test -race ./...` 通过；
- `python3 -m unittest discover -s agents -p 'test_*.py'`：7 项通过；
- `git diff --check` 通过；
- 桌面与仓库内两份总体规划字节一致；
- 本阶段未新增或修改 JSON 工件，因此没有新的 schema 实例需要校验。
