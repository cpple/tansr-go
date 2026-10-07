# Go 工具与档案接入

适用版本：`github.com/tansrai/tansr-go v0.3.0`。安装、认证、会话与错误处理先读 [Go 开发手册](Go开发手册.md)。本文面向把 SDK 嵌入自身程序的开发者，说明业务工具、本地档案和恢复的具体接线；合同以 [SDK2 / UAPI 冻结记录](GO-01-SDK2与UAPI合同冻结-2026-10-07.md) 为准。

v0.3.0 从旧 `github.com/cpple/tansr-go v0.2.0` 迁移模块路径，工具、档案和 ACK 行为不变。升级须同步修改 `go.mod` 与全部 imports，不混用两种路径的类型；历史标签保持原样。迁移验收见 [GO-03](GO-03-tansrai开源迁移与发布.md)，MIT 范围见 [NOTICE](../NOTICE.md)。

通信使用 HTTP 请求、SSE 事件和终端协议的 HTTP 分块上传。Go 终端安装业务函数、提供当前设备授权并保存档案；Serve/kernel 负责智能体运行、工具调度、权限裁决、上下文组织和用量。声明平台、持有旧文件或收到派工，都不能替代当前授权。

## 1. 让模型调用 Go 业务函数

`executor.Runner` 当前只执行显式注册的 `tool.invoke`。业务工具声明、声明摘要、本地 handler 必须一致；Serve 的应用策略也须允许这个工具。完整程序见 [go-tools](../examples/go-tools/main.go)，它与 [go-chat](../examples/go-chat/main.go) 连接同一会话，查询合成订单。Go 进程保持运行，才能接收并完成工具派工。

### 1.1 工具声明与结果

以下代码是可放进业务包的函数文件，不是独立的 `main` 程序。它返回传给会话的声明、注册项和 handler。工具参数的数字按 `json.Number` 提供；`parameters` 是项目的 `ClientToolDecl` 形状，不要直接替换成另一套函数调用协议的 JSON Schema。

```go
package docsample

import (
    "context"
    "encoding/json"

    "github.com/tansrai/tansr-go/executor"
)

func OrderTool() (json.RawMessage, executor.ToolDefinition, executor.Tool, error) {
    declaration := map[string]any{
        "name": "DemoOrderStatus",
        "description": "Read the status of sample order DEMO-001; this is demonstration data.",
        "parameters": map[string]any{
            "orderId": map[string]any{"type": "string", "description": "Sample order ID: DEMO-001"},
        },
        "readOnly": true,
    }
    digest, err := executor.DefinitionDigest(declaration)
    if err != nil {
        return nil, executor.ToolDefinition{}, executor.Tool{}, err
    }
    raw, err := json.Marshal(declaration)
    if err != nil {
        return nil, executor.ToolDefinition{}, executor.Tool{}, err
    }
    definition := executor.ToolDefinition{Name: "DemoOrderStatus", DefinitionDigest: digest}
    tool := executor.Tool{DefinitionDigest: digest, Handle: func(ctx context.Context, args map[string]any) (any, error) {
        if err := ctx.Err(); err != nil {
            return nil, err
        }
        order, ok := args["orderId"].(string)
        if !ok || len(args) != 1 {
            return nil, &executor.Rejected{Code: "EINVAL"}
        }
        if order != "DEMO-001" {
            return map[string]any{"status": "error", "message": "演示订单不存在"}, nil
        }
        return map[string]any{
            "status": "ok",
            "content": []any{map[string]any{"t": "text", "text": "DEMO-001：待发货（演示数据）"}},
        }, nil
    }}
    return raw, definition, tool, nil
}
```

将返回的 `raw` 放入 `session.CreateOptions.ClientTools`（类型 `[]json.RawMessage`）创建会话；附着已有会话时，它必须已声明同一工具。`DefinitionDigest` 使用冻结的客户端工具摘要规则，不能用 `json.Marshal` 后直接 SHA256 代替。该方法只接受其已实现的声明子集，如可打印 ASCII 参数键和安全整数控制字段；不支持的声明会明确失败。普通业务参数中的小数和负数不因此被禁止。

结果区分如下：

| handler 结果 | 执行含义 |
|---|---|
| 合法 `status: "ok"` / `content` | 业务工具产生结果，执行回执为 `completed` |
| 合法 `status: "error"` / `message` | 工具正常返回了业务错误，执行仍有确定结果 |
| `&executor.Rejected{Code: ...}` | 宿主明确证明尚未产生副作用，执行回执为 `failed` |
| 普通 error、panic、非法结果 | 不能证明副作用未发生，执行回执为 `unknown` |

