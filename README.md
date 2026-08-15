# ConsensusAtlas

ConsensusAtlas 当前是一个面向 leader-based CFT 共识库、运行在受控协议/host-order 执行环境中的可信
Agentic 测试原型。Agent 负责提出语义目标、选择当前可执行 Action 和根据机械反馈修正计划；可信 Go 代码负责
动作资格、确定性执行、Replay、PSS/Risk 投影、Oracle 与结果聚合。受限 BFT 是后续扩展方向，不是当前实验能力声明。

项目希望让 Agent 利用共识知识主动发现值得测试的风险，并在真实实现上产生可重放、可由独立 Oracle 确认的
问题证据。当前结果已经证明这一受限闭环可以跨两个 CFT 实现复用，但尚未证明其具有与 Agora 相同的问题发现
召回率，也尚未发现新的共识实现问题。

核心原则是：

> Agent 可以扩大探索能力，但不能自行制造可执行事实、覆盖结果或故障结论。

第一篇论文的核心主张应是 proposal、evidence 与 verdict 的权力分离：Agent 生成开放式风险假设和语义干预，
确定性执行层产生可审计证据，Replay 与 evaluator-owned Oracle 独立决定哪些结果可以被接受。确定性执行本身、
PSS 数量或多 Agent 编排都不单独构成方法有效性结论。

## 当前流程

```text
协议知识 + workload/预算 JSON + thin Adapter/Qualification
                              |
                              v
                 Risk / Scenario Agent
                              |
                    当前可信 Frontier
                              |
                              v
             Control Runtime 具体化并执行 Action
                              |
             +----------------+----------------+
             |                                 |
             v                                 v
      fresh Replay          Observation / PSS / Risk / Oracle
             |                                 |
             +----------------+----------------+
                              v
                    Campaign artifact/summary
```

当前已支持：

- etcd/raft：完整 scheduler-owned 消息、自然时间、crash/restart、持久化 effect 与 workload 路径；
- OmniPaxos：通过外部 worker 复用同一 Action、Scenario、Campaign、Replay 与 Oracle 闭环；
- HashiCorp Raft：保留部分能力路径，用于展示黑盒接口的真实上限；
- OpenRouter：模型 ID 显式配置，调用有 durable journal、bounded retry、token/work 计费和无 provider 恢复；
- deterministic baseline：canonical DFS 与 uniform stateless Campaign；
- semantic planning：受限 Semantic Explorer 与单步战略 Scenario Agent；
- qualified result：完整 ExecutionBundle、fresh Replay、Core PSS、RiskWitness 和独立 Oracle。

旧单 episode Scenario CLI 已删除。单 episode 实验由同一 Session 的 `MaxAttempts=1` 表达。

## 输入与输出

主要输入：

- `plans/agent/*.json`：A9e Agentic Episode 使用 Primer、Property、Historical Issue Pattern、workload 和预算；
  legacy A2/A4 校准路径仍可使用固定 hypothesis；
- `benchmarks/experiments/.../root-corpus.json`：可信 root corpus；
- `adapters/<target>/`：目标薄适配；
- `qualifications/<target>/`：机械能力资格；
- `-agent-model` 与显式 opt-in key 文件：仅在真实 Agent 运行时使用。

主要输出：

- Campaign config/checkpoint 与每次 attempt artifact；
- 模型请求/响应审计和 token/transport 计数；
- 完整 Trace、ExecutionBundle 和 Replay 结果；
- Core PSS 状态并集、Risk 进展和 Oracle violations；
- 明确的 completed、stopped、failed 与 stop reason。

PSS 和 Risk 只解释测试探索，不直接产生缺陷结论。缺陷判定必须来自 replay-stable 的独立 Oracle/evaluator。

## 代码结构

