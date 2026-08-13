# ConsensusAtlas

ConsensusAtlas 是一个面向分布式共识实现的 Agent 测试研究系统：Agent 决定“优先测试什么”，统一控制层
决定“当前实际能执行什么”，Replay、语义投影和 Oracle 决定“执行中真实发生了什么”。

当前分支：`feature/agentic-consensus-testing`。

## 当前系统流程

```text
SUT + Target Pack + Protocol Pack + Experiment Config
                         |
                         v
              Adapter qualification
                         |
                         v
       Protocol knowledge + TestHypothesis
                         |
                         v
        Explorer 查看可信语义候选并排序
                         |
                         v
     可信具体化 + Control Runtime 执行
                         |
              Trace / Evidence / PSS / Risk
                         |
            +------------+-------------+
            |                          |
      机械反馈给 Explorer          fresh Replay
                                       |
                                       v
                              Oracle / evaluator
```

输入包括：

- 被测共识实现及一个薄 Adapter；
- 协议知识、风险目标和语义映射；
- workload、允许的故障范围、搜索与模型预算；
- 可选的模型访问，仅由显式 Agent 命令使用。

处理包括：

- Runtime 统一管理消息、自然时间、节点生命周期、持久化副作用和外部输入；
- Explorer 只能排列可信内核给出的候选，不能创造 enabled Action 或修改 Oracle；
- 执行产生 Trace/Evidence，可信代码计算 PSS、Risk progress 和成本；
- fresh Replay 检查可复现性，Oracle/evaluator 独立给出正式结果。

输出分三类：

- finding：可重放的性质违反或候选检出结果；
- testing evidence：PSS/义务/Risk 的发现与覆盖统计；
- cost：决策数、执行工作量、模型调用与 token。

Coverage/PSS 不是“协议正确率”。Agent 也不能用自己的解释替代正式 verdict。

## 当前已实现到哪里

- etcd/raft 可走完整统一 Runtime、qualified execution、fresh Replay、PSS 和 Agreement Oracle；
- OmniPaxos 证明同一 Action/Runtime 不只适用于 Raft；
- HashiCorp Raft 保留部分能力资格，明确表示统一 Action 不等于统一控制强度；
- deterministic semantic best-first 和单 Explorer 已在真实 etcd/raft prefix 上连通；
- Explorer 首个可信选择现在会进入同一 qualified executor，产出包含 Bundle、Core PSS、RiskWitness、
  fresh Replay 和 TraceIntegrity/Agreement 结果的单 episode testing result；
- A4a 已加入最多 8 步的短 `ScenarioPlan`：每一步只能引用当前 ActionID 或有限公共语义 selector，可信层按
  当前 admissible frontier 唯一解析，并对零匹配、歧义和外部预算耗尽返回机械反馈；
- A4b 已把 ScenarioPlan 接到 durable model-call journal：最多两次提议，第二次只能依据公开机械反馈修正；
  成功多步计划会编译为 exact Policy，并进入 qualified Bundle/PSS/Replay/Oracle；
- A4c0 已把活动模型传输收敛为唯一 OpenRouter 入口；模型必须通过 `-agent-model`
  显式选择，不保留厂商专用客户端或默认模型；
- A4c 已增加可恢复的 Scenario Agent 真实运行入口和紧凑 summary；第二次独立 OpenRouter 运行成功获得两次
  模型响应，Agent 根据 `no-match` 修正计划并进入 qualified testing，恢复没有再次访问 provider；
- 一次公开 DeepSeek 校准中，Explorer 改变了首个扩展，但没有达到完整 RiskWitness，因此不构成 Agent
  优势证据；它属于旧的 direct-DeepSeek 历史实验；
- 真实 A4c 运行完成 Replay/PSS/Oracle，但 RiskWitness 仍为 `not-reached`，因此只证明反馈闭环可用，不能证明
  Agent 优于 baseline。
- A4r 已删除无活动消费者的 A1 episode 契约；唯一 `TestHypothesis` 由 A2 Explorer 和 A4 Scenario
  按各自实际 backend 显式验证，不再默认 DFS。
- A5a 已把 etcd/raft Agent 路径的 ProtocolKnowledge 和 TestHypothesis 外移到
  `plans/agent/etcdraft-leader-change-inflight-v1.json`；A2/A4 运行必须显式提供 `-semantic-input`。
