# RFC SDK2：私人逻辑会话缓存连续性

创建：2026-09-17。候选v0.2（C1／C2修订）。任务SDK2-01b／07／08／09；**完整候选待独立会签，尚未授权据此实现或宣告产品支持**。机读合同：[sdk2-cache-v1.schema.json](sdk2-cache-v1.schema.json)。参考验收：[sdk2-cache-contract.test.mjs](../../scripts/test/sdk2-cache-contract.test.mjs)。本案不修改archive schema、SDK1、L0、原TWP、现有SessionStore或默认缓存策略。

依据：[缓存子案](../report/SDK2.0-跨端上下文与缓存连续性子方案-2026-09-15.md)、[S0 D07～09](SDK2-S0-分阶段决策与实现门.md)、[SDK2兼容RFC](RFC-SDK2-1-兼容SDK1的扩展协议.md)。首链只把**同一可信应用／用户的逻辑会话关系、有效投影和上游缓存分组**贯通；不保证缓存命中、不搬运供应商KV、不复用旧模型答案。

## 1. 现有落点与保护边界

本次实际读取CLI工作树`worktrees/cli-SDK2-01`及API固定主仓，指纹见`archive/20260917-SDK2-full/cache-contract/task-inputs.json`。API没有AGENTS.md，遵守工作区工程纪律；仅读取，不在固定主仓开发。

| 当前真实代码 | 必须保护／新增边界 |
|---|---|
| `packages/providers/src/twp/adapter.ts`，首次序列化读取platformSessionId；prompt-cache feature开启才供给cache.key，重试复用原body | 原默认请求不换键、不增字段／等待；新连续性独立协商与路由 |
| `packages/sdk/src/session.ts`恢复存储platformSessionId，410回调换新号；`packages/server/src/v2/routes.ts`活跃resume直接attach | 连接、逻辑关系和运行号分开；缓存票据不复活410租约 |
| API `src/t1/translate.ts`的cacheKeyOf优先cache.key、回落meta.sessionId；OpenAI两面及Anthropic按实际能力出站 | 新route只在可信解析成功后供给另一个内部cache意图，原body和旧默认解释不变 |
| API `src/models/prompt-cache-caps.ts`现有ModelVerse owner＋UTC日亲和；`src/t1/member-cache-facts.ts`按实走成员取face.cache | 不重做旧应用亲和，不把默认改成用户粒度；新私人组必须经该具体adapter的受支持规则显式接合 |
| API `src/gateway/pool-select.ts`按session＋catalog粘性，健康／管理启停优先 | 新映射只作为可选路由偏好，不能复用已撤权／禁用／冷却成员 |
| `packages/kernel/src/context/system.ts`组装规则／工具／skills／环境／记忆；现治理prepare—publish屏障 | 同一核心产出投影，不另造压缩器；真实环境与P/S/A更新不为缓存冻结 |
| API `src/auth/app-token.ts` app_user提供可信eu；AppKey双头径endUserId=null | 私人组首链要求app_user或后续另行会签的等价可信委托；不能拿meta.endUserId补足身份 |
| API `src/t1/routes.ts`直接注册`/t1/exchange`，32MiB体帽；`src/t1/signature.ts`签原body和path | 新绝对路由另注册；原路由、body schema、加密字段、签名及重试字节保护 |

未启用时新增目录／文件打开、网络、持久等待为零。原Node嵌入式、CLI、serve、ACP、headless、BYOK和旧端缓存行为持续支持。只有AppKey而无可信终端用户的调用不自动宣告私人连续性。

## 2. 两段协商与路由

协议族唯一`sdk2-cache-v1`，能力唯一`private-logical-cache-v1`。独立发现响应不复用archive的CapabilitiesResponse，不把新枚举加进旧严格解析器。两段必须均确认：SDK／serve与serve／API；本机嵌入式同等可信宿主走相同内部合同，不能靠OS标签取得权限。

| 操作 | SDK→serve | serve→API | 请求／响应 |
|---|---|---|---|
| 发现 | GET `/v3/sdk2/cache/capabilities` | GET `/t1/cache/v1/capabilities` | 无query/body；CapabilitiesResponse |
| 建立／恢复关系 | POST `/v3/sdk2/cache/bindings` | POST `/t1/cache/v1/bindings` | OpenRequest／GatewayOpenRequest → OpenResponse |
| 查询 | GET `/v3/sdk2/cache/bindings/:bindingId` | GET `/t1/cache/v1/bindings/:bindingId` | query只有protocol → BindingView |
| 续票 | POST上述binding路径＋`/renew` | 同左API前缀 | RenewRequest → MutationReceipt |
| 再验证绑定 | POST上述binding路径＋`/rebind` | 同左API前缀（sessionId指当前受验运行资源） | RebindRequest → MutationReceipt |
| 轮换组 | POST上述binding路径＋`/rotate` | 同左API前缀 | RotateRequest → MutationReceipt |
| 关闭缓存绑定 | POST上述binding路径＋`/close` | 同左API前缀 | CloseRequest → MutationReceipt |
| 原操作查询 | GET各前缀＋`/operations` | 同左API前缀 | protocol、operation、operationEpoch、requestId及bindingId扁平query → 原OpenResponse或MutationReceipt；open时bindingId缺席并解为null，其余必填 |
| 诊断 | GET上述binding路径＋`/diagnostics` | 同左API前缀 | protocol、after（缺席=null）、limit → DiagnosticPage |
| 模型交换 | 无额外SDK用户消息协议 | POST `/t1/cache/v1/exchange` | 下述二进制信封 → 原TWP响应／事件原样；新缓存错误只在模型受理前返回 |

