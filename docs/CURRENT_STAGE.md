# 当前阶段

更新时间：2026-08-21
分支：`feature/agentic-consensus-testing`
阶段：M4n10R 长盲测前审计收口

## 一句话状态

活动主线是：

```text
知识包/源码目录/Target 能力
        ↓
Risk Agent 生成并修订候选 portfolio
        ↓
机械可执行（wire value: `executable`）审查 → Scenario Agent 单路径选择一个战略 Action
        ↓
可信绑定 → Runtime → Trace → fresh Replay
        ↓
Target-local Observation/PSS/Oracle → witness-instantiated / finding 与成本
```

M4n9 在 M4n8 三层术语上修复了 Witness 的端点歧义。Risk 候选通过类型、Observation 与
Action 能力审查后，报告称为“机械可执行”（为保持工件兼容，wire value 仍为 `executable`）；
执行轨迹满足全部可信 milestone 时称为 `witness-instantiated`；只有独立
Oracle violation 才称为 `finding`。底层历史 Bundle 的 `qualified/reached` 枚举仍为只读
兼容字段，不能在新报告中解释为候选正确或发现问题。

公共 Observation 现在分别暴露 `message-source-node/message-target-node` 和
`new-coordinator-node/previous-coordinator-node`。消息 Risk 不再能用含义随 Drop/Deliver
变化的 `participant-node` 充当稳定端点；etcd/raft 与 OmniPaxos projector 均升级为 v4。
本次盲测中 Episode 1、3、4 的三类错误 predicate 已成为负向回归，稳定端点版本有正向回归。

默认 Scenario Agent 现在是单路径、单战略 Action 协议：每次只提出一个当前前沿选择，
只使用 `continue/revise/abandon`，不能默认创建 branch/control/ablate/select 状态机。
公共自然推进每次最多执行 4 个非干预 Action，并在新 milestone 或新的可干预语义前沿
出现时立即返回可信 `ProgressDelta`。Target closure 仍可持续拥有已接管的因果闭合，且
只能消费公共 Runtime 已 enabled 的 Effect/Deliver/Temporal。

`RiskWitnessProgress` v2 将实际 milestone participant、related participant 和跨 milestone
binding 的可信解析结果反馈给 Scenario Agent，避免模型从摘要猜测“同一请求/参与者”。
closure 仅在 Target 明确支持当前 Risk 且真实干预已执行后暴露，不再把“Target 存在某个
factory”误报为“当前候选可闭合”。

Risk Agent portfolio 的全部机械 executable 候选都会进入 Episode summary。Oracle-backed
只保留为 prompt 中的可验证性软偏好；可信侧不会因 portfolio 暂无 Oracle-backed 候选而
拒绝或丢弃其他已通过资格审查的候选。多 Episode
Investigation 在现有统一 calls/tokens/runtime-decision 总预算内，按 Agent 原优先级依次
调查尚未执行的语义不同候选；只修改显示 ID 的重复候选不会获得额外调查额度。候选只有在
`scenario_attempts > 0` 后才算已经调查；若 Risk 响应使 Episode 越过 token 阈值、候选已经
accepted 但 Scenario 尚未开始，下一 Episode 仍从同一候选继续，不会错误跳到 portfolio 下一项；
`token-stopped` 只终止当前 Episode，只有 Investigation 总 token/call/decision 额度不足以启动
下一 Episode 时才终止整个调查。

源码能力改为显式 mount 内的中立关键词检索和后续限行读取：每个新 portfolio 在提交前
必须先完成一次 Agent 自选的中立搜索，再读取一个该搜索实际返回的精确源码引用；可信侧
拒绝直接 portfolio、跳过搜索的直接读取以及读取未由搜索返回的路径。搜索最多检查 4096 个源码
文件/8 MiB，返回至多 20 个路径与行号；每次只允许一个请求。正常流程仍为一次成功 search、
一次成功 bounded read；其中一次请求返回 `stopped` 时，允许在同一 4-call Risk 预算内重试一次，
并自然失去 portfolio 的资格反馈修复机会。读取仍受 80 行 Agent 请求上限和 24 KiB 摘要上限。完成 read 后不会再
提供 continuation；`*_test.go`、`.git`、build target、vendor、artifacts/benchmarks 等不进入搜索。五节点 etcd 输入中
指向选举计票函数的实验定向材料已删除；Agent 不获得本地修改文件、变更函数或候选差异提示。
M4n10 在既有材料结构内增加紧凑的 CFT fault-model card 和协议族不变量：etcd/raft
材料包含由实际 `n` 推导 `Q=floor(n/2)+1`、多数派相交、每 term 单次投票、Election
Safety/Log Matching/Leader Completeness、持久与易失状态以及活性所需的 quorum/公平条件；
OmniPaxos 材料包含 ballot、prepare/promise、quorum intersection、accepted/decided prefix
与 recovery adoption。公共 Core 不解释这些协议语义，也没有硬编码五节点多数派。

Risk Prompt 要求按 property → invariant → 合法 fault condition → 实际拓扑与 fault allowance
充分性 → 正常恢复与可疑偏离 → milestone/binding 的顺序形成假设。源码 search/read 只确认
机制存在、组件位置或公开 contract，不构成缺陷结论。Prompt 鼓励包含 oracle-backed
候选，但 observable-only/hypothesis-only 候选不会因此被准入层淘汰。可信反馈允许在
search→read→portfolio 后最多一次机械资格修复。

