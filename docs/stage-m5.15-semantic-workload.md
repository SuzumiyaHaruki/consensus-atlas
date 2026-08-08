# M5.15：确定性 Semantic Workload 与 Fault Envelope

日期：2026-08-08

## 结论

M5.15 在唯一 v2 Experiment/Runtime 路径上补齐了第一条真实客户端写入闭环，没有新增 Campaign、
调度器、协议 Action 或模型调用。公开 etcd/raft calibration 在 96 decisions 内等待自然选主和 Ready
稳定，通过 `OfferInvoke` 提交一个 opaque write，得到 `committed` client result、applied Core PSS
witness 和 96/96 strict replay。

## 为什么不是按 decision 注入

M5.13 已证明静态策略无法预测未来 exact enabled frontier。Workload 也不应把“decision 26 必须写入”
作为前提。本阶段改用现有 Core Semantic Mapping 的协议无关角色：

```text
EvidenceEnvelope
      |
SemanticMapper.Map
      |
exactly one participant(mode=coordinating)?
      | yes
Runtime.OfferInvoke(target, opaque payload)
      |
Runtime.EnabledActions -> Policy -> Runtime.Select
```

在本次真实轨迹中，Provider 首次看见 coordinator 时目标节点仍有 outstanding Ready effect；
`OfferInvoke` 返回 not eligible，Provider 将其视为 guard 尚未稳定而继续推进已有 effect/message/timer。
它没有强制超时、绕过 Check 或直接调用 `RawNode.Propose`。稍后节点稳定后同一 guard 成功 Offer。

## Workload 边界

`WorkloadPlan` 包含：

- 版本与 plan ID；
- `single-coordinating-participant` target selector；
- 最多 8 个串行 `WorkloadInvocation`；
- 每项 opaque `PayloadEnvelope`、通用 request ID 和 expected completion status。

Provider 不读取 enabled set。Offer 只把一个冻结 Action 放入 Runtime；随后 Policy 必须从 Runtime 返回的
候选中选择同一 ActionID，否则实验显式失败。多项 workload 时，前一 request 必须先产生唯一、terminal
且状态匹配的 `ClientResponse`，才能 Offer 下一项。

`ExpectedStatus` 仅表示“这条 workload 是否完成”。它不能将协议判为正确，也不能产生 defect kill。
Client result 的 owner/status/payload digest 被写入 RunReport，完整 payload 不被通用层解释。

`single-coordinating-participant` 只是第一个版本化 selector，不是对所有共识的预设。如果协议同时
存在多个合法 coordinator，或客户端可以向任意 replica 提交，应当增加新的通用 selector
语义并独立资格审查；不应在 Adapter 里伪造唯一 leader 来迁就当前 selector。

## Fault Envelope 边界

`FaultEnvelope` 为每个 run 冻结：

- crash 总量和同时 stopped 节点上限；
- message drop 总量；
- message duplicate 总量；
- partition 总量和 active partition 上限。

它在 Policy 已经从 Runtime enabled set 选择 Action 后、`Runtime.Select` 前检查。Envelope 不删除候选、
不产生 Action、也不调用 `OfferPartition`。因此 Runtime `EnabledActions` 仍是唯一事实来源；超限选择得到
稳定 `EXPERIMENT_FAULT_ENVELOPE_EXCEEDED`，不计一次成功 scheduler decision。

公开 calibration 的 fault usage 为 0，不能据此声称已覆盖故障轨迹。独立集成测试使用 max=0 envelope
选择 crash，机械验证在 decision 1 前被拒绝且 charged decisions 保持 0。

## Replay 与成本

Primary 每次成功 `OfferInvoke` 增加一个 `PrepareActions`。Replay 不重新运行 Provider，而是从 digest-bound
Trace 中恢复已选择的 Invoke/Partition Action；`ReplayProgress` 对这种恢复同样计 prepare，并在最终状态
重新抽取 workload results 与 primary 比较。

本次预算与实际成本完全一致：

