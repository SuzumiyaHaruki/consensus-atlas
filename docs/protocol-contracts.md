# Protocol Knowledge Contract 与自动接入

## 目标

正常接入流程只要求人工或 Family Pack 提供一次确定性的最小协议知识。后续 API 定位、实现绑定、Driver 草稿和见证生成可以交给 Agent；是否可用完全由程序判断，不依赖逐项人工标签。

```text
Protocol Knowledge Contract
  -> deterministic compiler
  -> immutable obligation denominator

Onboarding Agent(repository + contract + previous findings)
  -> Binding + Driver + executable witness scenarios
  -> mechanical validator
       -> invalid: structured findings feed next attempt
       -> validated: runtime Profile
       -> unsupported: remains in Profile denominator
```

## Contract 是人工输入边界

`contracts/schema-v1.json` 固定以下内容：

- protocol/family/PSS identity 和节点边界；
- 初始 voters/learners 成员关系；
- 需要观察的状态维度；
- Runtime 必须验证的能力及其固定 evidence labels；
- 协议无关操作及其 event kind；
- transition、ordering、fault、boundary、invariant 义务；
- 每项义务的固定 observation label、Oracle monitor 和能力依赖；
- 五类覆盖权重和阈值。

Contract 是一次正式评测的语义真值与分母来源。Agent 无权在 Binding 中增删义务、更换标签、关闭 monitor 或修改权重。Contract 的规范化 SHA-256 digest 被 Binding 锁定，修改后必须重新接入。

## Binding 是 Agent 输出边界

`onboarding/schema-v1.json` 只允许三类提案：

- Contract capability ID 到 Driver manifest capability ID 的映射（v1 使用统一 capability vocabulary，因此两者必须同名）；
- Contract operation ID 到通用 Runtime event kind 的映射；
- scenario 路径、其声称覆盖的 Contract obligation ID，以及需要用固定 evidence labels 验证的 capability ID。

Binding 故意没有 `status`、置信度和自定义 assertion。Supported/Unsupported、见证是否有效和最终状态只能由验证器产生。

## 自动验收规则

`internal/autoonboard` 对每个 proposal 检查：

1. Contract identity 和 canonical digest；
2. capability/operation 的完整性、唯一性和精确类型；
3. Binding、Adapter、Driver manifest、protocol 和 nodes 的一致性；
4. runtime manifest 中是否真实存在被引用 capability；Manifest 的 `supported=true` 只是候选，不是最终证据；
5. witness 路径不能逃逸仓库，scenario 使用严格 JSON；
6. 每个见证在全新实例上执行两次，执行 fingerprint 必须一致；
7. Driver 在执行前后通过 conformance；
8. `trace-integrity` 和 `agreement` Oracle 已运行且无违规；
9. 每个被声称覆盖的义务产生 Contract 固定的 label，且对应 monitor 确实执行；
10. 每项 Runtime capability 的 Contract 固定 evidence labels 都由一个或多个强见证实际产生；
11. 每个 Runtime 可支持的义务至少有一个强见证。

Agent 可修复的问题产生 `actionable=true` finding，Coordinator 将其原样返回下一次生成尝试。Runtime 明确报告的能力限制产生非 actionable `Unsupported`；它不会阻止部分接入，但相应 atom 永远保留且不能计为已覆盖。

## etcd/raft v1

```bash
make contract-compile-etcdraft
make auto-onboard-etcdraft
make run-raft
```

输入：

- `contracts/etcdraft-v1.json`：12 个固定义务；
- `onboarding/etcdraft-binding-v1.json`：官方 `go.etcd.io/raft/v3` 嵌入式 Driver 的映射和见证；
- `scenarios/etcdraft-election-crash.json`：选举、提交、宕机、恢复见证。

当前机械结果是 7/9 capability 与 10/12 obligation 可支持。`exact-ready-send-barriers` 和 `natural-election-timeout-replay` 由 Driver manifest 明确报告为不支持，对应两个义务仍留在生成的 Profile 中。

## Agent 扩展点

Model-backed Onboarding Agent 实现 `autoonboard.Generator`：读取 Contract、受限仓库上下文、上一轮 Binding/findings/完整验证报告，生成下一版 Binding/见证。Coordinator 是确定性程序，不是负责“投票裁决”的 Agent。旧 Scout/Integration Pack/人工 Gate 路径已从主仓库删除。

DeepSeek V4 Flash 实现位于 `agents/deepseek_onboarding.py`，通过 `CommandGenerator` 的 versioned JSON stdin/stdout 边界调用。子进程不继承父进程环境；只获得 key 文件路径、model 和 endpoint。密钥必须是权限受限的普通文件，以 no-follow 方式打开，并且只允许发送到官方 DeepSeek Chat Completions endpoint；密钥不写入 prompt、输出或审计报告。

```bash
make auto-onboard-etcdraft-llm
make run-raft-llm
```

当前 live smoke 中，DeepSeek 第 1 轮的见证映射被机械验证器拒绝；在收到 witness observed labels 后，第 2 轮自动修正并通过，生成的 Profile 与静态基线完全一致。

每轮报告记录 provider/model、endpoint、temperature、thinking mode、token 上限与实际用量、prompt/request/response digest、响应 ID/fingerprint 和耗时。`make test` 只运行 mock/offline 测试，不会自动产生 API 调用与费用。

API 实现依据 [DeepSeek Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion) 和 [JSON Output](https://api-docs.deepseek.com/guides/json_mode/)。
