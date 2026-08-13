# A4c0：OpenRouter 单一模型入口

日期：2026-08-13

## 结论

活动 Agent 调用已从 DeepSeek 专用客户端收敛为一个 OpenRouter 客户端。生产代码不再设置默认模型；每次
显式 Agent 运行必须提供 OpenRouter `model_id`。切换模型只改变该输入，不增加新的 provider client，也不改变
ScenarioPlan、journal、Runtime、Replay 或 Oracle。

```text
Agent prompt
  -> OpenRouter model_id + fixed request shape
  -> durable intent / dispatch / result
  -> ScenarioPlan or Explorer proposal
  -> trusted concretization and execution
```

## 已实现

- 固定 OpenRouter `/api/v1/chat/completions` 端点和 Bearer 认证；
- 使用显式 OpenRouter 模型 slug，不保留厂商专用默认值；
- CLI 的显式 Agent 策略必须用 `-agent-model` 选择模型；
- 使用 `max_completion_tokens`、JSON response format、temperature 0 和 reasoning effort `none`；
- 请求在读取 key 前形成并进入原有 durable journal；
- 响应记录 OpenRouter 返回的实际模型标识，不假定它一定与请求别名逐字相同；
- provider HTTP、响应校验、token 和失败成本继续由同一实现负责；
- 本地模拟服务覆盖多个普通 fixture model ID，以及固定端点和非法 ID 拒绝。

## 兼容边界

旧的 direct-DeepSeek 校准工件是历史事实，不改写，也不要求由 OpenRouter 客户端恢复。新运行的 provider、
endpoint、请求模型和响应实际模型均写入已有审计，因此不会把两种传输误认为同一次调用。

本阶段没有读取 `key.txt`，没有访问模型 API，也没有宣称不同模型具有相同能力。

阶段验证通过：`make test`、`go vet ./...`、`git diff --check`。未运行 race。实现没有新增 package 或第二套
journal；生产代码净增主要来自 OpenRouter 模型 ID 校验和 CLI 选择入口。

## 后续进展

A4c 已增加紧凑、显式 opt-in 的 Scenario Agent 运行入口。首次真实调用受瞬时传输问题阻断；保留失败工件后，
第二个独立运行已成功获得两次模型响应并完成 qualified testing 与无外部调用恢复。详见
[A4c 阶段总结](stage-a4c-openrouter-scenario-calibration.md)。
