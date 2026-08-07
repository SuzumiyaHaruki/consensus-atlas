# 当前阶段

日期：2026-08-07

阶段：M5.6c Gateway Actuator 重叠与失败边界已完成

## 当前输入、处理与输出

```text
typed Partition/Heal Action
          |
          v
selection-time Adapter eligibility
          |
          v
Gateway binding + overlap refcount
          |
          v
all-or-rollback controller gate state
```

M5.6c 在 M5.6b typed binding 上增加选择期资格、重叠 partition 引用计数与多 Gateway 失败回滚。
三个独立子进程和两条 Unix Gateway 已验证同一 Runtime Action 对真实连接流量的隔离与恢复。

## 本阶段结果

- 外部 gate 状态漂移会在 Action 选择前被过滤；
- 两个重叠 partition 共享同一 Gateway，不会重复关闭或过早恢复；
- partition/heal 中途失败都会逆序回滚已改变的 controller gate state；
- 三进程双 Gateway 流量测试连续 20 次通过；
- 生产 Go 净增 165 行，没有新增 Action、Profile、Schema、CLI 或 backend selector。

详细结果见 [M5.6c 阶段总结](stage-m5.6c-gateway-actuator.md)。

## 当前没有完成

- 多 Gateway 调用仍是顺序的，外部进程可能观察到瞬时 partial cut；
- 回滚不能恢复已经关闭的旧 connection 或在途字节；
- composition wrapper 仍是 test-only，尚无规范化 evidence 或 Qualification；
- topology 完整性只相对于显式 inventory；
- 黑盒路径仍不能 strict replay，也没有取得 scheduler-owned message qualification；
- PSS、Coverage、Oracle、Agent、Campaign 和 benchmark 尚未迁移到 v2。

## 下一步

进入 M5.6d，但先做能力语义机械化，不扩张通用黑盒网络代码。能力必须拆成 Control Surface、
Control Grade 与 Deterministic Guarantees；默认接入目标调整为“官方接口优先的最小灰盒”，纯黑盒
保留为 Level 1，协议核心修改是最后手段。详细决策见
[能力分级与灰盒 Control Port 设计](graybox-control-ports.md)。

M5.6d 只允许把当前 etcd/raft、HashiCorp Raft、Gateway 投影为机械能力报告，并添加防止自授资格的
负例；不新增 Action，生产 Go 目标净增不超过 100 行。随后 M5.7 再冻结 Adapter 内部最小
MessagePort/TemporalPort，M5.8 才选择真实进程式共识做灰盒纵向切片。

## 阅读入口

1. [能力分级与灰盒 Control Port 设计](graybox-control-ports.md)
2. [M5.6c 阶段总结](stage-m5.6c-gateway-actuator.md)
3. `internal/blackbox/actuator.go`
4. `internal/blackbox/actuator_process_test.go`
5. [M5.6b 阶段总结](stage-m5.6b-typed-partition-binding.md)
6. [Control Runtime v2 设计](control-runtime-v2.md)
7. [总体规划](ConsensusAtlas-总体规划.md)
