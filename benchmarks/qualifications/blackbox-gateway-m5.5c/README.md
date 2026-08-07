# M5.5c 两进程 connection gateway

该工件由两个真实独立子进程和一个不解析 payload 的 Unix socket gateway 生成。测试验证开放链路、
partition、receiver 不变、heal 后恢复以及双向 opaque byte 计数。

本报告只支持以下结论：

- peer connection：`interceptable`；
- partition gate：`interceptable`；
- peer bytes：`observable`。

该 API 尚未注册为 Control Runtime Action，也没有 Qualification，所以不能取得 `scheduler-owned`
credit。接入要求目标能把 peer endpoint 配置到 gateway；长连接被 partition 时只能整体关闭，不能
选择其中某条消息。

报告内 canonical digest 为
`3cb2e0ca23230f2590eba86d1d51a487330c8d17ae7d7206ef1e0ece55e560fe`。

```bash
make audit-blackbox-gateway
```