```text
internal/control/              公共 Action、Item、Adapter 类型
internal/controlruntime/       唯一确定性执行与 Replay substrate
internal/conformance/          Adapter 能力机械检查
internal/controlexperiment/    qualified execution、搜索、Scenario、Campaign
internal/semantic/             RiskWitness 等协议族语义
internal/psscore/              Core PSS 投影
internal/oracle/               独立结果检查
internal/defectbench/          candidate/control 评测
adapters/                      目标专用薄适配
qualifications/                目标能力组合
cmd/control-experiment/        实验 CLI 与目标组合根
plans/agent/                   人工可编辑的 Agent 输入
benchmarks/                    紧凑校准、资格和评测证据
docs/                          当前设计与评测边界
```

通用 `internal/` 生产代码不导入具体共识实现。目标专用 Risk、semantic projector 和 monitor 位于 Adapter
或 CLI 组合边界。

## 常用验证

小改动优先运行受影响包：

```bash
go test ./internal/control ./internal/controlruntime
go test ./internal/controlexperiment
go test ./cmd/control-experiment -run 'Scenario|SemanticExplorer'
```

完整阶段检查：

```bash
make test
go vet ./...
git diff --check
```

完整 race 只用于明确里程碑：

```bash
make test-race-full
```

普通测试不会读取 key，也不会访问模型服务。

## 运行 Agentic Episode

etcd/raft：

```bash
go run ./cmd/control-experiment \
  -strategy agentic-episode-v1 \
  -target etcdraft-v2 \
  -campaign-dir artifacts/agentic/etcdraft-episode \
  -semantic-input plans/agent/etcdraft-agentic-calibration-v1.json \
  -agent-key-file /path/to/openrouter-key.txt \
  -agent-model provider/model
```

OmniPaxos：

```bash
go run ./cmd/control-experiment \
  -strategy agentic-episode-v1 \
  -target omnipaxos-v2 \
  -campaign-dir artifacts/agentic/omnipaxos-episode \
  -worker adapters/omnipaxosv2/worker/target/debug/consensus-atlas-omnipaxos-worker \
  -semantic-input plans/agent/omnipaxos-agentic-calibration-v1.json \
  -agent-key-file /path/to/openrouter-key.txt \
  -agent-model provider/model
```

两个 A9e1 输入都不包含预置 Risk/TestHypothesis；其模型预算允许 Risk Agent 最多三次提议/修复，并为
Scenario Agent 保留一次调用。旧 Session、Semantic Explorer 和配对路径仍用于历史校准，但不是当前论文活动入口。

## 当前研究边界

目前已经证明两个真实 CFT 协议库可以复用同一受限 Agent/Runtime/Replay/Oracle 流程，并能各自投影到同一组
Observation 语义，但尚未证明 Agent 优于 deterministic baseline，也没有发现新的共识实现问题。etcd/raft
使用官方 `RawNode` 与内存存储，OmniPaxos 使用单线程外部 worker 与内存存储；因此当前结果属于协议库级、
受控 host-order 测试，不覆盖真实线程竞争、网络栈、WAL/fsync、部分写入或进程级资源故障。

现阶段还存在四个会直接影响论文结论的已知证据缺口：

- 通用 Agreement 只比较相同 frontier position 的累积前缀摘要，可能漏掉不同 frontier 间的前缀冲突；
- etcd/raft log-progress 只在同一 `(node, incarnation)` 内检查单调性，尚无跨重启持久性 Oracle；
- SUT panic、worker 退出或超时尚未统一成为可重放、可计费的执行失败证据；
- Core PSS 当前包含 Runtime pending queue。A9d6 OmniPaxos 工件的 29 个唯一状态在诊断性去除
  `control.pending` 后只剩 5 个，说明原数值主要混合了协议状态与调度队列变化，不能直接解释为 29 个不同协议状态。

当前 A9 双 Agent 方法尚未进入 formal candidate/control evaluator，仓库也没有真实 private holdout；公开 pair
只能校准构建、monitor 与 evaluator plumbing，不能支持 Agent 方法效果结论。

