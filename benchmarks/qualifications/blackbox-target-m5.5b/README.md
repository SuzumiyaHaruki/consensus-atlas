# M5.5b 最小黑盒 Target Envelope

该工件由真实独立子进程 fixture 生成。测试执行 argv 形式命令、连接显式 readiness endpoint、冻结
opaque 客户端调用，并验证 drop、deliver、进程 kill/restart、数据保留和跨 incarnation pending call。

当前只获得：

- external input：`interceptable`；
- lifecycle：`interceptable`；
- durability：`observable`；
- opaque endpoint：`observable`。

报告明确保留 wall-clock readiness、one-call-per-connection、direct-child-process-only、无 peer
message boundary 和无 strict replay。digest 为
`868a8c4bd97de25d2d2cecd67b5c3c84d5b5bf14d7d4b416abfbfd8faa42d95d`。

```bash
make audit-blackbox-target
```
