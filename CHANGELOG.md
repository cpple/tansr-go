# Changelog · tansr-go

本仓无版本号 / 标签(由主线在收编时决定);条目按泳道登记。事实源对齐以 `contract/PROVENANCE.json` 为准。

## 未发布 · U8-GO(2026-10-03,对齐 tansr-cli main `64df76b2`,manifest revision 7)

### 破坏性变更(D19 统一码为主的公开异常模型,用户拍板,0.14.0 批次)

`*api.APIError` 以统一码(`code` 19 码表)+ `retryAction` 为主字段;族码退为 `Detail.DomainCode`。`errors.Is` / `errors.As` 边界不变:`*APIError`(统一信封)、`*DomainError`(未包装族信封,今日仅 `archive-sync-v1`)、`*ClientError`(本地)、`*ContractUnavailableError`(非 unified-v1 Serve)。

| 旧(≤ `870d098`) | 新 | 说明 |
|---|---|---|
| `APIError.Code string` | `APIError.Code ErrorCode` | 19 常量 `api.Code*`(`CodeInvalidRequest` … `CodeNotCanonical`);`ErrorCodes []string` 为词表。`CodePayloadTooLarge` 为无类型常量,同时可作 `ErrorCode` / `ClientErrorCode` |
| `APIError.RetryAction string` | `APIError.RetryAction RetryAction` | 6 常量 `api.Action*`(`ActionNone` / `ActionSameRequest` / `ActionQueryStatus` / `ActionRebind` / `ActionRefresh` / `ActionRediscover`);`RetryActions []string` 为词表 |
| `APIError.Detail map[string]any` | `APIError.Detail ErrorDetail` | 结构字段 `Domain` / `Family` / `DomainCode` / `DomainStatus` / `DomainRetryAction` / `Fallback` / `Reason` / `Header` / `LimitBytes` / `ClosureID` / `Operation` / `State`;`Raw map[string]any` 保留原 detail;`Present()` 表示信封携 detail |
| `APIError.DomainCode()` | `APIError.Detail.DomainCode` | 族码降为次级字段;取值方法删除 |
| `APIError.DomainStatus()` | `APIError.Detail.DomainStatus` | 同上 |
| `APIError.DomainRetryAction()` | `APIError.Detail.DomainRetryAction` | 同上 |
| `apiErr.Code == "not_found"` | `apiErr.Code == api.CodeNotFound` 或 `errors.Is(err, &api.APIError{Code: api.CodeNotFound})` | `APIError.Is` 只按统一码匹配(目标的其它字段须为零值) |
| `ErrorEnvelope.Code / RetryAction string` | `ErrorCode` / `RetryAction` | wire 结构同步定型 |
| `RetryAdvice.Action string` | `RetryAdvice.Action RetryAction` | 与 `api.Action*` 直接比较 |
| `DomainRetryActionMap map[string]string` | `map[string]RetryAction` | 族动作词 → 统一动作 |
| (无) | `api.Reason*` 17 常量 | `detail.reason` 细因词表(`not_installed` / `outside_closure` + 15 个请求头细因,如 `if_match_stale` / `deadline_exceeded` / `idempotency_key_reused`) |
| (无) | `ClientErrorCode`:`CodeInvalidIfMatch` / `CodeIfMatchNotApplicable` / `CodeInvalidDeadline` / `CodeDeadlineExceeded`;哨兵 `ErrDeadlineExceeded` | 三头本地守卫 |

迁移要点:`switch apiErr.Code { case api.CodeCapabilityUnavailable: … }` 是跨 SDK 的同一分支结构;需要族内细分时读 `apiErr.Detail.DomainCode` / `apiErr.Detail.Reason`。`DomainError` 不在统一码模型内,`api.Advice()` 仍把其族动作词映射到统一动作。

### 三头接线(manifest r7 / U7-HDR,零新码)

- `CallOptions.IfMatch string`:取上次读的 `Meta.ETag`(强形 `"<revision>"`,裸十进制上线时加引号);只对 `Operation.AcceptsIfMatch()` 的写操作放行(manifest `expectedRevision` 非 null,9 个操作),否则本地 `CodeIfMatchNotApplicable`;弱校验器 / 通配符本地 `CodeInvalidIfMatch`。
- `CallOptions.Deadline time.Time`:以 RFC 3339 UTC 上线;`Deadline ≤ now` 本地 `CodeDeadlineExceeded` 不出网;`RetrySameRequest` 在 `now + wait ≥ Deadline` 时拒绝且不休眠。`Options.Now` 可注入时钟。
- `Meta.ETag string`:只收强形;`HeaderIfMatch` / `HeaderETag` / `HeaderDeadline` 常量;`NormalizeIfMatch` / `FormatDeadline` 与 Node `./api` 同形。
- `IdempotencyKeyOf` 改按族 `requestIdPath`(`Operation.RequestIDPath()`,`FamilyRequestIDPaths`)取体内键:`sdk2-ext-v1` → `request.requestId`;`agent-session-v1` 无体内位,只认 `Idempotency-Key` 头(此前对任意族读取顶层 `requestId` 的行为移除)。
- `Operation` 新增 `ETagPath` / `ExpectedRevision{Path, Kind}` / `Versioned()` / `AcceptsIfMatch()`;生成器承载 r7 三头事实字段。

### 事实源对齐(r7)

- `contract/api-manifest.json` sha256 `f6255bc868b4de5c…`(revision 7,81 操作 / 77 围栏,新增 `approval.credential.submit` → `OpApprovalCredentialSubmit`);`unified-v1.schema.json` `fd07962916f7662e…`;`unified-v1.golden.json` `42531a3937563d8c…`(165 向量 = 41 正 / 124 负);cross-vectors / sdk2-wire 未变。
- `api/schema.go`:`KeyPath`、`reason` 17 枚举(门面错误仍只允许 2 个)、`expectedRevision` 只许写操作且 kind ∈ `sequence|integer`、`precondition_failed.retryAction ∈ {rediscover, refresh}`、`limitBytes` 整数 ≥ 1、旧路径前缀 `^/v[123]`。

### 测试

- `api/golden_test.go` 锁 165 / 41 / 124(EventEnvelope 7 / 18),按名锁 6 个 r7 向量;`api/replay_test.go`(A29)165 向量经假 Serve 走 `/api` 全量回放 + 能力交集 74 操作越界恒 `capability_unavailable`;`TestThreeHeads` 覆盖 412 / 409 / 408 与同键回执重放。
