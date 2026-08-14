# ConsensusAtlas

ConsensusAtlas 是面向 CFT 与受限 BFT 共识实现的 Agentic 测试系统。Agent 负责提出语义目标、选择当前可执行
Action 和根据机械反馈修正计划；可信 Go 代码负责动作资格、确定性执行、Replay、PSS/Risk 投影、Oracle 与结果聚合。

核心原则是：

> Agent 可以扩大探索能力，但不能自行制造可执行事实、覆盖结果或故障结论。

## 当前流程

```text
协议知识 + TestHypothesis + workload/预算 JSON + thin Adapter
                              |
                              v
                 Explorer / Scenario Agent
                              |
                    当前可信 Frontier
                              |
                              v
             Control Runtime 具体化并执行 Action
                              |
             +----------------+----------------+
             |                                 |
             v                                 v
      fresh Replay                   PSS / Risk / Oracle
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

- `plans/agent/*.json`：可编辑 protocol knowledge、hypothesis、workload 和实验预算；
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

## 运行 Agent Session

etcd/raft：

```bash
go run ./cmd/control-experiment \
  -strategy etcdraft-agent-session-v1 \
  -campaign-dir artifacts/agentic/etcdraft-session \
  -stateless-corpus benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e/root-corpus.json \
  -semantic-input plans/agent/etcdraft-leader-change-inflight-v1.json \
  -scenario-semantic-exposure full \
  -agent-key-file /path/to/openrouter-key.txt \
  -agent-model provider/model
```

OmniPaxos：

```bash
go run ./cmd/control-experiment \
  -strategy omnipaxos-agent-session-v1 \
  -campaign-dir artifacts/agentic/omnipaxos-session \
  -worker adapters/omnipaxosv2/worker/target/debug/consensus-atlas-omnipaxos-worker \
  -semantic-input plans/agent/omnipaxos-message-loss-before-decision-v1.json \
  -agent-key-file /path/to/openrouter-key.txt \
  -agent-model provider/model
```

## 当前研究边界

目前已经证明两个真实协议可以复用同一受限 Agent/Runtime/Replay/Oracle 流程，但尚未证明 Agent 优于
deterministic baseline，也没有证明 PSS 数量或义务覆盖能预测隐藏缺陷检出。

下一阶段 A8 将在同一模型与 Runtime 预算下比较 deterministic baseline、单 Agent 和双角色 Agent，并以非公开
candidate/control 的独立根因检出与正确 control 的 false positive 作为主要结果。

## 阅读顺序

1. [当前阶段](docs/CURRENT_STAGE.md)
2. [总体规划](docs/ConsensusAtlas-总体规划.md)
3. [架构](docs/architecture.md)
4. [Control Runtime](docs/control-runtime-v2.md)
5. [指标](docs/metrics.md)
6. [Defect Benchmark](docs/defect-benchmark.md)