路径ID按archive wire的单段encodeURIComponent、一次解码；未知／重复query、body/path不一致均400。控制JSON沿既有严格规范编码：UTF-8无BOM、重复键拒绝、控制安全整数、原Unicode不归一化；GET无body，POST无query。schema中的host-only定义不是公网可写DTO，必须按路由具名校验，不能根anyOf通过即执行。

发现只读现有epoch与能力，不创建映射／票据。serve仅在已验证API能力及自身实现均支持、未超过negotiatedTtlMs时返回非空features和gateway=confirmed；API为audience=gateway-cache、gateway=not-applicable。缺后段时serve返回空features及unsupported或unavailable；没有当前epoch不得承诺新受理。发现结果不能缓存超过60秒或授权／配置更新点，也不替代每次请求的校验。

旧网关缺路由不是已认证unsupported证据，不能把任意404／代理HTML／认证失败当作自动兼容协商。已知旧部署可由可信装配明确标记未实现，维持旧调用；运行中的新路径受理结果未知不能回退旧route再发一次。API的新路由在既有t1注册体系内独立装配，不暗改原`TWP_FEATURES`或原`/t1/exchange`。

两跳不共享票据audience。serve持久GatewayLink保存本地binding到API binding／logical／revision的映射，以及受保护API票据和原操作意图引用；客户端只有serve-cache票据。API的logical／group为平台组的权威，本地logical只是该域的不透明别名，不另派生一个竞争组。纯SDK直连API时只用gateway-cache合同；经serve时由可信宿主保管上游票据，按当前已验app_user身份调用API，不能把body用户号当委托。没有可用可信身份传递的宿主不得公布该feature。

跨服务不是分布式原子事务：先在本地事务固定原请求、上游原键与精确意图并占有限pending库存，再发API；远端已知成功才在本地同事务发布active link及公共原回执。远端COMMIT未知或已成功但本地发布失败时，保留原GatewayLink，后续同键查／重放API原操作，再完成本地发布；未知阶段禁止新起轮及换键重开。新的认证令牌可用于当前鉴权，原控制意图及epoch／requestId保持不变。已知远端失败终结本地失败回执；本地取消但远端可能成功必须先确定其事实，再以固定补偿close键关闭远端，不能把未决link当垃圾清除。

renew／rotate／rebind／close在本地保持同一绑定写排他，复用这一持久待决流程，查询先当前授权再原回执。模型exchange不跨这条控制事务重新发起：一旦可能受理只沿旧TWP幂等／状态处理。API端票据和本地票据独立轮换；upstream pending或撤权时不能凭本地旧active快照继续发送。实际跨实例数据库提交与断网重启实证是该合同落地门，参考测试不替代。

### 2.1 保留原TWP字节的二进制信封

Content-Type精确`application/vnd.tansr.cache-exchange.v1`，无压缩，body = `uint32be(metadataBytes) || metadataUTF8 || originalTwpUTF8`。metadata按ExchangeMetadata，长度1～65536；原TWP体1～33554432字节，整帧最大33619972字节。总长必须恰为4＋metadata长度＋payloadBytes，拒绝尾部垃圾、截断、非法UTF-8、SHA不符、控制区BOM／重复键或声明超限；先核长度再分配内存。

原TWP字节在首次发送时固定，包含其原数字表示、字段次序和已加密内容；**不能解码再规范化／再加密／重排后声称字节相同**。网关保留原字节供原签名／幂等证据，另外用现有TwpExchangeRequestSchema校验其解码值；不能把IR浮点、opaque或供应商私有结构塞入新控制数值子集。新route的原TWP响应/SSE保持旧消费者形状，新诊断通过独立查询读取。

认证沿原app_user短令牌链，无客户端私钥；签名适用的可信宿主模式必须覆盖新path和完整信封，不能只签内层放任票据被替换。未来AppKey＋可信用户委托要另会签，首链不开放。新exchange保留原财务唯一锚点`[ownerType, ownerId, twp:<inner.meta.requestId>]`，应用／用户／绑定／运行／原信封作为完整受理见证附着于该锚点，不能反过来派生另一财务键；重试内外字节一致。不能用logicalRef／groupRef代替运行ID、幂等域或费用主体。

## 3. 可信关系、票据、代际与生命周期

scope为认证得到的`[applicationScopeId, originalEndUserId]`结构元组，核心真实授权修订另存，API未知核心修订时Scope.authorizationRevision明确为null。API沿authenticateAppToken实时校验签名、jti／user／app撤销、应用行、owner与license；检查不可用按原fail-closed。serve也核该用户对原sessionId的访问权和会话排他边界。任何请求不得自报scope、其他用户、providerKey或缓存组。

| intent | 逻辑关系／组 | 当前历史来源 |
|---|---|---|
| new | 新随机logicalRef及私人组 | 当前授权会话，不与相似文本自动合并 |
| resume | 有效本域本audience票据指向原logicalRef，可跨合法新runtimeId延续组 | 先旧Store或已验证档案恢复当前有效投影；票据不授予正文读取 |
| fork | 必须验证父票据当前归属，建立独立子logicalRef和新组 | 原fork权限及历史边界照旧，不能继承父审批、运行租约或缓存组 |
| import | 新logicalRef和新组；没有parentTicket | 导入内容是candidate，不能以摘要／工具结果自报获得权威 |

