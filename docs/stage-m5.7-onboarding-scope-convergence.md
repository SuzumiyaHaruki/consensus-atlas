# M5.7 接入与 PSS 目标收敛

日期：2026-08-07

状态：设计冻结；M5.7a 最小实现已完成

## 结论

ConsensusAtlas 不再把“每个目标直接实现完整 `control.Adapter`”作为薄接入的定义，也不再允许每个
协议把任意私有结构直接作为 PSS 状态。新系统的边际接入面收敛为两个目标专用产物：

```text
Execution Binding  = 原生 lifecycle / input / transport / clock / observation 接口映射
Semantic Mapping   = 原生 observation -> 固定 Core PSS IR
```

完整 Adapter 生命周期、稳定 ID、Yield/Collect、digest、审计、能力推导和通用 conformance 应由共享
代码提供，但共享代码只能从两个真实消费者的重复职责中提取，不能预先搭空框架。Runtime、Action、
Replay、PSS ledger、Coverage 和 Agent 不得因新协议修改。

## 为什么收敛

M5.1—M5.6d 已分别证明了确定性 Runtime、两个 Raft 实现、进程黑盒、Gateway 和分级能力，但第三个
非 Raft 目标暴露出两个具体问题：

1. `control.Adapter` 是严谨的 Runtime 边界，却包含不应由每个目标重复手写的生命周期样板；
2. 通用 `protocolstate.Projector` 返回 `state any`，现有实现仍让具体 Family 决定整个状态形状，
   因而从 Raft 换到 EPaxos 会重做 PSS，而不是只增加映射。

这两个问题足以支持一次最小公共收敛；它们不授权增加新的 Action、Agent、义务语言或 Runtime backend。

## 固定的最小控制面

基础接入只要求映射当前目标实际支持的以下环境语义：

- opaque `Submit`；
- `Crash` / `Restart`；
- pending message 的 `Deliver` / `Drop` / `Duplicate`；
- `Partition` / `Heal`；
- 最早 temporal item 的自然触发；
- 只读 observation。

高级 effect/callback/durability 能力继续存在，但不是首版接入的必选项。缺少原生入口时机械记录
`Unsupported`，不能用协议专用 Action 或 Runtime 分支补洞。

## 共享 Adapter kit 的边界

规划中的 Adapter kit 只是具体 Adapter 内部的复用库，不是第二套 Runtime backend。只有两个真实目标
已经需要同一职责时，才允许把该职责提升到 kit。它可以负责：

- 实现 Manifest、Check、Submit、RunUntilYield、Collect 的公共状态机样板；
- 生成稳定 Action/Item/Yield identity 和 canonical digest；
- 维护冻结 emission、幂等 Collect 和结构化 finding；
- 组装能力声明并调用外部 conformance；
- 将目标绑定的消息、时间和观察输出翻译成既有 `control` 类型。

在 EPaxos 成为第二消费者前，M5.7a 只做现有 Adapter 重复职责清单，不新建空的 kit 包。目标专用
Binding 只负责调用官方接口和编码转换。它不得复制 scheduler、replay、Oracle、PSS ledger、
Coverage matcher 或协议算法。若目标无法形成可靠 yield 或稳定消息 ID，kit 必须降级其能力，而不是
模拟成功。

## Core PSS IR v1

PSS 的通用状态由两部分组合，而不是 Raft/Paxos 字段并集：

```text
Core PSS State = Runtime Control Context + Consensus Semantic Graph
```

Control Context 直接由可信 Runtime 投影，不由协议 Adapter/Mapping 提供：

- participant 的 running/stopped 与相对 incarnation；
- link 的 connected/partitioned 关系；
- pending message/temporal/effect 的种类、路由和保守因果形状；
- 最早 temporal frontier 的相对关系。

