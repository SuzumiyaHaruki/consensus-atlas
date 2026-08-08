# M5.12 deterministic stub Planner

`attempt.json` 是受限 `PlannerProposal -> Policy -> Execute` 边界的公开确定性见证。它由：

```bash
make experiment-etcdraft-v2-stub-planner
```

生成。Planner scope 由可信 composition root 固定；proposal 只能为已冻结 run 提供 Action priority、
精确 decision rule 或 public random seed。本工件使用无模型 stub，故 model calls/tokens 均为 0。

结果为 1 次 proposal attempt、2×32 primary decisions、2×32 fresh replay decisions、45 个 Core PSS
状态和 1454 prefix area。它复用了 M5.10 的行为策略，目的是验证编译边界而非声称 Agent 带来改进。

- attempt digest：`16f75884e58703bfaad970e06d455573a410600c72a193c25a1a8ef87a3715b9`
- 文件 SHA-256：`38bd333fb18af03ba958863cfbab83356f09689325c4361e4568ab8c3db60f52`
- 文件大小：210,539 bytes

非法 proposal 会得到 `proposal-rejected`；规则在实际 enabled set 中不可达会得到
`execution-failed`。二者都计 1 次 proposal attempt，后者还保存失败前已完成的 setup、Runtime
初始化和 scheduler decision 工作量。相应路径由自动化测试覆盖，但本目录不重复保存失败样例。

这不是 LLM/Agent 实验，不包含 Oracle 或 Coverage 结论，也不能证明策略优于 fixed/random。