真实订单接口需检查订单归属、业务权限及返回字段，不能仅用模型给出的订单号查询所有用户。`readOnly: true` 是声明，不会把实际写操作变成只读。handler 应尊重 `ctx` 的取消；Runner 无法安全强杀一个忽略取消的 Go 函数。

### 1.2 注册、初始化、绑定、启动

按照同一顺序接线，具体参数见 [go-tools 的 run 函数](../examples/go-tools/main.go)：

1. 取得当前已认证的 `executor.Scope`，包含 `ApplicationScopeID`、`EndUserID`、`AuthorizationRevision`。这些字段用于交叉校验，最终权限由令牌与 Serve 决定，不能由未登录用户自报。
2. `executor.NewClient(transport, scope)` 创建执行客户端；在宿主配置的绝对私有目录调用 `executor.NewFileJournal`，并在退出时 `Close`。
3. 构造 `executor.Registration`：`Protocol: executor.Protocol`、已授权的 `ExecutorID`、`Platform: executor.CurrentPlatform()`、逻辑 `Workspaces`、`Operations: []string{"tool.invoke"}` 与上述 `Tools`。`Workspace` 的 ID/Revision 是逻辑边界，不是开放磁盘路径。平台应描述正在执行 handler 的 Go 进程，不是远端 UI 或 Serve 机器。
4. `executor.NewRunner(executor.RunnerOptions{...})` 传入 `Client`、`Registration`、`Journal`、以工具名为键的 `Tools` 和必填 `Authorize`。`Authorize` 核对会话、scope、预期绑定及宿主当前登录/撤销/资源权限；只返回 `nil` 不能作为生产授权实现。SDK 会在耐久认领前和真正执行前分别调用它。
5. `runner.Connect(ctx)` 注册执行器并获得连接及租约；读取 `transport.SessionCapabilities(ctx, sessionID)`，将返回的 `ClosureID` 传给 `client.Initialize(ctx, sessionID, platform, toolNames, closureID)`。
6. 初始化后重新读取会话 closure，再执行 `client.Bind(ctx, sessionID, connection, workspace, initialized.CapabilityRevision, closureID)`。记录返回的完整 `Binding` 供 `Authorize` 逐次核对；确认目标工具在 `EffectiveTools` 中 `Available == true` 后才能告知 UI 已就绪。
7. 调用 `runner.Run(ctx)`，它负责心跳、派工轮询、串行执行和提交回执。UI 可通过同一会话发送消息；停止时取消 context 并等待 Run 返回，再关闭 journal。

连接过期、绑定变化、策略撤销或 handler 失败应回到业务层处理。不要丢弃旧 journal、重新铸造操作 ID 或改用 Serve 宿主执行以绕过错误。若使用低层 `runner.Execute`，它只执行并产生回执，**不会自动续租或提交回执**；调用方须维护租约并用 `client.Submit` 提交。

### 1.3 控制端凭据与执行器凭据

`RunnerOptions.Status` 默认调用 `client.Status`，对应控制端授权的 `execution.status`。如 Go 进程只持受限执行器票据，应由控制端先协商该会话的 terminal 绑定，再把 Status 回调接到 `client.ExecutorStatus(ctx, sessionReference, connection, operation)`；其中 `sessionReference` 是 `executor.TerminalSessionReference`，`connection` 应从 `runner.Connection()` 读取当前值。

`ExecutorStatus` 使用受限的 `terminal.execution.state`，不会提升票据、代替协商或自动回退到控制端接口。自定义 Status 返回也会重新核对 schema、摘要、scope 和原操作。分离控制端和执行端时，应同时将初始化/绑定放到真正持有控制权的一端，不能让受限票据照搬整个单进程 Demo。

### 1.4 执行日志与未知副作用

`FileJournal` 先耐久保存操作认领，再执行 handler，最后保存回执。重复派工若已有回执，直接使用原回执；若只留下认领而没有可确认结果，则返回未知，避免重复扣款、发货等副作用。相同操作 ID 携带不同摘要会被拒绝。回执提交失回应查询原操作，不会重新调用 handler。

该 journal 是私有目录内的明文执行记录，不是加密层或 OS 沙箱；目录和备份须由宿主保护。Windows 上的 `File.Sync` 与进程重启恢复已有验证，但不能宣称完整断电耐久 SLA。如业务要求交易级断电恢复，应提供满足 `executor.Journal` 的事务存储，在 `Claim` 和 `Complete` 中兑现原子性及耐久性，保留未知状态，不能定时删除未完成认领再重跑。

