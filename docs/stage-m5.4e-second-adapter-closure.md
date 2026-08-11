# M5.4e 第二实现能力矩阵与减负收口

日期：2026-08-07

## 结论

M5.4 的第二实现实验已经收口。相同的 `portable-cft-control-v2` Profile 与 Qualification 机制可同时
审查官方 etcd/raft v3.6.0 和 HashiCorp Raft v1.7.3，但两者并不具有相同的已验证控制能力：
etcd/raft 为 8/8 required validated；HashiCorp 为 3 validated、6 Unsupported、`qualified=false`。

这证明公共 Runtime/Adapter Contract 能承载第二种控制表面，并不证明完整确定性控制已经跨实现
成立。M5.4 原定的“自然时间和受控 entropy 均通过”退出条件没有满足；本阶段以稳定 Unsupported
边界结束继续补丁化 HashiCorp 的尝试。

## 输入、机械处理与输出

```text
Portable CFT Profile v2
 + Adapter Manifest
 + trusted conformance reports
 + explicit stable Unsupported declarations
                    |
          mechanical Qualification
                    v
 two QualificationReports -> validated intersection -> frozen matrix
```

矩阵测试每次 fresh 运行两个官方实现的资格流程，检查 Profile digest 一致，并由报告状态重建矩阵；
手写 JSON 状态、过期资格工件或被修改的 digest 都会使测试失败。

冻结工件见 [capability matrix](../benchmarks/qualifications/portable-cft-v2-m5.4e/capability-matrix.json)，
canonical digest 为 `6329a13996993024172a48303fd672b8287273214799ef96a755c51c61b54813`。

## 双实现能力结果

| capability | etcd/raft | HashiCorp Raft | 跨实现可用 |
|---|---|---|---|
| strict-yield-evidence | validated | Unsupported | 否 |
| pure-enabled-check | validated | Unsupported | 否 |
| natural-temporal-progress | validated | Unsupported | 否 |
| runtime-owned-message | validated | validated | 是 |
| crash-restart-incarnation | validated | validated | 是 |
| strict-decision-replay | validated | Unsupported | 否 |
| audited-entropy-replay | validated | Unsupported | 否 |
| opaque-invoke-boundary | validated | validated | 是 |
| formal-process-isolation（optional） | Unsupported | Unsupported | 否 |

消费规则已经冻结：单实现实验只能使用该实现 `validated` 的 capability；跨实现实验只能使用
`validated` 交集；Unsupported、unvalidated、failed 和 undeclared 均不得获得 Coverage credit。

## 减负结果

M5.4a 的一次性捕获探针已被 Runtime Adapter 和 Qualification 取代，因此：

- 删除生产 `adapters/hashicorpraftv2/probe.go`；
- 删除独立 `cmd/hashicorpraft-probe`；
- 只在 test-only helper 中保留冻结 M5.4a 工件的 fresh 复验能力；
- 官方模块身份与稳定 Unsupported reason code 留在真实 Adapter；
- HashiCorp 生产 Go 从 923 行降至 732 行，净减 191 行；
- 公共 Runtime、Conformance、Action/Item 和官方依赖源码均未修改。

## 当前能够证明与不能证明

能够证明：第二实现可复用公共消息所有权、生命周期代际和 opaque invoke 控制语义；能力准入与交集
可以机械计算；历史探针不再占用生产 API。

不能证明：HashiCorp strict replay、虚拟时钟或受控 SUT entropy；跨协议而非两个 Raft 实现的普适性；
协议安全性/活性完备；Coverage/PSS 质量；Agent 相对 Random/DFS/专家的优势。

## 下一阶段

进入 M5.5a，先建立 v2 的协议无关 Observation/Evidence 导出和消费者依赖审计，再迁移 Family PSS、
Coverage、搜索和 Agent。任何消费者必须读取资格矩阵或等价的 fresh Qualification gate，不能因 v1
曾支持某功能而默认第二实现也支持。

## 复验

```bash
make audit-portable-cft-matrix
make audit-hashicorp-determinism
go test ./...
```
