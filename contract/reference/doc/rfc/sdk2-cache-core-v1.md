# SDK2-07～09 Core 旁带与诊断加法合同

日期：2026-09-23。配套严格结构见 `sdk2-cache-core-v1.schema.json` 与两端运行时校验器。本合同只增加 `sdk2-cache-core-v1`，不更改冻结 SDK1、L0、TWP、`sdk2-cache-v1` 帧/控制面或旧四 null Gateway witness。

## 接线与权威

快照登记只对可信装配显式启用 Core 的客户端生效，内部弱标记不扩展冻结 ModelClient。默认关闭时不遍历、编码或复制请求，新 Core 的尺寸和编码限制不进入旧 SDK1 请求路径。透传包装须保留内部启用关系；启用后的快照与来源校验失败必须阻断，不得吞错或降级。

Serve 的原执行权限刷新仍在发送前执行。父/子工具表经过该可信屏障收窄后，只允许由原 Core 对象重新绑定实际最终 tools；保持原请求未变和原材料当前授权检查，同时独立快照最终工具表，重算工具指纹。终端提交的结构副本、已变异请求或已撤权材料不能通过此内部转换获得品牌；不导出新的 SDK/线协议接管接口。

共同 kernel 在最终 IRRequest 装配后登记内部弱映射。仅启用 Core 的原 TwpAdapter 读取；用户传入普通 IRRequest、其结构副本或公开 DTO 不能继承品牌。system/tools 指纹是该次输入的 JSON UTF-8 SHA256，不是平台权限或七项策略版本。材料来自原 MaterialCoreLease 的真实准入；只记录仍在最终请求中的原 text block，引用占位、正文同值复制、材料被裁剪、源切换/删除/治理代际失效都不承接品牌。新旁带不传正文或密钥。

可信宿主在现 HostRuntimeContinuity 配置中显式启用 `coreProjection: true`。本版只支持宿主持有原 continuity issuer 的配置，普通终端 bridge 不获签发能力；原 Electron 仍使用完整嵌入式 SDK。宿主用原 remote source 的 namespace/id/revision/history/deletionGeneration/runtime/binding 构造 intent，API 再核原已 enroll 来源及当前 app_user。新的签名域复用原 issuer 认证根，不新设用户权限、缓存或资金系统。

```ts
// 可信 serve 宿主的原 cacheHost 配置片段；密钥来自其现有密钥设施。
continuity: {
  sourceNamespace: 'developer-backend',
  issuer: { issuerId, keyId, key: issuerKey },
  coreProjection: true,
}
```

## HTTP 与字节

- `GET /t1/cache/core/v1/capabilities`：无查询，返回独立特性 `trusted-core-projection-v1`、`bounded-diagnostics-v1` 与严格限值。
- `POST /t1/cache/core/v1/exchange`：正文仍是原缓存二进制 frame，内部原 TWP 不重编码。请求头 `x-tansr-cache-core-proof` 为无填充 base64url 的规范 JSON，最长 8192 ASCII 字节，解码最长 6144 字节。
- `POST /t1/cache/core/v1/exchange/lookup`：显式查询入口，传原持久 frame、当前 app_user 认证和原 intent 的查询证明。签名域为 `tansr.sdk2.cache.core.lookup.v1\0`，其余 unsigned envelope、密钥根、时窗及证明上限复用原规则。执行入口不能接受查询签名，查询不触发首次执行。旧 capabilities 字段、特性和限值完全不变；没有查询端点时保留原错误与 pending，不回退执行。
- `GET /t1/cache/core/v1/bindings/:bindingId/diagnostics?protocol=sdk2-cache-core-v1&limit=100&after=...`：当前鉴权后返回有界非敏感行。旧 diagnostics 仍为空行，不在旧格式添加字段。
- serve 终端诊断使用 `GET /v3/sdk2/cache/core/v1/bindings/:localBindingId/diagnostics` 及同查询参数。沿原用户鉴权和本地 alias 映射转发，终端不取得 API bindingId 或票据。

证明的 unsigned envelope 为 `{protocol,issuerId,keyId,issuedAtMs,expiresAtMs,intent}`，签名是 `HMAC-SHA256(key, "tansr.sdk2.cache.core.v1\0" + canonicalJSON(unsigned))` 的 64 位小写 hex。时窗不超过 300000ms，允许签发时钟最多超前 30000ms；当前宿主签发 60000ms。`intent` 字段精确为应用/用户、原登记源、历史及删除代际、runtime/binding/revision/projectionRef/requestId/payloadSha256 与 Core。续签不能改变 intent；API 把固定 intent 摘要绑定原 C2 owner 事务和原资金锚。

Core 为 `{assemblerVersion:'tansr-core-request-v1',effectiveInput:{systemSha256,toolsSha256},materials}`，材料最多 8 项，沿原认证元数据结构。sourceGeneration/historyEpoch 是原不透明标识，不强转数字。coverage 的 fromSequence/throughSequence 均为正数且有序；coverage、assemblerVersion、sourceHeadDigest 必须同时为 null 或同时存在。不虚构没有证据的摘要/策略修订。

