# 能力分级与灰盒 Control Port 设计

日期：2026-08-07

## 1. 决策

ConsensusAtlas 继续采用统一 `Action`，但不再把“接口统一”解释成“所有目标具有相同控制能力”。
不同实现的差异由三组相互独立的事实表达：

```text
Capability = Control Surface + Control Grade + Deterministic Guarantees
```

默认接入路径从纯黑盒调整为**最小、非侵入式灰盒**：优先使用官方接口或替换外围依赖，不修改协议
状态转换逻辑。纯黑盒仍用于启动、客户端输入、连接级控制和外部观察；白盒只在官方 step API 已存在，
或灰盒边界无法支撑研究目标时使用。

统一 Action 的含义是上层 Planner 不需要知道 `RawNode.Step`、Transport RPC、proxy 或 WAL 的具体
调用方式。它不保证每个 Adapter 都能 qualified 所有 Action，也不允许 Runtime 根据 Adapter 类型分支。

## 2. 三个正交维度

### 2.1 Control Surface

继续复用 M5.5a 已冻结的五个、与协议家族无关的运行表面：

- `external-input`：客户端或管理输入；
- `message`：节点间消息、RPC、连接或字节流；
- `lifecycle`：start、crash、restart 与 incarnation；
- `temporal`：timer、periodic pulse、sleep wakeup；
- `durability`：write、sync、apply 等 effect 边界。

Surface 只表示该类行为存在，不能产生控制资格。entropy、yield/evidence 和 process isolation 继续作为
测试框架保证，不重复包装成协议运行表面。

### 2.2 Control Grade

每个 surface 独立分级，顺序由弱到强：

| Grade | 机械含义 |
|---|---|
| `unavailable` | surface 不存在或没有可用控制入口 |
| `opaque` | 只能通过不透明入口触发或等待，无法检查内部控制对象 |
| `observable` | 只能观察结果，不能阻止或安排发生 |
| `interceptable` | 可以截获、阻断或延迟，但控制器不拥有完整可选集合 |
| `scheduler-actuated` | Runtime Action 可以触发外部机制，选择前可检查资格，但外部系统仍可能观察到顺序副作用 |
| `scheduler-owned` | 项目在终态前持有稳定 Item，Runtime 枚举其机械 enabled Action 并决定投递/完成顺序 |

Grade 不能由 Manifest 自行声明获得。Qualification 必须把声明与独立 conformance witness 相交；缺失
witness 时保持 `unsupported` 或较低等级。

### 2.3 Deterministic Guarantees

控制等级不隐含确定性。以下保证逐项独立记录：

- 现有保证：`pure-enabled-set`、`audited-entropy`、`stable-yield-evidence`、
  `strict-decision-replay`、`process-isolation`；
- M5.6d 待机械化的控制细节：`stable-item-id`、`atomic-controller-state`、
  `atomic-external-effect`。

例如 Gateway 可以在 `message` surface 上以 connection 粒度取得 `scheduler-actuated` 和
`atomic-controller-state`，但没有
`stable-item-id`、`atomic-external-effect` 或 `strict-replay`。etcd/raft Runtime mailbox 可以对单条
peer message 取得 stable ID、scheduler ownership 和 strict replay。两者使用同一 `Partition Action`
不意味着证据强度相同。

## 3. 接入等级

### Level 1：Process Blackbox

允许使用：

- 显式 argv/workdir/data directory 启动；
- readiness endpoint；
- opaque client invoke；
- process crash/restart；
- connection gateway 和外部 observation。

该等级不能声称单消息控制、自然虚拟时间、精确持久化切点或 strict replay。

### Level 2：Instrumented Graybox（默认目标）

通过官方依赖注入点或外围替换暴露 Transport、Clock、Storage、Entropy 和 Evidence 边界。协议核心仍是
官方实现，candidate/control 使用完全相同的接入层。

### Level 3：Step-driven Embedded

目标实现提供 step/run-until-yield API，或本身是可嵌入状态机。Adapter 可以冻结全部 ProducedItem，
Runtime 拥有消息、timer 和 effect 的终态调度。只有通过 strict replay witness 后才取得对应保证。

Level 是接入形态，不是单一质量分数。一个进程式实现也可以通过可替换 Transport 获得某个 surface 的
Level-2 控制；一个嵌入式库若内部仍使用未受控墙钟，也不能自动取得 temporal strict replay。

## 4. Control Port

Control Port 是**目标专用薄 Adapter 内部**的可选构件，不是 Runtime 的第二套 backend，也不应一次性
膨胀为新的巨大公共接口：

