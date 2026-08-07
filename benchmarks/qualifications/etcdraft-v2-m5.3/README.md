# etcd/raft v2 M5.3 Adapter qualification

这是一份公开开发期准入工件，不是缺陷实验、holdout 结果或协议正确性证明。

重新生成：

```bash
make adapter-qualify-etcdraftv2
```

本工件把三个 fresh 外部 suite 的 case 与 `portable-cft-control-v1` Profile 机械合并：

- Core：stable yield/evidence、pure enabled check、audited entropy replay；
- Natural lifecycle：自然时间产生消息、crash incarnation、Runtime 消息保留和 strict replay；
- Opaque invoke：不解释协议 payload 的输入边界和 strict replay。

结果：8/8 required capability validated，`qualified=true`。第九项
`formal-process-isolation` 是 optional，明确为 `ADAPTER_PROCESS_ISOLATION_REQUIRED`；因此这份结果
只允许说明 etcd/raft v2 通过当前公共控制面 Profile，不能用于正式隔离 benchmark，也不能证明
第二实现复用或跨协议普适性。

可信边界：CLI 在同一进程 fresh 执行 suite 后生成 bundle。JSON digest 用于完整性和可重复性，
不是签名；不能把任意外部提供、仅重新计算 digest 的 JSON 当成执行证据。
