# RFC-UAPI-1 — 统一信封、错误矩阵与 canonical 单模块

状态：**v1.0-rc（2026-10-01 02:05 用户授权代拍会签通过，裁定见方案 §0 D7：canonical 入 `packages/protocol/src/canonical/` 双入口、统一码 19、`ticketId`、协商头 `tansr-event-envelope`、Q1/O1、音频面围栏；§6 各项按 D7 结案，正文回填随阶段三收编进行）；v0.3 = 2026-09-30 围栏接线后按实现回填 §1.4 并纳入泳道 B/C/E 结论；v0.2 = 17:29 按用户确认的审查建议修订** · 日期：2026-09-30 · 归属：UAPI-01 阶段二／三 · 方案：[UAPI 开发方案](../report/UAPI-统一API合同-开发方案-2026-09-30.md) §3.2、§3.6、§4
会签栏：protocol（L0）□ · server □ · api-client □ · providers/kernel（三份现有编解码器持有方）□ · 用户拍板 □

本 RFC 只定义 `unified-v1` 的公共层；不改任何现有 `additionalProperties:false` schema 的字段，不改 `/v2`、`sdk2-ext-v1`、cache、terminal 各族 wire 字节。`packages/protocol` 受 `contract-v0` 冻结约束，本 RFC 会签前不落任何 protocol 代码。

## 1. 统一信封（Common Envelope）

### 1.1 响应元数据（阶段二已实施，本节定版）

全部 `/api/*` 响应（含 SSE 首帧、错误响应）携带：

| 头 | 值 | 语义 |
|---|---|---|
| `tansr-contract` | `unified-v1` | 统一合同修订 |
| `tansr-manifest-revision` | 整数 | `packages/server/contract/api-manifest.json.revision` |
| `tansr-domain` | `discovery / session / execution / archive / archive-sync / cache / terminal / terminal-observation / terminal-profile` | 受理域 |
| `tansr-schema-hash` | `sha256:<hex>` \| `none` | 受理域合同族的源 schema SHA256（discovery = 聚合 `schemaHash`） |

头内恒不含 `endUserId`、`sessionId`、令牌、内部文件路径。

### 1.2 请求侧统一头（`tansr-session-family`、`tansr-closure-id` 阶段二；`Idempotency-Key` / `If-Match`+`ETag` / `deadline` 已接线（0.14.0，`packages/server/src/api/request-headers.ts`，语义按下表既定文本实现、错误码不新增——D27））

| 头 | 语义 | 映射 |
|---|---|---|
| `Authorization` | 已有 | 不变 |
| `traceparent` / `x-request-id` | 已有（观测 requestId） | 不变；与业务 `requestId` 分离 |
| `Idempotency-Key` | 写操作幂等键（ASCII 1–128） | 门面映射到域 `requestId`（SDK2/cache 族体内 `request.requestId` 缺席时填入；`/v2` 族本 RFC 起对 `POST /api/sessions/:id/messages`、`/inputs` 以墓碑窗去重，语义 = 现有 `receiptTombstones`）；作用域 = 应用域 + 操作名 + 合同修订，TTL 由服务端声明 |
| `If-Match` / `ETag` | 带资源版本的变更 | 映射到各域 `expectedRevision`（档案绑定 revision、终端配置 revision、缓存 mapping revision）；不匹配 → 412（域码 `revision_conflict`/`stale_revision` 保留于 `detail.domainCode`） |
| `deadline`（RFC‑3339） | 请求截止 | 过期请求 → 408/`invalid_request`；**重试不得延长 `deadline`**；`retryAfterMs` 受 manifest 上限约束 |
| `tansr-session-family` | `sdk1` \| `sdk2-offload-v1`（可选） | 选择会话族；缺省取部署首选族；未装配族 → 404 `capability_unavailable` |
| `tansr-closure-id` | 围栏前置条件（可选） | 与当前 `closureId` 不等 → 412 `precondition_failed`（`detail.domainCode: closure_stale`，`retryAction: rediscover`） |

