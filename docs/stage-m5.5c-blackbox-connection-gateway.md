# M5.5c 黑盒 connection gateway 决策实验

日期：2026-08-07

## 结论

框架现在能在不 import 协议类型、不解析 payload 的情况下，把一个节点的可配置 peer endpoint 指向
connection gateway，并对整条连接执行 partition/heal。两个真实独立子进程已验证开放时转发、隔离时
不触达接收端、恢复后重新转发。

这证明 connection-level 黑盒控制可作为薄接入层的一部分，也同时冻结了它的上限：它控制连接和字节，
不是协议消息。M5.5 到此停止通用黑盒网络功能扩张，不为追求更高能力在公共层加入 framing 或 codec。

## 控制边界

`GatewaySpec` 只描述稳定 ID、listen endpoint 和 forward endpoint；endpoint 只允许 TCP/Unix，Unix
路径必须为绝对路径，listen/forward 不能相同。gateway 执行以下状态转换：

```text
Start(open) -> accept + opaque byte forwarding
            -> Partition(close active + reject new connections)
            -> Heal(accept + forwarding)
            -> Close
```

转发使用双向 byte stream，不读取、修改或生成协议字段。快照只包含 partition 状态、连接计数、
dial failure、active connection 和双向 byte 数。

## 两进程实测

测试启动 `n1`、`n2` 两个真实子进程。`n1` 的 peer endpoint 指向 gateway，gateway 再连接 `n2`：

1. open 时 `n1` 发送 `before`，`n2` 保存并可读出；
2. partition 后 `n1` 的新 peer write 失败，`n2` 仍保持 `before`；
3. heal 后 `n1` 发送 `after`，`n2` 保存并可读出；
4. 最终 accepted=3、forwarded=2、rejected=1、dial failure=0、active=0；
5. 双向 byte 计数均非零。

冻结报告见 [blackbox gateway report](../benchmarks/qualifications/blackbox-gateway-m5.5c/report.json)，
canonical digest 为
`3cb2e0ca23230f2590eba86d1d51a487330c8d17ae7d7206ef1e0ece55e560fe`。

## 能力等级

| 表面 | 当前等级 | 证据 |
|---|---|---|
| peer connection | interceptable | accept/forward/reject |
| partition gate | interceptable | partition/heal API |
| peer bytes | observable | 双向 byte counters |
| protocol message | unavailable | 不存在 framing/codec |
| deterministic ordering | unavailable | goroutine 与 wall-clock I/O |
| strict replay | unavailable | 无稳定 message yield/enabled ID |

partition/heal 当前只是直接可调用的控制 API，尚未注册为 Control Runtime typed Action，也未通过
Qualification，因此只能记为 `interceptable`。它不能向 message surface、stable yield、pure enabled
set 或 strict replay 转移能力 credit。

## 黑盒接入的冻结上限

- 目标必须允许配置 peer endpoint；不能重定向 endpoint 时该能力不可用；
- partition 会关闭已有连接并拒绝新连接，无法在长连接内选择单条协议消息；
- 不识别一条 write 是否对应完整消息，也不支持 duplicate/reorder 某条消息；
- accept、dial 和 copy 仍由 goroutine/wall clock 排序；
- fixture 证明边界可执行，不证明任意真实共识已经接入；
- 更强的消息、自然时间、持久化边界和严格重放仍需要薄 Adapter 或实现提供的 hook。

因此，多数可部署系统可以先获得黑盒 external input、lifecycle、durability observation 和
connection partition；能否获得更强控制由机械 Capability Report 决定，而不是更换或筛选目标实现。

## 代码账本

- `internal/blackbox/gateway.go`：231 行生产 Go；
- gateway 两进程测试：146 行 test-only Go；
- 为共用独立进程 fixture 增加 peer 模式：40 行 test-only Go；
- Runtime、Adapter、ControlSurface taxonomy、PSS/Coverage/Agent：0 行变化；
- 没有新增 CLI、schema、消息 framing 或协议专用分支。

## 下一阶段

M5.6a 不再增加黑盒网络机制，而是实现 capability-resolved backend selection：测试意图声明最低控制
等级，机械选择黑盒 Envelope/Gateway 或 qualified Adapter；两类 backend 只输出规范化 Execution
Evidence，缺失能力必须 deferred。先用 external input、lifecycle、partition 三类最小场景验证，随后再
迁移 PSS/Coverage，避免形成两套互不相通的执行系统。

## 复验

```bash
make audit-blackbox-gateway
go test ./...
```
