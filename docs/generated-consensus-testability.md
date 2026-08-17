# Agent 生成共识的可测试性规范（草案）

本文定义未来由 Agent 生成或改写的共识实现，如何进入 ConsensusAtlas 的确定性测试闭环。它是可修订草案，
不是新的冻结 contract，也不替代现有 `PortableCFTProfile`、Adapter Manifest、Target surface 或普通组合测试。

## 1. 目标与非目标

目标是让生成实现把所有影响协议状态的外部原因显式交给宿主，并让 ConsensusAtlas 能够控制、观察和 Replay
这些原因。公共层统一的是执行控制和证据接口，不要求 Raft、Paxos、HotStuff 等协议共享同一内部状态机。

本规范禁止结果型测试接口。实现不得通过 `SetLeader`、`SetTerm`、`ForceCommit`、`InjectDecision` 等入口直接
写入期望结果；测试只能控制消息、时钟、输入、生命周期、外部效果和受声明约束的随机源。

## 2. 四层接口

```text
公共控制动词
    Invoke / Deliver / Drop / FireTemporal / CompleteEffect / FailEffect / Crash / Restart
                              ↓
受控资源和宿主效果
    Message / Timer / Persist / Sync / Apply / Snapshot / ClientResult
                              ↓
Target-local 类型与语义投影
    消息类别、阶段、角色、epoch/view/ballot、slot/index、操作状态
                              ↓
当前实验 FaultEnvelope
    本轮允许的 crash/drop/duplicate/partition 数量和并发边界
```

公共 `ActionKind` 继续表示原因型控制动词。协议差异进入 ProducedItem、HostEffect、namespaced Observation、
Target projector 和 Oracle；不因一个协议的新消息名扩大全局枚举。

## 3. 必备性质

### 3.1 确定性协议核心

两次 yield 之间，网络、墙钟、随机数、存储和后台任务不得绕过宿主偷偷改变协议状态。相同配置、seed 和 Action
Trace 必须得到相同的 yield、evidence 和终态；`Check`/enabled 查询不得有副作用。

### 3.2 统一原因入口

算法级实现至少接受：ClientInvoke、PeerMessage、TimerFired 和 EffectCompleted/EffectFailed。宿主可以把语言或
库的原生接口包装成这些入口，但不能用包装器直接改协议内部 leader、epoch、log 或决定状态。

### 3.3 外部效果输出

协议应产生不可变的 Message、Timer、Persist/Sync/Apply/Snapshot Effect 与 ClientResult，由宿主决定何时完成、
失败或投递。payload 在释放给 Runtime 后冻结；Agent 只能选择已产生对象，不能伪造或修改内容。

每种 HostEffect 应声明：

- 类型和稳定实例 ID；
- 前置依赖及完成后释放的对象；
- 阶段和 durability 语义；
- 允许的完成/失败结果；
- 对 visible、durable、applied 状态的影响。

M4j1 已将其中可机械检查的最小部分落到现有 Manifest：消息可声明 type hint 与 metadata key；HostEffect 可声明
kind、phase、durability、完成结果和失败结果。声明存在时，Runtime 会逐个校验实际 emission；声明为空的历史
Target 保持原行为，并不因此被推断为具备这些细粒度能力。当前 frontier 会把实际依赖、消息提示/metadata、
effect phase 和本 Action outcome 投影给 Agent，selector 只能使用当前 enabled Action 上已有的值。

### 3.4 生命周期与持久化

节点生命周期和存储生命周期必须分离。实现应明确 volatile、visible、durable、applied 四种状态，并能从精确
durable image 重启；crash 不得把 volatile 状态伪装成持久化状态，也不得隐式删除 Runtime 已接管的消息。

### 3.5 可观察证据

Target 至少应能投影 participant/node、role、epoch/ballot/view、决定/提交/应用进度、request ID、slot/index 与
前缀摘要。协议特有内容使用 namespaced Observation；Core 只校验声明、类型、Trace 绑定和 Replay，不解释语义。

关键 Observation 和消息类别应能指向实现或伪代码中的真实 transition。Target projector 可以抽象，但不能运行
一套与被测实现不同的简化共识。

### 3.6 能力与盲区声明

生成实现必须声明支持和不支持的 Action、Item、Effect、Observation、Oracle 与 fidelity boundary。声明本身不
产生可信能力；Qualification 和普通 composition test 必须从实际 Manifest、frontier、Trace 与 Replay 验证。

## 4. 分级要求

### T1：算法级确定性闭环

- strict yield 与纯 enabled check；
- Invoke、Runtime-owned message、自然 Timer；
- 显式 entropy source 或无隐藏随机性证明；
- exact Action Trace Replay；
- 基本角色、epoch 与决定进度 Observation。

所有声称可进行算法级确定性测试的生成实现必须满足 T1。它对应并扩充现有 `PortableCFTProfileV3` 的严格控制
能力，不建立平行资格系统。

### T2：完整节点与恢复闭环

- crash/restart 与 incarnation；
- Persist/Sync/Apply/Snapshot 等可分阶段 HostEffect；
- effect failure 和 precise durable image；
- 恢复后的消息、持久化与应用一致性 Oracle 接口。

声称是完整实现或生产候选时必须满足 T2；只满足 T1 的 Target 必须明确标注持久化和恢复 fidelity gap。

### T3：协议扩展

成员变更、快照传输、动态 quorum、BFT 投票/证书、密码学和协议专用外部效果按 Target-local profile 声明。
T3 不进入所有 CFT Target 的公共必选分母。

## 5. Agent 与可信代码的职责

Agent 可以生成核心代码、host/wrapper、Adapter 草案、能力声明、Observation/PSS projector、测试假设和场景计划。
可信代码仍负责：

- 校验 Manifest 和 Profile；
- 将 selector 绑定到当前真实 enabled Action；
- 执行、Trace、Replay 与预算核算；
- 验证 Observation 来源；
- 运行 Oracle 并形成最终结论。

生成代码不能通过自报 capability、Observation 或测试结果获得资格。Agent 输出的 Oracle 只能作为候选，必须由
确定性 monitor 实现并在正确 control 上验证不会误报。

## 6. 接入工件与验收

一个新生成 Target 的最小交付包括：

1. 协议知识、公开性质、Target Dossier 和 workload；
2. Adapter/host 组合与 Manifest；
3. T1/T2/T3 支持矩阵及 fidelity boundary；
4. namespaced Observation 与 Target-local Oracle registry；
5. 每项 composable Action 的 offered → selected → executed → replayed 普通测试；
6. 一个至少 128 Action 的确定性长轨迹与 fresh Replay；
7. 正常 control 上的 Oracle 无误报回归。

验收只证明声明的测试接口可执行和可复现，不证明协议正确、完备或不存在缺陷。

## 7. 后续验证方式

规范稳定前用三个不同形态的样本反复检查：leader-based log replication、Paxos/ballot 系列以及
HotStuff/Tendermint 类 view/QC 协议。若新增协议只能通过修改公共 Core 中的协议分支接入，应优先检查
Target-local Item/Effect/Observation 扩展缝，而不是立即扩大全局 ActionKind。

只有 wrapper/host 无法接管实现内部墙钟、随机源或后台状态变化时，才允许维护最小 instrumented variant；
原始 upstream Target 与 instrumented Target 必须拥有不同实现身份和 fidelity 声明。
