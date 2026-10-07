# SDK2-ext-v1：S1 HTTP、SSE 与字节编码合同

任务：SDK2-01b。日期：2026-09-17。ACK修订：split-receipts-v1，已独立复核，合同／wire 74项通过。状态：开发协议草案；不是已发布产品能力。与[主 RFC](RFC-SDK2-1-兼容SDK1的扩展协议.md)、[独立 schema](sdk2-ext-v1.schema.json)共同会签；不修改 SDK1 的路径、JSON、IR、事件、认证或默认持久化。

对应金样：[sdk2-wire-v1.json](../../scripts/sdk2/fixtures/sdk2-wire-v1.json)；草案参考编码器：[draft-wire-codec.mjs](../../scripts/sdk2/draft-wire-codec.mjs)；测试：[sdk2-wire-encoding.test.mjs](../../scripts/test/sdk2-wire-encoding.test.mjs)。编码器只在脚本目录消费，产品不得据此提前宣告扩展支持。结构验证须使用操作对应的具名 schema，不能对根 anyOf 验过一个别的 DTO 就受理。

## 1. 传输共约

- 基路径唯一为 `/v3/sdk2`，协议值唯一为 `sdk2-ext-v1`。认证与当前资源授权沿可信宿主配置，不从 OS、请求体、query 或来源标识取得权限。每次查询、写操作及 SSE 重连均复验授权。
- GET 不接收请求体。除 capabilities 外，GET 都须有扁平 query `protocol=sdk2-ext-v1`；capabilities 没有 query。不得使用 GET 创建绑定、epoch、上传对象或延长期限。
- POST 无 query，JSON 请求体包含 `protocol`。S1 控制 JSON 使用 UTF-8、无 BOM；`Content-Type: application/json`，若声明 charset 只能是 UTF-8。请求 `Content-Encoding` 只接受缺省或 identity。非法媒体类型／编码返回 400 `invalid_request`，不套用新限制到旧 API。
- 请求控制体上界为当前 `controlBytes`，且不超过 256KiB；判断原始 UTF-8 字节，不按 JS 字符数。原始 request-target 不超过 8KiB。JSON 响应为 UTF-8、无 BOM；控制响应不超过控制帽，ArchivePage 按 `pageBytes`、ArtifactChunk 按其单独上界。附件下载块二进制至多 256KiB，base64 响应按至多 349528 字符加有界信封核算，不错误套用 256KiB 请求控制帽。
- 未列出的 query、重复键、空必填值、互斥资源参数同时出现、path 与 body 的资源 ID 不一致，均返回 400 `invalid_request`。协议值不匹配返回 400 `protocol_version_mismatch`。必须先校验长度/编码/结构，再执行认证范围内的资源查找和语义校验；没有副作用的查询也不能跨主体泄漏存在性。
- JSON 错误体始终为独立 `ErrorResponse`。GET、分块上传或非法请求尚无合法业务 requestId 时，由宿主生成仅用于诊断的 ID；它不是操作 ID、不创建幂等账本。合法写请求／操作查询的错误可关联原 requestId。

## 2. 不透明路径 ID 与扁平 query

调用者对每个不透明路径 ID 独立执行 `encodeURIComponent`，再拼固定路径；不能对整条路径编码。路由器先按**尚未解码的 `/`** 切分路径段，再对动态段百分号解码恰好一次，拒绝畸形 `%`、非法 UTF-8 和孤立代理项。代理与路由库不得提前展开 `%2F`、重复解码、大小写折叠或 Unicode 归一化。

| 原 ID | 原始单 path 段 | 单次解码所得 |
|---|---|---|
| `a/b` | `a%2Fb` | `a/b`，仍是一个 ID |
| `汉😀` | `%E6%B1%89%F0%9F%98%80` | `汉😀` |
| `%2F` | `%252F` | 字面值 `%2F`，不得再变成 `/` |
| `100%+` | `100%25%2B` | `100%+` |

百分号转义的十六进制大小写可接收，但发送金样使用大写。原资源是否为合法会话仍按旧资源校验决定；路径解码不创造新会话 ID 能力。对会被客户端／代理消解的特殊路径段，部署必须使用保留原始 request-target 的路由能力；不能通过替换 ID 静默访问另一个资源。

GET 的 query 键是下表所列的**未转义 ASCII 名字**；值独立用 `encodeURIComponent` 编码。空格只写 `%20`，字面 `+` 写 `%2B`，不接受 form-urlencoded 的 `+` 空格规则。值只解码一次。不接受嵌套 JSON、点号键、方括号键或未知别名。字段顺序接收时不敏感，金样发送顺序固定如下：

