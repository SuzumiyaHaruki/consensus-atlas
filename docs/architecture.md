# Architecture

## 信任边界

```text
Human/Family Pack
       |
       v
Protocol Knowledge Contract --digest--> fixed semantic denominator
       |                                      |
       |                                      v
       +--> Onboarding Agent --> Binding + Driver + witnesses
                                      |
                                      v
                    deterministic onboarding validator
                    compile / conformance / replay / Oracle
                                      |
                     +----------------+----------------+
                     | invalid: findings -> Agent      |
                     | validated: Profile + Manifest   |
                     +----------------+----------------+
                |
                v
Scenario Interpreter --> Deterministic Engine
                                |
                                v
                       Generic Host Runtime
                                |
                                v
                        Thin Protocol Driver
                                |
                                v
                         Official SUT APIs

raw trace --> Replay --> PSS Projector --> State Discovery Ledger
     |                         |
     +--> Canonicalizer --> Oracle --> Fixed Profile Ledger

Strategy Agent ----- bounded Test Plan proposal ----- deterministic concretizer

private defect manifest --> opaque trials --> Campaign v2 --> trusted Oracle recompute
          |                                                    |
          +---------------- root-cause/false-positive ledger <-+
```

当前 model-backed 阶段只生成 Binding 和已有 witness 的引用，不生成 Driver 源码。DeepSeek 客户端运行在不继承父环境的 JSON 子进程边界中，只接收 Contract、Driver Manifest、候选场景和上一轮完整机械报告。key 由客户端在运行时从权限受限文件读取；最终 Profile 仍只由 Go 验证器产生。

接入 Agent 位于测试执行之前。它可以发现 API、生成实现绑定、薄 Driver 和可执行见证，但不能提出或改写当次协议语义、覆盖分母和验收条件。人工输入边界是版本化 Protocol Knowledge Contract；其后由代码验证，无逐事实人工批准步骤。缺少可验证见证是 actionable failure；目标实现确实不具备的能力是 `Unsupported`，对应义务仍在分母中。

旧 Source Catalog、Integration Pack、Scout benchmark 和人工 Gate 已从主仓库删除。

The experiment path adds a protocol-neutral layer above Engine:

```text
shared setup -> measurement root -> Explorer decisions -> measured traces
                                      |
                                      v
                         cross-run PSS discovery ledger
```

Go 核心在不启动 Python Agent 的情况下独立运行。Agent 不参与契约编译、事件执行、等价判断、Oracle、验证结论或计分。

## 依赖方向

`internal/engine`、`internal/host`、`internal/core` 和 `internal/driver` 不得 import 任意具体共识包。etcd/raft 类型只允许出现在：

```text
drivers/etcdraft
```

CLI 可以注册具体 Driver，但协议注册不能把其类型传播到 Runtime。

## Driver 与 Host Runtime

`ProtocolDriver` 是一个薄原生边界：

- `Invoke` 调用 `Campaign/Propose/Step` 等原生输入；
- `Poll` 冻结一个原生输出批次；
- `ExecuteHostOp` 执行 Runtime 选中的持久化、应用或 acknowledge；
- `Crash/Restart` 释放 volatile state，并从 durable image 重新创建节点；
- `Inspect/Snapshot` 提供只读证据；
- `Capabilities` 公开支持与不支持的控制边界。

Driver 不投递、丢弃、复制或重排消息。它把原生输出翻译成通用 `OutputBatch`：

```text
batch token
  persist
    -> sync
       -> emit(message 1)
       -> emit(message 2)
       -> apply
          \____________
                       -> acknowledge
```

通用 Host Runtime 验证 operation token、依赖 DAG、唯一 acknowledge，以及 acknowledge 是否传递依赖全部 operation。之后它将 operation 转换为普通可调度事件。

## 消息所有权

