# 真实 Serve 集成验收

此目录是 Go SDK 的开发验收入口。Go SDK、Go demo 和 Go 消费应用不依赖 Node；只有此验收宿主需要已安装依赖的 `tansr-cli` 源码、Node 22 和 Go 1.25。

从 Go 仓根执行：

```powershell
$env:TANSR_GO_SERVE_CLI_ROOT = 'J:/tansr/tansr-cli'
$env:GOCACHE = 'J:/tansr/archive/GO-01-sdk-demo-20261007/go-cache'
go test ./integration -count=1 -v
```

`TANSR_GO_SERVE_CLI_ROOT` 可指向已安装依赖的 CLI 隔离树。可选 `TANSR_GO_SERVE_NODE` 指定 Node 可执行文件。既未设置 CLI 路径、也未设置下述便携宿主时，普通 `go test ./...` 会明确跳过本组，不能把跳过记录当作真实 Serve 验收通过。先按仓库合同锁检查 CLI 候选与 Go 的来源；宿主也会检查 manifest revision。

## 三端同源便携宿主

Windows、Linux 和 macOS 可以共用同一份构建产物，无需把 CLI 整仓和 `node_modules` 复制到每台机器。先在安装好 CLI 开发依赖的机器构建；`--source-commit` 必须是审定的完整 SHA，CLI 的已跟踪文件必须干净：

```powershell
node integration/build-serve-fixture.mjs --cli-root J:/tansr/tansr-cli --source-commit 3a5ba4f87b387b375e2b5b86bfceeda2c6f6db54 --out J:/tansr/archive/GO-02-runtime-release-20261007/fixture/candidate
```

产出 `serve-fixture.mjs` 和 `serve-fixture.provenance.json`。构建器先核对 Go 合同锁的 39 份来源文件，复用 CLI 单文件 ESM 构建配置，将原夹具确定引用的十二个模块替换成静态源导入，再将其真实依赖一起打包。档案 helper 原本按 `import.meta.url` 读取的冻结 schema 使用完全相同 UTF-8 字节内嵌，仍由原 `JSON.parse` 和原默认值提取逻辑处理。运行时外部依赖只允许 Node 内建模块；不存在用假实现代替内核、SQLite、HTTP 路由或权限逻辑的打包分支。

来源记录包含 CLI commit/tree、构建脚本与原夹具指纹、实际输入文件及构建文件哈希、冻结资源哈希、esbuild/Node 版本和产物 SHA-256。复制两份产物到各端并核对同一 SHA 后执行：

```powershell
$env:TANSR_GO_SERVE_FIXTURE = 'C:/test-host/serve-fixture.mjs'
go test ./... -count=1 -v
```

```sh
TANSR_GO_SERVE_FIXTURE=/tmp/test-host/serve-fixture.mjs go test ./... -count=1 -v
```

`TANSR_GO_SERVE_FIXTURE` 显式优先于源码路径。宿主在全新测试临时目录启动，不从 CLI 工作目录补找模块。原集成断言和三个实际 Go Demo 保持相同，只有宿主加载方式不同。接收机器只需合适的 Node 与 Go，不需要 tsx 或 npm 安装；Go SDK 和 Demo 的用户仍然不依赖 Node。

便携宿主包含私有核心源码，仅用于获授权的内部验收；**不得提交或发布进 Go SDK、公开 release 或标准库下载产物**。构建器拒绝输出到 CLI 或 Go 源码仓内，并拒绝覆盖已有产物。

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
| `TestRealServeArchiveRebase` 的两族子例 | 第一轮档案落盘为原 ACK，第二轮真实收尾推进修订；原 ACK 明确冲突后显式恢复，丢弃真实 rebase 成功响应，重开文件并按原恢复身份取得回执，覆盖只推进已落盘部分 |
| `TestRealServeChatAndArchiveDemos` | 将 `go-chat` 和 `go-archive` 编译为临时可执行文件；聊天创建原会话，档案命令持久保存并以相同文件重开同步 |
| `TestRealServeToolsDemo` | 编译并运行实际 `go-tools` 命令；通过真实声明摘要、注册、绑定、审批、模型工具调用与回执继续，确认命令执行的 `DemoOrderStatus` 回到原会话 |