身份字段（`applicationScopeId`、`endUserId`、`authorizationRevision`）**恒不接受客户端自报**（RFC‑SDK2‑1 §9.1）；上述头只表达意图与前置条件。

### 1.3 门面自有错误信封（本节定版；实现中 `requestId` 字段改名 `traceId` 随阶段二接线落地）

```json
{ "contract": "unified-v1", "traceId": "<观测 x-request-id>", "requestId": null, "code": "<unified code>", "status": 404, "retryAction": "none", "message": "…" }
```

`traceId` 为观测关联；`requestId` 恒指客户端幂等键，门面自有错误无请求体故为 `null`。领域错误在阶段三统一时同样携带两者。

### 1.4 能力闭合围栏（阶段二已实施：`packages/server/src/api/{closure,closure-sources,facade}.ts`）

- `GET /api/sessions/:id/capabilities`（`tansr-domain: discovery`）返回 `{ contract, closureId, authorizationRevision, domains, operations }`，响应头 `tansr-closure-id = closureId`；`closureId = SHA256(UTF8('tansr.unified.closure.v1') ‖ 0x00 ‖ canonical({authorizationRevision, domains, operations}))`（canonical = 内核严格编码；对象只含固定 ASCII 键与字符串/布尔/null 值），不含时间，同输入同 id。
- `domains[d] = { installed, revision }`：`revision` 为该域自报代际（执行 = `executionBoundary.revision`、档案 = `operationEpoch.id`、终端 = `schemaRevision`；其余 null）。`operations` 键集 = manifest 操作目录去 4 条部署级发现读（`discovery.*`、`session.capabilities`），当前 76 条；三态 `enabled | disabled | unavailable`。
- 三态语义：`unavailable` = 域未装配，或域已装配但对本会话无能力源（SDK1 会话未进入执行宿主、句柄无输入通道）；`disabled` = 有能力源但当前不放行（工具不可用、终端 feature 非 `installed`、档案/缓存能力令牌缺席、会话已结束/关闭中的会话内写操作）；围栏不复制各域状态机的细粒度 409/422 转移。
- 取源纪律：归属先于取源（未知 / 他人 / 不在注册仓的存储会话 → 404 `not_found`，不区分）；只读（不 `refresh`、不续租、不铸造 epoch；GET 恒不铸造）；跨 await 以末态复核归属；能力源读取失败 → 503 `upstream_unavailable`（不以 null 冒充“无源”静默降级）。
- 写前置与复核（`/api/sessions/:id/**` 非 GET，客户端可选携 `tansr-closure-id`）：鉴权 → 归属 → 重新取源 → 比对；不等 → 412 `precondition_failed`（`retryAction: rediscover`，`detail: { domainCode: 'closure_stale', closureId }`，响应头回传当前 id）；相等 → 按 manifest 操作目录（含 `aliases`）把 (method, path) 映射到操作：`disabled` → 403、`unavailable` → 404，码同为 `capability_unavailable`，`detail: { reason: 'outside_closure', operation, state, closureId }`；目录外子路径交域 handler。域未装配的 404 仍为 `detail.reason: not_installed`。该头出现在 GET 或非会话内路径 → 400 `invalid_request`（显式协商，不静默忽略）。**不携头的写操作不做围栏复核**（避免逐请求读策略源 / 档案 / 缓存能力），域 handler 仍是最终权威；携头时门面鉴权一次、域 handler 再鉴权一次（仅此路径双计）。
- 围栏是联邦视图（执行边界 revision、档案 epoch、终端会话级 feature 三态、INJ 能力、缓存 feature、会话状态、授权代际），不替代各域逐请求鉴权 / epoch / lease 复核；`platformCapabilities`（平台能力位）不联邦，阶段三与 `session.audio.*` 一并处理。

### 1.5 统一事件包络（阶段三实施，仅新 SDK `./api` 子入口消费；旧 SSE 帧字节不变）

