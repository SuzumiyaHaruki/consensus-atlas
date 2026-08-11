# M5.5b 最小黑盒 Target Envelope

日期：2026-08-07

## 结论

框架现在可以在不 import 协议或实现类型的情况下启动一个独立本地进程，观察显式 opaque endpoint，
冻结客户端字节调用，并选择 deliver/drop；进程可被真实 kill、从同一数据目录重启并进入新
incarnation。该能力适合大部分可部署服务的第一层接入，但不等同于协议消息控制或严格重放。

## 最小接口

`internal/blackbox` 的 `Spec` 只包含：

- target/implementation identity；
- shell-free executable + argv；
- 绝对 work/data directory；
- TCP 或 Unix opaque endpoint；
- 显式 readiness endpoint 和 wall-clock timeout。

Target Envelope 提供：

```text
Start -> FreezeCall -> Deliver | Drop -> Crash -> Restart -> Deliver
```

`FreezeCall` 只冻结 schema、endpoint、sequence、opaque bytes 和 digest，不解释请求。pending call
由 Envelope 持有，因此可以跨目标 crash/restart；deliver 只有在目标 running 时才能发生。

readiness 只连接显式标记的 endpoint，不扫描或触碰 peer endpoint。命令不经过 shell，数据目录不得
是根目录。公共 architecture test 保证该包不依赖具体共识或 v1 Runtime。

## 独立进程实测

测试二进制以子进程方式运行 Unix socket KV fixture，实际验证：

1. 子进程启动且 endpoint 可达；
2. `PUT ignored` 被 drop，随后 `GET` 仍为 `EMPTY`；
3. opaque `PUT durable-value` deliver 成功；
4. `GET` 在 crash 前冻结，直接子进程被 `Process.Kill`；
5. stopped 状态拒绝 deliver，但 pending call 不丢失；
6. 从同一数据目录 restart 到 incarnation 2；
7. 原 pending `GET` deliver 后读到 `durable-value`。

冻结报告见 [blackbox report](../benchmarks/qualifications/blackbox-target-m5.5b/report.json)，digest 为
`868a8c4bd97de25d2d2cecd67b5c3c84d5b5bf14d7d4b416abfbfd8faa42d95d`。

## 能力等级

| 表面 | 当前等级 | 证据 |
|---|---|---|
| external input | interceptable | freeze/deliver/drop |
| lifecycle | interceptable | kill/restart/incarnation |
| durability | observable | restart 后值仍可见 |
| opaque endpoint | observable | readiness/response |
| peer message | unavailable | 尚无 peer connection gateway |
| temporal | opaque | 只等待原生 wall clock |

## 明确保留的限制

- readiness 依赖 wall clock，因此不是 strict replay；
- 一次调用使用一次 connection，不声称适配任意长连接 framing；
- 只 kill 直接子进程，不控制其派生进程树或容器；
- 尚未代理节点间连接，不能选择某条协议消息；
- 没有虚拟时间、SUT entropy、PSS 或 Coverage credit；
- fixture 证明通用边界可执行，不证明任意真实共识已接入。

## 代码账本

- `internal/blackbox`：313 行生产 Go；
- 独立进程 fixture/负例：244 行 test-only Go；
- Runtime、Adapter、Conformance taxonomy、Agent/PSS/Coverage：0 行变化；
- 没有新增 CLI、JSON Schema 或协议专用分支。

## 下一阶段

M5.5c 先做一个决策性最小实验：仅对可配置 peer endpoint 的多进程 fixture 增加 connection-level
gateway，验证 partition/heal 和 opaque byte forwarding。它必须明确是连接/字节控制而非协议消息
控制；若需要 framing 或实现专用 codec，则停在 interceptable 并交给薄 Adapter。

## 复验

```bash
make audit-blackbox-target
go test ./...
```
