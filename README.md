# ConsensusAtlas

ConsensusAtlas 是一个“利用 Agent 测试分布式共识实现”的研究原型。它将开放式理解和确定性证据分开：

- Risk Agent 阅读协议材料并提出值得调查的性质、机制和 Observation predicates；
- Scenario Agent 根据当前可信 frontier 生成或修正多步 Action 意图；
- Go Runtime 只执行当前真实可达的 Action，并保存完整 Trace；
- fresh Replay、PSS 投影和独立 Oracle 从真实执行结果生成证据；
- Agent 不能制造 Action、Observation、覆盖率或缺陷结论。

当前活动 Agent 方法入口是 `agentic-episode-v1`；`workload` 只保留为确定性执行/评测 fixture。
旧 A2 Semantic Explorer、A8 session/paired wrapper、独立 DFS、stateless Campaign 和通用 Campaign store
已删除；它们不再与当前方法并存。

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
              continue / revise / abandon
                         │
                         ▼
       Target composition → Control Runtime → Trace
                         │
             fresh Replay + target observations
                         │
              PSS / Risk progress / Oracle
                         │
                         ▼
     summary.json + bundle.json / branch-evidence.json
```

客户端 workload 返回只结束当前自然推进段，不等于调查结束。Risk 尚未达到且模型、决策预算仍存在时，系统会把
最新 frontier 和 `ProgressDelta` 返回 Scenario Agent。一个完整计划内的战略 Action、`after_milestone` 和自然推进
共享同一 live Runtime；形成候选后再做一次 fresh Replay。
`ProgressDelta` 区分 milestone 停滞/推进、客户端返回、自然闭包静止和重复调度，并分别统计 Timer callback 与
真实逻辑时钟推进；它还报告 fault 配额消耗和当前可用干预，不把这些机械事实解释成协议 verdict。
高级 branch/control/ablate 仍可用于受控实验，但不是默认调查主线。已有分支候选不会隐式覆盖主路径；
每个唯一且 fresh-Replay 稳定的候选仍会独立运行 Target Oracle。跨 Episode Memory 不包含 Oracle finding
或 Oracle 派生 outcome。

## 本地 SUT 源码

三个共识实现不再从语言包缓存直接参与构建，而是以固定提交的 Git
submodule 位于：

```text
suts/etcdraft/       go.etcd.io/raft/v3 v3.6.0
suts/hashicorpraft/  github.com/hashicorp/raft v1.7.3
suts/omnipaxos/      github.com/haraldng/omnipaxos e3e989b (v0.2.2 source)
```

`go.mod` 保留上游版本身份，同时用 `replace` 强制所有 `go test/go run/go build`
解析两个 Go 目录；OmniPaxos worker 的 `Cargo.toml` 则用 path dependency 强制解析
`suts/omnipaxos/omnipaxos` 与 `suts/omnipaxos/omnipaxos_storage`。首次克隆应使用：

```bash
git clone --recurse-submodules git@github.com:SuzumiyaHaruki/consensus-atlas.git
```

已有工作区执行：

```bash
git submodule update --init --recursive
go list -m -json go.etcd.io/raft/v3 github.com/hashicorp/raft
cargo fetch --locked --manifest-path adapters/omnipaxosv2/worker/Cargo.toml
```

`cargo fetch` 是显式的一次性依赖准备；正式 preparation 和 build audit 继续使用
`--locked --offline`，不会在实验中静默访问网络。若 semantic input 位于仓库外，使用
`-repository-root /path/to/consensus-atlas` 显式指定本地 SUT 工作区；机器本地路径不进入
MethodSpec，进入身份的仍是源码树内容摘要。

因此可以直接修改 `suts/` 内源码并立即运行 Adapter/Runtime 测试。普通本地构建的
Manifest 会使用 `local-source-unsealed:*`，不会把尚未封存的修改冒充成官方版本。
正式实验仍须由 SUT build audit 对实际源码树和二进制计算内容身份。若要让另一台
机器复现修改，应把子模块提交推送到可访问的 fork 并更新 submodule pointer；只在
本机生成一个不可获取的子模块提交是不完整交付。

etcd/raft 与 OmniPaxos 的 Agent 源码读取仍必须由 `-knowledge-source-mount` 显式授权；
未提供 mount 时 Agent 只能使用知识包，不能读取本地源码。提供官方 module/crate reference
prefix 后，CLI 会机械核对执行依赖、语言构建解析目录和 Agent mount 都指向同一 checkout。
OmniPaxos preparation 还会使用 `cargo build --locked --offline` 从该 checkout 重建规范 worker，
并拒绝传入另一条 worker 路径。完整源码树 digest 写入现有 MethodSpec；运行开始、每次源码
读取和工件封存前都会重新检查。因而同一 reference prefix 不能挂载另一份源码，也不能在
调用过程中静默改动本地 SUT。普通运行仍属于
`local-source-unsealed`；正式 finding 继续要求 build audit 将源码 digest 与实际二进制绑定。
当前 Cargo build audit 可用同一 evidence 模型封存协议源码树、worker 源码/锁文件、
离线 Cargo 命令和最终 worker 二进制，并令 Audit `SUTBuildIdentity` 与 Adapter 的
`sha256:<worker>` BuildID 一致。已封存二进制可以在不暴露 SUT 源码的情况下运行；若要同时向 Agent 暴露其 staged
源码，还需把对应 build-audit 输入接入 composition，当前会明确拒绝而不会假定本地
checkout 与该二进制相同。

## 当前 Target

- `etcdraft-v2`：固定官方提交的本地 `go.etcd.io/raft/v3` RawNode，支持可配置静态节点数、消息调度、自然 Tick、
  crash/restart、partition/heal、Ready persist/advance effect 和 target-local Oracle。它不是完整
  etcd server/WAL 部署。`adapter_config.node_count=N`（当前静态上限 64）会生成 `n1..nN`/Raft ID `1..N`；需要自定义
  节点身份时仍可改用显式 `nodes`，两种写法不能同时出现；活动 Agent 输入完全省略
  `adapter_config` 时解析为三节点默认配置。
- `omnipaxos-v2`：固定本地 OmniPaxos 源码构建的外部 Rust worker，支持可配置静态节点数、produced-message
  调度、自然 Tick、workload、target-local Observation 和 Oracle；当前没有持久化 crash/restart 能力。
  `experiment.adapter_config.node_count=N` 生成 `n1..nN`（当前支持 3–64）；字段缺省时使用三节点默认值。
- HashiCorp Raft 的资格/Adapter 代码保留为控制面能力边界样本，但尚未成为活动 Agentic Target。
  其底层 `NewWithConfig(Config{NodeCount: N})` 已采用相同的缺省三节点规则，但当前没有对应 Agent JSON 入口。

协议特有 Observation 和 monitor 位于 Target 边界；公共 Core 只理解 namespaced declaration、类型、匹配、Trace 和
Replay，不理解 term、ballot 或具体消息语义。

## 输入与输出

主要输入：

- `plans/agent/*.json`：协议知识、Target Dossier、性质、历史问题模式、workload、Runtime 与预算；
- `adapters/<target>/`：把目标实现映射到公共 Action/Item 生命周期；
- `qualifications/<target>/`：机械验证目标真实提供的能力；
- `-knowledge-source-mount`：可选的只读源码 reference 映射；
- `-agent-provider`、`-agent-model` 和显式 key 文件：仅经授权的真实模型调用使用；
  当前 provider 为 `deepseek` 或 `openrouter`，二者不自动 fallback。

主要输出：

- Risk/Scenario provider journal，支持精确恢复且不重复已完成调用；
- `summary.json`：停止原因、预算、Risk、PSS 和 Oracle 摘要；
- `bundle.json`：完整 Trace、决策投影、Replay 和执行证据；
- `branch-evidence.json`：未选择但已验证候选的 Bundle、Oracle 和独立执行成本；
- capability gap、fidelity notice、planning/execution failure 的分离状态。

PSS、Risk reached 和 candidate accepted 都不是缺陷 verdict。缺陷结论必须来自 replay-stable 的独立 Oracle，或由
后续 evaluator 对保存证据重新判断。
对 formal trial，evaluator 在给予 finding credit 前汇总主路径和全部分支的 decisions 与
qualified primary work，不允许每个分支单独重用完整预算。

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
suts/                          固定提交、可本地修改的共识实现源码
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
  -agent-key-file /path/to/deepseek-key.txt \
  -agent-provider deepseek \
  -agent-model deepseek-v4-flash \
  -capability-feedback structured-gaps
```

OmniPaxos 另需 worker：

```bash
go run ./cmd/control-experiment \
  -strategy agentic-episode-v1 \
  -target omnipaxos-v2 \
  -worker adapters/omnipaxosv2/worker/target/debug/consensus-atlas-omnipaxos-worker \
  -campaign-dir artifacts/agentic/omnipaxos-episode \
  -semantic-input plans/agent/omnipaxos-agentic-calibration-v1.json \
  -agent-key-file /path/to/deepseek-key.txt \
  -agent-provider deepseek \
  -agent-model deepseek-v4-flash
```

fresh 单 Episode 校准使用新的空 `-campaign-dir`。最低闭环条件不是 finding，而是实际执行非零 Action、
生成 `bundle.json` 并通过 fresh Replay。`planning-failed` 必须结合字段级 feedback 分析，不能单独视为
模型或协议结论。

连续 Investigation 增加 `-investigation-episodes N`；中断后使用相同参数和 `-campaign-resume`。源码查询需显式增加：

```bash
-knowledge-source-mount repo=/path/to/consensus-atlas
-knowledge-source-mount go.etcd.io/raft/v3@v3.6.0/=/home/nitro/Desktop/consensus-atlas/suts/etcdraft
-knowledge-source-mount crates.io/omnipaxos@0.2.2/=/home/nitro/Desktop/consensus-atlas/suts/omnipaxos
```

Risk Agent 只能搜索显式挂载的只读源码目录；搜索结果仅返回有界的路径、行号和单行预览。正常
grounding 是一次成功的中立 search，再读取一个该 search 实际返回的精确路径；Dossier 原有 reference
不会绕过 search 直接授权 read，本地绝对路径不会进入 prompt 或 verdict。每次模型调用只允许一个知识
请求，每个 bounded read 最多 80 行；源码搜索词最多 128 bytes、每次最多返回 20 项。一次 search 或 read
返回 `stopped` 时允许在同一 4-call Risk 预算内重试一次，并自然失去 portfolio repair 机会；成功 read 后
不再继续翻页。重复请求、未搜索得到的任意路径和越过挂载根目录的引用由可信代码拒绝。最多 4 次 Risk
模型调用不等于可以读取 4 个源码片段。
`-capability-feedback` 可选 `reason-codes` 或 `structured-gaps`，默认后者；该值同时控制实际 Memory 输入并进入
MethodSpec。前者用于公开配对消融，不会删除 durable artifact 中的可信 capability-gap 证据。
公开反馈校准可指定 `-capability-feedback-probe`；探针只由真实 TargetSurface 预检，不执行 Action，并进入
MethodSpec。
短 closure capability pilot 可显式使用 `-closure-mode public-fixed` 或
`-closure-mode target-local`。该值不是自由标签：CLI 先改变实际 Target composition，
再从 factory 是否存在机械派生到 MethodSpec；没有专属 factory 的 Target 会拒绝
`target-local`。
未提供 `-closure-mode` 时默认为 `public-fixed`；专属闭合后端不再默认代替 Agent
完成后续时序。

Risk 输入有两种正式模式。默认由 Risk Agent 生成；需要让不同方法使用同一个已有
Risk 时，可增加：

```bash
-risk-input plans/agent/etcdraft-alternate-quorum-risk-v1.json
```

`-risk-input` 既可读取独立 RiskCandidate/assessment，也可直接读取先前 Agentic
Episode 的 `summary.json`（使用其中的 `accepted_risk`）。历史 qualification 不被
信任：CLI 会按当前 Target 的知识、Observation 和 Action 能力重新审查，只接受
qualified 且无 capability gap 的 Risk。读取模式下 Risk provider 调用为 0；候选的
规范化摘要及 `existing-candidate` 模式进入 MethodSpec。该模式同样支持连续
Investigation，因此不同 closure、搜索方法或模型可以机械证明使用了同一个 Risk。

## 验证与研究边界

```bash
go test ./...
go vet ./...
git diff --check
```

普通测试不读取 key、不访问模型服务。完整 race 只在明确里程碑运行。

当前已经证明两个真实 CFT 库可以复用同一 Agent/Runtime/Replay/Oracle 流程，且 Agentic Episode
目录的主 Bundle 和分支 Bundle 都可直接进入 private pair/exposure/Oracle 评测边界。
正式 Agentic 评测现在同时核算 Scenario 搜索、全部 qualified evidence 和模型成本，
并要求由真实运行配置派生的 typed `AgenticMethodSpec` 将 Episode/Bundle 绑定到 formal contract。
多轮 trial 必须提交完整连续的 Investigation，所有 Episode 的成本一起核算；search child verification
按 Replay 计费。Scenario Agent 还可在收到可信 `ProgressDelta` 后用零 Action 的 `abandon` 将低收益假设
交还给下一 Episode；这只是搜索选择，不是 verdict。这仍未证明 Agent 优于其他搜索方法，
也尚未发现新的实现问题。旧 A8 paired evaluator/session 已删除，不参与新 Agentic holdout 路径。

配置 target-local closure 时，Scenario view 会声明 `post_intervention_closure=true`。
Target factory 只基于真实已执行前缀判断是否接管；一旦认可某个干预，执行器会忽略
Agent 对后续尚未 enabled Action 的预测并转入受限 closure。未配置 factory、未识别
干预及 public-fixed 路径不变。

继续阅读：[`docs/architecture.md`](docs/architecture.md)、[`docs/CURRENT_STAGE.md`](docs/CURRENT_STAGE.md) 和
[`docs/ConsensusAtlas-总体规划.md`](docs/ConsensusAtlas-总体规划.md)。