**D18 定形（用户授权代拍，2026-10-01，方案 §0 D18）**：wire 形为 **7 键**、键序固定，服务端 `packages/server/src/api/event-envelope.ts`、`unified-v1.schema.json` `EventEnvelope` 与 api-client `src/api/envelope.ts` 三方同形（`v1.0-rc` 原样例的 8 键形废止）：

```json
{ "contract": "unified-v1", "eventId": "12", "domain": "session", "type": "msg.text.delta", "cursorSet": { "eventCursor": "12", "archiveCoverage": null, "outputWatermark": null, "materialConsumed": null, "ackReceipt": null }, "terminalStatus": null, "raw": { "type": "msg.text.delta", "sessionId": "s-1", "seq": 12, "ts": 1700000000000, "text": "hello" } }
```

- 协商：请求头 `tansr-event-envelope: unified-v1`，服务端回响同名头确认；未回响 = 未协商，客户端不得自行假设（§6 第 8 条）。缺席时门面零介入，帧字节与旧前缀逐字节相同。
- `contract`：恒 `unified-v1`（与 §1.3 错误信封同律，信封自述合同修订）。
- `eventId`：= 本帧 SSE `id:` 原样（缺席 → `null`，不合成、不继承上一帧）；与 `cursorSet.eventCursor` 同源——前者是帧身份，后者是 `Last-Event-ID` 续订游标位。**不出 `seq`**：流位置由 `cursorSet.eventCursor` 表达，各域自有序号仍在 `raw` 内（内核 `raw.seq`、档案 `raw.cursor`、终端 `raw.eventId`）。
- `domain`：门面路由表所归域（= 响应头 `tansr-domain`）；`type`：SSE `event:` 字段，缺席取 `raw.type` / `raw.eventType`，无法判定 → `null`（视为未知事件）。
- `cursorSet` 五位分离，不得互换为普通字符串；各位只在自身合同条件满足后推进（SSE EOF、HTTP 202、连接成功均不推进）。`archiveCoverage` 在 wire 上为 `archive.status.payload.acknowledgedCoverage` 的 Coverage 对象（schema `ArchiveCoverage`）；取值来源逐域登记于[接线报告 §3.2](../report/UAPI-统一事件包络接线-2026-10-01.md)。
- `terminalStatus ∈ { null, accepted, completed, aborted, unknown }`；`accepted` 只表示受理；仅原事件明确表达终态时非 null。它裁定的是**被观察的操作／流**的终态，不替客户端判定业务单元：会话域 `turn.completed` → completed、`turn.aborted` → aborted、**`turn.error{recoverable:false}` → aborted**（`/v2` 契约里不可恢复错误即本轮终止；内核径随后还出 `turn.aborted`，两帧同为终态、幂等；serve 驱动器内部失败径只出这一帧）、`session.ended` → completed（事件流随会话结束而完成——收一轮的客户端若在 `turn.completed` 之前见到它，须按“本轮未完成”处置，不得把它当轮完成）；可恢复 `turn.error` 与控制帧 `server.*` → null。各域取值表见[接线报告 §3.2](../report/UAPI-统一事件包络接线-2026-10-01.md)（主线 2026-10-01 增补 `turn.error{recoverable:false}` 一行）。
- `raw`：**恒为原事件对象**（原 `data:` 文本原样拼入，服务端不 parse→stringify 往返，字节不改），按 `type` 解析；未知 `type` 仍原样入 `raw`，SDK 类型层暴露 `UnknownEvent`。**不出 `payload`**（不再有“已知 type 解析后 / 未知 type 原文”二选一位）。控制信封严格：七键之外 / 缺键 / `contract` 不等 → `invalid_envelope`（api-client），不丢帧不崩；不放宽 SDK2 控制信封校验。

## 2. 错误矩阵

### 2.1 现状（2026-09-30 实测）

