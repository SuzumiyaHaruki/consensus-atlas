# ConsensusAtlas

> **分支状态：控制层重构前检查点。** 本分支保存截至 M4.17 的 Runtime、etcd/raft 接入、
> PSS/Coverage、Agent 与 benchmark 实验基础，但它**不是**面向任意共识实现的最终控制层。
> 当前 Host/Driver 边界仍以 etcd/raft `RawNode` 的输出与 `Ready` 宿主操作模型为第一个实例，
> PSS/Coverage 也主要绑定 Raft Family/Profile。下一主线是冻结协议无关的
> `Universal Control Runtime + Control Adapter Contract + Adapter Conformance Suite`：统一接管
> 消息、虚拟时间、生命周期、持久化完成、外部输入与宿主回调，再将 PSS、覆盖、Agent 搜索和
> benchmark 作为其上的模块。故本分支的公开 pilot 只能证明当前链路可运行，不能证明跨协议
> 普适性，也不能证明 Agent 方法优于基线。详见
> [当前阶段](docs/CURRENT_STAGE.md) 与 [总体规划](docs/ConsensusAtlas-总体规划.md)。

ConsensusAtlas 是一个面向 CFT/BFT 共识协议的确定性场景执行与语义覆盖研究框架。当前版本已经直接接入官方 `go.etcd.io/raft/v3 v3.6.0`，同时保持测试核心不依赖 Raft 类型。

项目采用以下主路径：

```text
Human/Family Protocol Knowledge Contract
        │ deterministic compile (fixed denominator)
        v
Onboarding Agent -> Binding + executable witnesses
        │ compile + conformance + replay + Oracle validation
        v
Validated Profile + Scenario
        |
Deterministic Engine
        │
Generic Host Runtime
        │
Thin Protocol Driver
        │
Official Consensus Implementation
```

通用 Runtime 管理调度、逻辑时间、消息邮箱、分区、复制、丢弃、host-operation 依赖、宕机和重启。薄 Driver 只负责调用原系统 API、冻结输出批次、执行目标系统的持久化/应用操作并提供观察。

## 当前实现

- 确定性事件队列和严格依赖；
- 消息完整信封、payload digest、因果 ID、clone lineage 和 per-link sequence；
- 消息 `Produced → Released → Delivered/Dropped` 分离；
- 消息 drop、duplicate、partition 和 heal；
- 通用 `OutputBatch` 与 host-operation DAG；
- DAG 环检测和 acknowledge join 校验；
- crash 取消未释放的 volatile batch，已释放消息继续由网络邮箱持有；
- visible storage 与 durable image 分离；
- restart 只从 durable Raft state 和持久 application image 恢复；
- 官方 etcd/raft `RawNode` Driver；
- `Ready → persist → sync → release/apply → Advance` 显式阶段；
- JSON TestSpec、严格重放指纹和保守结构规范化键；
- Raft Family PSS v1、相对 term/index 与节点/值重命名；
- 协议无关状态发现账本、发现曲线和首见状态 witness；
- 显式 measurement window 与跨运行 PSS 状态并集；
- 等 scheduler-decision 预算的 Random/DFS Explorer 基线；
- versioned Protocol Knowledge Contract 和确定性 Profile 编译器；
- Agent Binding 固定 schema，以及不可由 Agent 降级的见证验收；
- contract digest、runtime capability、operation kind、双次 replay、conformance、Oracle 和契约标签自动验证；
- 自动接入 Coordinator 接口，将机械 finding 返回下一次 Agent 尝试；
- DeepSeek V4 Flash model-backed Generator、隔离的 JSON 子进程边界和逐轮 token/digest 审计；
- trace-integrity 与按日志 index 比较的 agreement monitor；
- Profile v2 结构化 Coverage Obligation、固定 denominator digest 和严格事件/顺序证据；
- 有界 Raft Campaign Spec 与确定性编译器，将接入 Profile 和 55 项测试分母分离；
- 同批次/同目标/同冻结消息、区间排除、去重计数和 Raft Family 语义证据；
- 跨运行 Coverage Ledger、风险优先 Coverage Debt 和可下钻 Reach/Observe/Check/Replay/Conform 证据；
- 受限 Test Plan DSL、Profile digest 绑定、全局预算与确定性 concretizer；
- 多运行 Campaign 执行器，Random/DFS decision log 强制重放后自动累计 Ledger；
- 私有增量 Campaign Session、hash-chained Blackboard 和确定性 Agent Coordinator；
- DeepSeek Test Plan Planner、机械 finding 反馈、协议因果结构去重与总预算停止；
- Campaign v2 完整执行成本，重复 setup/prepare 与 measurement、replay 分离计费；
- 私有 Defect Benchmark Manifest、Agent-facing opaque trial、可信 Oracle 重算和 root-cause/false-positive 账本；
- typed Candidate Catalog、六类可信 CapabilitySnapshot 与纯集合资格报告；
- 官方模块只读临时替换构建、唯一文本转换、digest-bound SUT identity 和完整 build audit；
- etcd/raft calibration 与首个公开历史 ReadIndex candidate/control 的端到端 pilot；
- Profile-required Capability 支持率；