新运行的 MethodSpec implementation identity 为
`m4n10-deep-candidate-investigation-v1`，源码暴露模式为
`mounted-repository-search-readonly-v3`。Risk Prompt/Schema 已升级为
`risk-agent-navigation-v8`：search 前只暴露 search schema，成功 search 后只暴露其真实
match 引用的 bounded-read schema，完成 read 后只暴露 portfolio schema。search query 明确为
单行上的一个大小写不敏感字面子串，不进行分词或 OR；最多只允许一次 stopped grounding 重试。

模型预算不再共享或转移。每个候选固定获得 8 次 Scenario 调用、64 个 Runtime decisions、
每次一个战略 Action、每次最多 4 个公共自然推进 decision；Risk 最多 4 次调用，Episode
总上限为 12 calls、240,000 observed tokens 和 30 分钟。Risk 与 Scenario reasoning 均为
high，Scenario 输出仍限制为 8,192 tokens。新 portfolio 的首候选与从队列恢复的候选使用
相同 Scenario 深度；Risk 未使用额度不再扩大某个候选的搜索机会。除统一 Episode/call/token/
decision 预算外不再设置 portfolio 数量上限；发现 finding 不会提前终止。

Scenario prompt 的压缩视图保留上一轮唯一战略 Action、机械执行步骤、自然推进切片和
closure handoff step ID，使 stateless 调用可以针对实际 source/target/type 修订，而不恢复
完整历史 Trace。

Exploration Memory 新增 `property_ref` 与 `evidence_level`。它们只说明语义重复和候选可验证性，
不能把 `oracle-backed` 解释成已有 finding，也不参与可信 verdict。当前仍不实现跨 Episode 的
live Trace continuation；一个候选只在单 Episode 固定预算内深入调查。

etcd/raft registry 新增 `etcdraft-election-safety`：它只读取 Adapter-owned Evidence，按 term
记录实际运行的 `StateLeader`，同一 term 出现不同 leader 时产生 violation；不读取 vote
tracker 或 Agent 结论。`election-safety` 因此由 observable-only 升为 oracle-backed。

活动 `scenarioTestingResult.outcome` 只使用 `oracle-clean/oracle-finding`，不再把“当前
Oracle 没有 violation”写成 `passed`。最终 finding 仍只来自 registry Oracle；
`witness-instantiated` 但缺少对应 property monitor 时保持 unverified。

不进入正式 Memory 的零模型/fixture 机械校准已经完成：search→read→portfolio→一次修复、
search no-match 后换词成功、stopped read 后改读另一搜索结果、
固定 8-call Scenario 配额、超过三次反馈循环、五节点 quorum 知识、端点 binding、fresh Replay
和 election-safety 合成冲突回归均通过。下一步是先从干净独立 checkout 重新构建并运行一个
不写入正式 Memory 的单 Episode 真模型 canary，确认 `search → bounded read → portfolio → Scenario`；
canary 通过后，才在单独授权下运行六 Episode、约三小时的
无修改特定提示的受控盲测；上限为 72 calls、1,440,000 observed tokens 和 384 Scenario
decision allowance。盲测不提供变更文件、函数、diff 或测试名，但明确提供通用协议不变量、
fault model 和 Oracle-backed properties；因此不能称为“无性质提示”。没有 finding 只能表述为
“该预算内未发现”。正式运行必须在独立、清理过解释性修改注释与专用测试的 checkout 内编译
并运行；不能从主仓库构建后把 `-repository-root` 指向另一棵树。

本地 controlled etcd/raft 变体的独立 upstream 测试已能直接发现问题：当前稳定失败
包括多个 `TestLeaderElection*`、`TestProposal` 以及
`TestInteraction/async_storage_writes_append_aba_race`/
`TestInteraction/probe_and_replicate`；某些全量运行还会出现 `rafttest/TestRestart`。根模块
`go test ./...` 不递归进入该嵌套 module；后续报告必须把它称为普通上游测试已知
可发现的 controlled known-failure，不能暗示只有 Agent 才能发现。正式结果应保存当次
独立 SUT 测试的完整失败清单，不依赖预写的两个名称。

etcd/raft 与 OmniPaxos 两个真实 Target 已走通该链路。M4l2/M4l3 说明
Target-local 消息 leaf type 可以在不扩展公共 ActionKind 的情况下减少
Scenario Agent 的消息选择歧义；这是表达与选择能力的校准证据，不是协议
finding，也不是多 Agent 优于 baseline 的证据。

M4m7 将两个 Go 共识依赖从 module cache 切换为仓库内固定提交、可编辑的 Git
submodule：`suts/etcdraft` 固定 `v3.6.0/f35a022...`，
`suts/hashicorpraft` 固定 `v1.7.3/c0dc6a0...`。根 `go.mod replace` 强制普通
build/test/run 使用这两个目录，模块版本只保留依赖与上游基线身份。普通 Adapter
Manifest 改为 `local-source-unsealed:*`，因此本地修改不会继续冒充官方 clean build。
正式 SUT builder 仍对完整解析源码树建立 digest 和 opaque binary identity；复制和
度量会忽略 submodule 的 `.git` 管理入口，并在生成 modfile 时用已度量 staging copy
替换活动 local checkout。

M4n3 将 OmniPaxos 0.2.2 对应的上游提交 `e3e989b...` 固定为
`suts/omnipaxos` submodule，并把 worker 的 `omnipaxos`、`omnipaxos_storage`
依赖改为本地 Cargo path dependency。Agentic preparation 通过离线、锁定的 Cargo
metadata 验证依赖目录，从该源码重建规范 worker，并要求传入 worker 路径与 Cargo
target 一致；显式源码 mount 只能指向同一 checkout，其树 digest 进入 MethodSpec。
因此协议修改、worker 执行和 Agent 只读源码可以来自同一棵本地树，且仍保留显式
源码授权边界。