| 族 | 信封键 | 码数 | `retryAction` 词表 |
|---|---|---|---|
| `/v2` | `error.{code,message,detail?}`；429/503 携 `Retry-After` 头 | 38 | 无 |
| `sdk2-ext-v1` | `protocol,requestId,code,status,message,retryAction,retryAfterMs?` | 23 | `none, same-request, query-status, rebind` |
| `sdk2-cache-v1` | 同上 + 必填 `fallback∈{none,legacy-cold}` | 17 | `none, same-request, query-status, refresh-projection` |
| terminal 三面 | `contract,requestId,code,status,retryAction`（无 message） | 20 | `none, discover, refresh, reconcile, backoff` |

### 2.2 统一码（17 + 门面自有 2 = 19）与映射

统一错误信封 `code` 取下表左列；原族码保留于 `detail.domainCode`，原族 `retryAction` 保留于 `detail.domainRetryAction`，cache `fallback` 保留于 `detail.fallback`（不进统一层：统一合同禁止静默降级）。HTTP 列为**统一缺省**（生成器 `UNIFIED_ERROR_CODES` 取单值：invalid_request 400、conflict 409、stale_generation 409、gap 409、capability_unavailable 404（越围栏时门面覆盖为 403）、capacity_exceeded 429、result_unknown 503）；各族 wire 的原状态在阶段二不变，阶段三统一信封落地时以缺省出线、原状态留 `detail.domainStatus`。

| 统一码 | HTTP | `/v2` | `sdk2-ext-v1` | `sdk2-cache-v1` | terminal |
|---|---|---|---|---|---|
| `invalid_request` | 400/422 | validation_failed, invalid_json, midturn_blocks_rejected, checkpoint_import_invalid, cwd_invalid | invalid_request, invalid_coverage | invalid_request | invalid_request |
| `protocol_mismatch` | 400 | — | protocol_version_mismatch | protocol_version_mismatch | protocol_version_mismatch |
| `unauthorized` | 401 | unauthorized | unauthorized | unauthorized | unauthorized |
| `forbidden` | 403 | forbidden, cwd_not_allowed | forbidden | forbidden | forbidden |
| `not_found` | 404 | session_not_found, not_found, call_not_found, request_not_found, checkpoint_not_found, fork_source_not_found | — | mapping_unavailable | not_found |
| `method_not_allowed` | 405 | method_not_allowed | — | — | — |
| `gone` | 410 | session_ended, call_expired, request_expired | binding_closed, request_expired, receipt_expired, stream_cursor_expired | ticket_expired, mapping_expired, receipt_expired | — |
| `conflict` | 409/412 | call_already_resolved, request_already_resolved, turn_running, session_mismatch, digest_mismatch, resume_unavailable, cwd_unavailable | binding_conflict, request_id_conflict | stale_revision, request_id_conflict | binding_conflict, revision_conflict, request_conflict, busy, integrity_mismatch |
| `stale_generation` | 409 | — | stale_generation | stale_generation, projection_stale | stale_generation, context_transition_required |
| `gap` | 409/410 | — | archive_gap, stream_gap, stream_cursor_unknown | — | output_gap |
| `capability_unavailable` | 404/403 | capability_disabled, checkpoints_not_wired, cwd_switch_unsupported, platform_unavailable | unsupported_capability | unsupported_capability | unsupported_capability |
| `capacity_exceeded` | 429/503 | session_limit_exceeded, rate_limited, overloaded, draining | capacity_exceeded | capacity_exceeded | capacity_exceeded, request_limit |
| `payload_too_large` | 413 | payload_too_large | payload_too_large | payload_too_large | payload_too_large |
| `upstream_unavailable` | 503 | upstream_unavailable | source_unavailable, epoch_unavailable, capability_unconfirmed | epoch_unavailable | source_unavailable, capability_unconfirmed |
| `result_unknown` | 503 | — | — | result_unknown | commit_unknown |
| `rejected` | 422 | — | material_rejected | — | — |
| `internal_error` | 500 | internal_error, create_failed, store_corrupted | internal_error | — | — |

