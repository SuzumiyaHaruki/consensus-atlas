# M5.19a：Campaign crash-safe persistence

日期：2026-08-09

状态：完成

## 目的

本阶段只把 M5.19 的内存 checkpoint 链落到协议无关目录，不连接 SUT、Adapter、Agent 或模型。
提交顺序固定为：

```text
recover and validate full chain once
        |
        v
artifact bytes --sha256 match--> durable content-addressed artifact
        |
        v
terminal record + cumulative totals --> durable no-replace checkpoint
        |
        v
read back new checkpoint --> validate previous digest and cumulative totals
```

## 文件协议

```text
campaign/
  config.json
  artifacts/
    <sha256>.artifact
  checkpoints/
    00000000000000000000.json
    00000000000000000001.json
```

- 根目录必须是全新目录，不覆盖已有路径；
- config 和 checkpoint 以临时文件写入、`fsync` 后使用 no-replace link 提交，再同步目录；
- artifact 使用内容摘要命名，存在时只能复用完全相同的 bytes；
- checkpoint 文件名由 sequence 唯一决定，禁止覆盖；
- 同一进程后续提交必须携带已恢复 head，持久化层重新核对磁盘 head 后只追加一项，避免每个 attempt
  都从 sequence 0 重扫而产生 O(n²) I/O；
- 恢复时重新读取 config、全部 checkpoint 和全部 artifact，不信任调用方缓存；
- 普通未知文件、symlink、checkpoint 缺号、摘要错误、identity 漂移或引用缺失均拒绝；
- 进程在 artifact durable 后、checkpoint 提交前停止时会留下合法 orphan artifact。恢复报告它但不把它
  计入 Campaign；同一 bytes 后续可以安全复用；
- 原子写临时文件不进入可信状态。中断残留只作为 pending file 报告，不能影响 head 或 totals。

## 明确边界

1. 当前只支持单 writer；并发 Coordinator 和跨进程锁不在本阶段。
2. 初始化中断可能留下不完整的新目录；不会覆盖或猜测修复它。
3. wall-clock 的实际 deadline enforcement 留给 Coordinator；持久化层只保存已完成 attempt 的
   elapsed 值和机械 stop reason。
4. over-budget attempt 不能提交为有效 checkpoint。未来 provider 必须接收 remaining allowance；若违反，
   整个 Campaign 作为 provider/accounting failure 处理，不能静默漏计后继续比较。
5. orphan/pending 文件不参与覆盖率、PSS、Oracle 或方法成本。
6. durable/no-replace 结论以支持 regular-file `fsync`、directory `fsync` 和 hard link 的本地 POSIX
   文件系统为前提；本阶段不对 NFS、对象存储或 Windows 文件语义作跨平台承诺。

## 验收

- [x] 创建全新目录并恢复 sequence 0；
- [x] artifact digest 对账成功后，sequence 1 才可提交；
- [x] checkpoint 提交后重新打开得到相同 head、totals 和 stop reason；
- [x] 已有目录、checkpoint 覆盖、artifact 内容冲突均被拒绝；
- [x] artifact-only 中断点可恢复并报告 orphan，随后可复用 artifact 完成提交；
- [x] checkpoint/artifact 篡改、缺号、未知文件和 symlink 被拒绝；
- [x] target/spec/config identity 漂移被拒绝；
- [x] 普通全量、vet、受影响包 race 和旧路径审计通过。

## 实现结果

`internal/controlexperiment/campaign_store.go` 实现全新目录创建、完整恢复和增量提交。创建/恢复返回
带私有校验令牌的 `CampaignRecovery`；只有它的 `CommitAttempt` 可以续写，普通 checkpoint 不能绕过
完整恢复前置条件。每次提交核对磁盘 config/head，先 durable 写入 content-addressed artifact，再以
no-replace hard link 提交 checkpoint，并重新读取新 checkpoint 验证 previous digest 和 totals。

恢复扫描完整链和 artifact 集合，报告 orphan/pending，但二者都不改变可信 head。连续提交不重复扫描
历史 JSON/artifact，只验证恢复状态和当前磁盘 head，因此 N 个 attempt 的存储和正常提交 I/O 都保持
O(n)；进程重启时才执行一次 O(n) 全量恢复。

验证结果：

- `go test -count=1 ./...`：通过；`cmd/control-experiment` 97.454 秒；
- `go vet ./...`：通过；
- `go test -race -count=1 ./internal/controlexperiment`：通过，53.893 秒；
- `make audit-race-shards`、`make audit-no-v1`、`make audit-no-retired-experiment`：通过；
- 两份总体规划字节一致，`git diff --check` 通过。

本阶段没有运行 SUT、读取 key 或调用模型，也没有生成新的 benchmark JSON。

完成后仍没有自动多 attempt 循环。下一阶段再增加 deterministic attempt provider 与 remaining-budget
allowance，将 persistence 接到现有唯一 qualified executor 之上。