```text
SUT produced
    |
Driver.Poll 冻结 protobuf/bytes
    v
OutputBatch (node volatile state)
    |
host barrier succeeds
    v
emit/release
    |
Runtime mailbox
    +--> deliver -> target Driver.Invoke
    +--> drop
    +--> duplicate
    +--> delay / partition
```

消息只有执行 `emit` 后才进入网络邮箱。由此得到以下 crash 语义：

- emit 前 crash：取消 batch 中所有剩余 operation，不产生网络消息；
- emit 后 crash：网络邮箱继续持有消息，源节点停止不删除它；
- 目标停止：消息保持 pending，可显式 drop 或在目标恢复后 deliver。

每条消息携带：

- 全局事件 ID；
- source/target；
- sender epoch；
- per-link sequence；
- causation ID；
- clone-of ID；
- type hint；
- payload bytes 和 digest；
- 协议只读 metadata。

Payload 被写入 JSON trace 的 base64 字段，不依赖进程内消息表。

## 存储状态

```text
RawNode volatile state
       |
persist
       v
visible MemoryStorage
       |
sync
       v
durable storage image

committed entries --apply--> durable application image (v1 model)
```

power-loss crash 丢弃 RawNode、visible storage 和 outstanding Ready。restart 重新构建 MemoryStorage，再用 durable application `Applied` 和 `ConfState` 创建 RawNode。网络邮箱不属于节点存储。

第一版把 application apply 建模为原子 durable 操作。后续若拆分 application write/sync，必须升级 Capability 和 Profile。

## 启动

官方 `RawNode.Bootstrap` 会产生初始 Ready。场景使用显式 `start` 事件让 Runtime 收集并处理该输出，之后才执行 campaign。这避免把隐藏的初始化 I/O 排除在 trace 外。

## Enabled 与 pending

适配器的 read-only `Enabled(event)` 用于表达临时前置条件：

- 停止节点不能接收输入；
- 有 outstanding batch 的节点不能处理新输入；
- host operation 必须引用当前 batch；
- 分区阻塞跨组消息，但不删除它；
- 依赖事件成功前，后续 operation 不启用。

暂时不可用的事件保留在 pending 队列，不会被消费为 ignored。

## 虚拟时间与 timer queue

`Engine.Advance` 只推进 logical clock 并记录 `clock-advance` control record。可选的
`TimerSource` 以完整声明的方式把 native timer（稳定 ID、目标、绝对 deadline、payload）交给
Engine；Engine 才是 pending queue 的唯一所有者。deadline 到达只影响 enabled 集，不会自动执行
timeout。timeout 被执行后，下一次 declaration 必须 remove 或 re-arm 该 ID，否则 Engine 拒绝不变的
已消费声明。无 `TimerSource` 的 Driver 保持零 timer，已有非时间 trace 的 snapshot 形状不变。

因此该边界可用于未来具备确定性 timer 控制的 CFT/BFT Driver，却不会把 etcd/raft v3.6 的不透明
随机 election timeout 错写为可信能力。

## Drop 与依赖

drop 是 terminal trace outcome，并使依赖该事件的操作保持禁用。普通网络消息没有后续依赖，因此可安全删除。当前尚未把 drop feedback 回送给 Driver；snapshot `ReportSnapshot` 因此被标记 Unsupported。

## Replay 与 canonicalization

当前有两个不同指纹：

- `ExecutionFingerprint`：包含原始事件和 before/after 快照，用于严格确定性重放；
- `CanonicalFingerprint`：结构化重命名节点和事件，并删除物理 causation/link sequence，用于保守场景归类。

当前 canonicalization 仍保留协议 payload，没有实现 term 平移、日志关系和偏序因果图，所以不能把它描述成完整 PSS 场景键。

Raft Family Pack 另外提供 `raft-family-pss-v1` 状态 projector。它对节点、term、index 和 value 做语义重命名，生成协议状态键。这个键用于状态发现，不替代严格执行指纹，也不等于尚未实现的偏序场景键。

## Protocol-state discovery

