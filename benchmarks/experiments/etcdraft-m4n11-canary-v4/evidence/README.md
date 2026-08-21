# Canary v4 审计证据

- `provider-journal.tar.gz`：11 次调用的完整 intent/dispatch/result；不含 API key；
- `execution-evidence.tar.gz`：在线 Bundle、evaluator fresh Bundle、MethodSpec、summary、
  Oracle audit 和 evaluator report；
- `upstream-tests.tar.gz`：该 SUT checkout 的完整上游测试输出；
- `sut-canary.bundle`：Git ref `canary-artifact`，指向实验使用的本地 SUT commit
  `f740123`。可用 `git bundle verify` 检查并在隔离目录中 fetch。

这些文件来自 canary 的独立临时 checkout。两份 Bundle 的 digest 与主 README 中记录的
结果一致；归档不引入新的证据格式或身份规则。