## 2. 工具输出分块到 Serve

`executor.OutputWriter` 为**已派发的单个操作**传输观测字节，使用 `terminal-services-v1` 的 `execution-stream-v1` 能力。宿主先完成 terminal 协商并验证执行绑定，再将真实 `Session`、`Operation`、`ExecutorID`、`ConnectionID` 和协商得到的 `OutputLimits` 传给 `executor.NewOutputWriter`；填写这些 ID 本身不能建立授权。

| 方法 | 使用方式 |
|---|---|
| `Stdout()` / `Stderr()` | 返回可并发写入的 `io.Writer`，供宿主已有的受控输出源连接 |
| `Capture(channel, bytes)` | 捕获 stdout/stderr 字节；默认 `binary`，不擅自修复编码或跨块 UTF-8 |
| `Snapshot()` | 观察待发送、捕获、丢弃、截断和失败信息 |
| `Finish(ctx, truncated)` | 固定最终 seal，等待输出确认；写管道成功不能替代这一屏障 |
| `Reconcile(ctx)` | 对账同一操作的输出水位；水位缺口、摘要错误、未知 ACK 均需保留并上报 |
| `Abort()` | 取消输出上传，不代表业务工具已被终止 |

上传异步进行，待发送上限包括编码后的请求体及在途块。容量耗尽后保留连续前缀、标记截断并继续排空输出源，避免因日志阻塞生产进程；因此 pipe 的 Write 成功不等于每个字节均保留。重试围绕原批次和水位，不能在 `ErrOutputGap` 后重新执行工具或从零冒充续传。

v0.3.0 的业务 Runner **没有自动连接 OutputWriter**，也未附带跨平台 shell/PTY 实现、任意本地命令执行器或其权限 UI。输出 writer 已有合同级 HTTP 测试，不能把它描述成三个 Demo 已完成真实 shell 流联调。需要本地进程工具的宿主须另实现受控执行、取消和资源边界，不能把远端指令直接交给 `os/exec` 并宣称获得了完整 Node/Electron 体验。

## 3. 在终端保存完整档案

`archive` 保存校验后的档案记录、原始正文与附件，并在持久化后 ACK；它不在 Go 端重新实现上下文组装。SDK1 与显式 offload 两族均通过统一 `/api` 接入，`sdk1` 是会话族名称，不要求访问旧 `/v2` 路由。仅把会话族改为 `sdk2-offload-v1` 并不能完成 Source 生命周期接线。

### 3.1 建立绑定与打开文件

首次接入先检查 `client.BindingTarget(ctx, sessionID)`：已有 `BindingID` 时显式调用 `client.Binding`；没有绑定时使用新的请求 ID 调用 `client.Create(ctx, sessionID, stableSourceID, requestID)`。Create 建立单个授权 Source，要求 `archive-transfer-v1` / `split-receipts-v1`，可选协商 `context-materials-v1`。未知创建结果须查询原操作，不能换新请求 ID 新建替代绑定。需要跨进程恢复首次创建请求时，使用 `Capabilities`、`BindingTarget`、`CreateBinding` 显式保存完整 `RequestIdentity`；`CreationOperation` 查询需要原 request ID 和 operation epoch。

以下是**打开既有绑定并同步一次**的可嵌入函数文件。`path` 必须指向宿主预先创建的绝对私有目录中的文件；`key` 是宿主密钥库取得的 32 字节密钥；`authorize` 必须检查当前认证/撤销状态和文件所绑定的应用、用户及 Source。身份取自认证请求返回的 binding/status，不能从旧文件恢复为授权。

```go
package docsample

import (
    "context"
    "errors"

    "github.com/tansrai/tansr-go/archive"
)

func SyncArchivePage(
    ctx context.Context, client *archive.Client,
    bindingID, path, requestID string, key []byte,
    authorize func(archive.Identity) error,
) (archive.SyncResult, error) {
    if authorize == nil {
        return archive.SyncResult{}, errors.New("current host authorization is required")
    }
    binding, err := client.Binding(ctx, bindingID)
    if err != nil {
        return archive.SyncResult{}, err
    }
    status, err := client.Status(ctx, bindingID)
    if err != nil {
        return archive.SyncResult{}, err
    }
    identity, err := archive.IdentityFrom(binding, status)
    if err != nil {
        return archive.SyncResult{}, err
    }
    store, err := archive.OpenFileStore(archive.StoreOptions{
        Path: path, Key: key, Identity: identity, CheckAccess: authorize,
    })
    if err != nil {
        return archive.SyncResult{}, err
    }
    result, syncErr := archive.SyncOnce(ctx, client, store, requestID)
    return result, errors.Join(syncErr, store.Close())
}
```

