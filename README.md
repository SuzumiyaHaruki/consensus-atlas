# ConsensusAtlas

ConsensusAtlas 是一个“利用 Agent 测试分布式共识实现”的研究原型。它将开放式理解和确定性证据分开：

- Risk Agent 阅读协议材料并提出值得调查的性质、机制和 Observation predicates；
- Scenario Agent 根据当前可信 frontier 生成或修正多步 Action 意图；
- Go Runtime 只执行当前真实可达的 Action，并保存完整 Trace；
- fresh Replay、PSS 投影和独立 Oracle 从真实执行结果生成证据；
- Agent 不能制造 Action、Observation、覆盖率或缺陷结论。

当前活动入口是 `agentic-episode-v1`。旧 A2 Semantic Explorer、A8 session/paired wrapper、stateless Campaign
和通用 Campaign store 已删除；它们不再与当前方法并存。

## 活动流程

```text
ProtocolKnowledgePack + Target Dossier + workload/预算
                         │
                         ▼
                    Risk Agent
        候选 portfolio / 源码查询 / 机械资格反馈
                         │
                         ▼
                  accepted hypothesis
                         │
                         ▼
                  Scenario Agent
          continue / repair + 多步 Action 意图
                         │
                         ▼
       Target composition → Control Runtime → Trace
                         │
             fresh Replay + target observations
                         │
              PSS / Risk progress / Oracle
                         │
                         ▼
              summary.json + bundle.json
```

客户端 workload 返回只结束当前自然推进段，不等于调查结束。Risk 尚未达到且模型、决策预算仍存在时，系统会把
最新 frontier 和 `ProgressDelta` 返回 Scenario Agent。一个完整计划内的战略 Action、`after_milestone` 和自然推进
共享同一 live Runtime；形成候选后再做一次 fresh Replay。

## 当前 Target

- `etcdraft-v2`：官方 `go.etcd.io/raft/v3` RawNode，支持消息调度、自然 Tick、crash/restart、partition/heal、
  Ready persist/advance effect 和 target-local Oracle。它不是完整 etcd server/WAL 部署。
- `omnipaxos-v2`：官方 OmniPaxos Rust 库的外部 worker，支持 produced-message 调度、自然 Tick、workload、
  target-local Observation 和 Oracle；当前没有持久化 crash/restart 能力。
- HashiCorp Raft 的资格/Adapter 代码保留为控制面能力边界样本，但尚未成为活动 Agentic Target。

协议特有 Observation 和 monitor 位于 Target 边界；公共 Core 只理解 namespaced declaration、类型、匹配、Trace 和
Replay，不理解 term、ballot 或具体消息语义。

## 输入与输出

主要输入：

- `plans/agent/*.json`：协议知识、Target Dossier、性质、历史问题模式、workload、Runtime 与预算；
- `adapters/<target>/`：把目标实现映射到公共 Action/Item 生命周期；
- `qualifications/<target>/`：机械验证目标真实提供的能力；
- `-knowledge-source-mount`：可选的只读源码 reference 映射；
- `-agent-model` 和显式 key 文件：仅真实 OpenRouter 调用使用。

主要输出：

- Risk/Scenario provider journal，支持精确恢复且不重复已完成调用；
- `summary.json`：停止原因、预算、Risk、PSS 和 Oracle 摘要；
- `bundle.json`：完整 Trace、决策投影、Replay 和执行证据；
- capability gap、fidelity notice、planning/execution failure 的分离状态。

PSS、Risk reached 和 candidate accepted 都不是缺陷 verdict。缺陷结论必须来自 replay-stable 的独立 Oracle，或由
后续 evaluator 对保存证据重新判断。

## 代码结构

```text
internal/control/              公共 Action、Item、Adapter 类型
internal/controlruntime/       确定性执行、虚拟时间、消息存储与 Replay
internal/conformance/          Adapter 能力检查
internal/controlexperiment/    Agent 输入、Scenario、qualified execution 与证据结构
internal/semantic/             通用/target-local Observation 和 RiskWitness
internal/psscore/              protocol/control/joint PSS 投影
internal/oracle/               独立 monitor
internal/defectbench/          candidate/control Bundle 评测
adapters/                      Target 薄适配
qualifications/                Target 资格组合
cmd/control-experiment/        活动 CLI、Agent coordinator 与 Target composition
cmd/defect-eval/               Bundle/MethodSpec evaluator
plans/agent/                   人工可编辑的 Agent 输入
docs/                          当前设计、边界和路线
```

## 运行

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

OmniPaxos 另需 worker：

```bash
go run ./cmd/control-experiment \
  -strategy agentic-episode-v1 \
  -target omnipaxos-v2 \
  -worker adapters/omnipaxosv2/worker/target/debug/consensus-atlas-omnipaxos-worker \
  -campaign-dir artifacts/agentic/omnipaxos-episode \
  -semantic-input plans/agent/omnipaxos-agentic-calibration-v1.json \
  -agent-key-file /path/to/openrouter-key.txt \
  -agent-model provider/model
```

连续 Investigation 增加 `-investigation-episodes N`；中断后使用相同参数和 `-campaign-resume`。源码查询需显式增加：

```bash
-knowledge-source-mount repo=/path/to/consensus-atlas
-knowledge-source-mount go.etcd.io/raft/v3@v3.6.0/=/path/to/etcd-raft-module
```

Agent 只能读取 Dossier 已声明的精确 reference；本地路径不会进入 prompt 或 verdict。

## 验证与研究边界

```bash
go test ./...
go vet ./...
git diff --check
```

普通测试不读取 key、不访问模型服务。完整 race 只在明确里程碑运行。

当前已经证明两个真实 CFT 库可以复用同一 Agent/Runtime/Replay/Oracle 流程，但尚未证明 Agent 优于其他搜索方法，
也尚未发现新的实现问题。形式化 evaluator 当前仍评估保存的 Bundle/MethodSpec；旧 A8 paired evaluator 已删除，
新的 Agentic holdout 评测尚未实现。

继续阅读：[`docs/architecture.md`](docs/architecture.md)、[`docs/CURRENT_STAGE.md`](docs/CURRENT_STAGE.md) 和
[`docs/ConsensusAtlas-总体规划.md`](docs/ConsensusAtlas-总体规划.md)。
