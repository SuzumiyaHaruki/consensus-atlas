# M5.6d Control Path 能力语义机械化

日期：2026-08-07

## 结论

M5.6d 没有增加 Action、Gateway 功能或 Runtime backend，而是在 M5.5a 既有等级
`unavailable < opaque < observable < interceptable < scheduler-owned` 中加入
`scheduler-actuated`。新 `ControlPathAssessment` 不接受调用方直接填写 grade，只根据可信 witness facts
推导，并把 stable item ID、controller/external atomicity 与 strict replay 独立记录。

冻结矩阵首次用同一视图精确表达：两个实现都能 scheduler-own message，但只有 etcd/raft 能 strict
replay；Gateway 虽由同一个 Runtime Action 驱动，却只 scheduler-actuate connection。

## 输入、处理与输出

```text
fresh/frozen conformance witness facts
                 |
                 v
identity + implication validation
                 |
                 v
derived ControlGrade
                 |
                 +-- stable item ID
                 +-- controller/external atomicity
                 +-- strict replay
```

输入只有事实布尔值和 evidence digest，没有 `grade` 字段。以下不一致事实被稳定拒绝：

- 未观察却声称 intercept；
- 未 intercept 却声称 Runtime Action actuation；
- 未经过 Runtime Action 却声称 selection check；
- 没有 stable item ID/selection check 却声称 Runtime owns terminal；
- 没有 controller atomicity 却声称 external atomicity。

Assessment 自带 canonical digest，改写冻结 grade 会被验证拒绝。

## 三路径实测

| Target/path | Granularity | Grade | Stable item ID | Controller atomic | External atomic | Strict replay |
|---|---|---|---:|---:|---:|---:|
| etcd/raft v3.6.0 | message | scheduler-owned | 是 | 未在本报告证明 | 未在本报告证明 | 是 |
| HashiCorp Raft v1.7.3 | message | scheduler-owned | 是 | 未在本报告证明 | 未在本报告证明 | 否 |
| connection Gateway | connection | scheduler-actuated | 否 | 是 | 否 | 否 |

两个 Adapter 路径从 M5.5a 的机械 Qualification/ControlSurfaceReport digest 投影；Gateway 路径在同一测试
中重新执行 Runtime Action、选择期检查、重叠引用和失败回滚，再绑定实际计数 digest。冻结报告见
`benchmarks/qualifications/control-paths-m5.6d/report.json`，连续 20 次复验稳定。

## 证明了什么

- 统一 Action 可以跨不同执行机制复用；
- control grade 与 strict replay 必须分开，否则会错误贬低 HashiCorp 或错误抬高 Gateway；
- Gateway 在 M5.6a 以后已超过单纯 interceptable，但仍没有取得 message ownership；
- grade 可以从事实推导，调用方不能通过直接填写枚举值自授等级；
- 既有 M5.5a 报告和 digest 无需重写。

## 没有证明什么

- Gateway 仍不是完整生产 blackbox Adapter，当前只有 path witness；
- evidence digest 防止工件漂移，但不是签名，也不能证明不可信代码没有伪造 witness；
- controller rollback 仍不等于网络原子事务；
- 没有证明 HashiCorp 的执行可严格重放；
- 没有迁移 PSS、Coverage、Agent、Campaign 或 benchmark；
- 没有验证新的真实进程式共识。

## 代码账本

- `ControlPathWitness/Assessment`：97 行生产 Go；
- 既有 `ControlGrade` 插入 `scheduler-actuated`：净增 3 行；
- 本阶段生产 Go **净增恰好 100 行**，未超过停止线；
- Runtime、Adapter、Action、Profile、Gateway、CLI：0 行变化；
- 其余变化均为测试、冻结报告、Make 审计入口和文档。

## 下一步

进入 M5.7，但不能把五类端口一次性加入 Runtime。第一步只写 Adapter 内部的最小
MessagePort/TemporalPort 规范与 fixture，并先确定 M5.8 的真实目标是否确实需要这些接口。若没有真实
消费者，删除未使用端口而不是保留“架构完整性”代码。

## 复验

```bash
make audit-control-paths
go test ./...
go vet ./...
go test -race ./...
```
