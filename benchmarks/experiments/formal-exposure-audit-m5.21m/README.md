# M5.21m FormalExposureAudit

`summary.json` 绑定 M5.21l 的公开 synthetic contract fixture、exact opaque view、一份 clean audit 和
一份直接复制 private root-cause atom 的 rejected audit。所有字段由 Go 回归逐项重算。

该审计只能识别已枚举 private atom 在 JSON key/value 中的直接复制，不能证明模型无法从公开协议知识
或行为反馈推断语义，也不替代进程和文件系统隔离。fixture 已公开，不是 private holdout；fresh evaluator
仍未接线，因此 `formal_ready=false`。模型调用和新 SUT execution 均为零。