resume缺票据／票据过期不是new的别名。显式返回错误；应用可明确选择cold路径，但须独立满足当前历史权限与模型准入，不声称延续旧逻辑关系。旧记录没有缓存元数据时以legacy-missing冷建立新关系，不重新摘要已有效的旧摘要、不伪造来源。旧410平台换号可以正常发生，票据从不复活旧运行。

票据使用32随机字节base64url无padding（43字符），映射到持久票据行：票据散列、audience、scope、bindingId、logicalRef、groupGeneration、policy／删除代际、issuedAt、expiresAt、state。原值只在授权响应／受保护SDK存储及受保护幂等回执内出现，日志／诊断／上游参数不得出现。数据库若需重放原响应，票据原值加密保存；只存散列又声称能返回同票据不成立。TLS不替代当前授权与audience验证。

票据TTL默认24小时、硬上限7天，实际截止不晚于绑定／映射截止。映射默认24小时空闲／30天绝对寿命，续票不能越绝对寿命；读发现、查状态、重放旧操作、诊断均不续TTL。每逻辑最多16活跃绑定／票据，续票同事务消费旧票据并发行一张新票据，只有该设备绑定票据失效；并发旧票据续票由revision CAS决定一个成功，失败不能生成悬空票据。

手动rotate由当前有绑定管理权限的主体执行，整逻辑groupGeneration和mapping revision原子加一、旧组票据失效，响应发行当前绑定新票据；其他已绑定设备可显式rebind：先查当前BindingView、以expectedRevision回传当前授权sessionId；宿主必须从旧Store／已持久绑定关系证明该资源属于此logicalRef，并核当前删除／策略代际，然后在同事务废旧票、发新票、推进revision。仅持bindingId／自报sessionId不构成血缘证明。mapping与binding仍须active；关闭／撤销／过期映射不能rebind复活。没有可信血缘的旧端明确new冷开始，不能使用旧票据隐式复活。授权／删除／应用边界变化由可信宿主强制同类轮换或撤销，优先于缓存。组失效不宣称供应商KV已删除；应用需严格供应商删除保证时，须另接该供应商真实资源合同或禁用相关缓存。

close关闭缓存绑定及其票据；不关闭SDK1业务会话，不删除其他绑定或旧Store。最后绑定关闭可按保留策略清理映射，但先保留未决操作及原回执；不存在“为省缓存状态立即丢弃未知写”。被撤销、过期或已删除的映射不能靠新requestId复活。重新开始必须显式new并重新授权。

当前仍授权的提示词、能力、工具或记忆更新由核心域内部PolicyRefresh按既有轮级／治理屏障接收**实际生效的可信事实**：核expectedRevision，原子更新policy／generations、递增组代际及统一revision、废止旧票据和旧投影，随后设备通过可信血缘rebind加载新策略。公网无此写入口，SDK不能自报PolicyRefresh。source、删除或授权边界变化必须action=revoke，不能只改标记后保留旧组；一般政策更新可action=rotate。已到原生效屏障的内部更新在当前请求权限检查之后、使用新投影之前完成；并发CAS失败重新读事实，不能使用旧票据绕过，也不能为此提前刷新本轮固定system。核心MappingRecord保存当前真实generations；gateway域没有核心来源权威，generations固定null，仅保存自身GatewayPolicyWitness。网关观察变化用reason=gateway-observation-changed，observationRevision由该域数据库CAS单调推进，普通变化可rotate，授权边界变化必须revoke。两类PolicyRefresh不能互换域；单靠票据、平台配置HMAC或projectionRef不能恢复历史或取得resume材料权限。

## 4. 多实例持久映射和失败语义

平台持久库（后续API写域依既有MySQL）为唯一权威，唯一键为scope元组＋logicalRef；副本只缓存已验证快照。映射、票据生命周期、版本、原操作回执在同事务提交，以数据库CAS／唯一约束处理两个实例竞态，不依赖进程内Map或Redis NX即宣称耐久。随机ID在事务前固定、失败重试沿原键，不重复分组。部署要迁移／回滚持久格式，旧实例不懂sdk2-cache-mapping-v1时不得处理新缓存route。

**缓存控制操作**请求身份为scope＋audience＋operation＋epoch＋requestId（binding操作再绑定bindingId）；semantic以受信HMAC钥计算域`tansr.sdk2.cache-operation.v1`的规范新控制请求（剔除request及认证信封），旧TWP指纹仍核原字节。同域同键异义409；当前授权先于原回执，原回执先于新epoch／CAS／TTL。旧回执是历史结果，不能延长票据或跳过再次redeem时的当下代际。未知COMMIT只查／重放原键，不能换键或改走旧模型路径重复费用。

epoch默认1小时、上限24小时，由可信初始化／轮换器持久创建，GET不铸造。回执默认7天、未决不TTL删除；终局保留到期且epoch退休才可GC。活跃epoch不能删去重记录。清空mapping前须确认无未决操作／活跃票据，保留拒绝旧epoch的状态；不可通过删库再沿原ID建立不同关系。

有限帽见Limits，默认取schema上限但mappingIdleMs=24小时、ticketTtlMs=24小时、epoch=1小时、diagnosticSamplePerMillion=10000（1%）。映射每用户1000／应用10000／全局100000、每行16KiB；回执全局100000、每行32KiB；活跃与未决均计量，应用／全局字节按行实际编码计量并有相应乘积硬帽，不能把附件或大prompt藏入映射。事务前预留，超限429；有界清理最多100行／批，不能无限全表扫描阻塞用户请求。

