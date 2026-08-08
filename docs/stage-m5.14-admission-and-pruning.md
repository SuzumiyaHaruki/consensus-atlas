# M5.14：审计后减负与 Experiment admission

日期：2026-08-08

## 结论

本阶段接受审计报告中最关键的路线修正，但不重写 Control Runtime：M5.13 的绝对步号 Planner 只保留
为 transport smoke，原定一次同形 repair 退出主线。系统先建立 `Experiment requirements -> trusted
QualificationReport` 的机械准入，再补 workload、Oracle/DefectBench、强 baseline 和 Guarded
TestIntent。

## 为什么先做 admission

统一 Action 表示“动作语义相同”，不表示所有 Adapter 具有相同的确定性保证。HashiCorp Raft 已验证
message/lifecycle/invoke 控制，却没有 strict yield、受控 SUT entropy 或 strict replay。如果 Experiment
只看到统一 Adapter 接口，就可能把“可以探索性运行”误写成“可以进入严格方法比较”。

本阶段采用一套 Runtime、两个执行入口：

```text
legacy measurement config -- ExecuteLegacy ----> frozen M5.10-M5.13 behavior

qualified config
  + ExecutionAdmission
  + exact QualificationReport -- ExecuteQualified --> same internal executor
```

两个入口最终调用同一个 `execute`、`executeRun` 和 Replay，不存在第二套调度器。带 admission 的配置
不能退回显式兼容入口 `ExecuteLegacy`；`ExecuteQualified` 又要求 admission、Qualification identity 和实际 Adapter
Manifest 全部一致。

## Admission 模型

`ExecutionRequirements` 只包含 capability ID 集。`BindExecutionAdmission`：

1. 验证 QualificationReport schema 与 digest；
2. 拒绝空、重复或未知 capability；
3. 要求本实验所需的每个 capability 状态都是 `validated`；
4. 绑定 Profile、Qualification、Adapter、Implementation、Build、Configuration 和 Manifest identity；
5. 对规范化 required capability 集和全部 identity 计算 admission digest。

这里故意不要求 `QualificationReport.Qualified=true`。一个 Adapter 可以在完整 portable profile 下为
partial qualification，但仍可进入只使用其 validated message/lifecycle/invoke 子集的 exploratory
实验。相反，strict benchmark 声明 deterministic requirements 后会被机械拒绝。

## 资格路径收敛

- `PortableCFTProfile()` 现在唯一表示当前 `portable-cft-control-v2`；
- v1 Profile 改名为 `LegacyPortableCFTProfileV1()`；
- etcd/raft 的生产 `qualification.Run()` 使用 v2 Profile、core、natural lifecycle、released-message
  lifecycle、opaque invoke replay 和 opaque invoke accepted 五份报告；
- `RunLegacyV1()` 只复验 M5.3 冻结 bundle；
- 默认 Make target 把当前资格写入 ignored `artifacts/`，显式 legacy target 才复验历史路径。

机械结果：

| 项目 | 结果 |
|---|---|
| 当前 bundle digest | `a786035f4d2d94c06b81c62622871a8ee87c6746ae7f3e58a176c80a7c829fa3` |
| 当前 Qualification digest | `94630ed7766b0ffb13a448084ebf9aba0ba305d730ab403a132b29a55766a08f` |
| 当前 etcd required | 8/8 validated，qualified=true |
| HashiCorp strict requirements | `EXPERIMENT_ADMISSION_CAPABILITY_NOT_VALIDATED` |
| M5.3 legacy bundle | fresh 文件与 checked-in 文件逐字节一致 |

## 减负内容

仓库级引用检查确认以下公共符号没有运行状态转换或历史 JSON 使用：

- `NodeStarting`；
- `NodeCrashing`；
- `ItemSelected`；
- `ActionCancelTrial`。

它们已从 v2alpha1 模型和 PSS validator 删除。`ItemCanceled` 被 crash/restart 真实使用，保留。