实际常驻程序可以复用同一个 FileStore，退出时关闭；同一文件持有跨进程排他锁。`CheckAccess` 会在读取前后、提交前及传输关键边界复查，不能在回调中再次调用该 store 造成重入，也不能以文件中保存的身份作为当前权限。用户退出、切换账号、撤销授权时应立即让回调拒绝后续访问。

FileStore 使用 AES-256-GCM，密钥不写入档案。宿主负责密钥生成、保管和备份关系；不能把示例环境变量作为所有部署的安全密钥方案。Unix 目录权限与 Windows ACL 应分别配置，Windows 的 chmod 位不能代替 ACL。使用物理私有目录；符号链接目录、错误密钥、损坏或不匹配身份都应拒绝，不能“自动修复”为删除原文件重建。

### 3.2 同步、ACK 与完成条件

`SyncOnce` 每次只处理一页或一个待决 ACK，不会无限追赶。正常顺序是读取当前 binding/status → 检查身份和代际 → 验证档案链与附件原始字节 → `Store.Receive` 原子保存所有内容及原 ACK → 提交 ACK → 校验并持久保存 completed 回执。正文不能经过 JSON 重编码后再拿旧摘要上传。

| 值 | 表示什么 |
|---|---|
| `store.Head()` | 本地保存到哪个记录及摘要 |
| `store.Pending()` | 尚未确认的原 ACK，必须保留原身份和正文 |
| `store.Coverage()` | 已校验 completed 回执后确认的档案覆盖 |
| `SyncResult.Recovered` | 本次解决了既有待决 ACK，不意味着所有新页已同步 |
| `SyncResult.Complete` | 本次读到的发布快照已覆盖；不代表活动会话以后不再产生记录 |

应用在 `Complete == false` 时可按自定的页数/时间上限继续调用；网络失回应保留同一文件、密钥、绑定。存在 pending 时，新的 `requestID` 不会替换已保存 ACK；SyncOnce 会先重放原 ACK。SSE 游标与档案 coverage、ACK receipt、工具输出 watermark、材料 consumed 分别管理，不能相互推进。

### 3.3 明确处理旧修订 ACK

某些合法时序下，ACK 准备后绑定修订被 Serve 收尾推进。不要自行修改 pending ACK 的 `ExpectedRevision`。v0.3.0 提供显式恢复：

```go
package docsample

import (
    "context"

    "github.com/tansrai/tansr-go/archive"
)

func RecoverArchiveAck(
    ctx context.Context, client *archive.Client,
    store archive.RecoveryStore, recoveryRequestID string,
) (archive.SyncResult, error) {
    return archive.RecoverPending(ctx, client, store, recoveryRequestID)
}
```

这是供应用在展示恢复决策后调用的函数，不应包在“遇到任意错误就恢复”的循环中。`recoveryRequestID` 必须是新的、全局唯一的应用请求身份。函数先重放原 ACK；只有明确的 stale If-Match 响应才允许在原 epoch 准备恢复意图，具体新修订由 Serve 决定。首次 `PrepareRebase` 会原子保存旧 ACK 和恢复身份，并把 FileStore 内部格式从 `tansr-go-archive-v1` 升级为 `tansr-go-archive-v2`，预留确认容量；旧 SDK 不能再打开 v2 文件。

恢复意图一旦保存，失回、重启后必须续用它；后续传入的新身份不会覆盖旧意图。`SyncOnce` 能续接**已经存在**的恢复意图，但不会自动创建新的 rebase 或默认升级格式。busy、过期、撤销等响应不会证明恢复成功，也不会丢弃意图。保留原文件和密钥，根据返回原因恢复当前服务/授权条件或核查原回执；不能反复换 ID 试到成功。

[go-archive](../examples/go-archive/main.go) 提供 `-recover-ack REQUEST_ID`，只处理已有 `-binding` 和已有 `-file` 的待决 ACK，完成后退出；随后不带该参数重新运行才继续同步新页。已知重复身份本地会被拒绝，但 v1 文件未保存所有历史请求，调用方仍须保证新身份不与旧操作冲突。

### 3.4 容量与自有存储

