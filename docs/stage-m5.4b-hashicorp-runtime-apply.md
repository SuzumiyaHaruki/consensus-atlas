# M5.4b HashiCorp Raft Runtime 消息与 Apply 闭环

日期：2026-08-07

## 结论

第二个真实实现已形成最小 Runtime 闭环：官方 HashiCorp Raft 三节点产生的同步 RPC 先被冻结为公共
`ItemMessage`，由同一个 `controlruntime.Runtime` 选择 deliver/drop；随后 opaque invoke 经真实
`Raft.Apply`、`AppendEntries` 和多数派响应到达记录型 `FSM.Apply`。

五次公开运行均以 6 个决策完成，`FSM.Apply` 为 5/5。五个 trace digest 全部不同，所以本阶段仍为
`qualified=false`、`strict_replay=false`。这不是失败：它明确区分了“控制动作可以执行”和“整个实现
可以确定性重放”。

## 输入、处理、输出

```text
official HashiCorp Raft v1.7.3, n1/n2/n3
                  |
     synchronous Transport RPC is frozen
                  |
       common Runtime deliver / drop
                  |
     target Consumer + source RPC response
                  |
           opaque Raft.Apply
                  |
  controlled AppendEntries delivery to quorum
                  v
       typed fsm-apply observation + trace
```

为了给 Runtime 留出选择窗口，`n1` 使用 500ms follower heartbeat timeout、10s candidate election
timeout，`n2/n3` 使用 1h timeout。超时仍由官方实现自然触发，没有强制 timeout 或虚拟时间注入。

## 关键抽象修正

M5.4a 后发现：etcd/raft 在调用 `Step` 前已经产生独立消息对象，Runtime 本地标记 drop 即可；HashiCorp
`Transport` 则是同步调用，发送 goroutine 会等待响应。若 drop 不通知 Adapter，SUT 调用会永久挂起。

因此沿用原有 `ActionDropMessage`，只把执行次序改成：

1. Runtime 检查消息仍 enabled；
2. Adapter 确认实现侧消息身份，并使同步调用返回稳定 drop error；
3. Runtime 将消息标为 `ItemDropped`。

没有新增 Action、Item、协议条件或第二套 Runtime。fixture 与 etcd/raft 的 Adapter 只增加兼容性
acknowledgement；其消息语义不变。分区仍只阻止 deliver，heal 后原消息可继续投递。

## 实验结果

| 指标 | 结果 |
|---|---:|
| fresh trials | 5 |
| decisions/trial | 6 / 6 / 6 / 6 / 6 |
| drop / deliver / invoke | 2 / 3 / 1 |
| `FSM.Apply` | 5/5 |
| unique trace digests | 5 |
| strict replay | false |

工件：[M5.4b report](../benchmarks/probes/hashicorp-raft-v1.7.3-m5.4b/report.json)，摘要
`23f2a0c1f5a979304cd9059d69fb789eb7576b0590d1c37f156c113e2656dba1`。

trace 不一致的已知来源包括官方实现的墙钟、包级随机抖动以及 RPC 日志中的操作性
`Log.AppendedAt`。消息信封必须冻结真实 payload；未来 PSS 投影应忽略这类非协议状态，而不是在
Runtime 身份层偷偷删除。

## 代码增长门

| 范围 | M5.4 累计生产 Go |
|---|---:|
| `adapters/hashicorpraftv2` | 773 |
| `cmd/hashicorpraft-probe` | 27 |
| **HashiCorp 纵向切片合计** | **800** |
| generic `internal/controlruntime` 本阶段净增 | **5** |

累计值正好触及 M5.4b 800 行硬停止线。下一阶段增加功能前必须先合并 M5.4a/M5.4b 重复 transport、
FSM 和 report 代码，产生净删除；不得通过压缩格式伪造减行。

## 已证明与未证明

已证明：公共 Runtime 可以同时承载 etcd/raft 的异步消息对象和 HashiCorp Raft 的同步 Transport RPC；
deliver/drop 会影响真实实现执行；opaque command 能到达官方实现调用的 FSM。

未证明：自然时间和 SUT 随机仍不可控；没有 strict replay、crash/restart、持久 store 恢复、snapshot、
membership、Conformance/Qualification、PSS/Coverage、Agent 或缺陷检出结论。五次成功不是协议正确性
概率。

## 下一阶段门

先做 M5.4b.1 减负审计：保持工件行为不变，合并 probe/runtime 重复代码，目标净删至少 80 行。达到
净删除后才进入 M5.4c lifecycle：保留 Store、停止/重建 Raft、验证消息所有权与恢复边界，再生成
部分 Qualification。若 lifecycle 需要修改公共 Action/Item 或总生产代码超过 1,500 行，停止复核。

## 验证

- 集成测试普通模式连续 5 次通过，race 模式单独通过；
- fixture、etcd/raft、HashiCorp Adapter 和 Runtime 回归通过；
- 工件 JSON 与 canonical digest 机械检查；
- 全仓测试、vet、race、Python 与耦合检查见本阶段最终交付记录。
