# Agent boundary

Agent 只生成候选 PSS 字段、覆盖目标和 TestSpec 约束。以下能力不对 Agent 开放：

- 直接投递消息或修改逻辑时钟；
- 修改当前 Profile 的分母；
- 写入 Coverage Ledger；
- 宣布 safety/liveness violation 成立；
- 修改已经生成的重放证据。

`planner.py` 是一个无 LLM 依赖的接口占位程序：

```bash
printf '%s' '{"uncovered_atoms":[{"id":"fault.crash"}]}' | python3 agents/planner.py
```

未来接入 LLM 时应保留同样的 JSON 输入输出边界，并记录模型、提示词、采样参数和工具调用日志。