覆盖核对：`/v2` 38/38、sdk2 23/23、cache 17/17、terminal 20/20，无孤儿码；生成器 `scripts/api/generate-error-map.mjs`（阶段一，写区 D，已落 revision 2）把本表制成 `packages/server/src/api/error-map.generated.ts` 并以 `--check` 守住四族码表变化。归类裁定（2026-09-30 主线）：`capability_unconfirmed` 是瞬态“扩展能力未确认”（sdk2 503／query-status，RFC‑SDK2‑1 §2.3），归可恢复的 `upstream_unavailable`；`binding_closed` 归 `gone`（资源已关闭，wire 原 409 留 `detail.domainStatus`）；未知域码归 `internal_error`/500，但域已明示且可映射的 `retryAction` 仍透传，域动作亦未知才 `none`。归类订正（2026-10-01 主线，error-map revision 3）：`/v2` 的 `resume_unavailable`（store 未接线／需要可信 Store 的运行时检查点／需要归属对账）与 `cwd_unavailable`（存储 cwd 目录已失）由 `upstream_unavailable` 改归 `conflict`——二者是部署／归属／环境条件而非上游瞬态故障，“同一请求稍后重试”无益；统一出线保留 wire 409、`retryAction: none`，原码照留 `detail.domainCode`。

门面自有码（不属四族映射；D7 会签定稿）：`precondition_failed`（412；`detail.domainCode ∈ {closure_stale, revision_conflict}`，`retryAction: rediscover`，围栏前置与 `If-Match/ETag` 共用——**今日只出 `closure_stale`**：`If-Match/ETag` 尚未接线（§1.2），`unified-v1.schema.json` `FacadeErrorDetail.domainCode` 枚举据实只含 `closure_stale`，接线时随 schema 变更一并加 `revision_conflict` 并补 golden）、`not_canonical`（400；`retryAction: none`；严格入口 `parseStrict` 拒收 wire 时使用，`detail` 携首个违规 `code/offset/path`）。**统一码总数 = 17 + 2 = 19**；生成器 `UNIFIED_ERROR_CODES` 与 `unified-v1.schema.json` 的 `UnifiedError.code` 枚举以此为准（四族映射表不含这两码，它们只由门面 / 严格解析入口产生）。

### 2.3 统一 `retryAction`（6）与映射

| 统一动作 | 语义 | 来源映射 |
|---|---|---|
| `none` | 不可重试 | 各族 `none` |
| `same-request` | 同请求（同 `requestId`/`Idempotency-Key`）重放；`retryAfterMs` 在场时须等待 | sdk2/cache `same-request`；terminal `backoff` |
| `query-status` | 先查回执/状态再决定 | sdk2/cache `query-status`；terminal `reconcile` |
| `rebind` | 重新建立 binding/lease | sdk2 `rebind` |
| `refresh` | 重读修订（配置/投影）后重试 | cache `refresh-projection`；terminal `refresh` |
| `rediscover` | 重新能力发现后重试 | terminal `discover` |

未知副作用（`result_unknown`，含 cache `result_unknown` 与 terminal `commit_unknown`）恒只允许 `query-status` 或 `rebind`，不得换键重放。`/v2` 族无 `retryAction` 位，恒取统一码缺省（capacity_exceeded／upstream_unavailable → same-request，stale_generation → rebind，result_unknown → query-status，其余 none）。

## 3. 字段消歧

