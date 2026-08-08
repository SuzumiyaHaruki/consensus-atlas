# M5.17c2：Batch PSS Guidance 与 Qualified Uniform Baseline

日期：2026-08-08

状态：完成，保留公开负结果

## 目标

本阶段实现第一个真正消费可信 PSS feedback 的方法，同时补齐经过当前 Qualification、WorkloadRouter、
FaultEnvelope、共同 admissible frontier 和 strict replay 的 uniform-random 对照。它仍然不是 Agent，
也不引入 Coverage 分数、状态剪枝或新的 Runtime backend。

## 输入、处理、输出

```text
输入
  qualified Adapter + single-write workload + frozen FaultEnvelope
  + 96-decision run ceiling + seeds 1/2/3
  + trusted Core PSS Mapper
                         |
                         v
处理（uniform）
  canonical admissible Actions -> uniform sample -> 3 complete attempts

处理（PSS-guided）
  seeds 1/2 source bundles -> trusted feedback batch
  -> source-exclusive states / global visits ranking
  -> freeze one occurrence-aware adjacent-message mutation
  -> execute once; failure is terminal and charged
                         |
                         v
输出
  content-addressed local reports/bundles
  + small MethodObservation
  + MethodLedger + trusted PSS measurement
  + explicit completed/rejected outcome and exact work
```

## Qualified admissible-uniform

新增策略身份 `admissible-uniform-random-policy/v1`。它只读取 Experiment 已经生成的共同 admissible
frontier，先按 ActionID 规范化，再用 domain-separated seed 均匀选择。这样输入 slice 顺序不会改变
选择。唯一 hard priority 是 Experiment 刚准备的 `Invoke`；这不是搜索偏好，而是现有 workload
preparation 约束。旧 `uniform-random-policy/v1` 的身份和行为没有改写。

三个 run 使用 seeds 1/2/3、相同 runtime seed、single-write workload、FaultEnvelope、Qualification 和
96-decision 上限。每个 run 都完成 96 decisions 和 strict replay；seed 2 的 workload 合法保持
1 pending，seed 1/3 committed。三条运行都消费 98 primary / 98 replay work。

## Batch PSS-guided 规则

`PSSGuidedMutationChoice` 只在 source batch 完成后运行，不在线介入 scheduler：

1. 从两个 source bundle 重新投影 PSSFeedback；
2. 对每个 source 统计只在该 source 出现的状态数；
3. 选择 exclusive states 最多的 source，按 corpus ordinal 打破平局；
4. 枚举该 batch 中相邻的 `deliver-message` pair；
5. 在选中 source 内选择 pre-state 全局 visits 最少的 pair，再按最早 decision 打破平局；
6. 在执行前冻结 source、feedback、candidate-set、state、decision、occurrence refs、suffix 和 digest。

该规则不会查看 mutation 的执行结果，不会试多个 pair 后保留成功者，也不会修改 enabled/admissible
集合。`ValidateInputs` 必须从 source bundles 重算整个 candidate set 和排序；只校验自报 JSON 不构成
可信验证。

## MethodObservation

新增的小型方法工件绑定：

- 明确的 3 execution attempts、294 primary、294 replay ceiling；
- MethodLedger 中的 source/proposal/execution/失败成本；
- 每个 completed bundle 重新投影的 PSS feedback；
- 每条运行的 samples、states、new states 和 method-level state-set digest；
- 可选 PSS guidance 与 proposal context digest；
- full reports/bundles 的 content-addressed digest。

PSS measurement 没有 completeness denominator，也不能生成 defect verdict。相同 bundle、trace 或
feedback 不能通过改 ID 重复进入 corpus/measurement。资源采集仍明确为 `not-collected`。

## 真实 etcd/raft 结果

公开命令：

```bash
make experiment-etcdraft-v2-uniform-method
make experiment-etcdraft-v2-pss-guided-method
```

两个方法的前两个 source bundle 逐字节相同：

- seed 1：`ba5cda6aa1afcabd1f32efa4690bca76f13dfce8a707fdf43a291f3bc57d561b`
- seed 2：`e8ed79be14678e9fea5d34f3a428149f8d966e6eb26a11bed6f3f0afa5915272`

### Uniform method

| run | states | new states | workload |
|---:|---:|---:|---|
| seed 1 | 82 | 82 | committed |
| seed 2 | 84 | 75 | pending |
| seed 3 | 90 | 81 | committed |