lookup 返回严格 `{protocol,state,result}`。`state` 为 `not-found | accepted | result-unknown | completed | receipt-expired`；非 completed 的 result 必须为 null。completed 可返回 null（结算事实存在但原响应已不在），或 `{status,contentType,body,headers?}`：status 为 100～599 整数，contentType 最多 128 字符，原 body UTF-8 最多 262144 字节且不重编码；headers 仅可含 cost、balance、priceTable，各最多 1024 字符。完整 JSON 响应最多 1048576 字节，使用复用原严格解析算法的专用入口；旧控制/诊断 65536 字节上限不变。任何 lookup 结果都不清 pending，不把 not-found 当作未执行证明，不更新身份/资金锚，不将旧响应移植为新用户轮。

首次发起前确认新能力、身份、尺寸和当前来源；异步获取令牌/签名后再核原请求未变、材料当前授权及宿主 source。失败不退旧接口执行。新的 C2 请求仍沿原 requestId、原 payload/frame 和原资金链；未知恢复只续签同一固定意图，不能借恢复改正文/来源。每次恢复发送仍须取得本次真实 CoreRequest 的同值见证；含材料的冷恢复须先经原材料供给重新验证，不能以持久缓存中的旧事实代替当前材料授权。当前没有恢复该材料品牌时明确拒绝，不自动发起模型；不把这种拒绝写成冷恢复自动完成。

## 显式库存迁移与回滚

只有显式 `coreProjection: true` 发起新请求，且原缓存库没有控制未决或任何未结束交换，才迁入 `sdk2-cache-client-core-v1`。新格式 metadata、原 exchange frame、`core-exchange/<requestId>` 的固定 intent 与在飞标记 `flight/<requestId>` 在原 SQLite 事务一起提交（PERF-01(c) 单一耐久点：该事务在 send 完成能力／证明检查后、交 fetch 前执行；多条 Core 请求先后 prepare 时新格式 metadata 随其中**首个落盘**的交换事务提交，磁盘上 `core-exchange/` 行不早于新格式 metadata 出现，见 RFC-SDK2-缓存连续性 §4.2）；Core 字节纳入原内存和磁盘容量帽。旧模式仍写原 `sdk2-cache-client-v1`，不添加新记录。

新读者可只读查询新库存；关闭扩展不会把未决 Core 请求退回旧接口。需要补查/恢复远端结果时应保留原可信配置并沿同一意图处理，正式终结沿原锚删除 frame 与 Core sidecar。旧二进制不认识新格式时明确拒读并保留文件，不能擦库、重新铸造会话或降级重发；升级后的新库存不能直接交给旧二进制。启用前应把这一限制纳入宿主版本回退安排。没有启用扩展的原 Store、原库存与原 SDK 请求不受迁移影响。

## 诊断的含义与限制

SDK 宿主的 `relation` 是原 session/lineage 的二级索引。仅该索引丢失时，从同一受保护库存的唯一归属记录找到原来源，核当前授权并沿原接续意图取得验真回执；原 source 写入和补索引同事务提交。不得因此另建 lineage/runtime 或改变未知 requestId。主 session 行缺失且原 runtime/relation 仍有责任时，恢复和删除拒绝 `mapping_unavailable`，不以不完整库存制造新关系。未托管普通会话的删除只需原锁内元数据归属检查，不要求伪造完整历史；已托管删除仍执行原全史摘要及来源墓碑确认。五方法自建 Store 的普通 SDK 路径不增加守卫或证明要求，新缓存宿主仍要求原 FileStore 工厂的锁、提交和删除能力。

新诊断只读原同域实际请求/网关投影/usage/结算锚。单行最多 4096 字节、单页最多 100 行、完整响应最多 65536 字节、诊断保留最多 24 小时；超出完整响应帽时缩页并返回 next。cursor 是受限不透明分页令牌。必要 Core 意图随原 C2 七天保留与压缩，不等于诊断可读期限。输入/Core 比较、Gateway segment 比较、provider usage、实际账务分层呈现。原结算账中明确保留厂商计数字段在场证据：有效实报 0 返回数值 0，缺报、坏值或无法区分旧记录保持 null；正实报返回原数值。`cacheReadEvidence` 保留旧 positive/unknown 枚举，只表达正命中证据，实报零不制造 positive。`savedNet` 没有实际缓存读写正实报时保持 null，有报告时只是原账本差额，不能替代真实总费用对照。

`GatewayCacheClient.coreDiagnostics(bindingId, after, limit)` 显式读取新接口；`diagnostics` 的旧空视图保持不变。返回值不含 prompt、ticket、projectionRef、provider cache key 或跨用户原始摘要。诊断可观察性不授予执行或绕过删除权限。

English: enable `coreProjection: true` only on a trusted host using its existing continuity issuer. The kernel owns context assembly; endpoints and signed Core evidence are additive, while the existing SDK1/TWP/cache-v1 payload remains unchanged. An explicit, quiescent migration changes the encrypted cache inventory to `sdk2-cache-client-core-v1`. Older binaries must refuse this inventory without deleting it; disablement must never replay an unresolved Core exchange through the old route. Use the original configuration and request identity to reconcile outstanding work. `coreDiagnostics()` reports bounded request observations and distinguishes unknown usage from positive provider evidence; it does not claim savings without actual cost evidence. Electron retains the full embedded SDK and its existing tool ecosystem.