| 现状 | 统一合同 | 处置 |
|---|---|---|
| `/v2` ⑩⑪ 路径 `:requestId` = 服务端签发的权限/提问票据 | `ticketId` | wire 不变；SDK `./api` 类型层与手册改名 |
| SDK2/cache `request.requestId` = 客户端幂等键 | `requestId` | 统一语义；`Idempotency-Key` 头映射（§1.2） |
| `endUserId` `/v2` `^[\x21-\x7E]{1,128}$` vs SDK2 LegacyId ≤512 标量 | 取宽（LegacyId） | `/v2` wire 校验不变；统一层不因宽窄不一拒绝 |
| `revision` 五用 | `bindingRevision`(Sequence) / `configRevision`(integer) / `boundaryDigest`(Digest) / `manifestRevision`(int) / `authorizationRevision`(Sequence) | 类型层分名；wire 不变 |
| ACK 状态 camel（RFC §4）vs kebab（wire §7/§9.5） | wire 恒 kebab：`received`, `durably-stored`, `core-consumed` | RFC-SDK2-1 §4 文案回填 |
| `operationEpoch` 仅 sdk2/cache 铸造 | 统一层可空 | terminal/USDK 域填 `null`，不伪造 |

## 4. canonical 单模块（`packages/protocol/src/canonical/`）

### 4.1 规则（= `SDK2-ext-v1-wire.md` §5，定版为唯一规范文本）

- 对象键按 UTF-16 码元升序、无空白；键实际 **ASCII-only**（三份产品实现与参考实现对非 ASCII 键一致拒收，泳道 E 交叉向量锁定），故键序即字节序；
- 数字 token 仅 `0 | [1-9][0-9]*`，值 ≤ 2^53−1；拒 `-0`、`1e3`、`1.0`、`01`、`Infinity`、`NaN`；
- 字符串逐标量输出，不做 Unicode 归一化；拒孤立代理项、拒非法 UTF-8 输入；
- 所有层级重复键（含 `\u0061` 等转义解码后判重）拒绝；`__proto__` 等按自有属性处理，不触发原型；
- 深度 ≤ 32（按祖先数计，根为 0）、节点 ≤ 100 000（含标量）、单文档字节上限由调用方传入且作用于**原始输入字节**（非解码后）；
- 作用域：控制 DTO 与记录元数据；IR 正文、业务工具参数（`argsJson/resultJson`，允许小数、32 KiB/32 层）、附件字节**不套用**，其摘要恒为 `SHA256(rawBytes)`，禁止 parse→stringify。

### 4.2 域摘要

`digest = SHA256(UTF8(domain) ‖ 0x00 ‖ bytes)`。本 RFC 新增域名：`tansr.unified.closure.v1`（§1.4）。现有域名恒不改：`payload.v1`、`record.v1`、`operation.v1`、`source-snapshot.v1`、`execution.v1`、`client-tool.v1`、事件库 `event-scope/life/cursor/fact/batch/key.v1`；HMAC 域 `cache-operation.v1`、`effective-projection.v1`、`provider-prefix.v1`、`provider-request.v1`、`cache.core.v1`、`cache.core.lookup.v1`、`runtime-continuity.{signature,grant}.v1`。`client-tool.v1` 现走 kernel `canonicalStringify`（另一套规范化）→ 会签后迁入单模块并以现有金样锁字节。

### 4.3 收敛路径

| 现有实现 | 处置 |
|---|---|
| `packages/kernel/src/archive/record-codec.ts` `encode/decodeArchiveMetadata` | 改为单模块薄封装（保留导出名与错误码） |
| `packages/providers/src/twp/cache-control-json.ts` `encode/decodeCacheControl` | 同上 |
| `packages/api-client/src/sdk2/wire-codec.ts` `encode/decodeControl`（纯 Web） | **订正（泳道 U3-PROTO 实测，2026-10-01）**：`decodeControl` 为 canonical-only（≡ `parseStrict`），kernel / providers 的 `decodeArchiveMetadata` / `decodeCacheControl` 才是归一化接收（≡ `decodeCanonical`）——此前 v0.3 表述相反。19 类分歧仍属入口语义差异而非规则分歧，双入口定义不变：`parseStrict`（严格 canonical：服务端收 wire、摘要复算、api-client 解码）与 `decodeCanonical`（归一化解码：kernel / providers 现语义）。api-client 是零依赖发布包（`pack-api-client.mjs` 拒绝任何 `dependencies`），接法按方案 §0 **D17**：以生成式 vendoring 从 `packages/protocol/src/canonical/**` 产出 `packages/api-client/src/sdk2/canonical.generated.ts` 并以 `--check` 锁字节，不加运行时依赖；providers 的 64 KiB 上限保留为调用方传入值 |
| `scripts/sdk2/draft-wire-codec.mjs` | 保留为参考实现，加入交叉向量测试 |
| `packages/kernel/src/journal/hash.ts`、`packages/tansrd/src/runs/hash.ts`（旧式 JSON 往返排序） | 不迁：非 wire 域，登记为“非 canonical 域摘要”，禁止新增消费 |

