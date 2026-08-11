# M5.21v：OmniPaxos `Temporal + Message` 最小 Binding

日期：2026-08-11

## 结论

M5.21v 通过。未修改的 `omnipaxos 0.2.2` 已经通过 target-owned Rust worker 和 Go Adapter 接入唯一
Control Runtime。三节点运行只使用自然 logical tick 和完整 opaque peer message；Runtime 实际执行
消息复制、丢弃、克隆消息投递和后续调度，最终所有节点观察到 n1 leader。关闭第一个 worker 后，
第二个 fresh worker process 严格重放相同 51 个 decision，trace digest 保持
`9f50a2b0de3538978ca75faee00abb6fd93384ecf5149f75a06d8c1d11a373d3`。

公共 `internal/control`、`internal/controlruntime`、PSS 和 experiment 生产代码变化为 0；OmniPaxos
算法源码变化为 0。target-owned 生产 Binding 共 770 行，低于 850 行软目标和 1,000 行停止线。

这证明当前统一 Action/Runtime 能跨越 Raft 与 leader-based Sequence Paxos 的最小控制数据面；它仍然
不是 Adapter Qualification，也没有证明存储暂停、重启、语义映射或 leaderless 协议可移植性。

## 实际流程

```text
Control Runtime
  fire periodic temporal item
          |
          v
Go Adapter -- one JSONL command --> fresh Rust worker
          |                           OmniPaxos::tick/handle_incoming
          |                                      |
          +<-- node view + complete messages ----+
          |
          v
freeze ProducedItem(message) in Runtime
  -> duplicate first message
  -> drop original
  -> deliver clone
  -> continue explicit deliver/natural tick
  -> all node views agree leader=n1
  -> close worker and strict Replay in a new process
```

worker 只接受三个操作：`reset`、`tick`、`step`。它不拥有搜索策略、enabled set、消息队列、虚拟时间或
重放逻辑。OmniPaxos `Message<WorkerEntry>` 经 serde 编码为完整 bytes；Runtime 不解析 BLE/Sequence
Paxos payload，只保存稳定 source/target/type hint 和 payload digest。

## 实际证据

- 节点数：3；自然 leader：n1；
- decisions：51；fresh worker processes：2；
- actions：`fire-temporal-event`、`duplicate-message`、`drop-message`、`deliver-message`；
- first message 被复制，original 被 drop，clone 被实际送入目标节点；
- trace digest：`9f50a2b0de3538978ca75faee00abb6fd93384ecf5149f75a06d8c1d11a373d3`；
- worker binary SHA-256：`02afe07f93a99e460b412bae00f6a7912fe26f7d544a94b54e4550d5914dc1b0`；
- unsupported worker operation 被拒绝；异常 worker exit 被 Go 边界报告；
- `CapabilityManifest` 机械拒绝把 Invoke、crash/restart 或 effect 列入本阶段能力。

固定结果位于 `benchmarks/feasibility/omnipaxos-binding-m5.21v/summary.json`。BuildID/trace 绑定当前
工具链；其他工具链重建可能产生不同 binary/manifest/trace digest，但同一声明 BuildID 内必须精确重放。

## Binding 行数账本

| 类别 | 行数 | 说明 |
|---|---:|---|
| target-native Rust worker | 211 | OmniPaxos 构造、tick/step、message serde、node view |
| Go/Rust wire protocol + process client | 142 | typed JSONL model 与 worker 生命周期 |
| target normalization/bookkeeping | 417 | Adapter、稳定 ID、message/pulse、Evidence/entropy |
| **生产实现总计** | **770** | 不含 Cargo 文件和测试 |
| conformance integration test | 323 | 控制动作、fresh Replay、capability/error boundary |
| Cargo manifest / lock | 12 / 429 | 生成依赖锁，不计入生产 Binding |

生产总量低于 850 行软目标，因而不触发 contraction 阶段。它比 raft-rs pre-workload Binding 小，主要
因为 OmniPaxos 直接返回 outgoing messages，没有 RawNode Ready/persist/apply/advance 生命周期；这同时
意味着本阶段并没有获得可调度持久化能力，不能把“代码更少”解释成控制能力更强。

raft-rs 与 OmniPaxos 现在确实都具有相似的 83 行 Go JSONL process client。M5.21v 没有在功能阶段改动
共享 Core；只有后续再次出现真实边际成本时，才评估类型安全的 process helper 是否能在不弱化 error/
identity 边界下减少两个消费者的总 LOC。

## 没有证明

- 没有 Invoke、opaque workload、decided entry 到 ClientResult 的闭环；
- `MemoryStorage` 在 worker 内同步完成，没有 suspendable HostEffect、durable image 或 power-loss 语义；
- 没有 crash/restart、partition、Qualification 或 Experiment admission；
- 没有 Core/Extended PSS Mapping、RiskWitness、Oracle、Coverage、Agent 或 Campaign；
- empty harness entropy tape 加静态 no-RNG API 检查不等于全面 audited SUT entropy replay；
- OmniPaxos 仍有 Ballot Leader Election，不能替代 leaderless/dependency-graph 目标；
- strict Replay 通过不证明 OmniPaxos、Adapter 或 ConsensusAtlas 正确、完备或无缺陷。

## 验证

- `make test-omnipaxos-binding`：通过；
- worker `cargo fmt --check`、locked `cargo clippy -D warnings`：通过；
- `go test ./adapters/omnipaxosv2 -count=1`：4 项通过；
- M5.21u frozen probe 继续独立存在，不被 Binding 结果覆盖。

## 下一阶段：M5.21w opaque workload 纵向切片

下一阶段只补一个 `non-leader append → decided suffix → ClientResult`：

1. 从 n2 送入唯一 opaque request，保持 Runtime 不理解 Sequence Paxos payload；
2. 只在 request 的声明 origin 观察到 exact decided entry 后产生一次 ClientResult；
3. fresh worker-process strict Replay 必须继续通过；
4. 公共 Action/Runtime/Core PSS 生产 churn 保持 0，不加入 restart、durable effect、PSS、Agent 或
   Qualification；
5. workload 边际生产代码软目标 180 行，Binding 总量硬停止线 1,000 行；若超过则停止并做成本审计，
   不通过搬文件或提前抽共享 kit 改口径。