| 组 | query 次序 | 还原 DTO |
|---|---|---|
| Q | `protocol` | `protocol` |
| G | `historyEpoch`, `deletionGeneration`, `projectionRevision` | `generations` 的三个同名字段 |
| R | `operationEpoch`, `requestId` | `request.operationEpoch`, `request.requestId` |

修订／代际／档案序号始终保持十进制字符串，不经 IEEE754 Number。数量／字节／offset 只接收 `0` 或非零开头的十进制整数字面量，转成安全整数后再按 schema 和协商限值核验；`01`、`-0`、`1.0`、`1e3`、空串一律拒绝。`afterSequence=null` 的唯一 query 表达为**省略该键**；字符串 `null`、空键值不是别名。

## 3. 操作与 DTO 映射

以下路径省略 `/v3/sdk2`。path 字段还原到 DTO 同名字段；POST body 中仍存在的同名字段必须逐字符一致。常规错误按 schema 的 code/status 映射；成功状态在下表固定。API 暂未实现时不得返回伪成功或用旧路径代办。

| 方法／路径 | query 或 JSON body | 成功响应 |
|---|---|---|
| GET `/capabilities` | 无 query/body | 200 `CapabilitiesResponse` |
| GET `/sessions/:sessionId/binding-target` | Q；path sessionId → `BindingTargetRequest` | 200 `BindingTargetView` |
| POST `/bindings` | `BindingCreateRequest` | 201 `BindingView` |
| GET `/bindings/:bindingId` | Q；bindingId 只读定位 | 200 `BindingView` |
| GET `/bindings/:bindingId/events` | Q；游标仅 HTTP header | 200 独立 SSE；未开流错误见 §4 |
| GET `/bindings/:bindingId/archive/records` | Q, G, `afterSequence?`, `limit`, `maxBytes` → `ArchiveReadRequest` | 200 `ArchivePage` |
| GET `/bindings/:bindingId/archive/artifacts/:artifactId` | Q, G, `offset`, `maxBytes` → `ArtifactReadRequest` | 200 `ArtifactChunk`；不使用 HTTP Range 另一套语义 |
| POST `/bindings/:bindingId/archive/acks` | `ArchiveAckRequest` | 200 `MutationReceipt`；资源最新状态另查 archive/status |
| GET `/bindings/:bindingId/archive/status` | Q；bindingId 只读定位 | 200 `ArchiveStatus` |
| POST `/bindings/:bindingId/materials/:materialRequestId/uploads/:artifactId/chunks` | `MaterialUploadChunkRequest` | 200 `MaterialUploadStatus`，仅本次既有材料请求范围 |
| GET `/bindings/:bindingId/materials/:materialRequestId/uploads/:artifactId` | Q；三个 path ID → `MaterialUploadStatusRequest` | 200 `MaterialUploadStatus` |
| POST `/bindings/:bindingId/material-responses` | `MaterialResponseRequest` | 202 `MaterialReceipt`，表示受理，不表示核心消费 |
| GET `/bindings/:bindingId/materials/:materialRequestId` | Q；两个 path ID → `MaterialStatusRequest` | 200 最新 `MaterialReceipt` |
| POST `/bindings/:bindingId/close` | `BindingCloseRequest` | 200 `MutationReceipt`，关闭意图完成不等于资源已 closed |
| GET `/operations`（创建绑定） | Q, `operation=binding-create`, `sessionId`, R → `OperationStatusRequest` 第一分支 | 200 `MutationReceipt` |
| GET `/operations`（已有绑定） | Q, `operation`, `bindingId`, R → `OperationStatusRequest` 第二分支 | 200 `MutationReceipt` |

普通写操作的成功重放保留原 requestId、操作身份和原受理结果；资源的后续变化通过只读 View／Status 查询。不得把未知结果变成新 ID 重试。分块上传按已发材料请求＋artifact＋offset 自然幂等，不另造无限请求账本；重复同块保持同 uploadId，不重复计量/写入，响应可给同一对象的当前单调接收状态。异体块冲突，不能覆盖。

`POST /bindings` 的 201 响应与原成功重试均给已保存的创建结果，之后查询 BindingView 才取得当前状态；查询丢失创建回执不需要预知 bindingId。`GET /operations` 的 `outcomeRef` 定位已受理结果所属的绑定/材料对象；它不是 bearer token，查询仍使用认证和明确资源路由。

