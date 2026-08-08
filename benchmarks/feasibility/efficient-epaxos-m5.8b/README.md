# efficient/epaxos M5.8b message-port witness

本目录冻结一个 test-only 边界实验，不包含生产 Adapter，也不授予 Control Runtime capability。

固定目标：

- source commit: `791b115669fca472d3136f6a2eda46c00b3f8251`
- source tree: `708b8e37f6b50a4e6b6fcbb95045dc53ad7d6344`
- probe SHA-256: `511659094ee53280fe3f64b4ad8ee6806177e576cedc86ad88b68d50bd04f235`
- frozen result SHA-256: `48cc649460b2641535ee863b879f65f30fdc957e17ad8c8c18241aa2bc978013`

实验直接调用官方 `genericsmr.Replica.SendMsg`，消息使用官方 `epaxosproto.Commit` codec。外围 writer
先按 codec 组装完整帧，再产生 route/sequence/content-bound stable ID，并在选择 release/drop 前阻塞
`SendMsg` 返回。程序以 race detector 运行，并逐字节比较 `result.json`。

结果：55 字节小帧对应一次底层 Write；5,139 字节大帧对应两次 Write。按 Write chunk 调度会破坏
消息边界；codec-aware assembler 能将大帧恢复为一个稳定 item，release 转发完整 5,139 字节，drop
转发 0 字节，相同大帧的 ID 保持一致。

复验需要预先取得固定官方源码，且不能修改该 checkout：

```bash
make probe-efficient-epaxos-message-port EPAXOS_GOPATH=/path/to/efficient-epaxos-gopath
```

Make 目标会先检查 commit/tree、tracked 文件零修改和 `src/` 下零未跟踪 Go 文件。实验没有运行完整三节点 EPaxos、没有验证自动构造器的无竞争安装、
没有接入 Control Runtime，也没有证明 strict replay、yield、clock、restart 或协议正确性。