M4n4 收口成员配置。etcd/raft 保留既有 `adapter_config.node_count`/显式 `nodes`
单一路径，活动 Agent 输入完全省略该对象时解析为三节点；OmniPaxos 的 Agent 输入新增
`experiment.adapter_config.node_count`，缺省为 3，
当前界限为 3–64。该配置进入 worker reset、Manifest、qualification、Agent root、
Scenario factory、Bundle recipe 和 evaluator-owned Replay，而不是只改变展示字段。五节点
回归已证明真实 worker 选主、qualification/admission 身份和 fresh Replay 使用同一成员集。
HashiCorp 能力样本的 Adapter 也提供 `NewWithConfig`，缺省仍为三节点；因其尚无活动
Agent Target，暂不增加一份无消费者的 Agent JSON。三个 Adapter 均在分配节点结构前
检查上限，省略配置与显式三节点产生相同 Manifest 配置身份。

M4n5 将多节点成员配置接入 Target-local closure，而不是只让 `public-fixed` 能启动。
closure 按实际成员数计算多数派需要的 follower 数；当可替代参与者多于所需人数时，
不会按节点 ID 暗中挑选，而是把可信后端计算出的、仍属于当前 authoritative frontier 的
`closure_candidates` 反馈给 Scenario Agent。Agent 以普通 `revise` 执行其中一个 exact
Action；可信侧只从已经物化的 `FrontierChoice` 恢复所选参与者，并在仍缺 quorum 成员时
继续反馈候选。候选仍只能是 Effect/Deliver/Temporal，不能借歧义引入新的 Drop、Crash
或 Partition。公共层会拒绝 frontier 外、摘要不一致、重复或禁用类型的候选。

零模型五节点验收已分别贯通 etcd/raft 与 OmniPaxos 的 Agent 主路径。etcd/raft 在丢弃
一个 follower 的 `MsgAppResp` 后由 Agent 选择两个备用 follower；OmniPaxos 先通过两个
promise 形成 prepare quorum，丢弃 operation-carrying `accept-sync` 后由 Agent 选择额外
参与者，已唯一 enabled 的另一 quorum 路径由 closure 继续。两条路径最终都达到同一
请求结果、fresh Replay stable 且 Target Oracle clean。无 closure factory 的 Target 与
任意节点数的 `public-fixed` 路径没有改变。
新运行使用 `m4n5-multinode-closure-v1` 与 Scenario prompt v18；此前
`m4n2-preparation-replay-attribution-v1` MethodSpec 只保留只读校验，不能混入当前恢复运行。

M4n6 针对 M4n5 审计完成六项收口。多 Episode Investigation 现在只执行一次重准备，
复用的是不可变 Target composition、root、qualification 和 factory；不会复用 live Runtime
或 Adapter。只有实际承担准备工作的首个新 Episode 记录 preparation，后续 Episode 明确标记
cached。OmniPaxos root ledger 计入一次 `OfferInvoke` preparation Action。

现有 SUT build evidence 增加 Cargo v1 producer，而不是增加第二套账本。它核对本地
OmniPaxos 协议树、worker 源码与锁文件 digest，验证两个 path dependency 指向声明的
checkout，并执行精确的 `cargo/rustc --version`、`cargo metadata --locked --offline`
和 `cargo build --locked --offline`。最终 Audit 使用原有结构，`SUTBuildIdentity` 为
`sha256:<worker binary>`，与 Adapter Manifest BuildID 一致；真实离线构建和 worker 启动
回归已覆盖该链路。

Target-local composition 会按节点数计算 Scenario 调用下界。预算连一次干预和必要 quorum
参与者选择都容纳不了时，在运行前返回类型化错误；32/64 节点等超过全局 16-call 上限的
组合不会再被误接纳。闭合选择也从“路由+消息类型”收紧到具体因果实例：etcd/raft 绑定
term 和请求相邻 index，OmniPaxos 绑定 ballot、sequence 和 RequestID；旧 term、旧 index、
另一 ballot 或另一请求的 enabled 消息不会被闭合器消费。

仓库根新增显式 `-repository-root`，semantic input 可以安全放在仓库外；未提供时保留原有
向上发现作为兼容路径。OmniPaxos worker schema v5 会在每次成功响应中回传实际规范化配置，
Go Adapter 逐字段核对节点数、选举/重发超时、缓冲/批量和 priority，再构造 Manifest。
因此 Go/Rust 两侧配置漂移会直接失败。生产文件中 18 个仅测试使用的旧 wrapper/witness
脚手架已经移至 `_test.go` 或由测试直接调用活动 API，活动 Set selector 未删除。

M4n7 补齐 preparation 的最后一段机械成本。协议中立 `WorkMeter` 包装 qualification
使用的 Adapter factory：每次成功或失败的 Reset 都计为一个 setup/runtime initialization，
每次 Submit 或 ApplyRuntimeAction 都计为一个实际 scheduler decision；Manifest、Check、
Snapshot 等只读调用不冒充调度工作，继续由 wall time 覆盖。三个 qualification composition
均把该 work 封入现有 QualificationBundle；旧 bundle 的字段可缺省，只读验证保持兼容。
Agentic preparation 将这部分加入 formal primary decisions/work，不能再用较短最终 Bundle
掩盖 qualification 成本；回归证明 qualification 本身即可触发聚合预算超限。

