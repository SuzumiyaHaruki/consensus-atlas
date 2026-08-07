# Control Runtime v2 Adapter 接入模板

这个模板用于接入新的共识实现。它约束边界，不要求所有实现采用相同的内部 API，也不允许为了
某个协议在 `internal/controlruntime` 中增加名称、类型或条件分支。

## 1. 最小目录

```text
adapters/<implementation>/
  adapter.go       # control.Adapter 的薄翻译层
  model.go         # 私有配置、命令和原生对象映射
  storage.go       # 可见/持久/应用边界（实现需要时）
  evidence.go      # 原生状态到版本化 Evidence 的投影
  adapter_test.go  # 实现内单元测试，不代替外部 conformance

qualifications/<implementation>/
  run.go           # 组装通用 Profile、外部 suite 和 unsupported 声明
  run_test.go      # fresh run、digest 和 qualification 断言
```

只有 `adapters/<implementation>` 可以 import 目标实现。`qualifications/<implementation>` 是组合
根，可以同时 import 具体 Adapter 和 `internal/conformance`。通用 Runtime、Profile 和准入判定不
得 import 具体实现。

## 2. 必须实现的官方接口翻译

Adapter 实现 `control.Adapter`，并保持下面的所有权：

| 原生行为 | Adapter 输出 | Runtime 权限 |
|---|---|---|
| 节点准备发送 RPC | frozen `ItemMessage` | release、deliver、drop、duplicate、partition |
| 自然 timer/tick 到期 | `ItemTemporal` | 仅选择最早已到期事件并推进逻辑时间 |
| 持久化、fsync、apply | `ItemEffect` | complete/fail；决定何时成为 durable/applied |
| 客户端结果 | `ItemClientResult` | 保存为事实，不解释协议语义 |
| 协议状态 | `ItemObservation` / Evidence | 保存并交给 Family Evidence/Oracle |
| 节点生命周期 | `Crash`/`Restart` command | 管理 incarnation；保留已释放网络消息 |

Adapter 只调用官方实现 API 并翻译输入输出。它不得自行选择消息顺序、跳过未完成 effect、按墙钟
等待“稳定”、删除 Runtime 已拥有的消息或返回 Agent 自定义断言。

## 3. Manifest 和 Unsupported

1. `Manifest()` 必须通过 `control.AdapterManifest.Validate()`，其 JSON 形状见
   `manifest-schema-v2alpha1.json`。
2. Manifest 只声明控制面候选能力；声明本身不能产生 validated capability。
3. 无法提供的 Profile capability 使用固定 `UnsupportedDeclaration` reason code。不得写自由文本
   来改变资格逻辑，也不得省略后把缺口当成通过。
4. `BuildID`、`ImplementationID` 和 `ConfigurationDigest` 必须绑定实际构建与节点配置。

## 4. 外部准入步骤

```text
fresh Adapter factory
  -> Manifest digest
  -> EvaluateCore
  -> EvaluateNaturalLifecycle
  -> EvaluateOpaqueInvoke
  -> optional implementation-independent suites
  -> Qualify(manifest, frozen profile, unsupported, reports)
  -> sealed QualificationReport
```

`PortableCFTProfile` 当前要求八个公共能力：稳定 yield/evidence、纯 enabled 检查、自然时间进展、
Runtime 消息所有权、crash incarnation、严格 decision replay、可审计 entropy replay 和 opaque
invoke。`formal-process-isolation` 作为独立可见能力，不因进程内单测通过而自动成立。

资格状态完全机械生成：

- `validated`：Manifest requirement 满足，且所有指定外部 case 通过；
- `unsupported`：存在枚举化 Unsupported 声明；
- `undeclared`：Manifest requirement 不满足且没有 Unsupported 声明；
- `unvalidated`：声明存在但缺少外部 case；
- `failed`：外部 case 已运行但失败。

只有所有 `required=true` 的能力均为 `validated`，报告才得到 `qualified=true`。这只是相对于该
Profile 的控制面准入，不是协议正确性、安全性或跨协议普适性证明。

## 5. 新 Adapter 禁止修改的内容

- `internal/control` 的 Action/Item 语义；
- `internal/controlruntime` 的 enabled、消息所有权、时间推进和 replay；
- `internal/conformance` 的现有 case 验收条件；
- 当次冻结 Qualification Profile；
- Family Evidence、PSS、Coverage 或 Oracle 的可信结论。

如果官方实现无法映射到现有公共语义，先记录具体缺口和失败见证；只有两个独立实现都需要同一
新增语义时，才评审公共模型扩展。
