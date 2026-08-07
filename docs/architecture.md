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

## v1 与 Control Runtime v2 的共存边界

上图描述的是仍在运行现有实验的 v1 路径。M5.1 新增了一条独立 v2alpha1 路径：

```text
opaque input / selected ActionID
              |
              v
internal/controlruntime ---- owns ----> message / temporal / effect lifecycle
              |                              |
              | AdapterCommand               +--> exact state + v2 trace
              v
       Control Adapter
              |
              +--> stable Yield + frozen ProducedItem + Evidence + Entropy tape
```

v2 不复用 v1 的 `Engine/Event/OutputBatch/HostOperation` 抽象。公共契约在 `internal/control`，执行在
`internal/controlruntime`，确定性随机在 `internal/controlentropy`，外部准入在
`internal/conformance`。`adapters/fixture` 用于证明公共状态机；`adapters/etcdraftv2` 已接入官方
v3.6.0 的配置驱动静态 N 节点 Ready/effect/Tick/message/Step 切片。三节点真实选举已经验证
Runtime-owned message 的保留、复制、分区、恢复、投递、丢弃与严格重放。M5.2.3 又将 Ready
拆为 persist 与 application/Advance effect，并加入版本化 durable image、power-loss、fresh
Storage/RawNode restart、incarnation 和协议无关生命周期 Conformance。

M5.2.4 在相同边界上增加 opaque external input：Runtime 只冻结 `PayloadEnvelope`，具体 Adapter
映射到官方 `RawNode.Propose`；committed entry 才进入版本化 application durable image，并产生
client result。`OfferInvoke` 采用即时资格语义，不在 trace 外排队当前 ineligible 输入；协议拒绝
则作为结果进入轨迹。新增的 opaque-invoke Conformance 不解析 Raft evidence。

M5.2.5 新增独立 `internal/migration` 外部结果模型，并把同时依赖 v1/v2 的代码限制在临时
`migrations/etcdraftv1v2` composition root。三个冻结子集通过 command/applied-node/safety/replay/
witness expectation，对自然换主则因 v1 capability 缺失机械 deferred。比较结果是
`qualified=false`，不授权删除 legacy execution。

M5.3 已将 Manifest typed requirement、外部 Conformance case 和枚举 Unsupported 合并为版本化
Qualification Report。M5.4 又在 HashiCorp Raft 的 goroutine/同步 `Transport` 控制表面复用了同一
Runtime：消息由 Runtime 持有，`DropMessage` 先完成 Adapter acknowledgement，再记录终态；节点
restart 保留官方 store 并进入新 incarnation。异步消息实现可将 acknowledgement 实现为 no-op。

M5.4e 对 `portable-cft-control-v2` 冻结双实现能力矩阵：etcd/raft 的 8 项 required capability 全部
validated，HashiCorp 为 3 validated、6 Unsupported。共同 validated 交集只有 runtime-owned
message、crash/restart incarnation 和 opaque invoke。自然时间、SUT entropy 和 strict replay 没有
因第二实现缺少注入边界而改写公共语义，也没有被人工标为通过。当前迁移策略仍是并行替换：v1
继续保留 PSS/Coverage/Agent/benchmark 和历史工件；v2 消费者必须按实现读取 validated gate，直接
消费者归零前不删除 v1。

M5.5a 不修改上述资格模型，而是在其上派生 `ControlSurfaceReport`：external input、message、
lifecycle、temporal 和 durability 表示目标系统存在的通用场景表面；stable yield、pure enabled、
strict replay、audited entropy 和 process isolation 表示测试框架保证。presence 声明不能产生
validated control；scheduler-owned 由 Manifest 的 Action/Item 组合推导，最终 credit 还须对应
Qualification capability validated。该分层允许未来黑盒 Target 以 opaque/observable/interceptable
接入，而不假装获得 step-driven Adapter 的确定性能力。

M5.5b 新增与协议无关的 `internal/blackbox`。它使用 shell-free argv 启动独立进程，只连接显式
readiness endpoint，把客户端调用冻结为 opaque bytes，并提供 deliver/drop、直接进程 kill/restart
和数据目录复用。pending call 由 Envelope 而非目标进程持有，所以可跨 incarnation。该层依赖
wall-clock readiness，只控制单次客户端 connection，尚无 peer gateway；因此能力只记为
observable/interceptable，不接入 strict Control Runtime replay。

