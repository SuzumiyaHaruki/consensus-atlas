# M4.9 阶段总结：etcd/raft ReadIndex 历史回归闭环

日期：2026-08-06
状态：完成
工件目录：`benchmarks/pilots/etcdraft-readindex-v1/`

## 阶段结论

M4.9 完成了第一个基于公开 etcd/raft 历史修复 `63903dd` 的受控回归样本：官方
v3.6.0 candidate 在冻结的延迟 ReadIndex 响应时序中被独立的
`linearizable-read` monitor 检出；只应用该公开修复的 digest-bound control 通过。
可信 evaluator 重新验证构建审计和二进制、在最小环境中重跑两份 Campaign，并从保存 trace
重新计算 monitor；最终为 1/1 root cause killed、0 false positive、0 invalid。

这证明的是一个**历史回归复现与可信评测闭环**，不是 Agent 方法效果，更不是对
etcd/raft 的完备验证。

## 已落地能力

1. 通用 Runtime 的 `query` 输入经 etcd/raft Driver 受限映射到
   `RawNode.ReadIndex`；`Ready.ReadStates` 在 `Advance` 前作为 typed `read-state`
   observation 输出。
2. `families/raft.LinearizableRead` 从 query 前的可信 Driver Snapshot 推导 commit
   下界；它不相信 Plan 自报的 index，也不把没有返回 read state 解释为安全。
3. CapabilitySnapshot 只有在 Driver Manifest 声明 `read-index-input` 和
   `read-state-observation` 后才导出相应事实。机械资格报告现在使
   `etcdraft-63903dd-duplicate-read-index` 为 `qualified`；另外三个公开候选仍为
   `deferred`，其 reason code 完整保留在 `qualification-report.json`。
4. Test Plan DSL 增加受 Runtime 所有权限制的 `capture_message`、`execute_ref` 和
   `execute_optional`。前两者只能唯一捕获并稍后释放已存在的消息；可选执行只允许宿主
   输出操作，不能选择或注入输入/网络消息。Agent policy 与同一白名单同步，但保持
   `direct_message_inject=false` 与 `transient_selectors=false`。
5. `plans/defectbench/etcdraft-63903dd-v1.json` 将 76 个 setup step 和 1 个
   measurement decision 冻结为可重放 Test Plan。它显式保留旧 heartbeat response，在
   n2 提交更高日志 index 后释放该 response。
6. `sutbuild` 同时支持多文件精确替换的 control（audit v3）与未修改 module tree 的
   candidate（audit v4）；私有 manifest 将 source、build audit、binary 和 Driver Manifest
   四种身份绑定，evaluator 再执行一次受限环境重跑。

## 实际结果

| trial | 版本 | 外部结果 | runs / decisions | primary / replay work | Coverage |
|---|---|---|---:|---:|---:|
| `trial-candidate-v3.6.0-v2` | v3.6.0、未修改 module | `killed` | 1 / 1 | 218 / 218 | 26/55，45.67 |
| `trial-fixed-63903dd-v3` | `63903dd` 精确修复 control | `control-pass` | 1 / 1 | 218 / 218 | 26/55，45.67 |

candidate 的可信 finding 是 `linearizable-read` 在 step 201 报告：请求 `B` 的 read state
返回 index 4，低于可信下界 6。Driver 的 local RawNode API 将确认心跳前的同 context retry
视为新的本地 query，因此同一步还会观察到 `A` 的同类表现；这两个表现共享一个
`readindex-stale-lease-response` root cause，只计一次。

两份报告 Coverage 完全相同，故 Coverage 不参与 kill 判定。这个反例正说明本项目必须把
内部 Coverage/PSS 指标与外部 historical/control 结果分开报告。

## 可信性修复

第一次 evaluator 重跑曾拒绝 submitted report：执行 trace、Oracle、成本和整数统计一致，
但 Coverage score 因 Go map 迭代的浮点求和顺序出现最低有效位差异，从而改变 report digest。
`coverage.summarize` 与 Profile weight validation 已按 category 的字典序累计，并新增回归测试。
随后重新构建 candidate/control、重新生成 report 和 private manifest；最终 evaluator rerun
通过。这是可信重跑捕获并促成修复的框架确定性问题，不能被算作 SUT finding。

## 建模边界

- 初始 A 的自然选举仍受当前 Driver timer 边界限制；该时序使用一次合法的 A retry 来取得
  确认 heartbeat，未伪造消息、未改写 SUT 状态。这是对历史行为的受控复现，不等同于逐字节
  复刻上游测试 harness。
- 当前只有一个人工冻结的历史样本和一个修正 control，不能用于比较 Random、DFS、专家或
  Agent 方法，也不能证明 Coverage/PSS 对 holdout 缺陷有预测力。
- PSS/覆盖值是本次 trace 的解释字段，不能表示“etcd/raft 已测试 45.67%”。
- 此阶段没有调用 LLM，也没有读取任何密钥；Agent 只获得后续可提议的受限 DSL 面。

## 阅读顺序

1. `summary.json` 与本文件；
2. `qualification-report.json`、`capability-snapshot.json`；
3. `plans/defectbench/etcdraft-63903dd-v1.json`；
4. `build-input/`、最终的 `build-audit/*-v2.json` 和 `*-v3.json`；
5. `campaign/trial-candidate-v3.6.0-v2.json`、`campaign/trial-fixed-63903dd-v3.json`；
6. `evaluator/blind-manifest.json`、`submission.json`、`report.json`。

## 下一阶段

先冻结 M4.9 的复现实验和验证记录；不要据此增加 Agent 角色或复杂义务。下一项独立工作应是
为 Runtime 设计可验证的虚拟时间/timer queue 边界（参考 MODIST 的思想，但不强制超时），并
用新的 Driver integration fixture 证明其不会改变已冻结 M4.9 trace。只有在至少多个独立
历史样本和 controls 就绪后，才开始比较 Agent 搜索与 Random/DFS。
