# M5.17a Action-class Random 强基线

日期：2026-08-08

状态：实现与公开 calibration 完成

## 目标

M5.17a 在 M5.16 的同一条 `ExecuteQualifiedBundle` 路径上增加第一种强非 Agent 基线。它不恢复旧
Campaign/Coverage，也不增加第二套执行器。输入仍是同一个 qualified etcd/raft Adapter、
`single-write-v1` workload、FaultEnvelope、96 scheduler decisions 和完整 work budget；变化只在选择器。

```text
Runtime 当前 enabled Action
          |
          v
按 ActionKind 分组并规范排序
          |
          v
均匀选择 action class -> 在该 class 内均匀选择成员
          |
          v
唯一 Runtime Select -> Trace -> Replay -> ExecutionBundle
```

## 选择算法

策略版本为 `consensus-atlas/action-class-random-policy/v1`。随机值使用独立 domain、SHA-256 rejection
sampling、decision ordinal 和规范化 frontier；不会因为 Go map 顺序或 enabled Action 输入顺序改变。
原 M5.11 `consensus-atlas/random-policy/v1` 的均匀 Action 算法和冻结身份保持不变。

按 class 采样解决了“消息数量多就天然压过 crash/restart/timer”的 multiplicity bias，但不声称它是
最优搜索。workload 的 `Invoke` 是 digest-bound hard priority：当 Runtime 真正提供该 Action 时必须先选；
选择器仍不能构造或修改 ActionID。

## FaultEnvelope 约束

第一次不带选择约束的真实运行在 96 和 256 decisions 内都没有提供 workload：随机选择持续使用
crash/drop 干扰选举。这里没有筛选一个容易成功的 seed，也没有强制 timeout。最终实现让 action-class
选择器只在 Runtime 当前 enabled 集中、且执行后不会超过冻结 FaultEnvelope 的子集中采样：

- Runtime 的 enabled 集、状态和 ActionID 不被修改；
- workload/fault provider 没有获得选择权；
- fixed/Agent 路径选到超限动作时仍明确失败；
- envelope、priority 和完整 config digest 都进入 bundle/benchmark 身份。

因此 `enabled` 仍表示 Runtime 层机械可执行；这里增加的是本策略的 `selectable` 视图，不是把预算逻辑
塞进协议 Adapter。

## 官方 control 实测

冻结 seed 为 1，只运行一次，不做 seed 筛选：

| 指标 | 结果 |
|---|---:|
| scheduler decisions | 96 |
| primary / replay work | 98 / 98 |
| Core PSS unique states | 83 |
| Core PSS prefix area | 3681 |
| workload | 1 planned / 1 offered / 1 committed |
| crash / restart | 1 / 1 |
| drop / duplicate | 2 / 1 |
| deliver / temporal / effect | 17 / 27 / 46 |
| TraceIntegrity / Agreement | pass / pass |

report digest 为 `fc0cb500...72260a`，bundle digest 为 `b766e3f1...6c9b1`，trace digest 为
`18abe048...9fa1`。完整 report/bundle 是可再生本地证据，保存在 ignored `artifacts/`。

## 同一公开 candidate 的负结果

M5.17a 用与 M5.16 相同的唯一 source transform 再构建独立 candidate，并在 benchmark manifest 中分别
绑定 control/candidate 的完整 config digest，防止实验用不同策略、workload 或 envelope 冒充同一比较。

| policy | Core PSS states | candidate 结果 |
|---|---:|---|
| M5.16 fixed workload | 56 | `killed` at step 55 |
| M5.17a action-class random, seed 1 | 83 | `survived` |

M5.17a evaluator 的结果为 1 control-pass、0 false positive、1 surviving calibration candidate、0 killed
root cause、0 invalid。两侧都是 96 decisions、98 primary/replay work，且 TraceIntegrity/Agreement 重算
通过。DecisionHistory 在结束时只记录 n1 到 position 5，n2/n3 到 position 4；node 3 没有应用会触发
公开变换差异的 position 5，所以 Agreement 没有可比较的冲突值。

这不是失败工件。它说明：发现更多粗粒度 PSS 状态，不保证走过某个安全差异所需的消息/应用时序；
PSS discovery 适合做反馈和探索效率指标，不能单独作为测试质量结论。也不能反向用这一个公开 candidate
证明 fixed 一般优于 random。

## 可信边界与当前没有证明

- policy 只能从 Runtime 提供的稳定 ID 中选择，不能写 Trace、Evidence、PSS 或 Oracle；
- manifest 绑定 build identity、config digest、projector 和完整预算；
- calibration 是公开构造，不是非公开 holdout；
- 单 seed、单 workload、单 root cause 不能支持方法排序；
- 83 个 Core PSS 状态不是覆盖百分比，更不是正确性证明；
- 本阶段没有调用 LLM。

## 工件与复现

小型可信工件位于
`benchmarks/pilots/etcdraft-v2-action-class-random-m5.17a/`。完整 bundle 和二进制位于 ignored
`artifacts/`。

```bash
make experiment-etcdraft-v2-action-class-random
make experiment-etcdraft-v2-action-class-calibration
make evaluate-etcdraft-v2-action-class-calibration
```

阶段回归通过：`make test`、`go vet ./...`、`go test -race ./...`、全部 checked-in JSON 语法、M5.16/M5.17a
build input/audit 实例校验、本地 Markdown 链接和 `git diff --check`。本机 `jsonschema` 缺少本地
2020-12 metaschema，实例检查使用其兼容 validator 并产生 deprecation warning；实例本身通过。

校验同时修正了一个 M5.16 已存在的 schema 偏差：历史 v1 build schema 现在允许可选的可信
`identity_variable`，且 linker variable 正则与 Go validator 一致；不含该字段的旧 v1 输入继续有效。

## 下一步

M5.17b 实现 trace mutation baseline：只改变已有合法 trace 的选择序列，继续复用同一 admission、
FaultEnvelope、work budget、Replay 和 ExecutionBundle。它必须先定义可重放的 mutation operator 和无效
轨迹记账规则，不能恢复旧 Campaign，也不能把 mutation 后的不可执行动作静默替换。
