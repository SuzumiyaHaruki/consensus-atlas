# A4c：OpenRouter Scenario 校准运行器

日期：2026-08-13

## 结论

A4c 已完成真实模型运行所需的最小组合入口：一个命令创建或恢复 OpenRouter model-call journal，执行已有
Scenario Agent episode，并派生紧凑 `summary.json`。没有新增 Runtime、Planner 协议、Ledger 或模型客户端。

```text
OpenRouter response
  -> existing ScenarioPlan parser
  -> trusted concretizer / mechanical feedback
  -> at most one revision
  -> qualified Bundle / PSS / Replay / Oracle
  -> compact summary
```

## 实现与本地验证

- 策略：`etcdraft-openrouter-scenario-a4c`；
- 模型必须通过 `-agent-model` 显式提供；本次真实 trial 明确选择 `deepseek/deepseek-v4-flash`；
- 最多 2 次 provider call，每个计划最多执行 4 步；
- key 仅在 journal 请求 dispatch 时读取，dispatch 后立即清除；
- 成功 summary 报告 final plan、feedback、模型成本、Trace/Bundle digest、PSS、Risk、Replay 和 Oracle 计数；
- terminal provider failure 报告已消费调用和成本，不伪造 Agent feedback 或 testing 结果；
- resume 重建 episode 并核对 summary，已完成或终止的调用不会再次 dispatch。

本地 provider stub 验证了一次 `crash -> restart` 计划：1 次模型调用，qualified testing 完成，Replay stable，
Oracle 无 violation；恢复没有再次读取 key 或访问 provider。另一个 stub 验证 terminal transport failure 同样会
落 summary 并保持不可重试。

## 真实 OpenRouter 尝试

使用用户更新的权限为 `0600` 的 key 文件进行了显式 opt-in 调用，运行目录为 ignored：

`artifacts/agentic/a4c-openrouter-deepseek-v4-flash-v1/`

第一次结果：

- 1 次 dispatch；
- 约 5 秒后 terminal transport failure；
- 0 input/output token；
- 未收到模型 response，未产生 ScenarioPlan 或 testing result；
- resume 生成同一 `provider-failed` summary，没有再次访问 provider 或读取 key。

独立无凭据诊断中，OpenRouter models GET 返回 200，而最小 Chat Completions POST 同样在约 5 秒后以 TLS EOF
终止。因此第一次失败指向当时的出站链路，而不是模型输出、prompt 校验或 key 文件格式。没有改变 HTTP
客户端，也没有在原 journal 上重复 dispatch。

随后网络诊断显示相同无凭据 POST 已能正常到达 OpenRouter 并返回 401，因此使用新目录执行第二个独立 trial：

`artifacts/agentic/a4c-openrouter-deepseek-v4-flash-v2/`

第二次结果：

- 第一次模型计划执行 `deliver-message -> crash`，第三步 selector 得到 `no-match`；
- 第二次模型读取机械反馈，修正为 `deliver-message -> crash -> restart -> fire-temporal-event`；
- 2 calls，13,585 input tokens、587 output tokens、14,172 total tokens；
- qualified testing 产生 33 个 Core PSS sample 和 23 个唯一 Core PSS 状态；
- fresh Replay stable，TraceIntegrity/Agreement Oracle 为 0 violation；
- RiskWitness 仍为 `not-reached`，只满足 `workload-invoked-at-coordinator`，第一个缺失 milestone 为
  `coordinator-changed-while-inflight`；
- resume 得到同一 summary，provider call 目录仍为 2 个。

## 研究边界与下一步

两个 trial 分别证明终止失败恢复和真实反馈修正路径有效。成功 trial 表明 DeepSeek 能读取当前 Scenario view、
根据 `no-match` 形成合法修订计划，并把真实执行送入 qualified testing；但它没有达到语义 RiskWitness，也没有
与同预算 baseline 比较，因此不能作为 Agent 有效性优势证据。

下一步进入 A5，把 Hypothesis、ProtocolKnowledge 和运行配置外移为可审查 JSON 输入，并利用本次
`not-reached` 结果检查 Agent 看到的语义目标是否足够清楚；不放宽 selector 唯一匹配、Replay 或 Oracle。

阶段验证通过：`make test`、`go vet ./...`、`git diff --check`。未运行 race；普通测试没有读取 key 或访问外部
模型。
