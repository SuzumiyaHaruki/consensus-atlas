# Defect Benchmark 与完整执行成本

正式评价回答：

> 在相同完整预算下，一个方法能否检出事先不可见的独立缺陷根因，同时不在正确 control 上误报？

PSS、Risk milestone 和 Action novelty 是解释变量，不产生 defect credit。

## 输入与隔离

正式输入由 curator 管理，并形成 candidate/control pair：

- control：作为对照的正确或未含目标变化的实现；
- candidate：包含待检验实现变化；
- pair/root-cause mapping、variant、trigger 和私有 build identity 对 Agent 隐藏；
- Agent 只获得 opaque trial、相同知识/源码暴露模式和相同预算；
- online Memory 不包含 Oracle finding、candidate/control 标签或根因。

当前仓库只有公开 calibration，没有可支持方法结论的 private holdout。公开
candidate/control 不能改标成 holdout。

## Finding 判定

一个 candidate 只有同时满足以下条件才计为 killed：

1. contract、方法、Target、projector、build/config identity 匹配；
2. Bundle 来自真实执行且没有超出完整预算；
3. fresh Replay 与保存 Trace 一致；
4. evaluator 从保存证据重新运行 contract 选择的可信 monitor；
5. monitor 产生违例，且同方法的 control 没有相同误报。

Agent 自报、Risk reached、PSS 数量、Coverage、自然语言解释和在线保存的 Oracle
摘要都不能产生 finding。相同根因的多个表现只计一次。control 违例计为
false positive；身份、成本、Replay 或输入不一致计为 invalid。

## 当前评测链

```text
private FormalBenchmarkContract + exposure audit
                    ↓
完整 Agentic Investigation（连续 Episode）
                    ↓
summary + main/candidate Bundles + branch evidence + provider audit
                    ↓
trusted evaluator 交叉校验身份与完整成本
                    ↓
target Oracle registry 重算
                    ↓
killed / survived / false-positive / invalid
```

在线执行、formal fresh evaluator 和 Agentic evaluator 共用 `targetoracles`
registry。未知 monitor、target/projector 不匹配或声明 monitor 未实际执行都会拒绝，
不退化为 Agreement-only 硬编码。

## 完整成本

正式 trial 汇总：

- Scenario frontier reconstruction 和搜索 primary work；
- 所有 main/candidate qualified primary work；
- 搜索 child verification 和最终 Bundle Replay work；
- Agent calls、input/output/total tokens；
- wall time、CPU、内存等环境成本。

多 Episode Investigation 必须整体提交并累计前序失败探索，不能只挑最后成功
Episode。未选择但已真实执行且 Replay 稳定的候选也运行离线 Oracle，其成本进入
同一 trial。模型与 Runtime 预算来自 typed MethodSpec 的真实运行配置。

## 进入 private holdout 前的条件

1. curator 具备至少三类独立根因及正确 controls；
2. 每类 ground-truth trigger 能经过 Action → evidence → Oracle → Replay 闭环；
3. 所有方法使用相同公开知识、源码暴露和总预算；
4. 方法和数据在运行前冻结，private 标签不进入 Agent；
5. 先用公开 calibration 验证评测 plumbing，但不据此宣称方法有效。

不因为缺少 holdout 数据而新增第二套 ledger、预算 DSL 或历史 Campaign runner。
