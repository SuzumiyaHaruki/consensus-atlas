# M4.17 阶段总结：正式 benchmark 机械准入

> 日期：2026-08-06
> 阶段结论：private Manifest v2 在产生 Blind view 前必须通过独立、可复现的 readiness gate

## 已完成

- 新增 `internal/defectbench.PreflightReadiness` 与 `cmd/benchmark-preflight`。
- 默认准入策略要求：Manifest v2、至少三个 distinct root-cause label、至少三个 control、
  全部 historical provenance，以及每个 trial 的 build-audit/binary digest binding。
- 输出 [readiness report schema](../benchmarks/readiness-report-schema-v1.json) 所定义的私有
  报告，只含 benchmark digest、计数和稳定 finding code；不含 variant ID、root-cause label、
  source digest 或 monitor 名称。
- 开发 fixture 即使通过普通 Manifest validation，也会因 v1、样本/来源不足和缺少 artifact
  binding 而不能通过正式 readiness gate。反向测试确认满足这些机械条件的 v2 集合可以通过。

## 可信边界

Readiness gate 是冻结前的必要条件，不是充分条件。它能检查版本、数量、来源声明与工件绑定，
不能证明两个 root-cause label 真正因果独立，也不能判断 Agent 未通过其他渠道见过候选。curator
仍须在私有 workspace 审查来源、时间切分、candidate/control 对应关系和 Planner 可见工件，随后
运行 exposure audit。

## 下一项最小工作

在仓库外准备私有 Manifest v2 的真实三根因/三 control 集合，运行 readiness gate 和 exposure
audit。样本通过后，先在完全相同 scope、Profile、PSS 与完整 primary-work 预算下生成 Random、
DFS、专家与无反馈 Planner submission；仅随后运行 Blind Planner。当前公开 pilot 不可进入该集合。
