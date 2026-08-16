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
- OpenRouter：模型 ID 显式配置，调用有 durable journal、单次 POST、usage observed/unknown 记账和无 provider 恢复；
- deterministic baseline：canonical DFS 与 uniform stateless Campaign；
- semantic planning：受限 Semantic Explorer 与完整但有界的多步 Scenario Agent 意图；
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

如果允许 Risk Agent 按 Dossier 中已声明的 reference 读取少量源码，可在同一命令中显式增加只读映射：

```bash
  -knowledge-source-mount repo=/path/to/consensus-atlas \
  -knowledge-source-mount go.etcd.io/raft/v3@v3.6.0/=/path/to/etcd-raft-module
```

该参数可重复使用。未配置时 Risk Agent 仍直接返回 portfolio；配置后也只能读取 Dossier 已声明的
精确 reference，不会枚举目录或执行命令。本地路径不会进入 prompt 或 verdict。

连续 Investigation 在同一命令上增加轮数：

```bash
go run ./cmd/control-experiment \
  -strategy agentic-episode-v1 \
  -target etcdraft-v2 \
  -campaign-dir artifacts/agentic/etcdraft-investigation \
  -investigation-episodes 3 \
  -semantic-input plans/agent/etcdraft-agentic-calibration-v1.json \
  -agent-key-file /path/to/openrouter-key.txt \
  -agent-model provider/model
```

中断后使用相同参数并增加 `-campaign-resume`。已完成的 `episode-NNNN` 只恢复，不重新访问 provider；
若最后一轮只有部分 journal，则由现有精确请求恢复路径继续。

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

两个 A9e1 输入都不包含预置 Risk/TestHypothesis；当前模型预算允许 Risk Agent 最多三次提议/修复，并为
Scenario Agent 保留最多三次调用，其中后两次只用于机械拒绝后的修复。旧 Session、Semantic Explorer 和配对路径
仍用于历史校准，但不是当前论文活动入口。

## 当前研究边界

目前已经证明两个真实 CFT 协议库可以复用同一受限 Agent/Runtime/Replay/Oracle 流程，并能各自投影到同一组
Observation 语义，但尚未证明 Agent 优于 deterministic baseline，也没有发现新的共识实现问题。etcd/raft
使用官方 `RawNode` 与内存存储，OmniPaxos 使用单线程外部 worker 与内存存储；因此当前结果属于协议库级、
受控 host-order 测试，不覆盖真实线程竞争、网络栈、WAL/fsync、部分写入或进程级资源故障。

现阶段仍存在会直接影响论文结论的边界：panic 与 partial external side effect 尚未单独校准；当前 PSS/Risk
没有完备分母；现有 Agent 方法也尚未进入 private holdout evaluator。timeout/worker 退出已形成可计费的 terminal
outcome，但它不是共识 finding。

A9e3b 已把 Core PSS 统计拆为 protocol/control/joint 三列。A9d6 OmniPaxos 工件的 31 个 sample 对应 4 个
protocol state、28 个 control state、29 个 joint state；stateless novelty 只使用 protocol key。三列都只是探索
统计，不直接产生缺陷结论。

当前 A9 双 Agent 方法尚未进入 formal candidate/control evaluator，仓库也没有真实 private holdout；公开 pair
只能校准构建、monitor 与 evaluator plumbing，不能支持 Agent 方法效果结论。

当前 etcd/raft 与 OmniPaxos 的生产 Risk 已统一经 Observation matcher 计算，旧手写 Trace 扫描器已删除。
A9c 还会从 predicates 机械推导所需 Observation kind/field 和直接蕴含的 Action，并与 projector 声明及真实
Adapter Manifest 比较；当前两个 Risk 均通过，OmniPaxos restart 负例会在搜索前被明确拒绝。

A9d1 已加入 bounded Risk Agent 候选循环。A9e1 后 Agent 输出 property reference、机制步骤和新的
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
fresh Replay stable、31 个 PSS samples（4 protocol / 28 control / 29 joint）、Risk 未达、Oracle 0 finding。etcd/raft
校准暴露了 output/binding 约束和 provider 响应不稳定；可本地修正的约束已收紧，有界失败已完整
保留而不被记为协议结果。详见
[`benchmarks/experiments/agentic-episode-a9d6/`](benchmarks/experiments/agentic-episode-a9d6/README.md)。

A9e1 已完成 `agentic-episode-v1` authoring input 迁移：预置 Risk/TestHypothesis 已移除，输入改为 Primer、
Property 与 Historical Issue Pattern；Agent 需要自行提出 property reference、受历史模式启发并绑定到 milestone 的机制步骤和
可机械编译的 Observation predicates。迁移不改变 Runtime、Replay、Oracle 或 evaluator 的权力边界，也不把
Agent 的自由机制解释当作执行证据。