## 快速运行

```bash
make test
make auto-onboard-etcdraft
make auto-onboard-etcdraft-llm
make coverage-compile-etcdraft
make run-raft
make run-raft-onboarding
make campaign-etcdraft
make campaign-baselines-etcdraft
make agent-campaign-etcdraft
make run-raft-llm
make experiment-random
make experiment-dfs
```

`auto-onboard-etcdraft` 首先把 `contracts/etcdraft-v1.json` 与 Agent/开发者生成的 `onboarding/etcdraft-binding-v1.json` 做机械闭环验证，生成 12 项接入 Profile。`coverage-compile-etcdraft` 再把该验证身份、真实 Driver Manifest 与有界 Raft Family Spec 机械展开为 55 项测试 Campaign Profile。`run-raft` 使用后者；`run-raft-onboarding` 只用于复核接入基线。

`campaign-etcdraft` 使用受限、digest-bound 的专家 Test Plan Suite 调用 Random/DFS，并把每个严格重放的 run 自动加入同一个 Coverage Ledger。当前基线累计覆盖 39/55、得分 66.38，剩余 14 项可行动债务；33 个实际 run 和 1624 个调度决策均重放稳定且没有 Oracle 违规。计划命中失败只增加 attempted，不会直接获得覆盖分。

`agent-campaign-etcdraft` 是 Blind Planner v1 的真实 DeepSeek API 入口。Planner 只接收 opaque trial ID、Profile identity/node 投影、Driver 声明的受限输入形状、supported capability 名称、opaque Coverage Debt ref、预算和机械 finding；Driver/SUT identity、真实 obligation、trace、Oracle 与 Ledger 保持在可信 Go 路径。默认 Make 目标仅是公开开发范围，不能用于方法效果结论；正式实验必须替换为预冻结的私有 scope。

新生成的 Campaign v2 报告还记录 primary/replay `execution_cost`。fresh SUT、每次重复 bootstrap/prepare、批量 drain 的 Runtime event 和 measurement event 都进入逻辑 work；失败 setup 也收费。Agent 是否有效不再由它正在优化的 Coverage/PSS 自证，而由私有 Defect Benchmark 在统一 primary work 下统计隐藏独立根因检出和正确 control 误报。

M4.9 已为公开 `63903dd` ReadIndex 回归补齐 `ReadIndex` input、`read-state` observation 与独立 `linearizable-read` monitor，机械资格使四个官方候选中的 `63903dd` 成为唯一 `qualified` 样本。受控历史回归 pilot 中，未修改 v3.6.0 candidate 在 1 run/1 decision、218 primary/218 replay work 下被检出；只应用公开修复的 control 在同一冻结计划与相同 26/55（45.67）Coverage 下通过。可信 evaluator 重新验证 binary/audit、最小环境重跑并从 trace 重算 Oracle，得到 1/1 root cause killed、0 false positive、0 invalid。Coverage 相同而外部结果不同，正是它不能自证 Agent 的原因。该样本是回归复现，不是方法效果结论；见 [M4.9 阶段总结](docs/stage-m4.9-etcdraft-readindex.md)、[pilot 工件](benchmarks/pilots/etcdraft-readindex-v1/README.md) 和 [Defect Benchmark](docs/defect-benchmark.md)。