- A5b 已在同一 JSON 外移 Adapter 拓扑/tick、Runtime、FaultEnvelope、搜索/episode 预算和
  OpenRouter 输出上限；修改配置后不能恢复旧运行。
- A5c 已在同一 JSON 外移 workload authoring 输入；加载器生成现有 `WorkloadPlan` 和 payload digest，
  source bundle、root corpus 校验及 A2/A4 执行消费同一份 workload。
- A6a 已用现有 Campaign coordinator 串联多个 Scenario episode；后一 episode 只收到前一 episode 的机械
  feedback，Campaign 统一限制 work/model/time 并提供停止后恢复。
- A6b 已从 committed episode artifact 派生 session 级 PSS 状态并集、Risk 最佳进展、Replay/Oracle 和成本；
  PSS/Oracle/testing 字段只用于终端汇总，不进入下一次 Agent 请求。
- A6c 已用真实 `deepseek/deepseek-v4-flash` 完成两 episode session：2 次逻辑调用、10,984 tokens、
  23 个 Core PSS 状态并集、Replay stable、0 Oracle violation；RiskWitness 仍为 `not-reached`。
- OpenRouter 对无响应传输错误和 HTTP 408/429/5xx 最多重试 2 次。真实第一轮在第 3 次传输成功；
  审计分开记录逻辑 model call 与实际 transport attempt。

本轮 A2R 收敛删除了已经被替代的 trace/corpus/PSS 批处理搜索、Agent-v1 frontier campaign、未资格化的
raft-rs 路径以及仓库中的重复大型轨迹。历史内容仍可从 Git 提交 `0106e2c` 恢复。

## 核心目录

```text
internal/control/              Action、Item、Adapter 基础类型
internal/controlruntime/       唯一确定性执行与 Replay substrate
internal/conformance/          Adapter 能力机械检查
internal/controlexperiment/    qualified execution、搜索、Agent 计划、Campaign
internal/psscore/              协议状态语义投影
internal/semantic/             RiskWitness 等协议族语义
internal/oracle/               独立运行结果检查
internal/defectbench/          candidate/control 评价
adapters/                      目标专用薄适配
cmd/control-experiment/        etcd/raft 组合与 Agent 实验入口
plans/agent/                   Agent 可编辑的协议知识、测试假设、workload 与运行配置
benchmarks/                    当前输入和紧凑结果；大型运行产物放 ignored artifacts/
docs/                          当前设计与阶段总结
```

## 常用检查

日常小修改先运行相关包：

```bash
go test ./internal/control ./internal/controlruntime
go test ./internal/controlexperiment -run 'Semantic|Stateless'
go test ./cmd/control-experiment -run 'A2b2|A2b3'
```

一个完整阶段结束时再运行：

```bash
make test
go vet ./...
git diff --check
```

完整 race 检查只在明确的发布/里程碑检查中运行，不作为每次小改动的前置步骤。普通测试不会读取 key，
也不会调用外部模型。

A6 session 需要同时选择测试输入和 OpenRouter 模型；episode/work/token/time 上限来自同一 JSON：

```bash
go run ./cmd/control-experiment \
  -strategy etcdraft-openrouter-session-a6a \
  -campaign-dir artifacts/agentic/example \
  -stateless-corpus benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e/root-corpus.json \
  -semantic-input plans/agent/etcdraft-leader-change-inflight-v1.json \
  -agent-key-file /path/to/openrouter-key.txt \
  -agent-model provider/model
```

## 阅读顺序

1. [当前阶段](docs/CURRENT_STAGE.md)
2. [总体规划](docs/ConsensusAtlas-总体规划.md)
3. [架构](docs/architecture.md)
4. [Control Runtime](docs/control-runtime-v2.md)
5. [A2b3b 真实模型公开校准](docs/stage-a2b3b-etcdraft-real-model-calibration.md)
6. [A3 单 episode 测试闭环](docs/stage-a3-single-episode-testing-result.md)
7. [A4a 短计划具体化](docs/stage-a4a-scenario-concretizer.md)
8. [A4b Agent 修正与 qualified 测试](docs/stage-a4b-agent-repair-qualified-testing.md)
9. [A2R 架构收敛与减负](docs/stage-a2r-architecture-convergence.md)

历史阶段文档描述当时的实现，不应被当作当前 API。当前事实以 `CURRENT_STAGE.md`、总体规划和可编译代码
为准。