另外所有保留状态统一计量，包括映射、当前及撤销票据、已关闭绑定、投影、epoch、墓碑、未决、GatewayAdmissionRecord和原回执：每用户10000行／16MiB、应用100000行／256MiB、全局1000000行／2GiB。每类自身较小帽仍同时有效，禁止用“已撤销／已关闭”免记库存；原子提交前按实际字节核所有层级，任一层不足全部不发布。无用户归属的epoch等系统状态计入应用／全局。过期票据可在不影响原回执／未决恢复的前提下有界回收，缺失票据按拒绝处理；有限容量不能成为删除仍有效的去重事实的借口。参考内存模型只验证原子计量语义，实际库锁、物理增长及崩溃恢复必须另验。

映射／缓存优化缺席且原模型权限、当前历史、计量准入均已独立通过时，可以返回明确legacy-cold并走旧调用一次，不带未验证group或投影。授权／撤销检查未知、source权限失败、结果未知、模型可能已经受理时fallback=none。缓存票据／映射失败不能覆盖已发生模型费用，也不能让一个重试同时走新旧两条路。

### 4.1 原财务身份与新受理见证（C2）

API实码的原金融唯一键为`[owner_type, owner_id, request_id]`，request_id恒为`twp:<原inner.meta.requestId>`；原TWP ID仍是8～40个`[A-Za-z0-9_-]`字符，owner仍从当前已验应用／license／计费主体取得。FinancialAnchor只描述这把原键，不是第二账本。缓存logicalRef、scope、runtime、group、信封hash或HMAC key轮换不得改写它。

host-only GatewayAdmissionIdentity在原锚点上固定应用／原终端用户元组、gateway-cache audience、bindingId、runtimeSessionId、projectionRef、原innerRequestId、原TWP与完整信封的字节数及SHA-256。所有字段从当前认证、已验运行关系及实际信封解析取得，不能信客户端自报hash／scope。必须核financialAnchor.requestId恰为twp:加innerRequestId；字节/SHA与原帧相符。跨app、用户、binding、runtime、参数、票据或信封任一差异，即使内层JSON解码等价，仍对这把原owner键返回request_id_conflict，不返回另一主体回执、不续TTL、不调用provider、不换ID绕过。相同完整见证且当前仍有原回执读取权时才可读历史结果；当前鉴权／撤权检查先行。

每次**首次**模型准入先满足当前认证、运行归属、当前有效投影与原计量许可。在一个MySQL连接的同一事务内完成：原owner资金锁→有关缓存映射／绑定的固定锁序→再次查原owner键受理锚点及原reservation／usage→必要时执行原资金链的预扣→保存GatewayAdmissionRecord→更新lastAuthorizedUseAtMs=now、idleExpiresAtMs=min(now+mappingIdleMs, absoluteExpiresAtMs)。不改变绝对期限或已发票据期限。必须复用原资金实现的同连接原语，不得调用会再开事务的公共Store方法假称原子。Redis幂等缓存、每日额度与数据库不是一个分布式事务；这些既有机制继续各自处理，不能宣称全链恰一次。

GatewayAdmissionRecord是**非财务受理锚点**。只有原网关机制确认确实没有reservation（例如真正原价estMicro=0或原BYOK不预留路径）时，才与映射touch同事务耐久写入financialDisposition=not-required；不创建虚拟账单。**现金为零不等于没有reservation**：gross estMicro>0但赠送Token全额覆盖时，原tryTokenReservation仍返回reserved=true，持久reservation.amountMicro可为0，同时保留token_state及赠额hold，必须记existing-reservation，并在原MySQL事务内保留原Token租约。需要原reservation时existing-reservation意为关联原资金系统在本次事务已确认的预留，不是再建缓存资金表；不得只用最终现金金额判别或丢弃赠额hold。旧路由已有reservation／usage而没有本新信封完整见证时，不能补造受理事实：明确冲突或按原键对账未知，未证实前不调用。旧AppKey/BYOK不会因这一规则自动取得私人分组支持。

COMMIT后才可沿既有网关机制调用provider。COMMIT未知仅查／重放**同一原owner键和完整原帧**，不能走旧route补发或换ID。已存在accepted／result-unknown只是历史受理事实，重放／查询不再touch、不再次预扣，也不产生provider执行许可；已完成只取原结果。提交受理到provider启动／终结之间崩溃若缺确证，保留未决，不自动换实例接管重跑。真实执行与对账须另接原网关恢复逻辑，本参考模型不实现MySQL资金链或模型执行。

受理锚点／结果引用计入统一库存与回执行／字节帽。未决不得TTL删除；终局回收正文／回执时仍要在原ID可能再次提交期间保留有界拒绝墓碑或等价原唯一约束（零预扣亦然），不能以缓存epoch退休重新接纳原财务ID。帽满拒绝新受理，不能先接纳后丢去重。最终锁序、单库迁移、两连接CAS、真实COMMIT丢响应、零预扣崩溃和保留策略仍是API实施验收门，当前只完成合同与内存事务参考。

### 4.2 客户端侧 C2 单一耐久点（PERF-01(c)，2026-10-03）

宿主侧 `GatewayCacheClient` 对每次 C2 交换保留两条耐久记录：原帧 `exchange/<requestId>`（core 投影时另有 `core-exchange/<requestId>` 固定 intent，首次迁入时还有 metadata 升格）与在飞标记 `flight/<requestId>`。此前二者分两次 SQLite 事务提交（prepare 时写原帧，send 交 fetch 前写在飞标记），热窗口（send → 首派发）承担两次 FULL 同步 COMMIT，且 prepare 与 send 之间崩溃会留下"有原帧、无在飞标记"的中间态，恢复侧只能把从未出网的请求保守判为 `result_unknown`。