新档案记录从 sequence `1` 连续递增，`0` 不产生档案记录；空档案 head／覆盖使用 schema 的 null，不伪造第 0 条记录。历史 revision 或删除代际的 `0` 保持各自语义。分页中的 `complete` 仅指本次发布头快照已经读完；新的捕获不被当成此前分页承诺的一部分。缺口不得返回“空页且 complete”。

## 4. 独立 SSE 帧与恢复

响应 `Content-Type: text/event-stream; charset=utf-8`、`Cache-Control: no-store`；部署关闭代理缓冲。发送 UTF-8、无 BOM、LF 行尾；接收器兼容 CRLF。每个数据帧恰好一条 id、event、data 行，data 是单行规范编码的完整 `EventFrame`：

```text
id: <EventFrame.cursor>
event: <EventFrame.eventType>
data: <完整 EventFrame JSON>

```

`EventFrame.eventId` 保留于 data，不作为 SSE id；SSE id 恒为独立扩展 cursor。header、data 中 cursor/type 必须完全一致；先验证具名 schema、绑定、代际与事件语义，再保存“已校验 cursor”。不得因收到一个高 cursor 推进档案 ACK。未知必需事件、字段错配或非法帧停止扩展，不放入旧 KernelEvent 或 stdout。

心跳只使用 `: heartbeat\n\n`，无 id、不推进 cursor、不改变材料期限。S1 不使用 `retry:` 行或其他带业务语义的 SSE 字段。未知心跳注释可忽略。参考编码器只验证一个完整数据帧；实际流分块与 UTF-8 跨块解码在产品接线时另测。

重连仅使用一个 `Last-Event-ID` header；字段名按 HTTP 大小写规则处理，值按 Id 原字节保持，不 trim。空值、多 header、逗号合并值、CR/LF 或 query `cursor`／`after`／`lastEventId` 等歧义输入返回 400。每次仅重放所给 cursor **之后**的完整帧，然后接实时流。客户端不应让未经 schema 验证的浏览器自动游标代替自己保存的已校验位置。

未给 Last-Event-ID 的初次连接在同一排他边界取得当前流头并接实时流；服务端把当前 archive/status、binding/status 以及仍有效的材料请求重新通知写入同一有界扩展日志。重发保留原 materialRequestId／见证／原期限，只生成新的通知 cursor，不新建材料操作或续期。注册观察与状态通知之间不能丢失新事件；连接与重建也受应用／主体／宿主资源帽控制。

| 场景 | 开流前 HTTP / code | 客户端动作 |
|---|---|---|
| cursor 无效、伪造、不是该绑定的有效游标或无法识别 | 409 `stream_cursor_unknown` | 停止自动重连，查询该绑定档案/材料状态，显式重建观察 |
| 可验证属于该绑定但已超出留存下界／已退休流代际 | 410 `stream_cursor_expired` | 按状态重建；不得声称旧事件全部读完 |
| 有效游标位于应保留区间，但其后数据有洞或校验不一致 | 409 `stream_gap` | 保留最后已校验位置，报告缺口并对账；不推进 ACK |

三种错误 `retryAction=none`，不取消/重建原运行会话。服务端须保存可验证的游标所属绑定/流代际与保留下界，或等价的有界索引，才能区分已过期与未知；不能仅用“当前 Map 里找不到”同时声称完成这三种语义。游标不授予资源权限；先确认请求者对目标绑定有权，再诊断其 cursor。

HTTP 200 已发出后若检测到缺口，断开该扩展流；不得临时向 EventFrame 注入未声明 error 类型。客户端带最后已校验 cursor 重连时得到对应 JSON 错误。档案与材料的查询路由负责恢复权威状态；观察流不会代替正文连续覆盖证明。

## 5. 元数据规范字节与原 IR 正文

两种字节处理必须分开：

1. **原 IR／附件正文**是已捕获、已承诺摘要的不可重编码字节对象。通过 ArtifactRef 的原始 SHA 校验，保存/分块传输/回传原字节。不得先 JSON.parse 再 JSON.stringify、转换换行、规范化 Unicode、重新转义 `/` 或改写 `-0`/指数数字后验证原 SHA。需要理解 IR 时，先校验原字节与来源，再用被冻结的原 IR schema／现有读者解析；本节新增控制数字语法不约束原 IR。
2. **新控制 DTO／记录元数据**使用下面的有限规范编码。服务端交付其规范字节和原摘要供终端直接保存/回传；终端没有重新编码成“等价 JSON 后仍算原对象”的权利。发送新的控制 DTO 可按本节构造新规范字节，再通过指定 schema 验证。