FileStore 默认上限为 4,096 条记录、16,384 个对象、64 MiB 逻辑总量、8 MiB 单批量，每次事务重写有界加密快照。逻辑总量不等于磁盘配额，密文/JSON 编码有开销；它不适合无限增长档案，不能仅调大数字绕过实现上限。

开发者可实现 `archive.Store` 接入自有数据库或存储。必须同时实现下列语义，而不是只实现方法签名：

- `Identity` 和 `StorageLimits` 在 store 生命周期保持不变，`CheckAccess` 查询当前宿主授权。
- `Receive` 校验连续链、正文与附件，原子耐久保存整页、所有字节和原 ACK；失败不得留下一个可发送却未持久化的 ACK。
- `Pending`、`Head`、`Coverage` 分离；`Confirm` 验证原操作、scope、摘要及 completed 回执后才推进 coverage。
- `ReadRecordsByID` 和 `Body` 只提供本授权 Source 的精确原字节，不接受请求中的任意路径/URL，不以部分缺失冒充成功。
- 跨进程并发、容量预留、事务异常和崩溃恢复需要适配器自行验证。需要显式 ACK rebase 时额外实现 `archive.RecoveryStore`，原子保存不可变恢复意图及确认映射，不能原地改写旧 ACK。

Go FileStore 是本 SDK 的介质，不是 Node SQLite 数据库的直接读取器；接相同 wire 合同不等于可以互换两者磁盘文件。

## 4. 核心按需取回材料

档案保存在终端后，Serve 可按合同请求具体材料；核心仍决定哪些记录进入上下文、如何压缩和组织。宿主负责接收当前 binding 的材料请求、核对身份并保存请求状态，随后调用：

`archive.RespondMaterials(ctx, client, store, request, identity)`

这里 `request` 是 `archive.MaterialRequest`，`identity` 是已持久保存的 `archive.RequestIdentity`。函数只读取请求中精确指定且已校验的记录/附件，在请求的 TTL 和字节上限内分块上传；它不会自动创建常驻事件监听器或完整离线恢复队列。

返回的 `MaterialResult.Response` 保留原材料响应，提交失回时用于原身份对账；宿主须保存恢复所需事实，不能每次重试生成新 request ID。`client.MaterialStatus` 查询材料请求状态；`client.SubmitMaterials` 收到的 `202` 与 `state: "received"` 只证明 Serve 收件，**不是**核心已经使用材料。只有核心按合同确认 consumed 后，才推进材料消费状态；不得因为上传成功就删除终端档案或声称上下文已恢复。

## 5. 档案与记忆本地化的能力边界

会话历史/摘要档案、长期记忆、配置、工具执行日志是不同对象。加密保存聊天档案不会自动把 Serve 的全部记忆迁移到终端，也不会取代原有 Serve 自身持久化或开发者提供的存储。

| 当前需求 | v0.3.0 已提供 | 宿主仍须承担或后续补齐 |
|---|---|---|
| 保存会话档案并按需回传 | `archive.Client`、FileStore、SyncOnce、RespondMaterials、显式 ACK 恢复 | UI、生命周期调度、密钥与授权、容量治理 |
| 在设备运行显式业务函数 | Runner、Journal、连接与绑定、验证和回执 | 业务函数及资源权限，未知副作用的业务对账 |
| 读取/命令管理记忆 | 通用 API 操作 `OpTerminalMemoryRead`、`OpTerminalMemoryCommand`、`OpTerminalMemoryReceipt` | 当前部署的 terminal/memory 协商、对应请求/回执与本地介质适配 |
| 全量记忆发布与本地保存 | 冻结合同/通用 API 入口保留 | Go 高层 memory publication 执行器、记忆存储、同步/删除/恢复编排；业务 Runner 不承接保留的 MemoryPublication profile |
| 多端同档案、备份、保留策略、高级缓存 | 可通过已冻结通用操作进一步实现 | v0.3.0 没有这些完整高层工作流或 Demo，不能从81操作目录推定已自动具备 |

通用操作必须经 `api.Client.Call` 和生成的操作常量，不手写旧前缀或遇错切换合同。更详细的围栏、幂等、错误和会话使用方式见 [Go 开发手册](Go开发手册.md)。三个可运行示例是 [go-chat](../examples/go-chat/main.go)、[go-tools](../examples/go-tools/main.go)、[go-archive](../examples/go-archive/main.go)；它们展示既有固定范围，原 `v0.2.0` 三平台实跑证据见 [GO-02](GO-02-三平台运行验收与公开发布.md)，新模块的验收与发布证据见 [GO-03](GO-03-tansrai开源迁移与发布.md)。