M5.5c 在同一包中增加不解析 payload 的 connection gateway。目标把可配置 peer endpoint 指向
gateway，gateway 只执行 accept、双向 byte copy、partition/heal 和计数。两个真实子进程验证了开放、
隔离和恢复，但该 backend 没有 framing、stable message ID 或确定性 I/O 顺序。partition/heal 尚未
注册为 Runtime Action 或通过 Qualification，所以只记为 interceptable，不能据此给 message control
或 strict replay 记分。通用黑盒网络机制在此冻结；更强控制继续由薄 Adapter 提供。

M5.6a 将 Runtime-owned Action 的外部映射收回唯一 Adapter 契约：`ApplyRuntimeAction` 允许薄 Adapter
执行 Gateway partition 等外部副作用，Runtime 仍独占 mailbox 与 partition 状态。官方 etcd/raft 的
内存网络只需 no-op actuation；test-only Gateway wrapper 对完全相同的 Partition/Heal Action 调用外部
gate。该接缝取代了另建 backend selector 的计划，但尚未解决任意拓扑 typed binding 或规范化外部证据。

M5.6b 把 partition 的 `id/left/right` 提升为公共 typed parameters，并新增显式 Gateway topology binding。
binding 只按登记的 directed links 计算跨组集合，拒绝重复 ID/edge/gateway、未知节点和空 cut；它不猜测
目标真实拓扑。多 Gateway 的原子 apply、重叠 partition 状态和外部 evidence 仍留在未来薄 Adapter，
不能由 binding 解析结果自行取得控制资格。

M5.6c 在 Adapter 契约上增加 `CheckRuntimeAction`，使 Runtime-owned Action 在进入 enabled 集合前也经过
目标执行边界资格检查。`GatewayActuator` 串行解析 crossing links，以 partition ID 管理 active 集合并以
Gateway identity 管理引用计数；中途失败时逆序恢复已改变的 gate。该事务只覆盖 controller state：真实
socket 切换仍可被外部进程部分观察，已关闭 connection 不可恢复。因此 Gateway 保持
connection-level scheduler-actuated/interceptable，不能等同于 Runtime mailbox 的 scheduler-owned
message 或 strict replay。

M5.6c 后冻结新的接入决策：统一 Action 只统一上层语义，不承诺所有实现取得同一控制强度。能力由
Control Surface、Control Grade 和 Deterministic Guarantees 三个正交维度表达。默认接入目标是最小、
非侵入式灰盒：官方 step/transport/clock/storage API 优先，其次替换外围依赖，协议核心修改最后考虑。
MessagePort/TemporalPort 等 Control Port 只作为目标薄 Adapter 的内部构件，不能形成 Runtime 的第二套
backend 或 type switch。详细规则见 `docs/graybox-control-ports.md`。

## 依赖方向

`internal/engine`、`internal/host`、`internal/core` 和 `internal/driver` 不得 import 任意具体共识包。
具体共识类型只允许出现在对应接入边界：

```text
drivers/etcdraft
adapters/etcdraftv2
adapters/hashicorpraftv2
```

CLI 可以注册具体 Driver，但协议注册不能把其类型传播到 Runtime。

v2 另有机械依赖门：`internal/control*`、`internal/conformance` 和协议无关 fixture 禁止 import v1
`core/driver/host/engine`、`families/*` 或任何具体共识。具体 Adapter 可以依赖官方实现和 v2
公共契约，但不得把协议类型反向传播进 Runtime。etcd/raft v2 的节点数和原生 ID 映射只存在于
Adapter `Config`，其规范化 digest 进入 Manifest；Runtime 不按一节点或三节点分支。

## Driver 与 Host Runtime（v1）

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

## 消息所有权（v1）

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

## 存储状态（v1）

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

## Enabled 与 pending（v1）

适配器的 read-only `Enabled(event)` 用于表达临时前置条件：

- 停止节点不能接收输入；
- 有 outstanding batch 的节点不能处理新输入；
- host operation 必须引用当前 batch；
- 分区阻塞跨组消息，但不删除它；
- 依赖事件成功前，后续 operation 不启用。

暂时不可用的事件保留在 pending 队列，不会被消费为 ignored。

## 虚拟时间与 timer queue（v1）

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