元数据编码规则：

- 键均为 schema 已定义的 ASCII 名字；按 ASCII 码升序，不依赖插入顺序或本地语言排序；未知键仍由具名 schema 拒绝。数组保持顺序。输出无空白缩进，无尾部换行。
- 允许 null、布尔、字符串、安全非负整数、密集数组及普通对象。不允许 NaN、Infinity、负数、负零、小数、bigint、undefined、循环或 getter。十进制序号仍是字符串，可精确保留 uint63 范围。
- 线上数字 token 只能是 `0` 或 `[1-9][0-9]*`，值不超过 `9007199254740991`。`-0`、`1e3`、`1E+3`、`1.0`、`01` 即使某语言能解析也拒绝。必须在丢失 token 表达前检查，不用 Number(1e3) 的结果冒充原始字面量验证。
- 字符串逐 Unicode 标量保留，不归一化。双引号和反斜杠分别写 `\"`、`\\`；U+0008/0009/000A/000C/000D 写 `\b`/`\t`/`\n`/`\f`/`\r`，其他 U+0000…001F 写小写十六进制 `\u00xx`。`/` 不转义，汉字/emoji/组合字符及 U+2028/2029 直接 UTF-8。拒绝孤立代理项和非法 UTF-8。
- 输入结构解析必须检测所有层级的重复解码键，`{"a":1,"\u0061":2}` 也是重复；不能先用会覆盖重复键的 JSON.parse 再验证。有限元数据解析深度不超过 32、节点不超过 100000，并叠加具名 schema、实际字节/列表上界；原 IR 另走原合同。

原始对象 SHA 为 `SHA256(rawBytes)`。域摘要精确为 `SHA256(UTF8(domain) || 0x00 || bytes)`，domain 分别为 `tansr.sdk2.payload.v1`、`tansr.sdk2.record.v1`、`tansr.sdk2.operation.v1`。payload 用原正文 bytes；record 用排除 recordDigest 后的规范记录元数据 bytes；operation 用 schema 已定义的可信 scope＋operation＋排除请求身份后的 semantic 对象规范 bytes。来源快照若含原 IR，宿主必须使用实际已有原文/投影字节的见证，不把原 IR 强制转成控制元数据数字子集；其精确见证由主 RFC/source snapshot 合同固定。

## 6. 材料上传边界与验证范围

上传只响应服务端已发、未过期且仍允许接收的 materialRequestId；其 requestedRecords 固定 recordDigest、原 payload/attachment 的 ArtifactRef 见证及 chunkBytes。默认原文块 64KiB，base64 至多 87384 字符，足以落在 256KiB 控制请求帽内；实际请求若配置更低帽，发请求前同步减小冻结 chunkBytes，不在上传中自行协商更大值。

块 offset 固定为 chunkBytes 的整数倍，非末块完整，末块精确补足原对象；每对象至多 16 块。offset 不重叠、同位置同字节/块摘要重试不增量计费或重复占 spool；不同正文冲突。整对象长度、原 SHA、请求总 maxBytes、并发、期限和应用/主体/宿主 spool 帽都验证后，才成为 committed uploadId。MaterialResponse 只引用这次材料请求已 committed 的 uploadId，不能引用任意历史缓存、他人对象或 URL。

材料 CAS 以不可变请求身份、原目标/来源代际、期限及材料状态进行，不与 ACK 推进的 binding revision 耦合。旧 deadline 到期不延长；关闭/撤权停止新的受理，已由核心借用的正文须在核心释放后才可清理。上传回执丢失按 artifactId 查询同一对象；材料最终状态可按 materialRequestId 查询，即使通知 SSE 已过期也不需要重传或新建请求。

本轮脚本金样检查编码、path/query、DTO映射和帧一致性，不声称真正 HTTP 流、持久化、借用释放、完整上传或跨语言端实现已通过。上述行为必须由后续产品卡逐项实际验收。

## 7. split-receipts-v1 档案确认

此节修复旧草案“一正文加128附件不能装入128项混合收据”的矛盾。协议族仍为sdk2-ext-v1，路径仍为POST /v3/sdk2/bindings/:bindingId/archive/acks；不新增SDK1字段或改变旧JSON／SSE。旧草案与本节互不静默兼容，首次支持须在发现／绑定上可判别，不用对象内容猜版本。

