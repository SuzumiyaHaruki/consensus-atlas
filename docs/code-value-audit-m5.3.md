# M5.3 代码价值审计与 M5.4 增长门

日期：2026-08-07

## 结论

当前膨胀风险真实存在，但不应通过无依据的大规模删除解决。

M5.1—M5.3 新路径共有约 8,987 行生产 Go 代码，占当前 23,609 行生产 Go 代码的 38.1%。其中通用
控制核心只有 2,629 行；Conformance/Qualification、fixture、迁移和资格组合共 4,343 行。与此同时，
正式 runner、PSS、Coverage、Oracle、Agent 和 benchmark 仍主要运行 v1。这说明当前最需要的是
停止新增基础设施，并让第二个真实实现支付已有抽象的成本。

从本清单生效起：M5.3 是最后一个允许“只增加基础设施、没有新增实现”的阶段。M5.4 必须首先
产生第二实现的真实消息和应用证据；在此之前不增加 Agent、PSS、Coverage、Profile、Schema 或
新的通用 Action/Item。

## 体积基线

以下为工作树中的物理行数，包含注释和空行，只用于控制增长，不代表复杂度或研究贡献：

| 范围 | 生产 Go | 测试 Go | 性质 |
|---|---:|---:|---|
| `internal/control*` | 2,629 | 982 | 通用执行核心 |
| `internal/conformance` | 2,121 | 387 | 外部准入和资格逻辑 |
| `adapters/fixture` | 585 | 39 | 仅供通用测试使用的 fixture |
| `adapters/etcdraftv2` | 2,015 | 1,533 | 第一真实实现 |
| migration model/harness/CLI | 1,451 | 207 | v1 删除前的临时迁移证据 |
| qualification composition/CLI | 186 | 61 | 可再生 M5.3 工件 |
| **M5.1—M5.3 新路径合计** | **8,987** | **3,209** | 约占生产 Go 的 38.1% |
| 全仓库 | 23,609 | 7,980 | 当前工作树 |

文档与工件基线：

- 七份 M5 阶段总结：924 行；
- `control-runtime-v2.md`：778 行；
- 全部 `docs/*.md`：6,232 行；
- 三份 Adapter JSON Schema：259 行；
- M5.2.5/M5.3 两份冻结 JSON：606 行。

## 实际功能边界

已经得到的真实功能：

- 一个协议无关 Runtime，能管理 frozen message、自然 temporal event、effect、crash/restart 和 replay；
- 一个官方 etcd/raft Adapter，能执行三节点消息、proposal、持久化、应用和恢复；
- 三个 v1/v2 外部场景一致，一个自然换主场景 deferred；
- etcd/raft 对当前公共控制 Profile 的 8 项 required capability 通过机械准入。

尚未得到的关键功能：

- 没有第二个真实共识实现；
- 没有证明公共 Runtime 能适配非 `Ready/RawNode` 控制表面；
- PSS、Coverage、Oracle、Campaign 和 Agent 尚未消费 v2；
- 用户主要实验入口仍是 v1；
- 没有新的 Agent 方法效果或外部缺陷检出结论。

因此，M5.3 的 `qualified=true` 是已有控制面的准入结果，不是功能完成度百分比。

## 保留、冻结、合并和删除清单

| 范围 | 决定 | 允许的下一步 | 删除或重新开放条件 |
|---|---|---|---|
| `internal/control` | **保留并冻结 API** | 只修复可重放性或所有权缺陷 | 两个真实 Adapter 给出同一缺失语义前，不增加 Action/Item |
| `internal/controlruntime` | **保留并冻结** | 修 bug；重跑全部 Adapter conformance | M5.4 不得为 HashiCorp 名称或类型增加分支 |
| `internal/controlentropy` | **保留并冻结** | 只处理第二实现可证明的 entropy seam | 无具体非确定性见证不扩展 domain/model |
| `internal/conformance` | **保留但停止扩张** | 复用 Core/Natural/Invoke/Profile | 新实现缺少已冻结 case 时先记 Unsupported；不得先新增 suite |
| `adapters/etcdraftv2` | **保留并冻结功能面** | 回归修复、作为第二实现对照 | 不继续增加 ReadIndex/snapshot/membership 切片 |
| `adapters/fixture` | **保留为测试支撑** | 不新增行为 | 两个真实 Adapter 能替代其全部 Runtime 单测时再评审；移动目录不算减负 |
| `internal/migration`、`migrations/etcdraftv1v2` | **临时保留、禁止扩张** | 只维持冻结 M5.2.5 报告 | v2 Oracle/PSS/Campaign 迁移且 v1 直接消费者归零后整体删除 |
| `cmd/v1v2-compare` | **临时保留** | 只复验 checked report | 与 migration harness 同时删除 |
| `qualifications/etcdraftv2` | **保留并冻结** | 只复验 M5.3 工件 | 第二 Adapter 完成后与其重复组合代码一起合并 |
| `cmd/adapter-qualify-etcdraftv2` | **合并候选** | M5.4 期间不复制通用写文件逻辑 | 第二 Adapter 出现后合成一个 qualification CLI，要求净减行 |
| 三份 Adapter Schema | **冻结** | 只校验现有实例 | 第二实现产生无法表达的真实字段前不升版本、不新增 Schema |
| M5.1—M5.3 阶段总结 | **历史只读** | 只保留导航 | 不删除研究证据，也不继续同步更新历史文本 |
| README/architecture/总体规划 | **停止重复状态扩写** | 只保留入口、冻结决策和一行当前状态 | 详细数字只写 `CURRENT_STAGE` 或当期总结 |
| v1 Oracle/PSS/Coverage/Agent | **迁移，不删除研究逻辑** | 第二 Adapter 后按消费者逐项迁移 | 只有执行外壳的直接消费者归零后删除 v1 control plane |