适用边界（主线 2026-10-01，三端复验登记）：双入口只约束**定义了摘要或字节承诺的 wire**（SDK2 控制信封、档案元数据、缓存控制、`closureId` 的输入等）。统一错误信封、门面自有信封与其它 `/api` JSON 响应体以插入序普通 JSON 出线，**不是 canonical 字节**，也不是任何摘要的输入；客户端按 `decodeCanonical` 级归一化读取，不得字节比对。

验收：三份薄封装 + 参考实现对 `scripts/sdk2/fixtures/sdk2-wire-v1.json` 全部正/负向量字节一致；交叉向量集 `scripts/api/fixtures/canonical-cross-vectors.json`（写区 E，≥40 向量：重复键转义、`-0`、`1e3`、孤立代理项、非法 UTF-8、深度 33、节点上限+1）入 `pnpm test`，分歧显式登记为会签输入；C#/Go/Android/iOS/鸿蒙对同一向量集对照（阶段三～五）。

## 5. 不做

- 不新增 `/v4`、不改 URL 版本段；
- 不把 `fallback` 引入统一层；
- 不改任何现有 schema 字段、不删任何域 `retryAction`；
- 不在会签前触碰 `packages/protocol`。

## 6. 会签事项结案（2026-10-01 用户授权代拍，方案 §0 D7）

1. §2.2：17 统一码命名与 HTTP 缺省**通过**；`capability_unconfirmed → upstream_unavailable`、`binding_closed → gone`、未知域码 retryAction 透传三项归类**通过**；§1.4 三态以 403/404 分义与“不携头不复核”边界**通过**；门面自有 `precondition_failed`、`not_canonical` 进统一码表 → 19 码。
2. §2.3：`backoff → same-request`、`reconcile → query-status` 等价**通过**（`same-request` 须遵守 `Retry-After`/`retryAfterMs` 且同键）。
3. §3：统一层票据段命名 **`ticketId`**（wire 内仍 `requestId`）；观测模板 `/api/sessions/:id/{permission,questions}/:ticketId` 与 manifest `apiEntries` 占位随下一次 manifest 再生改名（阶段三主线）。
4. §4.3：`client-tool.v1`（kernel `canonicalStringify`）**独立批**迁入，以现有金样锁字节；本批三份产品实现改薄封装。
5. §4.3：双入口定名 **`parseStrict`**（服务端收 wire、摘要复算、`closureId` 等）与 **`decodeCanonical`**（客户端容错读）；`encodeCanonical`、`domainDigest` 同模块导出；模块落 `packages/protocol/src/canonical/`，以 `@tansr/protocol/canonical` 子入口导出，`@tansr/protocol` 0.1.0 → 0.2.0；`packages/protocol/**` 对本项解冻，其它改动仍冻结。
6. 泳道 B Q1：wire 503 胜，RFC‑SDK2‑1 §9.5 文案回填（不改 wire）；O1：`request_id_conflict`（`conflict`）与 `binding_closed`（`gone`）分属两码，阶段三接线以红例锁定。
7. §1.4：`platformCapabilities` 只以会话句柄音频面（`platformAudio.capabilities`）进围栏（`session.audio.transcribe/speak` 令牌），其余平台能力位不联邦。
8. §1.5：事件包络协商头 **`tansr-event-envelope: unified-v1`**，服务端回响同名头确认；未回响 = 未协商，客户端不得自行假设。