| DTO | 必需格式字段与规则 |
|---|---|
| CapabilitiesResponse | archiveAckFormats：有archive-transfer-v1时精确为["split-receipts-v1"]，无该能力时为[] |
| BindingCreateRequest.archive | ackFormat："split-receipts-v1"；须来自刚发现的共同支持集合 |
| BindingView | archiveAckFormat：接受archive-transfer-v1时为所选格式；未接受archive时为null |
| ArchiveAckRequest | ackFormat：与绑定一致；payloads：1～128项正文收据；attachments：0～128项额外附件收据 |

收据对象精确为{artifactId,sha256,state:"durably-stored"}，不允许附加source/path/url/bytes字段。整条请求仍必需protocol、request、bindingId、expectedRevision、generations、sourceId、sourceGeneration、coverage。未知格式或缺失新字段由具名schema拒绝；已知格式与绑定未协商archive不符时拒绝该能力，不能忽略新字段回退。旧草案服务端的additionalProperties:false也会拒绝新发现／绑定／ACK对象；旧客户端应停止扩展而保留SDK1会话。

发现只表示支持，不授权或分配绑定。创建的required/optional集合继续互斥；required archive缺共同格式则整次失败，optional archive不可用可以被拒绝但不阻止独立required context-materials-v1成功。返回的archiveAckFormat必须与实际acceptedCapabilities一致，不能在材料专用绑定上宣称已协商档案。绑定存续期间不得改释格式，原草案实验回执只按保存的旧语义读取，不改释旧attachments后重放释放。

### 7.1 完整区间与两个有序集合

服务端从可信源链读取coverage.fromSequence至throughSequence的每条完整记录（包括已确认的重叠部分）。范围必须非空、最多128条、无缺口、through不后退且不超过发布头、from不高于旧确认水位+1，headDigest精确匹配可信头。按sequence升序和每条原引用顺序，分别构建payloads与额外attachments的首次出现集合。集合内artifactId不得重复；一个ID的原SHA、bytes、mediaType、sourceId在所有引用中必须一致。两类别引用同一对象时各有一致收据，物理对象计量／回收一次。

实际请求必须逐项等于两个期望集合，不能只检查包含若干ID、只核新尾段、把body收据混入attachments或忽略多余项。数组顺序有意义，重排后不再是原操作语义。每个SHA是ArtifactRef.sha256的原字节SHA256，不能拿域摘要替换。两集合各自最多128项，整包还须满足controlBytes≤262144。两条各有128个不同附件的合法记录通常分成两次完整前缀确认；不引入局部附件确认状态或半条记录成功。

绑定的required交付准入先验最大允许ID／修订数字和合法单记录对象见证的ACK信封；限值、sourceGeneration或记录准入变化也先验。某低controlBytes配置不能容纳单记录最坏合法信封时拒绝该组合，不能先承诺后永久背压或减少合法附件数。接收控制体先检查原UTF-8字节，规范元数据编码所得信封也不得超帽；标准发送方使用§5规范编码。

### 7.2 原子确认、原键重试与释放

来源事务原子保存record metadata、完整body／attachments、连续接收头及原出站ACK意图。来源崩溃恢复沿原requestId、operationEpoch、expectedRevision和精确两集合查询／重放，事务或响应未知时不换键追赶。只有已知未受理的冲突，才按实际当前CAS产生后续新请求；未决意图不能靠TTL删除。

服务端顺序为：结构／字节校验→当前授权→同scope、binding、operation、operationEpoch、requestId的原回执及语义核对→新请求epoch与绑定格式、CAS、代际、完整覆盖核对→单事务写ACK事实、水位、revision和原终局回执。原语义摘要域和封装不变：tansr.sdk2.operation.v1，可信scope＋operation＋排除request后的semantic；ackFormat、payloads、attachments及原有所有语义字段均参与规范编码。不得排序数组或只对新字段子集算摘要。

同键同义返回原MutationReceipt，同键异义409 request_id_conflict；退役epoch只拒新受理，不遮蔽仍有当前读取权限的旧回执。后来revision、sourceGeneration或删除代际改变不改写历史已提交事实；它们仍可阻止新ACK或实际释放。网络失败不伪造成功，同键查询不再次推进水位或重复释放。回执GC继续同时要求终局保留期届满与所属epoch退休；旧请求查无回执后410，不能复活。

ACK事务之后才可按当前代际、core hold、未决运行／治理和来源策略安全释放。closing允许完成已有记录，close意图completed不等于资源closed；旧SessionStore的有效历史、提交和drain责任均不随档案ACK改变。此合同及参考测试不证明产品SQLite事务、冷恢复、各端存储或真实HTTP已验收；实现必须另附实际证据。
