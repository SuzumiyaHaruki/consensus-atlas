# M5.21m：FormalExposureAudit

日期：2026-08-11
状态：exact projection 与 direct private-atom audit 完成；`formal_ready=false`

## 目标

M5.21l 已能从 private contract 生成 opaque view，但“类型看起来安全”不足以证明实际交给 runner/Agent 的
JSON 没有复制 private identity。本阶段增加一个小型、协议无关、curator-side 审计，不恢复 M4 CLI，
也不允许审计报告本身成为新的泄露通道。

## 输入、处理、输出

```text
输入（curator private environment）
  sealed FormalBenchmarkContract
  + supplied FormalOpaqueView
  + exact public JSON byte snapshots
                         |
                         v
处理
  recompute exact opaque projection
  + SHA-256 every supplied artifact
  + recursively collect JSON key/value strings
  + compare with enumerated private atoms
                         |
                         v
输出
  artifact index/digest + stable finding code
  + passed + canonical audit digest
  - no matched value/path/private metadata
```

## 审计范围

private atom 集合来自 sealed contract：

- blinding nonce；
- projector 与 monitor ID；
- pair 与 root-cause ID；
- variant、build、config、BuildAudit 和 binary identity。

trial ID、benchmark commitment、Family/Profile/MethodSpec、bundle schema 和共同预算本来就是 exact opaque
view 的公开字段，不进入 private atom 集合。

审计递归检查 JSON string value 和 object key。一个 artifact 即使包含多个 private atom，也只产生一个
`formal-private-atom-exposed` finding；报告不会保存命中数量或具体值。损坏/多值 JSON 记
`formal-public-json-invalid`，supplied view 与可信重算不一致记 `formal-opaque-view-mismatch`。任一 finding
都令 `passed=false`。

`FormalExposureAudit/v1` 绑定：

- benchmark/contract identity；
- expected opaque view digest；
- supplied opaque object digest；
- 每个 public artifact 的原始字节 SHA；
- 排序后的稳定 findings；
- canonical report digest。

报告自身支持 Seal/Validate；篡改 artifact digest、finding、passed 或排序都会失效。

## Synthetic 验证

公开 fixture 保存两类结果摘要：

- clean opaque request：`passed=true`；
- 直接复制 private root-cause：`passed=false`、1 个 finding。

回归还覆盖 private atom 出现在 object key、同一 JSON 多次泄露、malformed/trailing JSON、重封装后篡改
opaque budget、audit digest 篡改，以及报告 JSON 中不出现任何已枚举 private atom。

摘要位于 `benchmarks/experiments/formal-exposure-audit-m5.21m/summary.json`。fixture 和 root identity 已公开，
所以只证明审计机制，不是 private holdout。

## 不能证明

该审计只检测 exact string atom 的直接复制，不能证明：

- 模型无法从协议知识、capability 组合或行为反馈推断候选语义；
- 编码、哈希、分片或自然语言改写后的 private 信息一定会被发现；
- runner 具有进程、网络或文件系统隔离；
- candidate/control 因果配对正确；
- Agent、Random 或其他方法有效。

因此 exposure audit 是必要门禁而非保密性证明。当前仍没有 formal fresh evaluator、真实 private dataset
或第二 strict CFT target，`formal_ready=false`。本阶段模型调用和新 SUT execution 均为 0。

## 下一阶段

下一步不再增加数据模型。应将现有 `EvaluateFreshBundles` 的硬编码 Agreement 判定提取成由
`ResolveFormalComposition` 选出的 monitor 集合，并新增接收 trial→fresh evidence map 的 formal entry。
它必须复用现有 bundle/build/method/budget 校验和 summary，不能复制 evaluator 或 Control Runtime。