自本条起二者合并为**同一耐久点、单一事务**：prepare 只建内存项并完成全部本地门禁（容量帽、同键异义、投影有效、票据未过期）；原帧、core intent、metadata 升格与在飞标记在 send 完成能力／尺寸／令牌／证明检查之后、把请求交给 fetch 之前，于**一次** `BEGIN IMMEDIATE … COMMIT` 内提交。耐久点仍在 send → 首派发窗口之内、fetch 之前，§2 "先在本地事务固定原请求再发API" 的次序不变，只是窗口内的两次 COMMIT 收为一次。

metadata 升格（`sdk2-cache-client-v1` → `sdk2-cache-client-core-v1`）是**客户端级待办**，不挂在某一条交换上：迁入判定在首个 Core 请求 prepare 时即于内存生效（随后的 prepare 不再重复迁入），升格记录由**首个落盘**的交换事务携带——不论该事务属于哪个 requestId——任何其它写入 metadata 的事务亦结清该待办。由此保持不变量：磁盘上出现任一 `core-exchange/<requestId>` 行时 metadata 必已是 core 格式（同事务或更早）。反例（PERF-01-CD 接续复验发现并修正）：若把升格挂在首个 Core 条目 A 上，而 B 随后 prepare 并先 send，磁盘会出现 `core-exchange/B` + 旧格式 metadata，恢复时按"旧格式库不得含 Core 行"拒读整库。

恢复语义因此收紧为"皆有或皆无"：

- 事务前（prepare 之后、send 之前）崩溃：磁盘无任何该请求痕迹；恢复后 `pendingExchanges()` 不含该键，同键新请求不被 `result_unknown` 锁止——因为模型请求从未发出；
- 事务中（INSERT 已写入日志、COMMIT 未执行）崩溃：DELETE 日志回滚，两键皆无，语义同上；
- 事务后（COMMIT 已完成，响应丢失或进程被杀）崩溃：两键皆有，恢复为 `uncertain: true`，只允许重放原帧或查询原操作，新键锁止，完成后写 `completed/` 墓碑并同事务删除两键（`finish` 不变）。

兼容：合并前旧格式可能留下无 `flight/` 的 `exchange/`；恢复仍接受并保守按 `result_unknown` 处理，其首次重放补写在飞标记（同事务幂等覆写原帧），之后不再出现该中间态。已耐久的重放项（恢复自磁盘或同进程内重试）在 send 不再写盘。本条不改变帽（128 控制／16 交换／4096 完成／64 MiB）、不改变 C1 控制操作的 `pending-control` 两段耐久、不改变 Serve 侧 vault 预留。崩溃注入回归：`packages/providers/test/twp/cache-durable-client.test.ts`（事务前／中／后三态 + 热窗口恰一次 COMMIT）。

## 5. 核心有效投影与网关观察分域（C1）

ProjectionWitness仅由核心＋provider受信适配产出，ProjectionPublish是内部发布接口，没有公网写摘要／历史路由。终端仅保存核心发出的不透明projectionRef和原Store／档案材料，不能自行填写witness变为权威。公共BindingView只返回ProjectionView，不暴露指纹、原组键或provider凭据域。

有效投影来源可为原SessionStore或已验证ArchiveStore，但必须是**当前有效历史**、已提交摘要及其coverage，不能取压缩前整段档案灌回。source.reference指向受控持久对象，effectiveSnapshot为本域HMAC；summaryCoverage为空表示没有档案序号覆盖，不能把message seq／SSE cursor当coverage。历史、删除、投影代际和sourceSnapshot全核，不因resume票据合法就跳过材料验证。

PolicyWitness是显式判别联合，不能用一套看似齐全的版本字段掩盖事实来源：

| 见证域 | 必填事实 | 明确不能宣称 |
|---|---|---|
| CorePolicyWitness：kind=core-effective-v1 | 核心实际生效的authorization／prompt／capability／permission／memory／toolSet／systemLayerPolicy七修订，真实generations、assemblerVersion；source为session-store或archive，gatewayInput=null | 最新平台配置已在旧逻辑轮生效；API凭输入hash即可知道这些版本 |
| GatewayPolicyWitness：kind=gateway-observed-v1 | 网关自有耐久observationRevision、当次已验授权事实authorizationSnapshot和当前平台配置platformConfigSnapshot的本域HMAC；corePolicy=null | 核心七版本、历史／删除代际、摘要coverage、P/S/A组装来源及恢复权限 |

网关ProjectionWitness.source.kind=gateway-request，summaryCoverage、generations和assemblerVersion固定null；serializerVersion只指实际TWP解析／转换合同，不伪造核心serializer版本。gatewayInput必填，分别见证实际校验后的原TWP、实际生效system、tools、净化参数及最终pool成员provider请求；素材保持旧序列化的原始数值／字段语义，不能用控制JSON重编码。provider adapter／face.cache版本、真实模型／协议／endpoint／account或project／region的部署域由最终buildBody／headersOf路径确定，不使用早期显示模型别名。实际参数或模型成员变化需重算对应投影；HMAC钥／比较域未知则not-comparable。

observationRevision只是网关自身**授权／平台配置观察**的版本：同一持久mapping域以CAS单调推进，不能用updatedAt、TTL、固定0或1冒充核心版本；普通模型messages／工具循环输入变化更新ProjectionWitness，不把每次正文改变都伪装成policy变更。authorizationSnapshot由当前鉴权事实取得，HMAC仅作本域内容见证，不可替代实时鉴权或撤权。当前平台配置与本轮实际system是两项独立事实：配置已更新但旧P/S/A轮仍固定时，可以记录新配置观察，却绝不自行组装／替换旧system。