M4.10 为未来确定性 Driver 加入可选虚拟 `TimerSource`：Engine 独占 timer queue，`advance` 只让到期项进入 enabled 集而不自动执行协议。etcd/raft v3.6 的随机自然选举 timeout 仍保持 Unsupported；冻结的 M4.9 candidate trace 三个 fingerprint 均保持不变。见 [M4.10 阶段总结](docs/stage-m4.10-virtual-time.md)。

M4.11 围绕公开 `0675f3d` Ready.MustSync 历史语义完成第二条独立 monitor/evaluator 链。该样本不是完整历史 checkout 复现，而是在当前 v3.6 module 上进行一处精确反向转换的语义重构；opt-in Profile 绑定 `conditional-ready-sync` 与 `ready-must-sync-observation`，默认 Driver 路径不变。Evaluator 重跑得到 candidate killed、官方未修改 control pass、0 false positive、0 invalid；两侧 Coverage 均为 2/3（75.00），不参与 kill 判定。见 [M4.11 阶段总结](docs/stage-m4.11-etcdraft-ready-must-sync.md) 和 [pilot 工件](benchmarks/pilots/etcdraft-ready-must-sync-v1/README.md)。

`auto-onboard-etcdraft-llm` 使用 `DEEPSEEK_KEY_FILE` 指定的文件调用 `deepseek-v4-flash`，默认是仓库根目录下被 git 忽略的 `key.txt`。密钥文件必须是普通文件且权限不能开放给 group/others，只会发送到官方 DeepSeek endpoint；密钥不会进入 prompt、子进程环境之外的日志或 artifacts。该目标会产生真实 API 费用，不由 `make test` 自动执行。当前 live smoke 在第 2 轮通过机械验证，生成 Profile 与静态基线一致。

在新机器上运行模型实验前执行 `chmod 600 /path/to/key.txt`，再使用 `make agent-campaign-etcdraft DEEPSEEK_KEY_FILE=/path/to/key.txt`。正式范围还必须显式提供 `BLIND_BENCHMARK_ID`、`BLIND_BENCHMARK_DIGEST` 与 `BLIND_TRIAL_ID`；只分析已提交结果不需要 API key。

- replay fingerprint 是否稳定；
- canonical scenario key；
- Oracle 违规；
- 五类覆盖分数；
- Profile 所需能力的支持率；
- Driver 完整能力清单；
- Raft PSS 状态发现曲线和每个 canonical state 的首见证据；
- 自包含执行轨迹和剩余 pending 事件。

直接执行：

```bash
go run ./cmd/runner \
  -profile artifacts/profiles/etcdraft-campaign-v1.json \
  -scenario scenarios/etcdraft-election-crash.json \
  -out artifacts/etcdraft-campaign-run.json
```

其他受控场景：

```bash
# 投票消息复制一份、丢弃另一链路，随后仍由最小多数选主并提交
go run ./cmd/runner \
  -profile artifacts/onboarding/etcdraft-profile-v1.json \
  -scenario scenarios/etcdraft-vote-drop-duplicate.json

# campaign 的 visible write 完成但 sync 前宕机；未 release 消息被取消
go run ./cmd/runner \
  -profile artifacts/onboarding/etcdraft-profile-v1.json \
  -scenario scenarios/etcdraft-crash-before-sync.json

# 隔离候选节点使选举消息保持 pending，heal 后恢复投递并选主
go run ./cmd/runner \
  -profile artifacts/onboarding/etcdraft-profile-v1.json \
  -scenario scenarios/etcdraft-partition-heal.json
```

