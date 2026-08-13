# A2R 架构收敛与减负

日期：2026-08-13

## 结论

本阶段停止 A2c 和新模型调用，先解决三个已经影响研究推进的问题：每个小阶段重复高成本测试、代码/JSON
持续膨胀、当前主流程被历史实验淹没。处理原则是“删除被替代的路径，保留仍服务当前主线的安全边界”。

## 代码收敛

删除：

- trace mutation、source corpus mutation、PSS-guided batch method 及其 CLI/测试；
- Agent-v1 当前 frontier permutation traversal 与 Stateless Agent Campaign 组合；
- 无生产消费者的 M5.23g compatibility structs、旧 Risk gate artifact tests；
- 未资格化 raft-rs Go/Rust Adapter、worker、probe 和 feasibility 工件；
- 旧 action-class fixture 与历史字节 digest baseline 测试；
- M5.23a–e 重复端到端校准测试和 M5.21k readiness 报告自证；
- 已无现行入口的 M5 构建/评价 Make 目标与 retired-symbol 检查。

保留：

- `internal/control` 与 `internal/controlruntime` 的 Action/消息/时间/生命周期/effect 语义；
- Adapter qualification；
- exact-prefix DFS 与 deterministic semantic best-first；
- A1/A2 的 TestHypothesis、episode、Explorer 和 proposal validator；
- provider intent/dispatch/result 的持久化与恢复；
- qualified ExecutionBundle、PSS、fresh Replay、Oracle 和 evaluator；
- deterministic Campaign coordinator，供后续配置时间 session 复用。

## 工件收敛

- 删除四组旧 pilot 中约 59.8 万行重复完整 Trace；
- 删除 M5.10–M5.12/M5.15 大型 baseline JSON 和对应的字节哈希断言；
- M5.23g 只保留 README 与紧凑负结果 summary；
- 完整运行产物以后写入 ignored `artifacts/`；
- Git 提交 `0106e2c` 是本轮删除内容的恢复点。

仍有少量旧 `.gz` 二进制归档由 Git 跟踪；它们不参与编译或当前流程。补丁工具不能安全读取其二进制内容，
本轮没有绕过编辑约束强删，后续可在明确提交范围时单独处理。

## 文档收敛

- README 只说明当前输入、流程、输出、能力和阅读入口；
- `architecture.md` 只说明现有代码模块与信任边界；
- 总体规划由 3,403 行历史累积稿重写为当前路线；
- 历史阶段日志不再定义当前 API。

## 验证策略变化

没有增加新的 gate。开发循环只运行受影响包；完整阶段结束时运行一次已有 `make test`、`go vet ./...` 和
`git diff --check`。race 保留为显式发布/里程碑检查，不在每个小阶段重复。

## 收敛结果

| 类别 | 收敛前盘点 | 当前 | 变化 |
|---|---:|---:|---:|
| Go 生产代码 | 33,331 行 | 29,053 行 | -4,278（-12.8%） |
| Go 测试代码 | 17,982 行 | 13,780 行 | -4,202（-23.4%） |
| Go 合计 | 51,313 行 | 42,833 行 | -8,480（-16.5%） |
| Markdown | 19,578 行 | 3,463 行 | -16,115（-82.3%） |
| JSON | 约 647,693 行 | 10,243 行 | -637,450（-98.4%） |

当前 diff 共涉及 328 个文件，净删除约 66.9 万行。这里的价值是减少现行维护面，不代表研究效果提高。

## 验证结果

- 全量测试首次运行暴露三个只依赖历史摘要/报告的失败；删除这些 baseline 自证后，受影响的
  `cmd/control-experiment` 与 `cmd/defect-eval` 通过；同次全量运行中的其余包均通过；
- `go vet ./...` 通过；
- `make audit-race-shards` 通过；
- `git diff --check` 通过；
- 按阶段策略未运行 race。

## 没有证明

- 删除代码不等于 Agent 效果提高；
- 当前仍没有隐藏 candidate/control 的方法优势结果；
- 当前公开模型样本仍只有校准意义；
- 当前还不是完整的配置时间自动测试产品。

## 下一步

A3 只做一个用户可见闭环：把 Semantic Explorer 选中的路径送入已有 qualified executor，产生
ExecutionBundle、PSS/Risk、fresh Replay 和 Oracle 结果。优先复用现有类型，不再横向扩展基础设施。