- 291 samples，238 个 method-level unique states；
- 294 primary / 294 replay work；
- 3/3 completed execution bundles；
- MethodObservation：`90fb30c3a789de5f7c52c66e82b410e4558cc21a8a701160f584eca2fbdeff75`。

### PSS-guided method

两个 source 共得到 194 samples、157 unique states。选择器在 4 个候选中选择 source 2：该 source
贡献 75 个 exclusive states；目标 pre-state 的 batch-global visits 为 1；mutation 从 decision 65
交换相邻 delivery。proposal digest 为
`313d66bac594616c237b7b2d85e6afbf91835193d92658e6a7d8a50425a5a5b2`。

在 decision 66，交换后的第二个精确 ActionID 已不再 enabled，执行以
`EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED` 结束：

- source：196 primary / 196 replay；
- failed mutation：67 primary / 0 replay；
- method total：263 primary / 196 replay；
- 没有 mutation bundle、mutation PSS feedback 或 defect verdict；
- MethodObservation：`8fd25881a9f1d9315cfbc6650213485be2689c7755cc3bc5e80c935bc386ecf1`。

这个失败被保留为本阶段主要结果。没有改成“不同 target 才交换”、尝试下一个 pair，或把剩余预算
交给 fallback，因为这些规则都是看到结果后的新方法。它们若未来实现，必须使用新身份，并把本次失败
作为公开设计反馈，而不能改写本工件。

## 如何解释结果

不能把 `238 > 157` 解释为 uniform 已经优于 PSS-guided：guided 方法没有完成第三条可测运行，实际只
消耗 263/196，而 uniform 消耗 294/294；两者也没有 candidate/control verdict。当前能够机械支持的
结论只有：

- qualified uniform 路径可以在共同 frontier 下完成三条可重放运行；
- batch PSS feedback 确实决定了 source 和 mutation 位置；
- PSS 稀有度不包含 Action 之间的因果/可交换性，单独使用会提出不可执行 mutation；
- 失败、partial work 和缺失 replay 没有被伪装成一次成功试验。

这正好限制了 PSS 的职责：它是 coarse search feedback，不是 schedule feasibility、状态等价、Oracle
或最终质量分数。

## 工件体积

两个 checked-at-runtime MethodObservation 约 56 KB/份。完整 report/bundle 使用 digest 文件名共享
存放在 ignored `artifacts/.../evidence/`；两个方法共同的 seed 1/2 证据只保存一次。仓库没有新增大型
JSON 或新 schema。

## 当前没有证明

- 没有证明 PSS-guided 优于 uniform、action-class random、trace mutation 或专家方法；
- 没有证明 structural eligibility 能保守判断两个动作可交换；
- 没有 candidate/control、Oracle verdict 或非公开 holdout；
- 没有 MethodSpecDigest、binary/build audit 同方法工件的正式绑定；
- 没有 Agent、模型调用、Coverage denominator 或最终综合分数；
- 没有证明 etcd/raft、ConsensusAtlas 或任何目标实现正确、完备或无缺陷。

## 验证

- `make test`：通过；包含源码回流守卫、旧 c1 identity 回归和 c2 真实方法回归；
- `go vet ./...`：通过；
- `go test -race ./...`：通过；其中真实 `cmd/control-experiment` 包耗时 556.036 秒；
- 125 个非 artifacts JSON 和 23 个 schema JSON 语法：通过；本阶段没有新增 schema 或 checked-in JSON；
- Python 历史 Agent 源码已删除，`unittest discover` 正常发现 0 项；
- 100 个 Markdown 本地链接：通过；两份总体规划逐字节一致，`git diff --check` 通过；
- 当前可编译范围为 16,934 行 production、6,852 行测试，共 23,786 行；Control Runtime 核心未修改。

竞态插桩下的真实方法回归已接近 Go 测试默认 10 分钟上限。M5.18a 应显式区分快速可信检查与完整
真实轨迹/race 回归，但发布前检查仍必须包含后者，不能通过删除真实回归来缩短反馈时间。

## 下一步

进入 M5.18a 的可信评价前提，不继续为得到成功 mutation 而增加启发式。下一阶段补 operation history、
MethodSpecDigest、binary/build identity 与 evaluator fresh rerun；只有正式公开 calibration 和独立
holdout 证明某个结构约束是必要的，才给 PSS-guided 方法增加新版本。Guarded TestIntent Agent 仍留到
M5.18b。