同一阶段增加了真实 Cargo Audit 到 OmniPaxos evaluator-owned Replay 的端到端验收：测试从
本地协议树和 worker 源码生成 Audit v5/worker，以该 worker qualification 并产生带 recipe 的
V3 Bundle，再由 `evaluator-replay-v1` 重新启动 audited worker。fresh Trace digest、Bundle
BuildID 与 Audit `sha256:<binary>` identity 必须同时一致。该链路不读取 Agent 输出，也不
调用模型；它闭合了此前“builder 能构建、evaluator 能启动，但没有共同验收”的缺口。

M4n1 补上了 etcd/raft Agent 源码与执行输入之间原先缺失的机械绑定。此前 module prefix
可以显式挂载到任意目录，MethodSpec 只记录 prefix 与 catalog，无法证明 Agent 读取的
代码就是执行代码。现在 etcd Agent composition 会验证当前可执行文件的 local replace、
`go list` 解析目录和显式 mount 三者都指向 `suts/etcdraft`，并把完整源码树 digest 写入
MethodSpec。运行开始、每次源码读取和工件封存前均检查 digest；目录失配和运行中源码
变化都有普通回归。源码访问保持显式授权：未提供 mount 时 Agent 不读取源码。该闭环只
覆盖当前活动 etcd Agent Target；HashiCorp 尚无 Agent Target。OmniPaxos 已由上述
M4n3 单独完成本地 Cargo 源码接入。审计构建的 sealed binary 可在不读取 SUT 源码时继续
运行；其 staged source exposure 尚未接入 build-audit 证据，显式请求会被拒绝而不会误绑
当前 checkout。

M4n2 修复三个正式实验边界：准备阶段现在受独立 deadline 约束，并将
qualification 检查数、root primary/replay work 和耗时纳入工件；其中 root work 进入正式
primary/replay 预算，qualification report/case 与 wall time 单列；当前 V3
Bundle 封存可执行 recipe，formal evaluator 通过 private build audit/SUT binary 在子进程
中重新调用现有 Runtime，Replay authority 升为 `evaluator-owned-sut-replay`；CLI 默认
为 `public-fixed`，`target-local` 必须显式开启，Agent/closure/public progress 的 decisions 在
formal result 中分开报告。旧 V3 Bundle 仍可做普通完整性检查，但缺少 recipe 时不能进入
当前 formal Agentic evaluation。

M4l4 进一步用零模型调用的脚本证明了一条完整垂直路径：在丢弃
`MsgAppResp n2→n1` 后，etcd Target-local closure selector 只沿 n3 备用复制链
和必要 Ready effect 前进，12 个 decision 后提交并应用同一 RequestID；最终
alternate-quorum Risk reached，fresh Replay 稳定，四个 Oracle monitor 无 violation。
闭合器只能从当前 frontier 选择 Effect/Deliver/Temporal；歧义会以正常停止和原
frontier 返回，不能继续制造 Drop、Crash 等干预。权威 frontier 与 selector 副本
已经隔离，且请求恰好在第 12 个闭合 decision 完成时仍报告 `client-terminal`。

M4l5 已将同一 closure 作为可选 Target composition 接入真实 Scenario Agent
执行路径。公共层只在计划已经执行 Drop/Duplicate/Crash/Partition 干预后调用
Target factory；factory 不识别当前 Risk/干预时仍使用原公共顺序，一旦激活则
`underdetermined`、`no-eligible` 和预算停止直接进入结构化 Agent feedback，不会
静默回退。`after_milestone` 与计划结束后的自然推进共用这一逻辑，fresh Replay
仍只执行已记录的精确 Trace，不重新调用 selector。

零模型 Agent Episode 回归已复现 M4l4：5 个 Agent 计划 decision 后，由 etcd
闭合器执行 12 个 decision，最终同一 RequestID committed、Risk reached、fresh
Replay 稳定，四个 Oracle monitor 无 violation。没有 ClosureFactory 的 Target
继续走原 `ExploreScenarioWithPlanner`/公共自然推进路径。

跨调用闭环也已收口：Scenario Agent 从已晋升的可信 `ScenarioExecution` 恢复最近
一次真实干预。零模型两轮回归先得到 `closure-underdetermined`，再以 `revise`
执行一个非干预 Action；第二轮仍使用原干预的 Target closure，并以三个专属闭合
Action 消耗完预算后返回 `closure-budget-exhausted`，没有退回公共固定顺序。
已经满足的 `after_milestone` 不再提前构造 factory。etcd 多候选 alternate 推导也
转换为 `closure-underdetermined`，不再作为执行错误。

当前新运行的 MethodSpec implementation identity 已更新为
`consensus-atlas/agentic-method/m4n10-deep-candidate-investigation-v1`。旧
`m4n8-agent-semantics-portfolio-search-v1`、
`m4n7-qualification-cost-cargo-replay-v1`、`m4n6-causal-closure-build-evidence-v1`、
`m4n5-multinode-closure-v1`、`m4m4-risk-fidelity-v1`、
`m4m1-closure-ownership-v1`、`m4l7-risk-input-closure-handoff-v1`、
`m4l5-closure-v1` 与 `m4d-v1` 仅保留只读
工件验证；新建和恢复运行仍必须与当前完整
MethodSpec 严格一致，不能把旧 Investigation 混入新实现。