`internal/protocolstate` 只定义通用 `Projector` 接口和 discovery ledger：

```text
Family Projector: snapshot -> canonical state + key
Generic Ledger:   trace + projector -> prefix curve + first witnesses
```

具体协议字段只存在于 Family Pack。CLI composition root 根据 Profile 的 `pss_id` 注册 projector；Runtime、Driver 契约和 ledger 都不依赖 Raft。

Raft v1 只在 Ready acknowledge 和 crash/restart 后采样，避免 persist/sync/emit/apply 数量虚增协议状态。普通 scenario runner 仍展示包含 bootstrap 的完整单轨迹账本；跨搜索器实验通过显式 measurement window 排除 setup，并把共享根状态记在预算 0。

`internal/explore` 不 import Family Pack 或具体 Driver。Random/DFS 只能在 Engine 当前候选事件上选择 execute/drop/duplicate；PSS 只在运行完成后投影证据。`bindings` 与 `families` 顶层包是 CLI composition root，具体系统注册不会反向进入通用 Runtime。

跨运行实验在 setup 后建立显式 measurement root。setup fingerprint 和根 PSS key 必须在所有运行中一致；根状态计入预算 0，之后每个 decision 必须对应恰好一条 measured trace record。

## Coverage

覆盖原子只有同时满足以下证据才算覆盖：

```text
Reach + Observe + Check + Replay + Conform
```

Profile 另外声明 `required_capabilities`。缺失或 unsupported 的所需能力降低 Capability 支持率；即使原子分数达标，支持率低于 90% 也不会得到 `High=true`。

Oracle FAIL 仍然算覆盖，但 Oracle 结果和覆盖结果分别报告。

状态发现数量没有固定分母，用于解释搜索方法在相同完整执行预算下的增长速度；Profile 覆盖有冻结分母，用于评价最终测试义务。二者都是内部指标。正式方法效果由 Agent 看不到的历史缺陷/语义 mutant 根因检出与正确 control 误报评价。完整区分见 `docs/metrics.md` 和 `docs/defect-benchmark.md`。

## Protocol Contract 与自动接入

Contract 固定协议家族、状态维度、能力、操作、语义义务、见证标签、监控器和评分权重。编译结果完全由代码决定。Binding 只允许把 Contract ID 映射到 Runtime ID，并引用可执行 scenario；Binding 格式没有自定义 assertion 字段，避免 Agent 通过降低断言强度获得接受。

每个可支持义务必须由至少一个见证同时满足：契约标签可达、契约 monitor 已执行、无 Oracle 违规、两次执行指纹一致、Driver 前后 conformance 均通过。已验证 Profile 是唯一正常运行输入。

## Agent Campaign

`internal/agentcampaign` 位于冻结 Profile 与 `internal/campaign` 之间。Blind Planner
只获得 opaque trial ID、Profile identity/node projection、supported capability names、
Driver 声明的受限输入形状、opaque debt ref、有限 DSL policy、历史 proposal 和机械 aggregate finding。Driver/SUT/build
identity、真实 obligation、predicate、monitor、trace、Oracle 和 Ledger 不会穿过该接口；私有
Ledger 只由 `campaign.Session.ExecutePlan` 更新。所有 blind request、生成审计、提案、执行
摘要和 finding 进入 hash-chained append-only Blackboard。

Blind Planner 在不继承父环境的 JSON 子进程中运行。Proposal 的 target ref 由可信
Coordinator 在 Runtime 前解析，随后仍必须通过 Test Plan schema、稳定 selector、ID、
run/decision budget 和协议因果结构去重检查。随机 seed 和不透明应用值不构成新的协议因果
结构；模型返回无效 proposal 时 provider usage 仍先进入 token budget。达到 attempt、连续
无进展、run、decision 或 token 限制时，Coordinator 确定性停止。

Blind Planner v1 当前只完成无模型 fixture 闭环，尚未证明模型效果；在独立 holdout
证明需要角色拆分前，不继续增加 Scenario/Critic。