API不信任终端传来的“核心已验证”声明。ExchangeMetadata.projectionRef只是当前绑定内来源关联，不是公共witness上传口，不授予材料或resume权限；本域网关投影必须从实际当前输入重算。未知核心事实始终null，不能从gateway hash反推历史来源。未来要关联核心签发见证，必须另行会签有认证、作用域和代际的可信委托合同，不能藏进旧TWP meta。跨core／gateway域及跨scope／密钥域不可直接比较指纹，输入相同亦不提升为核心档案权威。

prepare阶段读取同一会话排他边界内的旧有效快照、当前权限／P-S-A及来源见证，组装候选并持久保存引用；最终同步发布前重验历史成员／深值、代际及策略见证。失败不安装派生态、不回滚已发生usage；原capture／publish屏障复用，不能在旧默认路径插入等待。并发材料、迟到旧投影或恢复期间提示词更新均拒绝过期发布后重新取当前事实。

平台／SDK提示词沿SDK1既有P-S-A生命周期：在**逻辑轮启动**由prepareApplicationPromptTurn／prepareSystem刷新，本轮工具循环和模型重试继续使用该轮固定system；缓存合同不在轮中重新拉取／改写system。下一逻辑轮（含重连后下一次发送）加载新提示词时，使用新有效promptRevision和请求前缀见证使旧缓存比较失效。普通记忆／环境更新同样按已有屏障生效，不能由缓存另造刷新时机。

各次provider调用仍按原策略复验撤权、费用及运行准入，必要时拒绝该调用；安全撤权不因固定system或缓存票据而延迟。CorePolicyWitness记录的是当次**实际生效**的轮快照及策略；GatewayPolicyWitness仅记录自己的观察，两者不得互相补齐未知字段。核心见证不能把“平台已有新版本但旧设计尚未到刷新点”误写成旧轮已采用新system。正常核心治理可在轮内更新历史／摘要，按原屏障发布新投影revision；捕获原文和有效投影仍是不同事实。缓存接线不得放宽既有安全拒绝，也不得为了命中改变旧轮的prompt／工具行为。

### 5.1 两跳错误状态校准（2026-09-26）

新缓存错误合同覆盖 API 与 Serve 两跳。以下是既有产品分支的候选 schema 校准，不改变已运行路径的状态、错误名或重试建议，也不扩充票据权限。

| code | HTTP 状态 | 实际边界 |
| --- | --- | --- |
| mapping_unavailable | 404、409、503 | Serve 对同域不存在、跨用户不可见的绑定或票据返回404；原关系冲突、已补偿取消等返回409；API不可用映射及未给状态的上游错误返回503 |
| capacity_exceeded | 429、503 | 原限流/配额拒绝429；Serve进程过载、在飞请求体预算或认证后端不可用503 |
| epoch_unavailable | 409、503 | Serve原本地epoch不匹配/已轮换冲突409；API的epoch不可用503 |

mapping_unavailable的404/409必须为`retryAction:none`和`fallback:none`；capacity_exceeded和epoch_unavailable保持原fallback:none。其余旧约束及mapping_unavailable的503合同不变。404/409不证明能力未实现、不授权自动冷建或丢弃旧关系；503也不证明副作用未受理。客户端保留失败、原请求身份和未知状态，不能依据上述状态自动重发模型、换requestId或改走旧route。原失回只查/重放原意图的规则不变。

## 6. 指纹、前缀和供应商边界

稳定cacheGroup不取每轮全文hash、不混运行ID／费用ID；私人组向上游的键由API受控HMAC(scope、groupRef、groupGeneration、实际provider域、shardVersion)派生，长度服从实际adapter（首链≤64 ASCII）。组不等于请求指纹。若现有adapter仅支持应用owner＋日亲和，则新私人组不能冒称隔离生效：明确该adapter未接合，保留旧路径，不静默把私人组压成应用共享组。

投影及最终provider请求的指纹只在受信serve/API侧计算。HMAC-SHA256使用独立作用域密钥与keyId，域分别`tansr.sdk2.effective-projection.v1`、`tansr.sdk2.provider-prefix.v1`、`tansr.sdk2.provider-request.v1`；素材为长度编码的scope元组／逻辑引用／provider域／版本＋**原有效IR或实际provider可见字节**。这些字节不经新控制元数据数值规范改写。密钥轮换后标key-rotated，不能跨keyId强比相等。密钥不放客户端、URL、模型prompt或日志。

最多64个累积段，index从0连续；遵守provider真实工具／system／messages顺序，不能为排序改变角色或权限。每段见证包含之前所有可见前缀，不能拼出独立片段KV；最早变化段只在scope、keyId、provider域和比较版本一致时有意义。计时、nonce和诊断ID不进入模型可见前缀；真实当前时间、环境或必要政策变化仍须供给。实际请求的参数、工具schema、附件方式、reasoning配置等哪些影响前缀，以具体adapter字段清单版本化，不靠全HTTP字节相等推断KV相等。

2026-09-17重新读取官方资料，以下是直连事实，非兼容代理实测：