M4l6 已在同一个活动 CLI 中加入两个窄实验臂：`public-fixed` 与
`target-local`。它们不是调用方自报标签：`public-fixed` 会从实际 Target
composition 移除 closure factory；`target-local` 只有在 Target 已提供 factory 时
才可选择；随后 `AgenticMethodSpec.closure_mode` 从 composition 的实际 factory
机械派生。composition、MethodSpec 与 factory 不一致时普通输入校验直接拒绝，两个
arm 的 MethodSpec digest 必然不同。etcd/raft 与 OmniPaxos 当前均提供窄 closure
factory；旧 `m4d-v1` 工件没有该字段并继续按原 digest 只读验证。

`after_milestone` 在专属闭合恰好耗尽预算时，步骤和 Agent 顶层反馈现在都保留
`closure-budget-exhausted`，不再被通用 `budget-exhausted` 覆盖。零模型回归已覆盖
模式身份、factory/MethodSpec 失配拒绝和该预算边界。本轮没有调用外部模型。

短配对实验的 etcd 输入将单次 Scenario plan 上限从 4 调整为 5；这是已验证
`persist leader → advance leader → deliver MsgApp → persist follower → drop MsgAppResp`
干预前缀所需的最小长度；该项调整本身没有改变 Scenario 调用数、总 decision 或时间预算。

首轮 `public-fixed` 预算校准中，Risk 与首个 Scenario provider 调用合计 50,793
tokens，超过旧 Episode 上限 50,000；Scenario 响应因此没有进入 proposal 执行。
后续两个配对 arm 统一使用 120,000 token Episode 上限，其他调用、Action 与时间
预算保持不变，并使用新的输出目录和新的 MethodSpec digest。该首轮结果只作为预算
校准证据，不参与 closure 效果比较。

随后完成的两个 live arm 仍不能作为正式配对结果：`public-fixed-v2` 使用 3 次
Scenario 调用、111,234 tokens 且 Risk 未到达；`target-local-v2` 使用 1 次 Scenario
调用、41,157 tokens 且其 Risk 到达。虽然两者均完成确定执行与 stable Replay，
但 Risk Agent 实际生成了不同的 Risk spec；target-local arm 的 Risk 也没有要求
`MsgAppResp` 丢弃后的 alternate-quorum 闭合，因此不能把差异归因于 closure。

M4l6R 为此加入了读取 Risk 的原型。M4l7 将其固定为正式 `-risk-input`：可读取独立
RiskCandidate、RiskCandidateAssessment 或已有 Episode `summary.json` 中的
`accepted_risk`。可信代码只提取候选并针对当前 Target 重新执行资格与 capability-gap
检查；合格后跳过 Risk provider，仅运行 Scenario Agent、Runtime、fresh Replay 与
Oracle。规范化候选摘要、`existing-candidate` 模式和实际 `closure_mode` 均进入原有
MethodSpec。该模式支持单 Episode 和多 Episode Investigation；默认模式明确记录为
`agent-generated`。零模型回归已证明 Risk provider 为 0、
Scenario provider 为 1，并以同一 alternate-quorum Risk 完成 5 个计划 Action、12 个
closure Action、stable Replay 和四个 Oracle monitor。

真实 M4l6R 两臂已经运行。两者的 fixed Risk digest 完全相同，均为 0 Risk calls、
3 Scenario calls、stable Replay 和 0 Oracle finding；因此上一轮 Risk 漂移问题已消除。
但 public-fixed 使用 82,848 tokens/5 Scenario decisions，target-local 使用 92,913
tokens/7 Scenario decisions，二者均以 `call-budget-exhausted` 停止且 Risk 未到达。
target-local 在第 2 次调用已经真实 drop 目标 `MsgAppResp n2→n1`，但 Agent 在同一计划
中继续请求尚未 enabled 的 n3 response，导致 `no-match`；第 3 次调用再次过早请求，
所以只在完整计划结束后接管的 closure 没有激活。该结果是可信的接口负证据，不是
closure 无效或协议 finding。精简结果见
`benchmarks/experiments/etcdraft-fixed-risk-scenario-only-m4l6r-v1/`。

M4l7 已修复该接口缺口。Scenario view 现在显式声明
`post_intervention_closure`；prompt 要求 Agent 在目标干预处结束计划。可信执行器也不
仅依赖提示：每个成功 Action 后由 Target factory 检查真实前缀，只有 factory 明确认可
时才发生 handoff，剩余 Agent 预测步骤不会执行。handoff 后仍只能选择
Effect/Deliver/Temporal，并完整记录 handoff step、自动推进、fresh Replay 和 Oracle。
零模型回归使用“正确 drop 后附带过早 n3 response 投递”的过度计划，确认在 drop 后
截断为 5 个真实计划 Action并完成原 12 个 closure Action。public-fixed、无 factory
以及 factory 不识别干预的行为保持不变。

M4l7 真实 fixed-Risk 配对也已完成。两个 arm 的最终 Trace 在 Scenario step 29--31
共享完全相同的动作与 item identity，并丢弃同一个 `MsgAppResp n2→n1`。public-fixed
使用 3 次 Scenario 调用、83,202 tokens，在正确 drop 后错误预测 n3 response 的 enabled
时机，最终因调用预算耗尽而未达到 Risk。target-local 使用 2 次调用、41,133 tokens，
在同一 drop 后发生可信 handoff，执行 14 个受限闭合 Action，于 Trace step 45 提交
`a9d6-write-1` 并达到 Risk；fresh Replay stable，Oracle finding 为 0。该单样本证明
closure handoff 能在 Agent 已找到正确干预后闭合此因果路径。target-local view/prompt
明确暴露 closure，且 public-fixed 第2次调用因非法 `branch_id` 没有进入执行；因此少一次
调用和42,069 tokens 是完整方法臂结果，不能全部归因于 selector。记录的执行 work
反而是 public 198、target-local 250；没有 wall time/CPU/RSS，不能声称总成本降低。

