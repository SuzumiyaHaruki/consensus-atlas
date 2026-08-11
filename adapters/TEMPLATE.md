# Control Runtime v2 新目标接入模板

状态：M5.7a 已实现 Core PSS IR 与 etcd/raft Mapping；共享 Adapter kit 仍未创建。第三目标只能先做
feasibility spike，并以同一行为见证证明重复职责后再提取 helper。

这个模板约束新系统的**边际接入面**。Runtime 仍只依赖 `control.Adapter`，但目标接入者不应重复
实现与协议无关的生命周期、identity、digest、replay 和资格样板。

## 1. 新目标只提交两个核心产物

```text
adapters/<implementation>/
  binding.go          # Execution Binding：只调用官方接口和转换 opaque bytes
  semantic_map.go     # Semantic Mapping：只映射到固定 Core PSS IR
  binding_test.go     # 目标 API、正常路径和 Unsupported 见证

qualifications/<implementation>/
  run.go              # composition root：共享 kit + binding + 外部 suite
  run_test.go         # fresh report、digest 和能力断言
```

只有目标目录可以 import 具体实现。文件可按目标实际复杂度合并；不得为了目录对称创建空的 Storage、
Entropy、Evidence 或 Control Port。

## 2. Execution Binding 的职责

Binding 只映射目标实际提供的接口：

| 通用语义 | 目标映射 | 缺失时 |
|---|---|---|
| opaque Submit | 客户端/库输入 API | `Unsupported` |
| Crash/Restart | 进程监督或实例重建 | `Unsupported` 或较低 crash grade |
| produced message | Transport/Step/RPC 发送接缝 | 降为 connection/interceptable |
| Deliver/Drop | 官方接收入口或受控 Transport | `Unsupported` |
| earliest temporal item | Clock/timer/pulse 接缝 | `Unsupported` |
| observation | 稳定只读状态或冻结事件 | 只使用已有外部证据 |

Payload 默认不透明。Binding 不理解 term、ballot、view、quorum、commit 或依赖图，也不选择消息、timer
和故障顺序。高级 persist/sync/apply/effect/callback 只有目标与实验确实需要时才映射。

## 3. 共享 Adapter kit 的职责

规划中的 kit 负责实现现有 `control.Adapter` 的通用样板：

- Manifest/Check/Submit/RunUntilYield/Collect 生命周期；
- Action、Item、Yield、incarnation 和 lineage identity；
- frozen emission、幂等 Collect 和 canonical digest；
- 结构化 finding、capability declaration 和外部 conformance 组装。

kit 不得成为第二套 Runtime backend，不得决定 enabled、时间推进、消息终态、Oracle、PSS key 或资格
结果。无法形成稳定 yield/item 的目标必须降级能力，不能由 kit 伪造确定性。

在出现第二个真实消费者前，不创建空的 kit 包。EPaxos 接入时禁止复制一个新的完整
`control.Adapter`；只把 EPaxos 与至少一个现有 Adapter 确实共同需要的最小生命周期样板提升为 kit。

## 4. Semantic Mapping 的职责

Core PSS IR 由 Runtime Control Context 与协议 Semantic Graph 组合。节点运行状态、分区、pending item
形状和最早 temporal frontier 由 Runtime 直接投影，不属于目标 Mapping。Mapping 只将稳定
implementation evidence 投影成 Semantic Graph：

```text
entities:  Participant | Epoch | DecisionUnit | Value | Evidence
relations: belongs-to | proposes | supports | depends-on | conflicts-with
           | precedes | decides | persists | applies
stages:    unknown | proposed | supported | accepted | decided | applied
modes:     inactive | passive | contending | coordinating
```

它可以读取协议字段，但不能改变 IR schema、通用 canonicalizer、discovery ledger、Coverage 或 Oracle。
协议特有细节只能进入版本化 Extended PSS，并与 Core 指标分开报告。

## 5. Manifest、能力和 Unsupported

1. Manifest 声明候选能力，不能自行产生 validated credit；
2. 所有能力由目标外部的 fresh conformance witness 推导；
3. Unsupported 使用固定 reason code，不用自由文本改变资格逻辑；
4. Build、Implementation、Configuration、Binding 和 Mapping identity 必须绑定实际版本；
5. candidate/control 必须使用同一 Binding、Mapping 和 instrumentation；
6. 共同能力比较只取 validated 交集，最大能力结果单独报告。

## 6. 禁止修改

接入新目标不得修改：

- `internal/control` 的 Action/Item 语义；
- `internal/controlruntime` 的 enabled、消息所有权、时间推进和 replay；
- Core PSS IR schema 和通用状态发现账本；
- 当前冻结 Profile、Coverage denominator 或 Oracle；
- Agent 权限和可信评测边界。

若现有语义无法映射，先提交缺口、最小失败见证和能力降级报告。只有至少两个独立非同家族目标需要
同一新增语义，且现有 Core IR 无法保守表示时，才评审版本升级。

## 7. 边际接入验收

第三目标必须报告：

- Runtime/Action/Core PSS schema 变更数；
- 目标专用人工 LOC 与生成 LOC；
- 配置/语义映射条目数；
- 首次可执行测试时间和 conformance 修复轮数；
- validated/Unsupported 能力及 witness；
- Mapping 差分/突变校准错误数。

“薄”按职责与依赖方向判定，不用单一 LOC 阈值掩盖重复的调度、语义或验证逻辑。