`DropMessage` 在 `adapterDirected()` 中始终进入 Adapter command 路径，以便同步 Transport 先收到
acknowledgement；因此 `executeNative(ActionDropMessage)` 是不可达重复实现，已删除。Drop 的公开语义、
终态和 HashiCorp acknowledgement 均未改变。

ControlSurfaceReport/ControlPathAssessment 因现有冻结工件仍需复验而没有立即删除，但停止进入新的生产
依赖；未来资格消费者只使用 Manifest、外部 Conformance 和 QualificationReport。

## 历史兼容

新增 Config 字段使用 `omitempty`，无 admission 的历史 JSON canonical form 不变。fresh 复验结果：

| 工件 | SHA-256 | 结果 |
|---|---|---|
| M5.3 legacy qualification | `0887a373ed09a85b7860c9e5f5087acf6bcf7a16944053b7b6d91fa667744f2c` | unchanged |
| M5.10 fixed | `bf3c2cb50c8cfa37c8c6d9e90cc0a56575f18e3ad3ab9a3a9d07eec957091247` | unchanged |
| M5.11 random | `4a0ce9222a7cdbe2f0bc6d79ecc158f3029514795be84e7b76e447b3c59c3891` | unchanged |
| M5.12 stub planner | `38bd333fb18af03ba958863cfbab83356f09689325c4361e4568ab8c3db60f52` | unchanged |

M5.13 的 checked-in attempt 未修改并继续由默认测试验证。

## 本阶段证明了什么

- Experiment 可以机械区分“Adapter 能运行某些控制动作”和“满足本实验的严格确定性要求”；
- admission 不能只凭 Manifest 自报，且带 admission 的配置不能在 `ExecuteLegacy` 中被绕过；
- etcd/raft 当前生产资格已从历史 v1 Profile 收敛到 v2；
- v2alpha1 的四个 speculative 公共枚举和一个重复分支可以无行为变化删除；
- 新 admission 没有改变三个既有 Experiment 工件或 M5.3 历史资格工件。

## 本阶段没有证明什么

- 没有证明 etcd/raft、HashiCorp Raft 或 ConsensusAtlas 正确；
- 没有使 HashiCorp 获得 strict replay；
- 没有新增 workload、Oracle、Coverage、Defect kill 或 holdout 结果；
- 没有证明 Agent 优于任何 baseline；
- 没有实现 Guarded TestIntent，也没有调用 LLM；
- 没有删除仍被 28 条 legacy production edge 消费的 v1 包。

## 下一阶段

M5.15 只实现一个最小、确定性的 Workload/Fault Provider：

1. 版本化少量 write workload；
2. Provider 调用 `OfferInvoke`，不产生外部 Action 集；
3. Runtime `EnabledActions` 仍是唯一候选；
4. 固定 crash/drop/duplicate/partition 次数和并发上限；
5. setup、offer、primary 和 replay 全部计费；
6. 至少形成一次 proposal -> replication -> commit/apply -> strict replay 轨迹。

完成这一层以后再定义第一个 v2 ExecutionBundle 和 Agreement Oracle。

## 验证

交付前对最终工作树执行：

- `make test`：通过，包括全部 Go 测试、10 项 Python 单测和 28 条 legacy production edge 冻结检查；
- `go vet ./...`：通过；
- `go test -race ./...`：通过；
- admission 正/负例、真实 etcd/raft qualified execution、HashiCorp strict 拒绝和 Manifest
  mismatch 路径：通过；
- fresh M5.3 legacy、M5.10 fixed、M5.11 random 与 M5.12 stub 工件与 checked-in 文件逐字节一致；
- 116 个 JSON 文件语法、24 个 JSON schema 声明和文档本地链接：通过；旧版
  `jsonschema` 对本地缺失的 metaschema 使用 latest-draft 兼容检查并警告；
- 两份总体规划 SHA-256 同为
  `f9dd28da9c9b297cd268f933845c9d0a5680f539dd7e6bb99054808d1d7d1a1d`；
- `git diff --check`：通过。

验证过程没有调用 LLM，也没有读取 key 文件。