`property_ref` 当前只是规划上下文。candidate accepted 仅表示候选通过语法、可观察性与动作资格检查，不表示
predicates 已充分刻画该性质，也不表示 Oracle 已验证该性质。下一步是 defect capability / Oracle adequacy pilot。

A9e2a 已完成第一个可达性修正：client result 只结束当前自然推进 closure，并在仍有调用和执行预算时把可信
frontier 交回 Scenario Planner；它不再全局结束 episode。RiskReached 仍可作为搜索目标的停止条件，但只会在
当前可信 closure 完成后生效，不能跳过普通 effect/message/timer 执行。定向 etcd/raft 集成测试已验证下一轮
可以在客户端完成后选择 crash；这不是 durability finding，后续仍需独立 persistence Oracle adequacy pilot。

A9e2b 先用负校准证明 etcd/raft 与 OmniPaxos 的单一累计 frontier 摘要会漏掉共享位置冲突，随后把两个
Target 的证据改为逐位置累计承诺。通用 Agreement 不理解具体协议，仍只比较同一 position；现在能检出冲突的
不同前沿，同时放过一致的不同前沿。A9e2c 又让 etcd/raft target-local log-progress 跨 incarnation 比较
commit、applied 与既有 application prefix；正确 durable-image restore 通过，受控的重启丢失会在重启 step 检出。
这些结论限定在当前两个协议库 Adapter 的已校准语义。

A9e2d 已在真实 OmniPaxos worker 上完成 execution-outcome 负校准；A9e3a 随后完成最小修复。worker call 现在
尊重 Action context，deadline 会终止并回收 worker。失败动作不进入成功 Trace，而是以 terminal outcome sidecar
绑定成功前缀、enabled 集、尝试 Action、decision 与稳定 class/code；失败尝试会计费，并可由 Campaign 和 Agentic
Episode 落盘恢复。fresh Replay 仍只证明成功前缀，fresh execution 用于重复确认失败类别；进程异常本身不直接
等同于共识 finding。A9e3b 已进一步完成 PSS 的 protocol/control 双视图。A9e4a 已让同一个有界
`ScenarioPlan` 能在已有 Risk milestone 之间进行可信自然推进，且完整计划内不再使用固定 24-action 规划边界；
两个活动 Agentic 输入已迁移为最多 4 步的完整意图，后续 Scenario 调用只作为机械修复预算。
A9e4c1 已将 Risk Agent 的真实 provider 输出扩展为 2–3 项有序候选 portfolio；可信代码逐项审查并选择
首个机械合格项，不接受 Agent 自报分数或 verdict。A9e4c2 又从已恢复的 episode 工件重新派生紧凑
Exploration Memory，并可注入下一轮 Risk 请求；它不单独持久化，也不参与 verdict。A9e4c3 已完成内部多轮
Investigation coordinator：
每轮独立落盘、重新恢复后再生成下一轮 Memory；OmniPaxos fixture 证明第二轮接收了第一轮结果。A9e4c4
已通过 `-investigation-episodes N` 暴露多轮 CLI，并支持用 `-campaign-resume` 从连续 episode 目录继续；
两轮恢复到三轮的校准没有重复旧 provider 调用。真实 OpenRouter 两轮 OmniPaxos 校准随后完成 6 次模型调用、
57,727 tokens：第二轮读取首轮 Memory 后更换候选并三次修复 Scenario，但没有新增 protocol PSS state，
两轮 Risk 均未达到、Oracle 均为 0。第二轮还暴露了文字机制声称需要消息丢失、predicates 却没有对应 milestone
的一致性缺口。详见
[`benchmarks/experiments/agentic-investigation-a9e4c4-openrouter-omnipaxos-r5/`](benchmarks/experiments/agentic-investigation-a9e4c4-openrouter-omnipaxos-r5/README.md)。
A9e4c5 已闭合这个已复现的表示缺口：新 portfolio 必须为每个 predicate 提供同序、同 ID、同 Observation kind
的 `mechanism_step`，最终机制说明由可信代码生成；不一致候选会收到稳定反馈并由现有 Risk Agent 循环修复。
A9e4c6–c9 又增加了结构化 Target Dossier、动态 target surface、受限只读 Knowledge Discovery
和显式 source mounts。A9e4c10 已让 Risk Agent 在原调用预算内二选一：请求少量已声明源码，或提交
portfolio。读取片段不是执行证据，也不能产生 verdict；未配置 mount 时保持原 portfolio-only 路径。

## 阅读顺序

1. [当前阶段](docs/CURRENT_STAGE.md)
2. [总体规划](docs/ConsensusAtlas-总体规划.md)
3. [架构](docs/architecture.md)
4. [Control Runtime](docs/control-runtime-v2.md)
5. [指标](docs/metrics.md)
6. [Defect Benchmark](docs/defect-benchmark.md)