| phase | setup | runtime init | prepare | decisions | work units |
|---|---:|---:|---:|---:|---:|
| primary | 1 | 1 | 1 | 96 | 98 |
| replay | 1 | 1 | 1 | 96 | 98 |

Runtime initialization 仍是 setup 的明细，不重复加入 work-unit 公式；公式保持
`setup + prepare + scheduler decisions`。

## 公开 calibration 结果

| 项目 | 结果 |
|---|---|
| experiment | `public-etcdraft-v2-semantic-workload-m5.15` |
| requests | 1 planned / offered / completed |
| result | `m5.15-write-1`，owner `n1`，status `committed` |
| decisions | 96 primary + 96 replay |
| strict replay | stable |
| trace digest | `34d0d3895010aebf787c539937b771bd1f68bc1174a4c6e1740a5252290b225c` |
| Core PSS | 56 unique states，prefix area 3324，存在 applied witness |
| report digest | `e680aabd1c98181e2c87fa470610ed300689ed30deff8b2d543c9211a18948d7` |
| file SHA-256 | `e7a00826bf4881288696ad5d4c6685d7b82bcf684d7276af1bbf5cc7cad247b1` |
| model work | 0 calls / 0 tokens |

etcd/raft Adapter 只在应用 committed entry 时产生 `committed` client result，因此这条具体轨迹确实经过
proposal、Raft replication/commit 和 application effect。这个事实仍只是公开 calibration 的链路见证，
不是跨协议 Oracle 结论。

## 历史兼容

Workload、FaultEnvelope、Budget prepare targets 和 RunReport workload/fault usage 都是 additive
`omitempty` 字段。无 workload/admission 的 M5.10–M5.13 Config 和报告 canonical form 不变；M5.3 legacy
Qualification 入口也未修改。完整逐字节复验和仓库测试结果在交付前补入。

## 本阶段证明了什么

- workload 可以通过语义 guard 而非绝对步号进入统一 Runtime；
- Adapter-specific input 可保持 opaque，通用层不需要 Raft 类型；
- Offer、Policy selection、Runtime execution 和 Replay 可以共用一条可信路径并完整计费；
- first real write 到达 committed client result 和 applied Core PSS state；
- fault 总量/并发限制可以在不改 Runtime enabled set 的情况下机械执行。

## 本阶段没有证明什么

- 没有运行实际故障 workload；
- 没有保存完整 ExecutionBundle；
- 没有运行 Agreement/TraceIntegrity Oracle 或 DefectBench；
- 没有得到 Coverage 百分比或方法比较；
- 没有证明 etcd/raft、Adapter 或 ConsensusAtlas 正确；
- 没有证明 HashiCorp/EPaxos 可以执行同一 strict workload；
- 没有证明单 coordinator selector 适用于多主、无主或任意 replica 接入的协议；
- 没有调用 LLM，也没有 Agent 优势结果。

## 下一阶段

M5.16 只为本阶段已经存在的真实消费者定义最小 `ExecutionBundle`：完整 Trace、Evidence/client history、
Core PSS samples、Qualification、Replay/conformance 和成本。先迁 TraceIntegrity 与 Agreement，在正确
control 和公开 calibration candidate 上形成首个 v2 DefectBench 结果，再考虑 Coverage 和搜索方法。

## 验证

交付前对最终工作树执行：

- `make test`：通过，包括全部 Go 测试、10 项 Python 单测和 legacy production edge 冻结检查；
- `go vet ./...` 与 `go test -race ./...`：通过；
- fresh M5.3 legacy、M5.10 fixed、M5.11 random、M5.12 stub 与 M5.15 workload 工件均与
  checked-in 文件逐字节一致；
- 117 个 JSON 文件语法、24 个 JSON schema 声明和 88 个 Markdown 文档的本地链接：通过；
  旧版 `jsonschema` 对本地缺失的 metaschema 使用 latest-draft 兼容检查并警告；
- 两份总体规划逐字节一致，SHA-256 均为
  `b0e3ee17c87589c3fdc63b5956da84613e47f8ad7404ad7bf31211940ca1423d`；
- `git diff --check`：通过。

验证过程没有调用 LLM，也没有读取 key 文件。
