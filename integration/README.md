# 真实 Serve 集成验收

此目录是 Go SDK 的开发验收入口。Go SDK、Go demo 和 Go 消费应用不依赖 Node；只有此验收宿主需要已安装依赖的 `tansr-cli` 源码、Node 22 和 Go 1.25。

从 Go 仓根执行：

```powershell
$env:TANSR_GO_SERVE_CLI_ROOT = 'J:/tansr/tansr-cli'
$env:GOCACHE = 'J:/tansr/archive/GO-01-sdk-demo-20261007/go-cache'
go test ./integration -count=1 -v
```

`TANSR_GO_SERVE_CLI_ROOT` 可指向已安装依赖的 CLI 隔离树。可选 `TANSR_GO_SERVE_NODE` 指定 Node 可执行文件。未设置 CLI 路径时，普通 `go test ./...` 会明确跳过本组，不能把跳过记录当作真实 Serve 验收通过。先按仓库合同锁检查 CLI 候选与 Go 的来源；宿主也会检查 manifest revision。

## 真实边界

- 宿主直接启动 CLI 当前实现的 `startServer`、`createAgentSessionFactory`、`createServeArchiveHost` 和 `createServeOffloadArchiveHost`。实际 HTTP 路由、统一 `/api` 调度、围栏、SSE、内核、会话存储、档案分页、摘要校验及 ACK 均执行真实实现。
- 仅平台配置、鉴权身份和模型返回是合成夹具。工具处理器只返回合成业务事实，不执行 shell、文件编辑或外网调用；不读取 `.env`，不消耗模型额度。offload 冷存储使用现有内存 BlobStore，不能据此宣称跨 Serve 进程恢复或生产存储验收完成。
- SDK1 会话和 `sdk2-offload-v1` 会话显式选择，后者使用独立 offload 宿主与原保留 `requestId`，没有通过 legacy 路由降级。
- 每个宿主绑定 `127.0.0.1` 的动态端口，数据写入 Go 的测试临时目录。测试清理先关闭事件流和会话，再通过 stdin 请求宿主关闭并回收子进程。

## 本组断言

| 测试 | 原调用链与断言 |
| --- | --- |
| `TestRealServeSessionWire` | `/api` 创建、能力发现、消息、严格事件信封、正文、明确 `turn.completed` 与 SSE 游标 |
| `TestRealServeSessionClient` | 高层创建、原会话重新附着、历史读取、游标重连、真实中断；中断须以 `aborted` 结束 |
| `TestRealServeExecutorClient` | 执行器注册、心跳、初始化、首次绑定、控制端审批、真实业务工具派工；Go Runner 和 FileJournal 对重复 operation 只执行一次，重复回执继续原模型轮 |
| `TestRealServeArchiveClient/sdk1` | 集中持久会话的 SDK2 档案扩展，真实分页与工件入 Go 加密文件后 ACK |
| `TestRealServeArchiveClient/sdk2-offload-v1` | 显式 offload 会话使用同一 Go 档案链；不是以 SDK1 档案扩展替代 offload 会话 |
| `TestRealServeChatAndArchiveDemos` | 将 `go-chat` 和 `go-archive` 编译为临时可执行文件；聊天创建原会话，档案命令持久保存并以相同文件重开同步 |
| `TestRealServeToolsDemo` | 编译并运行实际 `go-tools` 命令；通过真实声明摘要、注册、绑定、审批、模型工具调用与回执继续，确认命令执行的 `DemoOrderStatus` 回到原会话 |

两个档案子例均在真实 Serve 已提交 ACK 后模拟响应丢失，再关闭并重开本地文件，用持久化的原请求身份续办。断言原 ACK 恢复、正文可读、服务端与终端覆盖一致及同一会话恢复。传输故障只丢弃真实响应，不伪造任何成功回执。

本组不会绕过能力围栏或把未装配能力改报成功。真实 Serve 接口若拒绝合法初始化链，测试会保留明确失败，应修复对应实现后重跑；不能以跳过该链、改走旧路由或私有 handler 替代验收。

Demo 子进程只传入本组的合成短期令牌与合成加密密钥，不读取宿主的令牌文件。命令产物同样仅留在测试临时目录，长期运行的工具命令在验收完成后由测试回收。