```text
Control Runtime
      |
single control.Adapter
      |
thin target Adapter
      +-- MessagePort  -> official Transport / RPC hook
      +-- TemporalPort -> injected Clock / timer queue
      +-- StoragePort  -> WAL / fsync / apply wrapper
      +-- EntropyPort  -> injected random source
      +-- EvidencePort -> implementation observation mapper
```

概念职责如下：

- `MessagePort`：冻结新产生的 peer item，并按稳定 ID 完成 deliver/drop；
- `TemporalPort`：只暴露最早 deadline 集合，并按 `TemporalID` 自然触发；
- `StoragePort`：冻结允许完成或失败的 effect，不解释协议日志语义；
- `EntropyPort`：按 node/incarnation/domain 请求并记录随机输入；
- `EvidencePort`：输出版本化 implementation evidence，不直接写 PSS/Coverage/Oracle 结论。

在第二个真实目标证明需要之前，这些名称只冻结职责，不进入通用 Runtime 接口。目标 Adapter 可以先只
实现 MessagePort 和 TemporalPort；缺失端口必须显式 Unsupported。

## 5. 注入优先级与可信约束

接入按以下顺序选择最小权限方案：

1. 官方公开的 step/transport/clock/storage API；
2. 替换官方支持的 Transport、Clock、Storage 或 random dependency；
3. build tag、link wrapper 或其他构建期外围注入；
4. 研究专用 instrumented build；
5. 只有前四项都不足且收益明确时，才修改协议核心。

灰盒接入必须满足：

- 不修改协议状态转换、quorum、commit、lock 或投票判断；
- candidate 和 control 使用同一 Adapter 与 injection patch；
- 官方源码、注入差异、工具链和二进制都有 digest identity；
- 接入前后运行正常路径差分 witness；
- 每个能力由 Adapter 外部的 conformance case 验证；
- 无法证明语义保持的能力标为 experimental，不能进入正式 Qualification。

## 6. 测试与比较规则

跨实现报告必须同时给出：

1. **共同能力结果**：只使用所有比较目标共同 validated 的 surface/grade/guarantee；
2. **最大能力结果**：每个目标使用自身全部 validated 能力；
3. **Unsupported 明细**：不从分母或能力矩阵中删除缺口。

共同能力结果用于公平比较；最大能力结果说明系统对该目标实际能测试多深。两者都不能替代隐藏缺陷、
正确 control 误报和完整成本组成的外部方法评价。

## 7. Agent 边界

Agent 只能读取冻结后的 capability projection、当前 enabled `ActionRef`、规范化 Observation 和
Coverage Debt。它可以建议使用哪个 Action、生成 Adapter/Control Port 草案和 conformance witness，
但不能：

- 调用未 qualified 的目标私有入口；
- 自行把 observable 提升为 scheduler-owned；
- 生成任意 wall-clock 时间、随机结果或未冻结消息引用；
- 写 Qualification、Evidence Ledger、Coverage Ledger 或 Oracle 结论。

## 8. 后续阶段

### M5.6d：能力语义机械化

- 保留五个既有 surface，在既有 grade 序列中加入 `scheduler-actuated`，并冻结必要 guarantee；
- 将当前 etcd/raft、HashiCorp Raft、Gateway 投影为机械报告；
- 添加“Manifest 不能自授资格”和“低等级不能暗含 strict replay”负例；
- 不增加新 Action，不扩张 Gateway，生产 Go 目标净增不超过 100 行。

### M5.7：最小灰盒端口模板

- 只冻结 MessagePort/TemporalPort 的最小职责和 conformance fixture；
- 保持唯一 `control.Adapter`；
- 第二真实目标证明需要前，不加入 Storage/Entropy/Evidence 通用接口。

### M5.8：真实进程式共识纵向切片

选择具有可替换 Transport、可定位 timer、三节点进程部署和可观察提交结果的真实实现。第一轮仅验证：

```text
start three nodes
-> natural leader election
-> freeze one real peer item
-> allow another item
-> fire only the earliest timer
-> deliver/drop frozen item
-> observe commit result
-> replay the same Action trace
```

若必须修改 Runtime 或新增协议专用 Action 才能完成，停止并重新审查抽象，而不是继续增加适配代码。

## 9. 停止条件

- M5.6d 不能机械区分 Gateway 与 mailbox 的控制强度时，停止新系统接入；
- M5.7 为端口模板增加超过实际目标所需的接口时，删除未被消费的抽象；
- M5.8 在预设代码预算内无法产生真实 peer item 与自然 timer witness 时，记录 Unsupported 并更换目标；
- 控制层稳定前，PSS/Coverage/Agent/benchmark 的 v2 迁移继续冻结。