| 面 | 具名一手证据及当前边界 | 本合同处理 |
|---|---|---|
| OpenAI Responses／Chat | [官方缓存指南](https://developers.openai.com/api/docs/guides/prompt-caching)：GPT-5.6及以后key主要可分组计账，较早模型影响路由；key不保证命中；最小前缀、断点、保留和读写费用依模型／设置 | 原face.cache及实走adapter为准，未知参数不发送；新options／显式断点尚无本仓全消费实证，不据文档即开启 |
| Anthropic Messages | [官方缓存指南](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)：tools→system→messages完整前缀，至多4断点；最低长度随模型；5分钟／1小时及各自读写usage | 复用当前anthropic-block的有限断点；未实现直连automatic top-level能力不向代理追加 |
| 供应商诊断 | [Claude诊断说明](https://platform.claude.com/docs/en/build-with-claude/cache-diagnostics)：前缀比较与cache_read_input_tokens是不同证据，未返回比较不等于命中 | 新diagnostic先使用本地事实＋已有usage；尚未适配的供应商诊断保持unknown，不发未经核准参数 |
| ModelVerse、百炼等兼容面 | 仓内prompt-cache-caps／face.cache、member-cache-facts及adapter注册为现有能力事实源，旧实测仅按当时具体成员成立 | 缺具体model／endpoint／协议／区域证据时不宣告该新组、TTL、账单折扣已兑现；正式实测单独登记 |

不得以协议名OpenAI推断直连能力，不自建矛盾model能力目录。provider缓存命中只看实报cacheReadTokens>0；0代表本次没有实报读取，可能是首写而非故障；字段缺席为null／unknown。ModelVerse亲和成功或prefix相等不构成缓存命中证据。

## 7. 有限诊断与计量

Diagnostic仅带受域binding／request诊断引用、组代际、前缀比较结果、有限原因、实报usage和延迟／重试数量；不含正文、票据、原用户号、裸内容hash、HMAC指纹、raw groupRef、provider key或request body SHA。读诊断需独立同域诊断权限；默认1%确定采样，每用户1000／应用10000／全局100000行、每行4KiB、24小时保留，单页100且控制响应≤64KiB，超帽缩页不截断单行。无有限保留配置不得启用。

causeAuthority区分local-fact、provider-report、unknown。本地组轮换／策略更新／前缀变化可确认；保留过期、机器溢出等无上游证据只能unknown，不能以cached_tokens=0猜因。关键拒绝安全审计沿现有审计合同，不因诊断采样而消失，也不把全量请求复制成新无界日志。

Usage保持原调用逐应用／用户／运行归属，不另建收费账本。provider-reported与账单核销分离；billingVerified只有既有对账证据才能true。OpenAI总input与Anthropic分列语义由原usage适配器归一，不能盲相加重复计费。最终经济评估计入未缓存输入、读／写、输出、摘要辅助、重试、网络与适用持久存储费用，未知项不能填0。

## 8. 先定实验，再测量

本节为候选测量协议`cache-baseline-v1`，须随本RFC独立会签后才能跑首轮性能／费用实验。现在没有实测结果或节费承诺；合同测试验证的是状态和编码。

固定12题：短文本；长文本稳定前缀；大工具结果；图像附件；活跃断线重连；旧Store冷恢复；410合法换运行号；microcompact／compact；切模及健康fallback；提示词／撤权／删除变化；双实例并发resume／轮换；超TTL及断网未知响应恢复。每题记录输入／模型／策略／版本／机器SHA，一套受控输出与一套具名真实provider，不能为了凑缓存门槛填无用文本。每题先5次热身，30组A/B配对，顺序交替；保留所有失败与重试，题目去重不缩分母。

先验门槛：默认关闭新增IO／网络／持久等待为0；所有旧SDK1有效基线新增失败0；跨域越权／过期投影／重复费用0；应可比受控恢复前缀一致100%；必要失效及时执行100%。局部实现的P95额外热路径延迟≤max(10ms,原P95的5%)、冷恢复≤max(50ms,原P95的10%)；进程内每活跃逻辑映射额外常驻内存≤32KiB（不含原上下文），所有持久行／队列／字节不超过协商帽。由最终实测环境报告噪声与95%区间；超门判失败或明确延期，不能事后抬帽。

真实provider只在账号／模型／协议／区域／费用资料完备且用户授权付费调用后运行。须保留最终请求指纹、实报读写、TTFT／总时长、原任务正确性和完整费用资料。正确性必须无回归；总成本改善必须配对均值下降且95%区间上界<0，才可在限定题集和该provider声称节省。未满足则写“无已证实收益”，不以高命中率抵消更高总费用。显式付费保活、跨域共享或改变资费不在本实验默认动作内。

## 9. 后继范围及准入

此候选关闭的是私人同域最小链的合同输入，不关闭SDK2-07～09整卡或全部S0／S1。跨用户公共前缀共享、组织范围授权与撤权传播、显式KV资源创建／删除、长期多来源同步、自动多实例运行接管、实际平台迁移与每端持久票据加密、完整供应商新选项适配及真实费用评估继续保留。它们须新增可识别能力与各自完整状态机，不在本能力字段中偷偷扩大scope。

多实例缓存映射的CAS／容量／恢复是本首链必需实现与实测，不因完整多实例运行接管尚未完成而被删掉。SDK1兼容矩阵、五形态、Windows／Linux／macOS／Android／iOS／HarmonyOS恢复及48项S2A都按最终实现另验；模拟参考模型不替代SQLite/MySQL提交、网络SSE、旧包替库或真机。

独立会签顺序：schema／固定字节／错误及参考反例→API身份与持久存储审查→核心投影／P-S-A审查→实验阈值确认→根任务登记有限产品写区。没有跨仓API负责人实际确认，不宣称生产网关已实现新路由。任何本合同未覆盖而实现必需的字段先回合同修订，不能以程序注释补隐形协议。

## 10. C1／C2候选迁移及编码器边界

本次v0.2修订仅影响未发布SDK2候选的host-only见证、持久映射策略形状和新增受理锚点合同。旧无kind的PolicyWitness、gateway伪核心generations／assembler／七版本不自动升级；实现前必须显式迁移可信核心记录或失效重建其本域观察，未知来源不能强制标成core。core／gateway域不得在已有mapping上原位切换以绕过恢复授权。已封存v0.1／v0.2前输入哈希及参考结果只作为历史证据，不覆盖。

公开ExchangeMetadata仍为原七必填字段protocol、bindingId、ticket、runtimeSessionId、projectionRef、payloadBytes、payloadSha256；各引用定义、32MiB＋64KiB＋4体帽、Ticket canonical 32随机字节语义、精确原TWP字节均不变。API纯codec的结构mirror与固定原帧golden继续成立，冻结codec仅负责解析／复制／校验，不会因新host-only定义自动获得认证、映射、资金或provider接线。原SDK1、原TWP、旧API路由及默认零新增IO路径无变化。

本轮定向反例：gateway缺／伪造核心事实拒绝、不同见证域不可比较／刷新互换、网关观察CAS不改旧轮输入；同owner跨app／用户／绑定／运行／票据／projectionRef／原数字字节复用ID拒绝；不同可信owner独立；真正无reservation仍有受理锚点，gross>0／cash=0的赠Token覆盖仍保留原reservation及Token占用；COMMIT丢响应冷实例只读原事实；容量失败使钱款／锚点／TTL全回滚，旧路由资金事实不得补造成新信封受理。均为合同参考证据，不替代真实MySQL、provider、部署或节费实证。

## 11. 实际网关的首次投影与来源关联生命周期

2026-09-18实际API接线补充。本节明确§5已经限定的网关来源关联语义，使用冻结的GatewayOpenRequest、BindingView／ProjectionView和ExchangeMetadata；不修改schema、固定字节金样或封套七字段，不新增公网witness上传口。CLI／SDK／serve本批产品未改；实现及实证范围见[全面开发登记§24](../report/SDK2-全面开发执行与问题登记-2026-09-17.md#24-api真实c1写入公开c2与恢复维护)。

GatewayOpenRequest只有当前受验runtime及关系意图，此时尚没有最终provider请求，不能在open时声称已建立有效投影。API同事务创建mapping、binding、票据和原操作加密回执，为binding固定服务端随机来源关联ref；公开projection返回`{projectionRef, revision:"0", status:"missing", reason:"source-missing"}`，数据库不插入伪造ProjectionWitness。该ref仅用于随后新exchange的必填projectionRef，不代表材料权限、客户端指纹或可用供应商KV。

首次exchange继续携原七字段及精确原TWP字节。网关在既有模型／权限／日额度链路通过、最终成员的buildBody／headers实际完成后，依据当前认证、mapping政策和真实provider可见请求构造网关witness。在原owner连接事务内重核当前binding／票据／关联ref及投影revision，首次发布witness，并与原资金预扣／赠Token持有、admission、idle和新增投影库存一起提交。只有确认COMMIT及本地终结后，原首次许可才可启动provider；未知提交仅沿原键查事实，不能补走旧请求或再预扣。安全换员仍需从实际新成员重算投影和当前权限，不派生第二财务身份。

正常正文／工具循环变化沿当前关联ref重算witness并以revision CAS递增，不因此改写mapping政策版本、私人组或逻辑轮固定system；旧候选revision拒绝发布。renew只替换本绑定票据，可保留仍有效的当前投影。rotate、rebind及内部政策刷新使投影失效时，须同时为受影响binding换一个服务端新ref并返回missing视图；旧ref／旧witness继续保留计量，不能把revoked行原地当首次投影复活。下一实际请求使用新ref发布revision为1的真实witness，旧帧不能借新票据越过关联检查。close仅关闭缓存绑定，不关闭SDK1业务会话。

当前认证先于原控制回执，同键同义原回执先于新epoch、CAS和票据TTL；读回历史票据不续期或授予当前执行。原操作查询沿owner锁等待在途控制事务；当前活跃epoch缺行返回result_unknown／query-status，退休或未知epoch下已无可读回执才返回receipt_expired。当前策略由真实授权／平台配置HMAC观察并在数据库CAS推进observationRevision，不能把固定初值冒充持续观察；普通配置变化rotate，授权边界变化revoke，公网不能自行提交这些见证。

网关启动仍默认off；active须已完成所有可触及同一资金域实例的guard接线与排空，并配置独立32字节钥环，保留所有仍有receipt_key_id／semantic_key_id引用的旧keyId，须待相关回执实际GC清零才能退钥。配置中的fleet-ready声明只是装配前置，不是实际全实例部署实证。096／097必须通过原迁移流程先行。epoch由可信初始化／维护产生，GET不铸造；新控制和exchange的成功、错误、SSE及重放均no-store。

维护分开处理：C2仅压缩超过保留期的completed详细回执，保留原完整identity／财务唯一锚与代际，不按年龄重发或删除accepted／unknown；C1单批至多100行，仅回收退休且过期epoch下到期原回执、失效票据及无回执依赖的退休epoch，并同事务扣库存。长期mapping／closed binding／revoked projection仍保留计量，其安全GC、可信跨runtime血缘与原生介质尚待后继。当前缺独立可信血缘时resume／rebind仅支持已登记的同runtime，不凭票据或自报session恢复另一运行；不据此宣布SDK2缓存整链或最终48项已完成。
