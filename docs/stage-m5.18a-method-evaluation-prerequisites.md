# M5.18a：可信方法评价前提

日期：2026-08-08

状态：完成最小单执行公开校准；尚未进入 Agent 或 private holdout

## 目标

M5.17c2 已有方法级 PSS measurement，但没有把方法定义、真实 client operation、构建 binary 和
evaluator-owned fresh execution 放进同一可信链。本阶段补齐这四项，同时保持 Control Runtime、Action、
Trace、PSS 和旧 bundle/method identity 不变。

## 输入、处理、输出

```text
输入
  frozen MethodSpec + public candidate/control manifest
  + exact build audit + exact binary digest
                         |
                         v
处理
  evaluator validates audit/binary
  -> copies verified bytes into isolated temporary executable
  -> executes exact method arguments itself
  -> validates Config projection + Bundle v3 + OperationHistory
  -> fresh TraceIntegrity + applied-prefix Agreement
                         |
                         v
输出
  small checked-in evaluator report
  + ignored full reports/bundles/binaries
  + control-pass / killed / false-positive / invalid ledger
```

## ExecutionBundle v3 与 OperationHistory

旧 `ExecuteQualifiedBundle` 继续生成冻结的 v1/v2。只有显式传入有效 MethodSpec digest 的
`ExecuteQualifiedBundleV3` 才生成 v3；两者复用同一个 `execute`，没有第二套 Runtime 或 scheduler。

OperationHistory 保存完整冻结 WorkloadPlan，以及实际 offered prefix 中每个 operation 的：

- ordinal、request ID、opaque input；
- invoke step 与 target incarnation；
- 可选 return step 和完整 opaque response。

验证器从 Trace 中的 Invoke、Action parameters、ClientHistory 和 WorkloadRunReport 重新构造对应关系。
planned-but-not-invoked 只保留在 Plan；pending invoke 没有伪造 return。修改 input、step、response、数量或
Plan digest 都会被拒绝。

第一次外部 fresh execution 暴露了一个过强校验：持久化后的 `json.RawMessage` 会有等价空白格式，旧检查
却要求字节等于紧凑 JSON。该次运行在评价前被拒绝且没有报告。最终规则改为严格字段解析、禁止未知字段和
尾随 JSON、并校验 payload digest；随后用新 output path 重建两份 binary/audit，未覆盖旧 binary。

## MethodSpec

`consensus-atlas/method-spec/v1` 冻结：

- executor interface、strategy、decisions、policy seed 和 timeout；
- execution attempt 与 primary/replay ceiling；
- PSS、DecisionProjector、Bundle v3、OperationHistory 身份；
- 非 SUT Config projection digest。

Config projection 只移除每个 build 必然不同的 BuildID、Qualification digest 和 Manifest digest；实验 schema、
ID、Runtime seed、FaultEnvelope、WorkloadRouter、Policy、Workload、termination 和 required capabilities 全部
保留。因此 candidate/control 不能通过改变方法配置获得 verdict。

本轮 MethodSpec digest 为
`ee856fbf62ff99a82cfc0c2753d3021e6ca879e6490242074b08ea28d48fdbcf`。

## Build 与 fresh evaluator

control 使用 `sutbuild` v4 unmodified-module audit；candidate 使用既有公开唯一文本转换。manifest v2 对每个
trial 绑定 BuildID、build-audit 文件 digest 和 binary digest。评价器读取并验证 audit 后再次计算 binary
digest，再从已验证 bytes 创建临时可执行副本，避免在校验与执行之间换文件。

评价器不接受 submitted bundle。它自己启动两份 binary，传入由 MethodSpec 机械生成的参数，并读取新生成
的 report/bundle。full evidence 写入 ignored `artifacts/`，checked-in report 只保存 digest、成本和 finding。

## 真实公开 calibration 结果

| trial | BuildID | decisions | primary/replay | operation history | 结果 |
|---|---|---:|---:|---|---|
| official control | `sut-5826353327bce116` | 96 | 98/98 | invoke 28、return 42 | `control-pass` |
| command-data candidate | `sut-c9811ab0ed8e2f39` | 96 | 98/98 | invoke 28、return 42 | `killed` at step 55 |

两边的 Config projection 都是
`8459b8a708f4b64e0718b9750109f709e3eec69d0dfdc76ac4eaa24b2a73e92f`，OperationHistory digest 都是
`778aae5d58351749884098cc1cc90971fca6fc3158efaadd358e5888131c7735`。client-visible 结果相同，candidate
差异由 target-owned applied-prefix projector 投影后，被协议无关 Agreement 在 position 5/step 55 检出。

最终 summary：1 control、0 false positive、1 calibration candidate、1 killed root cause、0 invalid。
evaluation digest 为
`4cac6cd0731bdb9d28b2c1d927d634e3ce0dfdc2ccfac271f55df0b5bd7ef4ee`，连续两次 evaluator-owned execution
得到相同 digest。

## Oracle 取舍

本轮没有为了满足规划文字而增加一个没有证据需求的通用 durability monitor。当前实际差异是 applied-prefix
冲突，已有 target-owned applied-prefix projection + generic Agreement 正好覆盖；OperationHistory 则证明
client history 本身不足以检出该差异。未来只有 calibration/holdout 暴露 durable/applied 前缀缺口时，才
增加独立版本化 monitor。

## 代码与工件成本

M5.17c2 为 16,934 行 production、6,852 行测试；本阶段为 17,824/7,106，共 24,930 行。净增加主要来自
OperationHistory 重建、MethodSpec projection 和 fresh binary evaluator。checked-in 新 pilot 工件合计约
15.1 KB；两份约 1.94 MB bundle、report 和 binary 均被 Git 忽略。

## 验证

- `make test-fast`、`make test`、`go vet ./...`：通过；
- `make test-race-full`：通过，使用显式 20 分钟 package ceiling；真实 `cmd/control-experiment` 包耗时
  302.177 秒；默认 10 分钟 ceiling 的首次运行只因累计耗时超限，未报告数据竞争；
- 132 个非 `artifacts` JSON、23 个 schema JSON、4 个 M5.18a build input/audit schema 实例：通过；
- 105 个 Markdown 本地链接：通过；Python 历史 Agent 已删除，`unittest discover` 正常发现 0 项；
- 两份总体规划逐字节一致，`git diff --check` 通过；旧 v1/v2 与 M5.17c1/c2 identity 回归通过。

## 当前没有证明

- 这是公开 calibration，不是 private holdout；
- 没有比较 Agent、uniform、action-class random、trace mutation 或专家方法；
- 当前 MethodSpec calibration 是单 execution，不是多 attempt 搜索方法 submission；
- 没有证明 operation history 已足以实现任意协议的 linearizability/durability Oracle；
- 没有证明 PSS、Coverage 或某个综合分数能预测缺陷检出；
- 没有证明 etcd/raft、ConsensusAtlas 或任何目标正确、完备或无缺陷。

## 下一步

进入 M5.18b 的最小 Guarded TestIntent 前，先把本阶段边界作为硬约束：Agent 只能提交少量意图，确定性
compiler 将其解析到已 qualified 的 Action/MethodSpec；Agent 不读取 private variant、build transform、
root cause 或 Oracle 私有结果。第一轮仍是单 Agent，并保留 fixed、admissible-uniform、action-class 和
trace mutation 基线。多 attempt Agent 方法必须扩展新的 MethodSpec/ledger 版本，不能把本轮单执行
calibration 冒充为完整方法比较。
