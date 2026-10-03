# tansr-go

tansr 统一 `/api` 合同(RFC-UAPI-1,`unified-v1`)的 Go SDK。
Go SDK for the tansr unified `/api` contract (RFC-UAPI-1, `unified-v1`).

**状态 / Status:骨架(skeleton,UAPI-01 阶段四 U4-GO;U8-GO 对齐 manifest revision 7、三头接线与 D19 统一码异常模型,165 向量金样回放,2026-10-03)。** 标准库实现,零第三方依赖;`api` / `canonical` / `sse` 有实现与测试,`executor` / `archive` 仅接口定义;真实 Serve 联调待主线。
Standard library only, zero third-party dependencies; `api` / `canonical` / `sse` are implemented and tested, `executor` / `archive` are interface definitions only; validation against a real Serve is pending on the mainline.

```
module github.com/cpple/tansr-go   (go 1.25)

api/        统一客户端:发现 / Call / Events / 错误信封 / 重试建议    unified client
canonical/  RFC-UAPI-1 §4 canonical JSON 与域摘要                     canonical JSON + domain digest
sse/        text/event-stream 帧读取                                  SSE frame reader
executor/   执行宿主接口(仅接口)                                     executor host interfaces
archive/    档案消费接口(仅接口)                                     archive consumer interfaces
contract/   事实源副本 + PROVENANCE.json                              vendored fact sources
examples/go-chat  发现 → 建会话 → 围栏 → 发消息 → 读事件流 → 关闭
```

## 七条纪律 / Seven disciplines(手册 §16.6)

1. **不静默降级** — 无 `tansr-contract: unified-v1` → `ErrContractUnavailable`,绝不回退 `/v2`、`/v3`;协商包络未回响 → `ErrEnvelopeNotNegotiated`。
   No silent downgrade.
2. **不从响应取 URL** — 路径恒由 manifest 生成的模板本地实例化;发现体出现导航字段即拒。
   No URLs from responses.
3. **不重放未知副作用** — `RetrySameRequest` 仅在服务端明示 `same-request` 且同键时重放一次;`result_unknown` 只查状态。
   No replay of unknown side effects.
4. **身份不进体** — 鉴权只在 `Authorization` 头;体内身份字段校验失败。
   Identity is not in the body.
5. **SSE EOF / 202 ≠ 完成** — 只有 `EventEnvelope.TerminalStatus` 决定终态。
   EOF / 202 are not completion.
6. **五游标不互换** — `EventCursor` / `ArchiveCoverage` / `OutputWatermark` / `MaterialConsumed` / `AckReceipt` 为不同类型。
   The five cursors are distinct types.
7. **不手写旧前缀** — 唯一的路径事实源是 `contract/api-manifest.json` → `api/operations_gen.go`。
   No hand-written legacy prefixes.

## 用法 / Usage

```go
client, err := api.New(api.Options{BaseURL: "http://127.0.0.1:8787", Token: token, EventEnvelope: true})
closure, err := client.SessionCapabilities(ctx, sessionID)
res, err := client.Call(ctx, api.OpSessionMessageSend, api.CallOptions{
    Params: map[string]string{"id": sessionID}, Body: map[string]any{"text": "hi"}, ClosureID: closure.ClosureID,
})
stream, err := client.Events(ctx, api.OpSessionEventsObserve, api.EventsOptions{Params: map[string]string{"id": sessionID}})
```

### 三头 / Three request heads(manifest r7,U7-HDR)

```go
read, _ := client.Call(ctx, api.OpArchiveBindingGet, api.CallOptions{Params: p})          // read.Meta.ETag = "\"3\""
_, err = client.Call(ctx, api.OpArchiveBindingClose, api.CallOptions{
    Params: p, Body: body,
    IdempotencyKey: "close-7",      // Serve 按键返回同一回执;RetrySameRequest 以同键重放一次
    IfMatch:        read.Meta.ETag, // 仅 Operation.AcceptsIfMatch 的写操作;陈旧 → 412 precondition_failed / refresh
    Deadline:       time.Now().Add(30 * time.Second), // 过期即拒(本地 CodeDeadlineExceeded / 服务端 408),SDK 永不延长
})
```

### 错误 / Errors(D19:统一码为主)

```go
var apiErr *api.APIError
if errors.As(err, &apiErr) {
    switch apiErr.Code {                    // 19 统一码(api.Code*),RetryAction 6 动作(api.Action*)
    case api.CodeCapabilityUnavailable:     // apiErr.Detail.Reason: not_installed | outside_closure
    case api.CodePreconditionFailed:        // apiErr.Detail.Reason: if_match_stale;apiErr.Detail.DomainCode: 族码(次级)
    }
}
errors.Is(err, &api.APIError{Code: api.CodeNotFound}) // 只按统一码匹配
advice := api.Advice(err)                            // RetryAdvice.Action api.RetryAction
```

旧字段 → 新字段对照见 `CHANGELOG.md`。`*api.DomainError`(未包装的族原信封,今日仅 `archive-sync-v1`)与 `*api.ClientError` / `*api.ContractUnavailableError` 边界不变。

## 验证 / Verification

```
gofmt -l .            # 须为空 / must be empty
go vet ./...
go test ./...         # -race 需 CGO / needs CGO
go test ./api -run TestOperationsGenerated     # = 生成器 -check
go run ./internal/gen/manifest2go -check       # 同上(需能启动新编译的可执行文件)
```

许可 / License:见 `LICENSE`(Tansr Proprietary License)。