两个 M4l7 Bundle 现已通过 `defect-eval -oracle-bundle` 独立验证 Bundle、projection
并重算 Target registry。两边均实际检查 trace-integrity、agreement、client application
binding 和 log progress，0 violation；canonical Bundle 以确定性 gzip 进入精简证据，
没有把未保存的 provider journal 列为证据。精简结果见
`benchmarks/experiments/etcdraft-existing-risk-closure-pair-m4l7-v1/`。

M4l8 随后完成零模型因果隔离。测试机械重建 M4l7 真实 Agent 的 step 29--31 共同
Trace，从同一个 drop 后状态分别运行 public-fixed 和 target-local。14-decision 等预算
下，两边 progress work 均为94、qualified primary/replay 均为47/47；target-local 在
step45 reached 并 committed 同一请求，public-fixed 预算耗尽且 not-reached。扩大 public
预算后，它在19个后干预 decision、step50 才 reached，progress work 为104、qualified
primary/replay 为52/52。三条 Trace 均 stable Replay，并由既有 registry 独立重算四个
Oracle monitor、0 violation。结果见
`benchmarks/experiments/etcdraft-shared-prefix-backend-ablation-m4l8-v1/`。

M4l9 已按预注册交替顺序完成五组真实模型重复。两臂均在 5/5 Episode 中选择语义等价的
follower→leader `MsgAppResp` 干预；其中两轮为 `n3→n1`，其余为 `n2→n1`。
target-local 通过 5/5 closure handoff 全部 reached，
public-fixed 仅 2/5 reached，其余三次在正确干预后耗尽调用预算。public-fixed 使用
15 次调用、395,236 observed tokens，target-local 使用12次、265,819 tokens；后者
qualified primary/replay work 略高，不能解释为所有成本均下降。target-local 中
4/5 是干预后直接闭合；`g01` 因旧的 per-call closure quantum 耗尽而交还 Agent，随后
多执行一次 Crash。10 个 Bundle 均记录 stable fresh Replay；独立 evaluator 校验
Bundle/projector 并重算四个 monitor、0 violation，但没有重新执行 Runtime Replay。结果仍只是
单 Risk/单协议的公开 capability 证据，不是缺陷 finding 或总体方法优越性证明。精简
报告见 `benchmarks/experiments/etcdraft-closure-repeats-m4l9-v1/`。

M4m1 已修复上述 closure 生命周期问题：Target 一旦从真实干预接管，只要 Episode
全局 decision 预算尚存，就自动延续同一 closure；per-call progress quantum 不再把
控制权提前交还 Agent。只有 `closure-underdetermined`、`closure-quiescent` 或全局预算
真正耗尽才结束接管。零模型回归以小于完整闭合长度的本轮 quantum 复现该边界，最终
仍完成 5+12 路径，Planner 只调用一次且 Trace 中没有额外 Crash。

M4m2 已完成 OmniPaxos 第二协议垂直切片。真实首请求产生 operation-carrying
`accept-sync` 而非 `accept-decide`；共同前缀在 step 26 丢弃 `accept-sync n1→n2`。
相同 4-decision 后干预算下，public-fixed 未 reached，OmniPaxos factory 从 evidence
推导备用参与者 n3，并只投递 enabled 的 `prepare→promise→accept-sync→accepted`，
恰好 4 步 reached；公共顺序放宽后需 8 步。target Trace 形成 qualified Bundle，
同一 RequestID completed，fresh Replay stable，trace-integrity/agreement 0 violation。
结果见 `benchmarks/experiments/omnipaxos-message-loss-closure-m4m2-v1/`。

M4m2R 已补齐真实 Scenario Agent 协调主路径。新增 existing Risk
历史 v1 输入（现归档于 M4m3 实验目录），每次加载均针对当前 Target 重新资格
审查，并保持 factory 精确要求的规范 Risk ID `message-loss-before-decision`。零模型
Planner 只调用一次，执行 `prepare→promise→Drop accept-sync` 三个计划 Action；随后
factory handoff 并执行四个 closure Action。最终与脚本消融得到相同 Trace/Bundle
digest，同一 RequestID completed、fresh Replay stable、两个 Oracle 0 violation，且
handoff 后没有第二次干预。非匹配 Risk、非 operation-carrying 消息、同层歧义和无候选
也已有独立边界回归。

M4m2O 已补齐 `omnipaxos-client-decision-binding` Target-local Oracle。monitor 只联结
Bundle 中的原始 Invoke、worker decided log 产生的 completed ClientResult payload，以及
同一 participant/index 的 decided-prefix observation；它不读取 Risk、Agent 输出或 PSS。
正常 M4m2R Bundle 现在由 registry 检查 trace-integrity、agreement 和 binding 三个
monitor，0 violation。普通回归覆盖客户端 RequestID、决定内容、请求—决定映射和
decided-prefix witness 篡改；尚未完成的 ClientResult 不产生 violation。OmniPaxos
`client-operation-continuity` 因此从 observable-only 提升为 oracle-backed，不需要修改
公共 Action、Runtime、Bundle schema 或 worker evidence schema。