两个原档案子例在公开会话状态为 idle 后开始同步，将真实 Serve 已提交 ACK 后的响应丢失与收尾修订变化分开验证，再关闭并重开本地文件，用持久化的原请求身份续办。独立 rebase 子例故意制造真实旧修订冲突，验证恢复映射、原身份重放及覆盖不超前。两组均断言正文可读与服务端/终端覆盖一致。传输故障只丢弃真实响应，不伪造任何成功回执。

本组不会绕过能力围栏或把未装配能力改报成功。真实 Serve 接口若拒绝合法初始化链，测试会保留明确失败，应修复对应实现后重跑；不能以跳过该链、改走旧路由或私有 handler 替代验收。

Demo 子进程只传入本组的合成短期令牌与合成加密密钥，不读取宿主的令牌文件。命令产物同样仅留在测试临时目录，长期运行的工具命令在验收完成后由测试回收。

## 显式 terminal-persistence-v1 真实链（PST-05）

`TestRealServeTerminalPersistencePermanentKeysAndReopen` 复用同一夹具的 `persistence` 模式、公开 `/api`、Go `terminalpersistence` Store/Host 和加密 execution journal。旧 `publication` 模式及旧专用合同保留；新模式只登记 `TansrTerminalPersistenceV1` 的冻结摘要，不使用 ready 中业务工具的 `definitionDigest`。

```powershell
$env:TANSR_GO_SERVE_CLI_ROOT = 'J:/tansr/tansr-cli'
$env:GOMAXPROCS = '2'
go test ./integration -run '^TestRealServeTerminalPersistencePermanentKeysAndReopen$' -count=1 -timeout 180s -v
```

该单条链完成真实 pin、已提交原回执的显式消费与归档、归档后永久双键查找、原请求重放和次键冲突，随后关闭原会话及两份介质，以 `reopen` 打开并绑定新会话，再查原键。原请求重放不得产生新的 begin/put/commit。首次真实 commit 执行回执在 Serve 返回 HTTP 200 后被运输层丢弃，Runner 查询同一 operation/digest；关闭重开后的 journal 仍返回同一 receipt，原 transfer 查询仍给出原结果。这是已受理 ACK 失回恢复，不冒称终态 unknown 的新绑定恢复或 Serve 进程重启。

stdin `TANSR_GO_CONTROL` 只用于内部验收宿主的 `facts`、`settle-publication`、`set-publication-mode` 和 `archive-receipts`。归档控制必须指定现存 idle session、非空原 operationIds 和 1–256 的显式 limit；实际数量不得超过 limit。缺省不代消费，未消费的 committed 回执原样拒绝。测试明确传 `consume: true` 后，宿主先核原状态、同生命周期和来源，再调用原 `source.consume` 与 `source.archiveReceipts`。不新增 HTTP 路由、不改造历史回执、不冒称模型或最终用户自动消费。测试模型调用数断言为 0。

先通过 `settle-publication` 等待原初始化/命令后台资源释放，再发新的写命令；不能以重试绕过 busy。旧会话全部 ended 且资源收尾后，才显式选择下一会话的 `reopen`。两份原介质不删除、不自动迁移、不开空替代。公开 Store 的原 transfer 查询和真实 HTTP 的原 execution 状态分别核验，不能互相代签。

源码加载模式适用于本地开发；便携包仍要求精确 HEAD、tracked clean、原 39 份锁和新增两份机器资产字节一致。新增 lifecycle accessor 仅在构建器确切 reviewedModules 集合内放行，未放松动态导入或清洁提交门。该宿主依旧是内部源码夹具，不是 npm 三包公开消费。此单条也不替代 257/513、4 MiB、全平台或完整门禁。