当前 etcd/raft 与 OmniPaxos 的生产 Risk 已统一经 Observation matcher 计算，旧手写 Trace 扫描器已删除。
A9c 还会从 predicates 机械推导所需 Observation kind/field 和直接蕴含的 Action，并与 projector 声明及真实
Adapter Manifest 比较；当前两个 Risk 均通过，OmniPaxos restart 负例会在搜索前被明确拒绝。

A9d1 已加入 bounded Risk Agent 候选循环。A9e1 后 Agent 输出 property reference、suspected mechanism 和新的
有序 Observation predicates；系统机械生成 spec，
拒绝已有 Risk、无意义 binding、未声明字面值和能力不足，并把原因反馈给下一轮。fixture 已跑通 unsupported
restart → supported message-loss/decision 的修正，provider 调用复用原 durable journal。accepted Risk 尚未进入
Scenario Agent 的限制已在 A9d2 fixture 中解除：系统机械派生 hypothesis，并在真实 OmniPaxos worker 上完成
Scenario Runtime、fresh Replay、PSS 和 Oracle。该候选与现有 message-loss Risk 等价，因此只是双 Agent 衔接证据，
不是新 Risk 或问题发现。

A9d3 将两者组合成单个 bounded episode，分别限制 Risk/Scenario/总调用、token 停止阈值、plan steps 和 Runtime
decisions，并分开报告 candidate accepted、Risk reached、PSS 与 Oracle finding。两个 durable journal 可以恢复而不
重复模型调用；当前尚无该 episode 的 CLI/终端工件恢复。

A9d4 把双 Agent 阶段控制抽成协议无关 coordinator。etcd/raft 与 OmniPaxos 现在只通过各自的薄 binding
提供 Observation、Runtime composition 与 qualified testing；协调器本身不包含 term、leader、ballot 或协议消息类型。
两种目标均通过定向复用测试，统一 CLI 与终端 episode 工件留到下一小阶段。

A9d5 已增加统一入口 `-strategy agentic-episode-v1 -target etcdraft-v2|omnipaxos-v2`。每次运行保存
Risk/Scenario provider journal、紧凑 `summary.json` 和唯一 `bundle.json`。终态 resume 只需 strategy、target、
directory；系统从 Bundle 重算 Risk/PSS/Oracle，不重访 SUT、provider 或 key。

A9d6 用真实 OpenRouter 完成了 OmniPaxos 的通用双 Agent episode：Risk 候选被接受、Scenario 执行、
fresh Replay stable、31 个 PSS samples / 29 个唯一状态、Risk 未达、Oracle 0 finding。etcd/raft
校准暴露了 output/binding 约束和 provider 响应不稳定；可本地修正的约束已收紧，有界失败已完整
保留而不被记为协议结果。详见
[`benchmarks/experiments/agentic-episode-a9d6/`](benchmarks/experiments/agentic-episode-a9d6/README.md)。

A9e1 已完成 `agentic-episode-v1` authoring input 迁移：预置 Risk/TestHypothesis 已移除，输入改为 Primer、
Property 与 Historical Issue Pattern；Agent 需要自行提出 property reference、受历史模式启发的 suspected mechanism 和
可机械编译的 Observation predicates。迁移不改变 Runtime、Replay、Oracle 或 evaluator 的权力边界，也不把
Agent 的自由机制解释当作执行证据。

`property_ref` 当前只是规划上下文。candidate accepted 仅表示候选通过语法、可观察性与动作资格检查，不表示
predicates 已充分刻画该性质，也不表示 Oracle 已验证该性质。下一步是 defect capability / Oracle adequacy pilot。

## 阅读顺序

1. [当前阶段](docs/CURRENT_STAGE.md)
2. [总体规划](docs/ConsensusAtlas-总体规划.md)
3. [架构](docs/architecture.md)
4. [Control Runtime](docs/control-runtime-v2.md)
5. [指标](docs/metrics.md)
6. [Defect Benchmark](docs/defect-benchmark.md)