正式 holdout 由 curator-side `cmd/blind-audit` 在模型调用前重算 private `Manifest.Blind()`，并检查
Blind Manifest、Planner request/transcript 和 submission 等公开 JSON 是否含有 private variant 元数据。
审计只输出 redacted code/digest，不把 private 字符串重新写入报告。

## Defect Benchmark

`internal/defectbench` 位于 Campaign 结果之后，不参与搜索。私有 Manifest 保存
variant 类型、root cause、source/SUT/BuildAudit/binary digest 和允许的可信 monitor；Agent-facing
Manifest 只包含 opaque trial ID 和共享预算。evaluator 不信任报告中保存的 Oracle
结论，也不信任提交者自报 report 来源。Submission v2 提交 report、BuildAudit、binary、
Profile 和 plans；evaluator 校验工件后亲自重跑 binary，要求报告 digest 相同，再从
setup + measurement trace 重建 Ledger 并执行注册 monitor。只有 replay-stable、
conformant、身份匹配且未超完整 primary work 预算的证据可以 kill defect；同样证据
出现在 control 上记为 false positive。Coverage/PSS 数值不会进入 kill 判定。

## Candidate Qualification

`internal/defectbench` 的 Candidate 只保存 provenance/source/root-cause 元数据和六类
typed requirements，不保存资格状态。具体 `catalogs/etcdraft` composition 层组装可信
CapabilitySnapshot：protocol、Family、controllable input、observable event、Driver capability、trusted
monitor、execution outcome 和 Profile bound 各自独立。Profile obligation 只能贡献
observable evidence 与范围，不能把 `persist/sync/emit/apply` 等观测事件变成直接输入。

通用资格器没有 Raft 分支；先强制 protocol/Family 作用域，再检查每类
`Requirements ⊆ CapabilitySnapshot`。Catalog 与 Snapshot 中的集合会在 digest 前排序。所有缺口
按固定类型序和 ID 排序，只有 QualificationReport 可以机械写入 `qualified/deferred`
和稳定 reason code。source commit/reference 不参与判定，资格过程不访问网络。

## Controlled SUT Build

`internal/sutbuild` 先用离线 `go list -mod=readonly` 绑定 module path/version，再验证目标
文件原始 digest 和唯一文本转换。由于 Go 禁止 overlay 直接覆盖 module cache 文件，
构建器把已验证模块复制到专用临时目录，只修改副本中的目标文件，并通过临时
`-modfile` local replacement 执行 `GOPROXY=off`、`GOSUMDB=off`、
`GOTOOLCHAIN=local`、`-mod=readonly` 构建。项目 go.mod/go.sum、Driver/Runtime/Profile/
Oracle 和 module cache 都不被改写。replacement 使用由 SUT identity 派生的稳定仓库相对
路径，避免随机临时绝对路径进入 Go build info。

build identity 由 module path/version、转换后的 source-set digest 和目标 package 派生，通过
`ldflags` 写入 etcdraft Driver Manifest。audit 支持单文件历史格式、多文件精确替换（v3）与
未修改 module tree（v4），并保存 source/module tree、binary、command 与 toolchain identity；构建后
复核整个 module cache tree。公开 calibration 或历史复现可提交具体转换；正式 holdout 转换不得出现在公开规划输入。

## 仍未实现

1. 正式 holdout 的隔离 curator/runner 和私有转换发布流程；
2. wall-clock/CPU/RSS/Agent-token 统一报告、重复 campaign 与统计置信区间；
3. 由多个真实漏检驱动的少量复合时序义务；
4. PSS 编译器、跨事件 causality graph 和保守 independence；
5. DPOR/SAMC/ordered t-way 和状态约简生成器；
6. snapshot feedback 与精确 Ready barrier；
7. 部分同步活性 Profile、第二个真实 Driver；
8. QC/lock shape 和有限 Twins BFT 算子。