## 关键目录

```text
drivers/etcdraft/       官方 RawNode 薄 Driver 和分层存储
internal/driver/        跨协议 Driver/OutputBatch 契约
internal/host/          通用 host Runtime
internal/engine/        确定性事件、消息邮箱和网络控制
internal/explore/       等预算 Explorer、Random 和无状态克隆 DFS
internal/testplan/      受限 Test Plan DSL、预算校验和确定性 concretizer
internal/campaign/      多运行 replay/Oracle/Ledger 覆盖闭环
internal/agentcampaign/ 私有 Agent Session、Blackboard、预算和反馈 Coordinator
internal/defectbench/   隐藏缺陷盲测视图、预算验证和 root-cause kill 账本
internal/scenario/      JSON 场景解释器
internal/oracle/        确定性 Oracle
internal/coverage/      Profile v2、Evidence Matcher、Coverage Ledger、债务和评分
internal/protocolcontract/ 确定性协议知识与固定分母编译器
internal/autoonboard/   Binding、见证验证和自动反馈 Coordinator
internal/protocolstate/ 跨协议状态发现账本
internal/semantic/      执行与结构规范化指纹
families/raft/          Raft PSS、状态投影和规范化
bindings/               具体系统的 CLI composition root
contracts/              人工/Family Pack 提供的最小协议知识契约
onboarding/             Agent 生成的实现绑定和可执行见证
profiles/               固定覆盖分母和 Profile v2 schema
plans/                  digest-bound Test Plan Suite 和 authoring schema
benchmarks/             私有缺陷 Manifest 与盲测 submission schema
scenarios/              声明式测试场景
```

## 当前能力边界

第一版有意保留以下 `Unsupported`：

- 官方 v3.6 自然选举 timeout 的强确定性重放；
- etcd/raft 允许的所有 Ready write/send 并行次序；
- `AsyncStorageWrites`；
- snapshot delivery 的 `ReportSnapshot` 成功/失败反馈；
- PSS 编译器、跨事件语义因果图、DPOR 和状态约简；
- 部分同步活性检查；
- 完整成员变更状态空间。

目前采用显式 `campaign`，并保守地要求当前 Ready sync 后再 release 消息。这是合法但较强的顺序，会漏掉部分合法并发，因此 Profile 保留相应 unsupported atom，结果不会被错误标为“高覆盖”。

状态发现曲线用于解释搜索效率，没有固定分母；Profile 分数用于解释冻结测试义务的完成情况。两者都不代表协议正确概率或剩余缺陷概率，也不能单独证明 Agent 有效。正式方法效果由隐藏历史缺陷/语义 mutant 的独立根因检出率与正确 control 误报率评价。

正常路径不要求人工逐条批准 Agent 提出的源码事实或测试。人工提供最小 Protocol Charter 或选择 Family Pack；Agent 可以生成协议差异、Binding、Driver、义务草案和测试计划，但只有 schema、digest、fixture、执行、Evidence Matcher、Replay、Conformance 与 Oracle 全部通过的产物才能进入 validated 状态。Agent 不能修改当次冻结分母、Oracle 或 replay/conformance 条件。

旧 Integration Pack、Source Catalog、Scout benchmark 及其人工 Gate 代码已从主仓库删除，避免与自动接入主线并存。

更详细的实现边界见 [总体规划](docs/ConsensusAtlas-总体规划.md)、[Defect Benchmark](docs/defect-benchmark.md)、[Coverage Kernel v2](docs/coverage-kernel.md)、[有界 Raft Campaign](docs/raft-campaign.md)、[Test Plan 与多运行 Campaign](docs/test-plan-campaign.md)、[协议契约与自动接入](docs/protocol-contracts.md)、[实验说明](docs/experiments.md)、[指标说明](docs/metrics.md)、[架构说明](docs/architecture.md) 和 [etcd/raft Driver 说明](docs/etcdraft.md)。
