# M5.17c1：Corpus 可信前提

日期：2026-08-08

状态：完成

## 目标

M5.17c1 不实现新的搜索算法，也不修改冻结的 Control Runtime。它只解决 PSS-guided corpus
开始前的四个账本问题：source 不能绑定某一种固定策略；重复出现的 ActionID 必须可消歧；反馈必须由
可信执行证据重算；一次方法运行必须完整计入 source、proposal、失败和 execution 成本。

## 输入、处理、输出

```text
输入
  ordered validated ExecutionBundle sources
  + explicit mutation operator/suffix
  + trusted Core PSS Mapper
                         |
                         v
处理
  source identity -> ordered MutationSourceCorpus
  -> ActionID + occurrence 引用 -> mutation proposal
  -> qualified executor + fresh replay -> ExecutionBundle
  -> 从 Trace/FinalSnapshot/Evidence 重构并重新投影 Core PSS
  -> source/proposal/execution 全部写入 MethodLedger
                         |
                         v
输出
  mutation report + self-contained bundle
  + small PSSFeedback + small MethodLedger
  + source/proposal/execution 的完整 primary/replay 及显式 model/resource 记账状态
```

## 四项实现

### 1. 有序、策略无关的 source corpus

`MutationSourceEntry` 只接受已经通过 `ExecutionBundle.Validate` 的源运行，并绑定 bundle、report、
config、trace、manifest、PSS、policy、qualification 和决策数。`MutationSourceCorpus` 显式保存 source
顺序，顺序参与 digest；重复 ID 或 digest 会被拒绝。通用构造器不再假定源策略必须是无 rule 的固定
priority policy，mutation 使用的 suffix 是 operator 的显式配置。

这表示 source 接口已经支持任意 qualified policy；本阶段的公开见证仍只有一个 fixed-workload source，
没有据此声称多策略 corpus 已经有效。

### 2. occurrence-aware Action 引用

旧 `adjacent-trace-mutation/v1` 用 ActionID 标识 trace 位置，并拒绝重复 ID。这个冻结身份保持不变。
新 v2 同时记录 `(ActionID, occurrence)`，例如同一 ID 第二次出现记为 occurrence 2；前缀和交换 pair
都逐项检查出现序号。这样状态循环导致同一 ActionID 再次 enabled 时，不会被误认为第一次出现。

### 3. 可重算的 PSS feedback

`NewPSSFeedback` 不接受调用方提交的 state key。它先验证 bundle，再从最终 Runtime Snapshot 和每步
transition 反向重构所有 Core PSS 所需的控制状态，随后使用绑定的 target-owned Mapper 和每步 Evidence
重新执行 `psscore.Project`。每个样本必须与 bundle 中的 `CorePSS` 完全一致，样本 digest 也必须一致，
之后才输出小型的 `state key + first step + visits` 列表。

反馈另外绑定 Runtime/config/manifest/qualification/trace seed/initial state 和 Evidence 序列 digest。
反向重构只恢复 Core PSS 实际消费的控制字段；Adapter 私有状态、entropy 和 offered actions 不被伪装成
协议状态。

### 4. 不可漏记失败的 MethodLedger

`MethodLedger` 内嵌小型 ordered corpus 和可选 feedback，并按顺序记录：

- source：必须与 corpus 对应条目的 bundle/report/source identity 完全一致；
- proposal：必须引用已有 source；被拒 proposal 必须保留 failure 与已消耗成本；
- execution：必须引用 completed proposal；执行失败不能伪装成没有尝试；
- feedback：只能引用本 ledger 中 completed execution 的 bundle。

账本机械求和 primary、replay 和 model token；各 record 保留显式资源字段。本阶段没有接入资源采集器，
因此 wall/CPU/RSS 明确记录为 `not-collected`，不能被解释为零。source 构造成本不能只存在于说明文字中，
也不能通过只展示最终成功 mutation 而消失。

## 真实 etcd/raft 见证

公开命令：