它不包含随机 Action/Item ID、绝对时间、原始 payload 或 host microstep。这样 Crash、Partition 和 pending
message 等根本场景不会从 PSS 消失，也不会要求每个协议重复映射环境状态。

Semantic Graph 使用一个小型、封闭词汇：

### 语义实体

- `Participant`：节点、副本或验证者；
- `Epoch`：term、ballot、view 或 round 的相对时期；
- `DecisionUnit`：log entry、EPaxos instance 或 block；
- `Value`：命令或提案的规范化身份；
- `Evidence`：vote、accept reply、QC 或其他支持证据。

### 关系

- `belongs-to`、`proposes`、`supports`；
- `depends-on`、`conflicts-with`、`precedes`；
- `decides`、`persists`、`applies`。

### 通用阶段

- `unknown`、`proposed`、`supported`、`accepted`、`decided`、`applied`。

Participant 使用封闭的 `inactive`、`passive`、`contending`、`coordinating` mode；它只抽象执行姿态，
不把某个协议的 role 枚举写进 Core。

Core key 组合规范化 Control Context 与上述版本化语义事实，并保守执行节点、epoch、value 和对象 ID
重命名。Raft log conflict、
EPaxos fast/slow path、HotStuff lock/QC shape 等细节属于可选 Extended PSS。Extended PSS 可以提高单协议
测试深度，但不能修改 Core schema、通用发现账本或跨协议基础指标。

PSS 发现曲线仍没有完备分母；固定覆盖百分比仍来自冻结 obligation。Core/Extended PSS 必须分别报告，
不能把某协议不存在的扩展字段算作未覆盖。

## EPaxos 的角色

`efficient/epaxos` 暂定为第三目标候选，因为其无稳定 leader、以 `(replica, instance)` 标识决策单元，
并使用 seq/dependencies，能有效否证 Raft 假设。它首先是抽象验收对象，不是为了增加一个实验数字。

EPaxos feasibility spike 只能新增：

- 目标构建描述；
- EPaxos Execution Binding；
- EPaxos Semantic Mapping；
- 目标专用 contract/conformance fixture 和能力工件。

若需要新增协议 Action、Runtime type switch、EPaxos 专用 Core PSS 字段，或复制协议状态机，立即停止。
上游缺少持久恢复、可注入时钟或稳定 yield 时如实标记 Unsupported。

## 验收指标

第三目标的主要验收不是“qualified 百分比”，而是边际接入差异：

- Runtime/Action/Core PSS schema 变更数必须为零；
- 目标专用代码只包含 Binding、Mapping、fixture 和 composition；
- 共享 kit 的新增必须同时被 etcd/raft 和第三目标消费；
- 同一个 Core PSS ledger 无协议分支地处理两类映射；
- 每项能力均由 fresh 外部 witness 推导；
- 缺失能力保持 Unsupported，不为通过率扩展接口。

“薄”按职责和依赖方向判断，不用任意 LOC 阈值代替审查；同时报告目标专用人工 LOC、配置量、首次
可执行时间、conformance 修复轮数和映射错误数。

## 没有声称

- 没有声称一个 Core PSS 能完整表达所有协议安全语义；
- 没有声称 Agent 可以零知识推断 commit、quorum 或恢复语义；
- 没有声称 EPaxos 已接入或可持久化重启；
- 没有改变隐藏缺陷评价、Coverage 分母或 Oracle 的可信边界；
- 没有授权删除 v1 PSS/Coverage/Agent/benchmark。

## 下一最小实现

M5.7a 只实现 Core PSS IR + etcd/raft 映射，并审计两个现有 Adapter 的重复职责。没有两个真实消费者
的 helper 不进入共享包。Core PSS 行为保持与依赖审计通过后开始 EPaxos feasibility spike，再把
etcd/raft 与 EPaxos 确实共同需要的生命周期样板提升为 kit。

实现结果与明确限制见 [M5.7a 阶段总结](stage-m5.7a-core-pss-etcdraft.md)。