M4m3 已按预注册镜像顺序完成四个真实 DeepSeek Scenario-only Episode。Risk provider
为 0，总计 10 次 Scenario 调用和 181,093 observed tokens，无传输歧义。target-local
两轮都 Drop enabled 的 operation-carrying `accept-sync n1→n2`，随后发生可信 handoff，
在没有后续模型调用或新干预的情况下直接闭合；同一 RequestID completed，保存的 fresh
Replay stable，三个 registry monitor 均为 0 violation。public-fixed 两轮也机械 reached，
但实际 Drop 是 `prepare` 而非预注册的 operation-carrying leaf。因此本轮应同时报告：
机械 Risk reached 为两臂 2/2；预注册正确干预为 public 0/2、target-local 2/2；handoff
为 target-local 2/2。该差异暴露出现有 Risk 的可执行谓词弱于其自然语言 summary，不能
把 public 的机械 reached 当成同类干预成功，也不能据四轮宣称总体成本/成功率优势。
四个 canonical Bundle、独立 Oracle audit 和完整结果见
`benchmarks/experiments/omnipaxos-closure-pair-m4m3-v1/`。

M4m4 已将上述差异闭合为可执行语义。OmniPaxos Drop Observation 只有在消息是
`accept-sync|accept-decide`、`entry_count > 0` 且携带 RequestID 时，才声明
`message-role=operation-replication` 并暴露该请求；Decision Observation 从实际消息
item dependency 链回溯唯一 RequestID。v2 Risk 用同一 `bind_as=request` 连接 Invoke、
operation Drop 和 Decision。四项普通回归覆盖 prepare、同请求、不同请求和 pending；
对 M4m3 四个保存 Bundle 的离线重投影得到 public 0/2、target-local 2/2。旧 v1 输入和
M4m3 预注册结果保持不变，严格输入新增为
`plans/agent/omnipaxos-message-loss-risk-v2.json`。新 implementation identity 防止旧
`m4m1` Investigation 在相同运行参数下被当前语义恢复。本阶段没有调用模型，也没有运行真实
问题版本；结果见 `benchmarks/experiments/omnipaxos-risk-fidelity-m4m4-v1/`。

M4m4R 修复了严格 Risk 引出的过期全仓测试：测试桩现在先投递
`prepare→promise`，再丢弃携带请求的 `accept-sync`，不再以首个 `prepare` Drop
冒充正确干预。OmniPaxos closure factory 仅在 post-intervention Risk 中的 Drop
milestone 精确对应当前 intervention decision、且 Trace 中唯一 Invoke 的 RequestID
与消息一致时接管。当前明确限定为单 outstanding request；批量消息的 RequestID
投影仍只支持单 entry，因此多请求/批量 workload 尚未宣称支持。closure 对
`entry_count` 也统一使用无符号数值解析。

saved Bundle Oracle audit 保持 v1 工件兼容：Go 字段仍名为
`RecordedReplayStable`，JSON 继续使用 `replay_stable`。该字段只表示 Bundle 已封存的
fresh Replay 结果；evaluator audit 不重新启动 Runtime。

M4m5 将 etcd/raft 的活动输入从固定三项 `nodes` 改为可配置的
`adapter_config.node_count`。紧凑写法按确定规则生成 `n1..nN` 和 Raft ID `1..N`；需要
非连续 ID 或自定义名称时仍可使用显式 `nodes`，但两种成员来源不能混用。Adapter 原本
已经支持任意静态成员列表，本轮关键修复是让 qualification、admission、root Trace 和
最终 Runtime 全部使用同一份 Adapter config，五节点执行不再借用三节点 Manifest。
五节点普通回归已验证 JSON 输入、资格报告和 root Trace 的 Manifest identity 一致。
etcd alternate-quorum closure 仍是窄语义后端，但不再被三节点假设限制：它按实际多数派
人数收集备用 follower。多个等价 quorum 子集通过 `closure-underdetermined` 和窄
`closure_candidates` 交给 Agent 选择，选择事实来自已执行 Trace，不来自模型自报。

M4m6R 针对可信侧资源和正式评测补了六项收口：

- etcd/raft 紧凑 `node_count` 与显式 `nodes` 都在任何成员 slice 分配前限制为最多 64；
- ordered Risk witness 使用失败状态记忆和固定操作额度，合法但高组合度的谓词不能无界占用可信侧；
- 每次 Scenario 尝试封存独立 execution work，Episode 汇总必须由这些 ledger 机械相加得到；
- private holdout 直接读取 `risk-agent/` 与 `scenario-agent/` 的 durable journal，重建调用审计，
  再与 summary 的模型成本逐项核对；只改 summary 不能降低正式 token/call 成本；
- formal Agentic report 明确标记 Replay authority 为 `evaluator-owned-sut-replay`；
  评测器从 sealed recipe 启动试验 SUT，比较 fresh Trace 后再重新投影和重算 Oracle；
- Risk Memory 按规范化谓词/性质/机制结构识别语义重复，不再相信 Agent 自换 CandidateID；
  Episode 同时分开记录 Agent 选择、Target closure 和公共自然推进的 decision 数。

etcd application prefix 与 OmniPaxos decided prefix 已改为增量缓存，避免每个 snapshot 从位置 0
重复计算所有历史摘要；Trace 仍保留完整 Evidence，因此这项只降低目标侧重复计算，不宣称已经
解决长 Trace 的存储放大。活性性质仍明确是 hypothesis-only：在没有可靠截止条件和必要证据前，
不会用一个通用“有界未进展” monitor 制造正式 finding。