```bash
make experiment-etcdraft-v2-corpus-mutation
```

它构造一个 96-decision source，按公开规则选择 source 中第一对相邻 message delivery，生成
occurrence-aware v2 proposal，再通过同一个 qualified executor 和 strict replay 执行 mutation。

| 项目 | 结果 |
|---|---:|
| mutation decisions | 96 |
| Core PSS samples / states | 97 / 56 |
| source primary / replay work | 98 / 98 |
| mutation primary / replay work | 98 / 98 |
| MethodLedger total primary / replay | 196 / 196 |
| report / source bundle / mutation bundle / method JSON | 272,673 / 1,902,046 / 1,902,828 / 15,086 bytes |

冻结见证 digest：

- report：`471d30662a53a9d18011a0eaa3a2b42eb0a3dea3d2a9f5b34d1ecd2808c8a6ab`
- bundle：`0019ec7d3b4f9ad5f24878b497ac0140cf34a73b07a41bd85340ee6ac0a9f303`
- mutation plan：`b3c716e51eff51c5620faa99182f549b565d2e16bcf04ec8ec9483e60c8f2b63`
- source corpus：`1f93dd84588d979c371bafa58195f566b3abe228f6f6fd77185d0ab78d69f600`
- feedback：`9b47d8a9add57b3113a926a353c51ecff772f9a0082e01cb36fa5009bfc8d52a`
- method ledger：`7a993f97fbed00246c1c595784be6d91958da7a38f7d484ae62d700fa7b95a08`

另外，M5.17c0 的真实 completed/pending bundle 分别被重新投影为 43/31 和 2/2 个
samples/states，证明反馈构造不只接受 96-decision mutation 形状。

source 和 mutation 两份完整 bundle 都由同一命令写入 Git 忽略的 `artifacts/`；MethodLedger 只保存
小型 source identity、feedback 和成本，不复制 source Trace body。source 记录的 input 是 config
digest，report/bundle 是输出证据，避免把已生成 bundle 同时伪装成带生成成本的输入。

## 保持不变

- Control Runtime、Action identity、Trace schema、消息所有权和自然时间语义未修改；
- `adjacent-trace-mutation/v1` 与 M5.17b 冻结 digest 未改写；
- PSS 仍只是 coarse feedback，不是等价关系、Oracle 或最终分数；
- proposal 不能提交 enabled/admissible set，execution 仍通过共同 qualification 和 frontier；
- 没有增加 LLM 或在线 Agent 接口。

## 当前没有证明

- 没有实现真正按 PSS debt 选择 source/pair 的 corpus 搜索；
- 没有多 source、多 policy 的效果比较，也没有 uniform-random 对照；
- 没有 candidate/control verdict 或非公开 holdout；
- 没有证明 PSS feedback 能预测根因检出；
- 没有证明 feedback state 可用于 visited-state pruning；
- 没有证明 Agent 优于固定、随机、mutation 或专家方法。

## 验证

- `make test`：通过；包含旧身份回归、真实 etcd/raft corpus 见证和两个源码回流守卫；
- `go vet ./...`：通过；
- `go test -race ./...`：通过；
- 125 个非 artifacts JSON 与 23 个 schema JSON 语法：通过；本阶段没有新增 schema/checked-in JSON；
- Python 历史 Agent 已删除，`unittest discover` 正常发现 0 项；
- 两份总体规划逐字节一致，`git diff --check` 通过；
- 当前可编译范围为 15,781 行 production、6,715 行测试，共 22,496 行；Control Runtime 核心未修改。

## 下一步

M5.17c2 只实现最小 batch PSS-guided corpus 和通过同一 qualification/admissible frontier 的
qualified uniform random。两者使用相同 workload、FaultEnvelope、决策/工作预算和 MethodLedger；
PSS 只影响下一批 proposal 的优先级，不做状态剪枝，也不参与 defect verdict。Guarded TestIntent Agent
仍留到 M5.18b。
