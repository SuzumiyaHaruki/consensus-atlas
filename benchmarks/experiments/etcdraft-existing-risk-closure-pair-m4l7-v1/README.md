# M4l7 fixed-Risk closure pair

本实验在提交 `f7b1a4a1534b4dfc127f644e36fe77217f3abae2` 上运行。两个 arm
使用同一个已经按当前 Target 重新资格审查的 Risk、同一个 etcd/raft root、同一个
DeepSeek 模型、同一个 Scenario prompt version、相同语义材料和相同预算。方法变量是：

```text
public-fixed
target-local
```

Risk input digest 为
`91646150ca0bbddc7613bcd6f94917c641e31911e6704ccb91124e9272506da8`，两边均为
`risk_calls=0`。MethodSpec 的其他运行字段相同，`closure_mode` 和由此派生的 digest
不同。

两个 arm 的模型输入并非逐字节相同：`target-local` view 会暴露
`post_intervention_closure=true`，同一版本的 prompt 也会要求 Agent 在目标干预后停止
预测。这是 target-local 方法的一部分，而不是需要消除的噪声。因此 token 差额只能
归因于完整方法臂，不能全部归因于闭合 selector 本身。

## 结果

| 指标 | public-fixed | target-local |
|---|---:|---:|
| Scenario calls | 3 | 2 |
| Model tokens | 83,202 | 41,133 |
| Scenario decisions | 4 | 17 |
| Qualified trace decisions | 32 | 45 |
| Scenario search work | 130 | 156 |
| Search + qualified primary + Replay | 198 | 250 |
| Risk reached | 否 | 是 |
| Client request committed | 否 | 是，`a9d6-write-1` |
| Fresh Replay | stable | stable |
| Oracle findings | 0 | 0 |

两条最终 Trace 在 Scenario step 29--31 具有完全相同的动作和 item identity：投递
`MsgApp n1→n2`、完成 n2 persistence、丢弃同一个 `MsgAppResp n2→n1`。因此两边
都证明 Scenario Agent 最终找到了同一正确干预，差异出现在干预之后。target-local
在第2次调用完成修订；public-fixed 的第2次 `revise` 因携带不允许的 `branch_id` 未进入
执行，正确 drop 到第3次调用才完成。因此 token 差额也包含一次输出契约错误。

public-fixed 随后直接投递 n3 的复制消息，但计划对后续 response 的 enabled 时机判断
错误；第三次调用仍未修复，最终以 `call-budget-exhausted` 停止，缺少
`alternate-quorum-decision-advanced`。

target-local 在同一 drop 后触发可信 handoff，忽略模型尚未执行的后缀。Target closure
只使用当前 enabled 的 Effect/Deliver Action，执行 14 个闭合 decision，于 Trace step
45 达到 Risk，并提交同一 RequestID。保存的精确 Trace fresh Replay 稳定，property
assessment 为 `property-oracle-clean`。

## 解释边界

这是一组公开 capability 样本，支持以下窄结论：

> 当 Agent 已经找到正确干预时，target-local closure 能把协议因果闭合从模型的预测
> 负担中移出，并在本样本中减少一次模型调用和 42,069 tokens，同时形成 Risk、客户端
> 结果和 Replay 闭环。

它不证明 target-local 方法在总体上优于 public-fixed，也不证明 Agent 优于 Random 或
专家策略。target-local 成功执行了更多 Runtime 工作，已核算执行 work 为 250，对照为
198，所以本轮优势是目标达成和模型成本，而不是总执行成本更低。没有采集 wall time、
CPU 或内存，不能声称总成本降低。

两个 canonical Bundle 已由独立的 `defect-eval -oracle-bundle` 路径重新校验 Bundle、
projection 和 Target registry。两边均检查 `trace-integrity`、`agreement`、
`etcdraft-client-application-binding`、`etcdraft-log-progress`，均无 violation。重算结果
分别保存在 `public-oracle-audit.json` 和 `target-oracle-audit.json`。

结构化数字与原始工件 SHA-256 见 `final-summary.json`。为使 Oracle 重算可复现，两个
Bundle 以确定性 gzip 保存在本目录；provider journal 本轮没有保留，报告不将其列为
可审计证据。