### 立即删除

当前没有经过依赖证明、可立即删除的大块代码。强行删除 fixture、migration 或 v1 会分别破坏
Runtime 单测、迁移证据或现有可信实验入口。

### 明确的未来删除目标

1. v1 消费者归零后删除 `internal/migration`、`migrations/etcdraftv1v2` 和 `cmd/v1v2-compare`：当前
   目标规模约 1,451 行生产 Go；
2. 第二 Adapter 完成后合并每实现 qualification CLI/composition 的重复写文件、排序和 bundle 代码；
3. M5.4 后审查 `internal/conformance` 的重复 report/factory boilerplate；只有能净减少至少 200 行且
   工件 digest/行为有显式迁移方案时才重构；
4. 不以拆文件、改名、移动目录或生成更多 schema 计为删除成果。

## M5.4 代码预算

| 项目 | 目标 | 硬停止线 |
|---|---:|---:|
| 通用 `internal/control*` 生产代码净增 | 0 行 | 100 行 |
| `internal/conformance` 生产代码净增 | 0 行 | 150 行 |
| HashiCorp Adapter 首个完整纵向切片 | 不超过 1,200 行 | 1,500 行 |
| Adapter-specific qualification/CLI | 不超过 150 行 | 200 行 |
| 新阶段文档 | 不超过 120 行 | 160 行 |
| 新 JSON Schema | 0 份 | 0 份，除非先批准公共模型变更 |
| 新 Agent/PSS/Coverage 代码 | 0 行 | 0 行 |

测试代码不设低上限，以免通过减少测试伪造“精简”；但重复 helper 导致测试代码超过生产代码 1.5
倍时必须先合并 helper。所有行数以本文件基线和 `git diff --numstat` 复核。

## M5.4 小步交付门

### M5.4a：控制表面探针

预算：最多 350 行新增生产 Go。

必须输出：

- 官方未修改 HashiCorp Raft 的固定版本和三节点配置身份；
- custom Transport/FSM/Store 能否形成稳定边界的机械报告；
- 至少一个真实 outbound RPC 被冻结为 Runtime `ItemMessage`，或者一个稳定 Unsupported 原因；
- 不使用 sleep/polling 的结果来声称 deterministic/strict replay。

若 350 行内无法得到真实消息证据，停止该候选并复核实现选择，不继续搭建代理框架。

### M5.4b：最小消息与应用闭环

累计预算：最多 800 行生产 Go。

必须输出同一条真实轨迹中的：opaque invoke、消息捕获、Runtime 选择、deliver/drop，以及
`FSM.Apply` 或等价应用证据。尚不能控制的自然时间、entropy、snapshot 和 membership 全部保持
Unsupported。

### M5.4c：生命周期与资格

累计目标：1,200 行，超过 1,500 行必须停止。

必须增加 crash/restart、持久状态边界和可重复执行；使用同一 `PortableCFTProfile` 生成部分或完整
资格报告。阶段成功要求公共 Runtime 没有实现名称分支，且一份相同的协议无关测试意图能在两个
真实 Adapter 上运行。

## 强制停止条件

出现任一项即停止编码并重新评审：

- 在产生第一条 HashiCorp 真实消息证据前，需要修改公共 Action/Item；
- 需要修改官方实现源码才能建立第一版控制边界；
- 依赖 wall-clock sleep、无界 drain 或 goroutine 调度顺序才能得到“稳定”轨迹；
- 为单一实现新增第二套 Runtime、Trace、Qualification 或 Coverage 模型；
- M5.4a 超过 350 行、M5.4 总生产代码超过 1,500 行仍没有同一测试意图的双实现结果；
- 用 fixture、Manifest 声明或 `qualified` 数字代替第二个真实实现的执行证据；
- 为了让报告好看而把缺失能力从 Profile 删除，而不是标记 Unsupported。

## 后续阶段的最小报告格式

每个小阶段只报告五项：

1. 新增、删除和净增生产代码行数；
2. 新增的真实可执行行为；
3. 两个真实实现共同复用的包和测试；
4. Unsupported/失败及其稳定原因；
5. 下一删除目标。

没有新增真实行为的阶段，生产代码净增必须不大于零。

## 可重复审计命令

```bash
rg --files -g '*.go'
find <scope> -type f -name '*.go' ! -name '*_test.go' -print0 | xargs -0 wc -l
find <scope> -type f -name '*_test.go' -print0 | xargs -0 wc -l
go list -f '{{.ImportPath}}|{{join .Imports ","}}' ./...
git diff --numstat
```

下一次完整审计点：M5.4a 探针结束时；无论成功还是 Unsupported，都先落盘数字再决定是否进入
M5.4b。