## 当前输入

- `plans/agent/`：Agent 方法、模型预算、协议知识和 Target 参数；etcd/raft 当前通过
  `adapter_config.node_count` 配置常规静态成员规模；
- `adapters/*v2/`：官方实现 API 到统一控制面的薄适配；
- `qualifications/*v2/`：Target 实际可控、可观测和可 Replay 能力；
- Target composition：Observation projector、Scenario 语义投影和 Oracle
  registry；
- 受限源码 catalog/mount：Agent 可查询的只读材料；
- 调查预算：调用数、tokens、Action decisions、primary/replay work 和时间。

旧 Campaign/Profile/固定 Scenario 数据不再是活动输入。

## 当前处理能力

- produced/released message 控制、自然虚拟时间、crash/restart、workload；
- target-local namespaced Observation 与通用/Target Oracle 组合；
- typed Risk portfolio、受限源码查询、机械资格与 capability/fidelity feedback；
- 多步 Scenario、live Runtime、周期 `ProgressDelta` 和最终 fresh Replay；
- durable provider journal、Episode/Investigation 恢复与逐调用/逐尝试成本核算；
- formal/private evaluator 从 durable journal 重建模型成本，从保存的 Bundle 重算投影与 Oracle；
- public saved-Bundle audit 可按 Target registry 重算完整 checked/violation 证据；
- 所有 Replay 稳定候选均可离线运行 Oracle，Agent 自报不能产生 finding。

分支实验能力仍存在，但当前主实验优先使用 `continue`、`revise`、`abandon`。
未对 Agent 开放且没有真实用途的 `minimize` 已移除。PSS 保留并冻结为离线
探索指标，不再扩 vocabulary，也不作为当前优化目标。

## 本轮瘦身

已完成：

- 删除约 4.3GB 可重建本地 build/cache 生成物；
- 删除旧 Campaign contract/profile/plan/scenario 数据和历史 coverage 文档；
- 删除生产中无调用者的独立 bounded DFS；保留并重命名 Scenario 实际使用的
  frontier reconstruction、live child execution、fresh verification 和成本核算；
- 删除 M4l2/M4l3 一次性 CLI runner 与其重复实验框架，保留 Adapter 消息语义、
  Target projector 和小型回归；
- 普通测试不再读取 `benchmarks/experiments` 或 `benchmarks/pilots`，必要的
  build-audit fixture 移入小型 `testdata/`；
- 历史阶段 summary 等值测试改为对象级行为测试；
- 压缩仓库约束和阶段文档，删除桌面规划副本要求；
- 删除两个确认无调用者的旧执行 wrapper，并让注释只指向 live Runtime 主线；
- 删除 `benchmarks/pilots` 以及除最终 M4l2/M4l3 外的历史、失败和基础设施实验；
- M4l3 使用 `final-summary.json` 取代有歧义的原始 summary；三份 canonical Bundle
  以确定性 gzip 保留，展开 Bundle、Scenario result、root Trace 和 provider journal
  不进入 HEAD；
- 清理所有本轮遗留的空目录；
- 删除零生产调用的 `ClearKey`、`DecisionLog`、公开 identity/parser 包装和未使用
  decision schema 常量；测试直接覆盖活动的 proposal inspection 入口；
- 删除 `controlentropy` 内只由自身测试使用的第二套 replay 模式；正式
  `controlruntime.Replay`、entropy tape、digest/prefix 校验和 Adapter entropy provider
  保持不变；
- etcd/OmniPaxos 共用一份 Scenario Bundle/Risk/Oracle 复核函数，CLI 共用
  composition finalizer；复核函数要求 Oracle registry 的 projector ID 与实际
  Decision Projector 一致，交叉错配由普通测试拒绝；Target-local projector、closure
  和 Oracle 语义仍独立；
- 弱语义 OmniPaxos v1 Risk 已从活动 `plans/agent/` 迁入 M4m3 实验目录，活动输入只保留 v2。

本轮验证已完成：`go test ./...`、`go vet ./...`、`audit-no-v1`、
`audit-race-shards` 和两个五节点 Agent/Replay/Oracle 定向回归通过。新增 race shard
清单已与 `go test -list` 对齐；两个真实 Target 的联合聚焦 race 在 300 秒内未完成且
未产生 race 报告，已按既定规则停止，未重复消耗时间。普通全量测试覆盖了同一路径。

## 当前结果边界

已经证明：

- 两个真实 Target 可按统一 Action/Trace/Replay 接口执行；
- Agent proposal 可以经过 typed repair 后绑定真实 enabled Action；
- target-local 消息语义能改善至少两个公开单样本中的目标选择；
- Replay、Oracle、方法身份和成本边界具有机械检查。

尚未证明：

- Agent 或多 Agent 优于 Random、专家或其他搜索方法；
- 当前 Risk/Scenario 策略能够长时间、全面测试任意共识；
- PSS 数量代表测试完整度或剩余缺陷概率；
- etcd/raft、OmniPaxos 或 ConsensusAtlas 正确、完整或无缺陷。

## 下一步

1. 选择一个历史问题版本或受控差异版本，先以零模型 Trace确认现有
   Action/evidence/Oracle 能得到首个非零结果；
2. 再让 Scenario Agent 从已知 Risk 复现同一干预，最后恢复 Risk Agent 完整流程；
3. 设计同预算 Random/单 Agent/双 Agent 对照，再运行长时公开实验。
