# M4.14 阶段总结：Blind Trial 的可信 Replay Bundle

日期：2026-08-06
状态：运行/复验链接完成；正式 sample 尚未创建

## 结论

Blind Planner 的公开 transcript 不能包含真实 obligation ID，因此不能直接交给 evaluator 重放；
普通 `cmd/campaign` 也不能重现 Agent Campaign 的 Coordinator identity。本阶段新增仅限可信侧的
`TrustedReplayBundle` 和 `cmd/blind-replay`，把 Agent 运行、提交和 evaluator 的 deterministic
replay 连成一条链，而没有扩大 Planner 可见信息。

## 已完成

1. `CoordinateBlind` 只在成功通过 opaque-ref 解析、DSL/预算校验后，私有保存真实 `testplan.Plan`。
   `BlindReport` JSON 仍不序列化这些计划或 Trusted Campaign。
2. `TrustedReplayBundle` 绑定 Config、opaque scope、Profile digest、capability-projection digest 和
   已接受的真实计划；它只能由可信 `BlindReport` 导出。
3. `ReplayTrusted` 使用相同 `campaign.ID` 与 Coordinator digest 重建 Session，不调用 Planner，
   并逐字节复现 fixture 的私有 Campaign report。
4. `cmd/agent-campaign` 可选写入一对私有文件：`-trusted-campaign-out` 与
   `-trusted-replay-out`。两者必须同时出现，防止只留下无法重放的 report。
5. 新增 `cmd/blind-replay`，接口兼容 evaluator 的 `-profile/-plans/-out/-artifact` 调用约定。
   它要求 artifact 精确为 `trial://<opaque trial id>`。

## Curator 使用方式

为一个已构建的 candidate/control 运行 agent-campaign binary 时，保存公开 transcript 与私有
replay bundle：

```text
candidate-specific agent-campaign
  -> public blind transcript (exposure-audit input)
  -> private Campaign report + TrustedReplayBundle
  -> candidate-specific blind-replay binary
  -> trusted evaluator rerun
```

candidate 的受控 Build Spec 必须构建 `./cmd/blind-replay`，而 submission 的 `plans` 指向私有
bundle。它不会改变 `defect-eval` 的 artifact contract：evaluator 仍通过统一 flags 重跑 binary，
比较 Campaign report digest，再执行私有 Oracle。

## 尚未证明

- 没有 candidate/control、private Manifest 或实际模型调用；
- 没有证明不同 candidate build 的 Planner feedback 不泄露缺陷语义；这仍需每 trial 的 exposure
  audit 与正式实验设计处理；
- 没有证明 Blind Planner 优于任何 baseline。

## 下一步

在私有 curator workspace 用一个非公开 candidate/control pair 演练“build Agent binary → 生成
private bundle → build replay binary → exposure audit → evaluator rerun”，确认真实构建链与本阶段
fixture 一致；随后再扩充为 3–5 个独立样本。
