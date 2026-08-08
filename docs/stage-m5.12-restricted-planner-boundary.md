# M5.12 受限 Planner Proposal 编译边界

日期：2026-08-07

## 结论

M5.12 已完成。系统现在有一条可机械复核的：

```text
trusted PlannerScope
        +
untrusted PlannerProposal
        |
        v
strict decode -> mechanical compile -> existing Execute -> existing Replay/PSS Aggregate
        |
        +-> measurement-complete
        +-> proposal-rejected
        `-> execution-failed + partial work ledger
```

无模型 deterministic stub 已端到端生成公开工件。它只证明 Agent 接入前的控制边界可用；它不是
LLM/Agent 方法结果。

## 冻结边界

可信 `PlannerScope` 独占以下字段：experiment/PSS identity、Runtime seed/config、decision budget、
run number/order 和 mandatory replay。`PlannerProposal` 的结构中根本不存在这些字段，只允许：

- 每个冻结 run 的公共 Action priority；
- exact decision number + Action kind + optional node 的 rule；
- 与 Runtime entropy 隔离的 public random-policy seed。

编译器要求 proposal run set 与 scope 完全相等，再按 scope 顺序生成 `Config`。Policy version 和
policy ID 由编译器生成。模型输出入口拒绝 unknown field 与 trailing JSON，因此不能通过携带
`runtime`、`pss_id`、budget 等额外字段修改可信输入。

## 三种机械结果

| 情况 | 状态 | 计费 |
|---|---|---|
| 合法且完成 | `measurement-complete` | 1 proposal attempt + 全部 primary/replay work |
| schema/run set/policy 非法 | `proposal-rejected` | 1 proposal attempt；不伪造 execution work |
| 编译合法但 rule 在当前 enabled set 不可达 | `execution-failed` | 1 proposal attempt + 失败前真实 setup/init/decisions |

运行期失败不生成半成品 `measurement-complete` Report。`ExecutionFailure` 只把既有唯一执行路径的
阶段、稳定 reason code、run/decision 和部分账本带入 `PlannerAttempt`。Replay 仍只有
`controlruntime.Replay` 一套算法；新增 progress view 只报告实际完成的初始化与 decision。

自动化测试中的不可达 proposal 在 run 1 decision 1 请求 restart 尚未 crash 的 n1，得到：

```text
status                  execution-failed
reason                  EXPERIMENT_POLICY_RULE_NOT_ENABLED
phase/run/decision      primary-policy / 1 / 1
proposal attempts       1
primary setup/init      1 / 1
primary decisions       0
primary work units      1
replay setup/decisions  0 / 0
```

非法 run set 在创建 Adapter 前得到 `proposal-rejected/PLANNER_PROPOSAL_RUN_SET_INVALID`，仍计 1 次
proposal attempt。

## Stub 实验结果

工件位于
`benchmarks/experiments/etcdraft-v2-stub-planner-m5.12/attempt.json`：

- planner：`deterministic-stub-planner/v1`；model calls/tokens 全为 0；
- 1 次 proposal attempt；2 个 run，各 32 primary decisions；
- fresh replay 2/2 稳定，各 phase 均为 2 setup + 64 decisions = 66 work units；
- 每 run 23 states，union 45，prefix area 1454；
- proposal digest：`0d339d5ec9a48991313941a85cbe5b3272dca284eb58c4132a82765aae6be332`；
- scope digest：`faaf6f8aa28c9ab927e1dc52f6f1d8ef17f2cf346bfa40ad2301f02a914dd5ba`；
- attempt digest：`16f75884e58703bfaad970e06d455573a410600c72a193c25a1a8ef87a3715b9`；
- 文件 SHA-256：`38bd333fb18af03ba958863cfbab83356f09689325c4361e4568ab8c3db60f52`；
- 文件大小：210,539 bytes。

stub 故意复用 M5.10 的 progress/lifecycle 策略，因此相同 45-state 结果只说明编译后行为保持，
不能声称 Planner 优于 fixed/random。

## 回归与代码体积

动态账本替代了失败时丢弃成本的旧行为，但成功报告结构和数值没有变化。fresh 运行与既有文件逐
字节比较：

- M5.10 fixed SHA-256 仍为
  `bf3c2cb50c8cfa37c8c6d9e90cc0a56575f18e3ad3ab9a3a9d07eec957091247`；
- M5.11 random SHA-256 仍为
  `4a0ce9222a7cdbe2f0bc6d79ecc158f3029514795be84e7b76e447b3c59c3891`。

相对 M5.11，本阶段生产 Go 约净增 579 行、测试约净增 200 行。主要增量是 352 行的编译/严格
decoder/结果 envelope 与自校验、86 行失败传播、47 行 typed selection/动态计费、18 行 replay
progress 和 76 行 composition 接入。
这是明显增量，但没有新增 package、Action、Adapter 接口、执行算法、Replay 算法、PSS 或 Coverage
路径。下一阶段必须复用该边界；如果真实 Planner 需要再复制执行/报告逻辑，应停止而不是继续膨胀。

## 当前未证明

- stub 没有调用 LLM，不证明 Agent 能生成有效或更好的策略；
- 没有 Agent 与 Random/fixed 的多 seed、等预算方法比较；
- PSS state discovery 仍不是完备覆盖率或正确性判定；
- Coverage、Oracle 和正式 holdout 尚未迁移到 v2；
- wall time、CPU、RSS 仍未采集；
- 当前 strict JSON decoder 拒绝 unknown/trailing JSON，但未单独审计重复 JSON key；接真实模型时
  还要冻结 output byte limit、超时、transport audit 和模型 token usage 来源。

## 下一步

进入 M5.13，只实现第一个真实受限 Planner transport：给模型发送冻结 scope 的只读投影和 Proposal
schema，严格解析一次响应，记录 request/response digest、模型标识和供应方 usage，再把 proposal
交给本阶段编译器。先做 1 attempt 的公开 smoke，不做多轮反馈、不读取 Oracle/Coverage 私有结果，
也不据单次 PSS 数值声称方法优势。
