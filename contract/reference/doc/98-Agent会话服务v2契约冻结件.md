# doc/98 — /v2 Agent 会话服务契约 v1(P0 冻结件)

> **wire 权威**:本档为 /v2 面协议的唯一准绳文本(doc/96 §二自本档落定起退居施工导读,冲突以本档为准);架构与取舍依据 = doc/94(最高设计依据)。schema 持份:monorepo 侧 `packages/server/src/v2/contract.ts`(zod,与本档同笔冻结)+ tansr-android 侧 kotlinx.serialization 持份(P1 卡 A1-1 落仓时对表)。版本锚:双仓同名常量 **`AGENT_SESSION_CONTRACT_VERSION = 'v1'`**;对齐验证 = 常量+字段名+类型+约束逐项对表(doc/89 头注同律)。
>
> 改动纪律(doc/96 §1.4):可选位加法 + 未知键忽略;双仓 schema 同笔 + 本档 §十增笔区逐笔登记(日期/变更/双仓提交号);单边改动即违约;破坏性改动恒 bump 前缀(/v3),恒不在 v2 内破。
>
> **服务端定名(doc/118 §八 G1,2026-09-03 用户授权代拍)**:本档所述服务端 = tansr **Agent 会话引擎**(npm 包 `@tansr/serve`,workspace 名 `@tansr/server`);「网关」一词在 tansr 体系内只指 tansr-api——引擎恒不承担网关职责(鉴权策略/限流/配额/计费归开发者登录态体系与平台侧;§〇-2 最大放权拍板不变),其准入帽、`rateLimit` 缝与上游治理层是自保护面。定名不改任何 wire 字面;Kotlin/Swift 持份零随动。

> SDK2加法执行端点独立说明：[RFC-USDK-1实施切面](rfc/RFC-USDK-1-运行端与终端执行契约.md#1-目的与不变量)。原24端点、常量与已有字段语义保持；八个执行端点使用原SDK2单源具名定义，不修改本冻结合同来覆盖旧消费者。

## 〇、决策点终拍登记

用户 2026-08-31 凌晨「可以继续开发安卓 SDK」即 P0 开工授权;以下五项随契约冻结登记终拍(doc/94 §十建议值,冻结提交前系用户改拍最后窗口):

| # | 决策点 | 终拍结论 | 登记 |
|---|---|---|---|
| D1 | 传输选型 | **A. SSE(下行)+ POST(上行)** | 按建议值终拍,2026-08-31 |
| D2 | 事件下发粒度 | **A. KernelEvent 直下发(客户端 reduce)** | 按建议值终拍,2026-08-31 |
| D7 | 会话服务对平台身份 | **A. per-endUser app_user 令牌(服务端自换自续)** | 按建议值终拍,2026-08-31 |
| D8 | 用户消息图片位 | **A. /v2 messages 加 blocks 位(空闲态;运行中恒 422)** | 按建议值终拍,2026-08-31 |
| D11 | /v2 面命名 | **A. `/v2/sessions`** | 按建议值终拍,2026-08-31 |

doc/96 全篇 ⚠ 施工基准位在本档 §二—§八逐处定稿;doc/97 §五 D 点勾格由主线登记。

## 一、总纪律

1. **前缀并列**:`/v2` 与 `/v1` 同进程同端口;/v1 六端点逐字节不动(金样卷 75c88d43 钉基线,diff 恒空即红线);/v2 分支在 /v1 匹配失败后才参与路由(路由顺序恒 /v1 先)。
2. **鉴权模型(架构拍板 2026-08-31 定盘:最大放权)**:/v2 恒不受理 /v1 Bearer token(两面身份体系刻意不通)。会话鉴权**完全归开发者**——serve 是裸 `/v2` 协议引擎(npm 包嵌进开发者自己的 node 服务),唯一鉴权面 = `authenticateEndUser(req) → Promise<{ endUserId } | null>` 注入缝(`StartServerOptions.v2.authenticate`,冻结签名见 contract.ts 注释);凭据形制/签发/校验恒不由 serve 规定(自家 JWT/session cookie/OAuth/API key 皆可),null/缺席凭证 → 401 `unauthorized`。serve 恒不内置任何 token 服务(原附录 B 内置 token-server 同日撤笔)。`endUserId` 形制 = doc/89 契约 1(`^[\x21-\x7E]{1,128}$`)。
3. **归属强制(⚠定稿:统一 403)**:每会话行绑 endUserId;一切 `:id` 端点(含 resume 目标)在业务逻辑前先校验「鉴权产物 == 会话归属」,不符恒 **403 `forbidden`**——含 resume 他租户 store 会话(与 doc/96 §2.2-① 的 404 备选二选一,取 403:多租矩阵验收锚统一、会话 id 恒非鉴权因子(§八-5),存在性泄露面可忽略)。
4. **错误信封**:`{ "error": { "code": "<机器码>", "message": "<i18n 文案>" } }`(/v1 sendError 同形);code 恒稳定机器码(§七词表),message 恒过 i18n `t()` 不承诺字面。
5. **载荷演进**:请求端未知键忽略;响应端新增键不通知;客户端(Kotlin)未知事件型/未知键恒容忍(events.ts L9 向前兼容铁律)。
6. **体帽**:见 §六体帽表;超限恒 413 `payload_too_large`。

## 二、端点全形(21 端点;①–⑪ P0 冻结,⑫–⑯ 会话持久化与上下文压缩增笔 2026-09-04,⑰⑱ + ①-g fork 与快照导出/导入二期增笔 2026-09-04,⑲ cwd 中途切换增笔 2026-09-05,⑳㉑ 音频直连增笔 2026-09-05)

### ① POST /v2/sessions — 创建 / attach / resume

```jsonc
// 请求(全键可选,除非另注)
{
  "prompt": "...",                    // 可选:初始输入;缺省空闲等待
  "model": "main",                    // 可选:/v1 SessionInit 同形
  "profile": "...",                   // 可选:同上
  "labels": { "runId": "..." },       // 可选:同上(SessionLabels 契约)
  "budget": { "maxUsd": 1, "maxTokens": 10000 },   // 可选:同上(账本口径 kernel 单点;思考 token 自 RF-P3a 起按各行模型能力位归账,doc/07 §1.6 ⑧补二;wire 零变化)
  "tools": ["Read", "Grep"],          // 可选:装配集收窄白名单(只减不增,/v1 同律)
  "endUser": { "id": "u_123" },       // 可选:双写一致校验——在场且与鉴权产物不符即 403
  "resume": { "sessionId": "..." },   // 可选:三级恢复链 L3(§五)
  "clientTools": [ /* §四申报形制 */ ],
  "capabilitiesProfile": "kiosk-two",       // 可选:服务端持档名(§二-①-c;宿主自定义档 = 交集收窄;内置 mobile-default = hosted 派生集,不收窄)
  "thinking": { "budget": 4096 },           // 可选:思考生成(§二-①-d;S-TH2 加法 2026-09-01)
  "cwd": "/srv/tenants/u_123/proj",         // 可选:会话工作目录(§二-①-f;doc/116 V-3 加法 2026-09-04)
  "fork": { "sessionId": "...", "checkpointId": "..." }   // 可选:自源会话快照 fork 出新会话(§二-①-g;doc/117 F-3 加法 2026-09-04;与 resume 互斥)
}
```

| 径 | 响应 | 语义 |
|---|---|---|
| 新建 | `201 { "sessionId", "resumed": false, "lastSeq": 0 }` | sessionId 恒服务端铸 UUID,客户端恒不供给 |
| attach | `200 { "sessionId", "resumed": false, "lastSeq": <水位> }` | resume 目标仍活跃于内存:不重灌,回当前流水位;本径忽略 prompt/clientTools 等其余键(附着不是重建) |
| resume 重灌 | `201 { "sessionId", "resumed": true, "lastSeq": <新纪元水位> }` | store 取回 → pairing 治理(§五)→ initialMessages 重灌起新驱动;**seq 新纪元自持久化水位续起(改笔 2026-09-11,RF-05b;起点规则 §三-2;此前从 0 起、响应 `lastSeq` 恒 0)**——L3 客户端仍弃旧游标全量重建(§五-5.3),持旧纪元最终游标的 L1 重连零 gap 续得新纪元首帧;sessionId 沿用原值;clientTools 随本次申报重建(恒不继承旧申报) |

- 失败形:store 未接线 resume → `409 resume_unavailable`;目标不在 store(本租户域内)→ `404 session_not_found`;归属不符 → `403 forbidden`(§一-3);per-endUser 活跃会话超帽 → `429 session_limit_exceeded`;载荷非法 → `400 validation_failed`;装配失败(未知别名/未知 profile 等)→ `400 create_failed`;`cwd` 被策略拒 → `400 cwd_not_allowed`;`cwd` 形态非法 → `400 cwd_invalid`;resume 按存储目录而目录已失 → `409 cwd_unavailable`(①-f)。
- **①-c capabilitiesProfile(⚠定稿)**:值 = 服务端配置的能力档名,解析为工具收窄白名单后与 `tools`(在场时)取交集,再叠加本次申报的 clientTools 名(customTools 语义);未知档名 → 400 `validation_failed`。档只能收窄装配集,恒不放宽(server/types.ts L24-25 同律)。**内置档 `mobile-default`(十王修案 FX-C-13 / N-11 改口,2026-09-07)= hosted 拓扑派生集**:sdk 契约 `CAPABILITY_TOPOLOGIES.hosted` 可勾选位全集对应的内置工具名(TodoWrite / AskUser / WebSearch / ImageGen / VideoGen / SpeechToText / TextToSpeech;customTools 由 clientTools 独立通路;单源 = sdk 导出 `hostedToolUniverse()`(FX-B-07),serve `BUILTIN_CAPABILITY_PROFILES['mobile-default']` 取其返回值),恒 ⊇ 按 bundle 位装配出的全集——带它 = 拿到控制台开的全部位,**不再是** doc/94 时代的 `["TodoWrite","AskUser"]` 两名字面(彼时控制台刚开的平台位会被静默剥掉,零告警)。**宿主自定义档**(`capabilityProfiles`)仍为交集收窄;当档实际剥掉了按 bundle 位装配出的工具时,服务端在装配期下发控制帧 `server.platform.warning { code: 'profile_narrowed_capabilities', message, dropped: string[] }`(§三 帧词表既有族,`dropped` 为载荷加法键 = 装配全集 − 档;紧随 `session.created` 占 seq 1),端上据此呈现「为什么控制台开了却不可用」;缺省档零剥不发;请求侧 `tools` 白名单自愿收窄不归此帧。
  - **行为增笔(doc/112 P-5,2026-09-02;契约形状零变更)**:平台内置形(`createAgentSessionFactory({ platform })`)的**装配集本体**改为按 bundle 有效能力档(平台 App 档,doc/89 §2.4)逐位装配——TodoWrite/AskUser/WebSearch(双门)/ImageGen/VideoGen 按位,clientTools 受 `tools.customTools` 门:**位关而本次申报 clientTools 非空 → 400 `validation_failed`**(消息携控制台修复指引;新失败形,加入上条失败形清单)。本键在其上仍只收窄(应用已为 `mobile` 拓扑时不传亦得 doc/94 §六档)。应用平台类型非 `mobile` → 控制帧 `server.platform.warning { code: 'app_platform_mismatch' }`(§三 帧词表既有族,**每 App 首次一次(装配器闭包级),非每会话**——同 App 后续会话不再下发;不拒)。注入形内层工厂(cli 重装配)零变化。**时序注记**:该帧产生于装配期(早于首轮),服务端在桥出线缝绑定前缓冲、绑定后冲刷——故 `server.platform.warning` 现可出现在 **seq 1(紧随 `session.created`)**,此前该族帧只在模型交换期出现;端上按「未知/已知帧按 code 渲染」无影响,消费方恒不得假定该帧仅轮内出现。旧 api(无 `app.platform` 键)下 platform 判 null → 亦发一次本帧(消息标 unknown),api 与 serve 同批上产即消失。
- **①-d thinking(S-TH2 加法,用户拍板 2026-09-01)**:`{ budget?: 正整数 }` = 思考生成预算,服务端逐轮透传内核 IRRequest.thinking(平台令牌档经 TWP reasoning 档位出线,budget≤1024→low/≤4096→medium/其余→high;模型方言映射 qwen 系 enable_thinking / anthropic thinking.budget_tokens)。取值三级恒定序:**wire 本键(per-会话)> 装配面缺省(平台形工厂 `options.thinking` / cli 形 profile effort 档)> 恒不注入(现状)**。思考流以既有 `msg.thinking.delta` 事件出线(客户端显隐走呈现档 delivery,与本键正交);attach 径同 §二-①表忽略本键(附着不是重建),resume 重灌径受理。
- **①-f cwd(doc/116 §3.7 / doc/117 V-3 加法,2026-09-04;D-6 A 代拍)**:`cwd?: string`(非空;**绝对路径**,`~` 恒不展开——展开是请求方职责)= 本会话工作目录(工具执行、权限工作区根、权限桥 digest、快照 `meta.cwd`、store `meta.cwd`、`session.created.cwd` 六处同值)。服务端经 kernel `resolveSessionCwd` + 工厂 `cwdPolicy` 闸:**缺省无策略 = 不接受 body.cwd**(携即 `400 cwd_not_allowed`,fail-closed:hosted 面不信任客户端路径;不携 = 服务器缺省 cwd,现状零漂移);`{ allowedRoots }` 策略按 realpath 子树判定(symlink/junction 指向根外亦拒);`{ resolve(requested, { endUserId, fallback }) }` 策略由宿主映射(返回 null / 抛错 = 拒)。形态四查:非绝对 / 不存在 / 非目录 → `400 cwd_invalid`(message 携 reason);越界 / 无策略 / 策略拒 → `400 cwd_not_allowed`。**attach 径忽略本键**(附着不是重建);**resume 径受理**并与 store `meta.cwd` 对账(**改口 2026-09-07,十王修案 FX-C-08 / 候拍 ⑪ / doc/130 R-04**):工厂 `cwdOnResume` 两策——`'stored'`(**配了 `cwdPolicy` 的宿主缺省**)按存储值运行,且存储值**重过** `resolveSessionCwd` 策略闸:策略拒 / 策略缺席 → `409 cwd_unavailable { detail.reason: 'not_allowed_on_resume' }`,目录已不存在 → `409 cwd_unavailable { detail.reason: 'missing' }`(`detail` 为加法键,两因端侧可判;恒不静默改用本次——会话建在专属目录的宿主自此不会被静默迁回服务器缺省 cwd);`'current'`(**未配 `cwdPolicy` 的宿主缺省**——这类宿主从不接受客户端 cwd,存储值 ≠ 本次只可能是服务器自身 cwd 变更;行为与此前逐字同)按本次值运行;显式写值恒优先,运维旋钮 `TANSR_SERVE_CWD_ON_RESUME=current|stored` 一键回退。两策下只要存储值 ≠ 本次值即下发一帧 `server.platform.warning { code: 'cwd_mismatch_on_resume' }`(§三-3 既有族;`'stored'` 下文案注明「按存储目录运行」;紧随 `session.created` 占 seq 1,SSE 首连重放可见——故 resume 重灌的 201 `lastSeq` 此时为 1 而非 0,与 ①-c 装配期提示帧同律,客户端恒按响应值续订不得假定 0)。`resolveSessionCwd` R-04 形(同步;`{ cwd } | { error:'cwd_unavailable', reason:'not_allowed'|'not_allowed_on_resume'|'missing'|'not_absolute' }`)由 kernel 导出(B-2 FX-B-17/片段;serve HTTP / acp / mcp 三入口同函数,serve 侧切换前为本地同签名映射 `TODO(R-04)`)。中途切换 cwd 恒不支持(二期 D-7)。**Kotlin/Swift 持份**:`CreateSessionRequest` 可选加法键 `cwd`(不传 = 现状),错误码三名进 §七 对表卡;`PlatformWarningFrame.code` 为自由串,新码零改动。
- **①-e 同 sessionId 并发创建/恢复(增笔 2026-09-03,用户授权代拍 doc/118 §八 G4-b;服务端行为增笔,端侧零改动)**:多设备/重试并发对同一 `sessionId` 发起 resume(或与在飞 resume 撞同 id)时,服务端把「取回 → 重灌 → 纳管」全链合并为**恰一次**创建;**后到者等待首请求 settle,成功后按 attach 语义回 `200 { "sessionId", "resumed": false, "lastSeq": <水位> }`**(与上表 attach 行同律:附着不是重建,后到者的 prompt/clientTools 等其余键忽略),首请求失败则各自按原错译码(404/403/409/400/503 同表)。归属先比对:后到者 endUser 与首请求不符恒 403,恒不等待他租户的创建。**残影逐出时序**:resume 目标若为保留窗内的终结残影,其内存记录**在新驱动创建成功之后**才逐出让位——失败的 resume 不再毁掉其他在线设备对该会话的 `Last-Event-ID` 重放窗。优雅关闭期(§五-5.5)新建/resume 一律 `503 draining`。

### ② POST /v2/sessions/:id/messages — 注入输入

```jsonc
{ "prompt": "..." }                          // 纯文本形
// 或(D8 终拍)
{ "blocks": [ { "t": "text", "text": "..." },
              { "t": "image", "mime": "image/jpeg", "data": "<base64>" } ] }
// 响应:202 { "sessionId": "...", "accepted": true }
```

- prompt 与 blocks 恒二选一(同在/同缺 → 400 `validation_failed`);prompt 非空 trim 后非空;
- **blocks 形制(⚠定稿)**:数组 1–64 项,项 = IRBlock 白名单子集 `{t:'text',text}`(text 1–262144 字符)| `{t:'image',mime,data}`(mime 匹配 `^image/(png|jpeg|webp|gif)$`,data 为 base64);
- 空闲态:blocks 直构 IRMessage 起轮;prompt 起轮。运行中:prompt 走内核注入屏障;**blocks 恒 422 `midturn_blocks_rejected`**(enqueueUserInput(text) 内核签名如实,扩内核属破坏面恒不做);
- 终态会话 → `409 session_ended`。

### ③ GET /v2/sessions/:id/events — SSE 事件流

- 请求:头 `Last-Event-ID`(十进制 seq;缺席 = -1 全量;非法形 → 400 `validation_failed`);query `?exclude=hook,mcp`(可选订阅过滤,见 §三-4);
- 响应头:`content-type: text/event-stream; charset=utf-8` + `cache-control: no-cache, no-transform` + `x-accel-buffering: no`;
- 帧序:首帧 `retry: 3000` → 重放段(缓冲内 seq > Last-Event-ID 逐帧;窗口有缺口时先发 gap 帧)→ 直播;心跳注释帧 `: hb` 15s(heartbeatMs 承 /v1 配置);
- 多客户端并发订阅同会话合法(多设备);会话终结后重放缓冲残影完即收流。帧形全表见 §三。

### ④ POST /v2/sessions/:id/interrupt — 中断当前轮

`202 { "sessionId", "accepted": true }`;幂等(空闲/终态为无害空操作);语义 = /v1 abort,词面与 SDK 对齐。

### ⑤ DELETE /v2/sessions/:id — 关闭会话

`202 { "sessionId", "accepted": true }`;幂等。释放运行态,内存记录进保留窗(§六)后逐出;**store 内容保留**(resume 可复活);恒不删平台注册表行。

### ⑥ GET /v2/sessions/:id — 元信息与恢复锚

```jsonc
200 {
  "sessionId": "...",
  "endUserId": "u_123",
  "status": "running" | "idle" | "ended",   // ⚠定稿词表:running=轮进行中;idle=空闲候输入;ended=运行态已终结
  "live": true,                             // false = 仅 store 可 resume(此时 status 恒 'ended';lastSeq = store 持久化水位,§三-2,改笔 2026-09-13 RF-S1,此前恒 0)
  "lastSeq": 1234,                          // 当前流水位(恢复锚;live=false 时 = 最近封印 / 落盘水位——store meta `lastSeq`,旧卷缺字段 → 0)
  "createdAt": "<ISO-8601>",
  "lastActivityAt": "<ISO-8601>",
  "title": "..."                            // 可选(store 标题;无题不落键)
}
```

字段终表如上(⚠定稿);服务端只报驱动粗态,视图七态是客户端投影。

OBS-05/09 加法（2026-09-11，开发中、未发布）：⑥及⑦活跃句柄可增加 `context` 与 `media`；旧服务、休眠记录或未提供对应能力的自实现句柄不带字段，客户端显示未知，不能以累计费用或本地猜测补值。旧客户端可忽略加法键。

- `context` 同 SDK `SessionContextState`：`selected`/`active` 为 `{model:{provider,model},alias,contextWindowTokens}`，active 无轮时 null；`lastObserved` 仅 usage 事件证据，含 model/observedAt/evidence；`lastUsage` 独立含 usage/model/observedAt；`fallback` 只有明确提供方切换证据时含 from/to/observedAt。`budget` 含 physicalWindowTokens/effectiveWindowTokens/estimatedInputTokens/outputReserveTokens/thinkingReserveTokens/remainingTokens/fits、thresholds（microcompactAt/warningAt/autoCompactAt/blockingAt/summaryReserveTokens，可 null）、source='local_estimate'、settingsSource='model'|'configured'|'external_manager'|'unknown'。无法估算的数字为 null。`sampledAt` 为毫秒，`history`='live'|'committed'，`transition`={status:'idle'|'preparing'|'committing',target:{provider,model}|null}。serve 当前无切模端点，transition 为 idle；不能据 SDK 本地切模能力推断 HTTP 有同名动作。
- `media` 为 `{capabilities:{imageGen,videoGen,speechToText,textToSpeech},models:{imageGen,videoGen,speechToText,textToSpeech},sampledAt}`。四能力为布尔授权位；四模型为顺序数组，条目沿既有媒体描述子 `{model,displayName,constraints?,outputHosting?}`，首项是该面的默认模型，空集是未配置，约束缺席即未知。TTS 使用 constraints.maxChars，汉字按2、其余码点按1计数；计费仍以上游回执为准。此为装配时授权快照，不证明工具已装配或当前授权永不变化，执行仍走既有权限及平台校验。
- 两投影均不含 token、appkey、fetch 函数、上游 URL、提示词或历史正文。客户端按需刷新⑥，不为每个 token 帧发网络请求；字段为观测面，不改变鉴权、事件流或消息协议。

### ⑦ GET /v2/sessions — 列会话

- query:`?limit=`(1–200,缺省 50)`&offset=`(≥0,缺省 0)(⚠分页形定稿);
- 响应:`200 { "sessions": [ <⑥同形> ], "total": <过滤后总数> }`;
- 恒按鉴权产物 endUserId 过滤;数据源 = store 元数据 + 内存活跃态合并(同 id 取内存态),按 lastActivityAt 倒序。

### ⑧ GET /v2/sessions/:id/history — 治理后历史快照(L2 重建消费)

```jsonc
200 { "sessionId": "...", "lastSeq": 1234, "messages": [ /* IRMessage[] */ ] }
```

- 活跃会话:驱动 `messages()` 深拷贝;**原子性(⚠定稿)**:快照与 lastSeq 在同一同步块内取,`lastSeq` = **最近一次历史收口时点的流水位**(轮末锚)——轮进行中重建者从该水位续订可重见本轮已发事件;若续订再遇 gap,重复 L2(轮末收敛,「运行中轮崩溃恒丢本轮」同律的读侧表述);
- 非活跃会话:store 读投影(轮粒度提交内容,天然配对完整),lastSeq = store meta 持久化水位(最近封印 / 落盘检查点;**改笔 2026-09-13,RF-S1 / DEC-RF-30,此前恒 0**;旧卷缺字段 → 0;resume 后新纪元起点见 §三-2——L3 恒弃游标重建,不读本值);
- **reminder 块出流滤除(代拍 A 案 2026-09-01)**:history 出流**恒不含内核 reminder 块**(store 保全,出流滤除)——内核以 user 消息 text block 形态注入的 `<system-reminder>` 元数据(todo 台账/记忆变更/会话事件等,kernel `makeReminder` 整块包裹形)在本端点响应投影里剔除;整条滤空的 meta-only 注入消息整条不出流。live SSE 径本就不含(msg.* 只流 assistant 增量,内核测试守卫在案),两径观感由此一致,客户端(TS `viewStateFromHistory` / Kotlin 同名实现)零处理义务。store 与 resume 重灌径**恒不滤**(模型上下文完整性);过滤单点 = serve routes history 投影(`history-projection.ts`),恒不得在客户端或 store 层重复实现。判据从紧(整块首尾恰为包裹标签,恒不 trim):用户正文里的转义形/内嵌形恒不误伤。
- **分页 `?offset=<n>&limit=<n>`(doc/116 §3.5 / doc/117 V-2 加法,2026-09-04;D-9 A 代拍:分页单位 = 消息下标)**:

```jsonc
GET /v2/sessions/:id/history?offset=40&limit=20
200 { "sessionId": "...", "lastSeq": 1234, "messages": [ /* IRMessage[] */ ], "total": 137, "offset": 39, "nextOffset": 61 }
```

  - **无参响应字节等价**冻结形(键集恒 `{sessionId,lastSeq,messages}`,恒不出 `total/offset/nextOffset`;测试锁);任一参数在场才出 `total`(治理后原始历史消息总数)、`offset`(**对齐后的实际起点**,≤ 请求 offset)与 `nextOffset`(下一页起点,原始下标空间);
  - `offset` / `limit` 皆为十进制**非负整数**(负数/小数/字母 → `400 validation_failed`);**`offset` 缺省 0;`limit` 缺省 = 自 `offset` 到末尾**;`limit=0` 合法 = 只取 `total`(空页);`offset ≥ total` = 空页且 `offset` 回填 `total`。**`limit` 单独在场 = 自 0 起的前 `limit` 条,不做隐式尾窗**(与 SDK `history({ limit })` / `normalizeHistoryPageOptions` 同律;V-5 对齐卡 2026-09-04);**尾窗取法** = 先 `?limit=0` 取 `total`,再 `?offset=total−N&limit=N`;
  - 分页下标以**治理后原始历史**(与 resume 重灌同一序列,含内核 reminder 注入消息)计,`total` 计原始条数;页内消息随后仍经 reminder 出流滤除——页内条数可少于原始跨度(是投影结果不是缺页);
  - **配对边界对齐**(sdk `alignHistoryPage` 同算法,返回切片两端皆干净边界、自身配对完整):起点向前取 ≤ `offset` 的最大干净边界(`offset` 如实回填);终点向后取 ≥ `min(offset+limit, total)` 的最小干净边界——**`limit` 是下限**,为凑齐工具轮返回区间可比 `limit` 长;源历史尾部 tool_call 悬而未决时取到末尾(分页只切不合成占位)。**翻页纪律**:以响应 **`nextOffset`** 推进(= 原始下标空间的对齐终点;空页 = 回填的 `offset`;自干净边界续页,起点不再回退,页间无缝无重);终止看 `nextOffset ≥ total`,不看空页。**恒不以 `offset + messages.length` 推进**——页内被投影剔除的 meta-only reminder 消息使 `messages.length` 小于原始跨度,按其推进会把这些位置重见一次(D-V11,主线裁定 2026-09-04 加 `nextOffset`,I-1a;SDK `history()`/`getHistoryPage` 同形携 `nextOffset`);
  - 活跃会话取 `historySnapshot()` 切片(`lastSeq` 为轮末锚,同 ⑧ 原子性);休眠会话且 `limit` 给定时经 store `getHistoryPage`(**一次调用**,段清单尾读:只读覆盖页位所需的尾部段,成本随页位距卷尾距离而非卷总长;对齐与 `offset` 回填在 store 内;§九),`lastSeq` 同为 store meta 持久化水位(RF-S1;为取水位历史在场后另读一次 meta,历史缺席 404 先于之);`limit` 缺席(到末尾本就要整卷)或自实现 store 未提供分页面时服务端全量取回后本地切片(语义同)。
  - **Kotlin/Swift 持份**:`getHistory` 加法可选参数 `offset/limit`,响应加法可选键 `total/offset/nextOffset`(旧端未知键忽略);既有无参调用字节零漂移。

### ⑨ POST /v2/sessions/:id/tool-results/:callId — 远程工具回执上行

```jsonc
{ "status": "ok",
  "content": [ { "t": "text", "text": "..." } | { "t": "image", "mime": "image/png", "data": "<base64>" } ],
  "isError": false }                         // isError 可选,缺省 false
// 或
{ "status": "error", "message": "..." }     // message 1–4096 字符 → isError 结构化结果,恒不炸查询环
// 响应:200 { "accepted": true }
//      | 404 call_not_found | 409 call_already_resolved(幂等重复,无害)
//      | 410 call_expired(已超时/已取消)| 400 validation_failed
```

content 1–64 项,项形 = IRToolContent 白名单(text|image,mime 同 §二-② 白名单)。

### ⑩ POST /v2/sessions/:id/permission/:requestId — 权限回执上行

```jsonc
{ "digest": "<帧内值逐字复述>", "verdict": "allow" | "deny",
  "reason"?: "expired_on_arrival" }   // 加法 2026-09-07(十王修案 FX-C-22):deny 归因,端侧 SDK 代答时携;词表外 → 400
// 响应:200 { "accepted": true } | 404 request_not_found
//      | 409 request_already_resolved | 409 digest_mismatch(复述不符恒不受理)
//      | 410 request_expired(服务端已按 deny-and-continue 收口)
```

- **`reason` 加法增笔(十王修案 FX-C-22 / doc/130 FX-C-S6,2026-09-07)**:端侧收到 `server.permission.request` 时经服务端时间基判定「到达即已过期」(§三-3 `ttlMs` / `ts` 注记),**不再静默自弃**——记 `SessionView.notices` 一条 `{ scope:'client.bridge', code:'bridge_expired_on_arrival', detail:{ skewMs, ttlMs? } }` 并回本端点 `deny` 携 `reason:'expired_on_arrival'`;服务端受理不改裁决语义(deny 即 deny),经结构化日志 `bridge.permission_receipt { sessionId, requestId, verdict, reason? }` 可观测。词表 `PERMISSION_DENY_REASON = { expired_on_arrival }`(contract.ts);`allow` 携 `reason` 被忽略;用户手答缺席该键。

### ⑪ POST /v2/sessions/:id/questions/:requestId — 提问回执上行

```jsonc
{ "answers": [ { "questionId": "...", "selectedOptionIds": ["..."], "freeText": "..." } ] }
// 响应:200 { "accepted": true } | 404 | 409 request_already_resolved | 410 request_expired | 400
```

answers 逐字段白名单归一为 kernel Answer 形;questionId 集合必须与请求帧 questions 一一对应(缺/多/未知 id → 400 `validation_failed`);freeText 可选 ≤ 16384 字符。

### ⑫–⑯ 手动压缩与上下文快照(doc/116 §3.2·§3.3·§4.4 / doc/117 V-1 加法,2026-09-04;代拍 D-1 A / D-2 A / D-3 A / D-4 A / D-11 A)

> 编号注记:doc/116 §4.4 / doc/117 V-4 写「新增 ⑨–⑫」,系以 ⑧ 为末端点的笔误——本档 ⑨⑩⑪ 为回执三端点(P0 冻结),故五端点顺延为 ⑫–⑯。五端点恒要求会话**在册**(休眠会话先 resume;与 ②④ 同律),归属校验 §一-3;**全部为空闲期语义**:轮进行中(含空闲直压在飞)恒 `409 turn_running`,不排队、不隐式等待(开发者空闲后重试——显式优于隐式)。服务端未接快照存储(工厂 `checkpoints` 缺席)→ ⑬–⑯ 恒 `409 checkpoints_not_wired`(可解释,不静默 404);⑫ 仅在显式要求落快照时同码。终结残影(保留窗内 ended)→ ⑫⑭⑮ `409 session_ended`,⑬ 仍可读。

**⑫ POST /v2/sessions/:id/compact — 手动压缩(空闲直压)**

```jsonc
{ "instructions": "keep the API decisions",   // 可选:≤ 4096 字符,并入压缩摘要指令(PreCompact hook 注入在其后)
  "checkpoint": true }                          // 可选:false 关 | true 开 | { "label": "≤120 字" } 开并标签;缺席 = 工厂 autoBeforeCompact 缺省(true)
// 200 CompactResult(三形之一;失败/拒绝也是 200 体——可解释,不是 HTTP 错)
{ "status": "compacted", "compactionId": "<uuid>", "removedRange": [0, 3], "checkpointId": "<ulid>" }   // checkpointId 仅落了 pre_compaction 快照时在场
{ "status": "rejected", "reason": "empty_history" | "not_configured" | "hook_blocked" }
{ "status": "failed", "reason": "<kernel CompactionFailureReason 原值>", "message": "...", "checkpointId": "<ulid>" }  // 失败快照恒保留(D-11 A),id 随结果
// 409 turn_running | 409 checkpoints_not_wired(body.checkpoint 真值而未接线)| 409 session_ended | 400 validation_failed
```

- 压缩前先落 `pre_compaction` 快照(内核 `beforeCompact` 钩子,计划落定后、fork 请求前;无可压 no-op 不落),快照 `messages` = 压缩前完整历史;成功即就地换史并整卷轮换落盘(§九),事件流依序 `session.checkpointed` → `session.compacted{ checkpointId }`(§三-1);`reason` 词表随 kernel 加法演进,客户端按未知值兜底。

**⑬ GET /v2/sessions/:id/checkpoints — 列举快照**

```jsonc
200 { "checkpoints": [ CheckpointMeta, ... ] }   // 按 checkpointId(ULID)升序 = 时间线;不含 messages
CheckpointMeta = { "checkpointId": "<ulid>", "sessionId": "...", "createdAt": "<ISO-8601>",
                   "trigger": "manual" | "pre_compaction" | "pre_clear" | "pre_restore",   // 自由串兜底(kernel 扩值不 bump)
                   "label"?: "...", "cwd": "...", "model"?: { "provider", "model" }, "tokens"?: { "estimated" },
                   "compactionId"?: "...", "messageCount": 12 }
```

**⑭ POST /v2/sessions/:id/checkpoints — 手动快照**

```jsonc
{ "label": "before refactor" }     // 可选,≤ 120 字
// 201 CheckpointMeta(trigger 'manual';空历史亦可,messageCount 0)| 409 turn_running | 409 checkpoints_not_wired | 400 validation_failed
```

**⑮ POST /v2/sessions/:id/checkpoints/:cid/restore — 自快照就地换史(D-2 A)**

```jsonc
{ "checkpoint": false }            // 可选:恢复前是否先落 pre_restore 快照(缺省 true)
// 200 { "status": "restored", "checkpointId": "<cid>", "fromMessages": 24, "toMessages": 12, "preRestoreCheckpointId"?: "<ulid>" }
// 404 checkpoint_not_found | 409 session_mismatch(快照归属他会话,跨会话恢复 = 越界)| 409 turn_running | 409 checkpoints_not_wired
```

- 语义:以快照 `messages` 整体替换当前历史 → 会话级压缩管理器重建 → 事件 `session.restored{ checkpointId, fromMessages, toMessages }` → 整卷轮换落盘;快照恒只读;同一快照连续恢复两次第二次仍换卷(改写事实如实,不短路);快照 `cwd` ≠ 会话 cwd 时**不改 cwd**,发 `server.platform.warning { code: 'cwd_mismatch_on_restore' }`。恢复后 ⑧ history(无参)字节等价快照内容(经出流投影)。

**⑯ DELETE /v2/sessions/:id/checkpoints/:cid — 删除快照**

`204`(无体)| `404 checkpoint_not_found` | `409 checkpoints_not_wired`。attachments 不做引用计数 GC(随会话目录 / 独立根整删)。

**服务端接线(L4 注入契约,非 wire)**:`createAgentSessionFactory({ checkpoints?: { dir?, store?, autoBeforeCompact? /*true*/, autoBeforeClear? /*true;serve 无 /clear 宿主动作,仅同形保留*/, max? /*不限*/ } })`——`store`(自实现 `AgentCheckpointStore` 四方法)> `dir`(独立根 `<dir>/agent-checkpoints/<endUserKey>/<sessionId>/…`,独立生命周期)> 会话存储同居根(`<sessionDir>/checkpoints/<sessionId>/…`,随会话删除;doc/116 §3.6 缺省);三者皆不可得为配置错误 fail-fast。**Kotlin/Swift 持份**:五端点为 HTTP 方法加法,新 KernelEvent 两型按 Unknown 兜底(§三-1),结构化 API 候各端批次对表卡。

### ⑲ POST /v2/sessions/:id/cwd — 会话工作目录中途切换(doc/116 §3.7 D-7 / doc/117 W-2 加法,2026-09-05;二期,暂不发版)

> 编号注记:⑰⑱ 为同批 F 泳道 fork / 导出导入预留(doc/117 §十 F-3),本端点取 ⑲。①-f 「中途切换 cwd 恒不支持(二期 D-7)」自本笔起改为**本端点支持**;①-f 其余语义不变。

```
POST /v2/sessions/:id/cwd
{ "cwd": "<绝对路径;非空>" }
→ 200 { "cwd": "<切换后会话 cwd(策略/解析产物,可与请求形态不同)>", "from": "<切换前 cwd>" }
// 400 validation_failed(cwd 缺席 / 空串 / 非字符串)| 400 cwd_not_allowed(未配 cwdPolicy 或策略拒;携 detail)
// 400 cwd_invalid(非绝对 / 不存在 / 非目录;message 携 reason)| 409 turn_running(轮进行中,含空闲直压在飞;不排队)
// 409 session_ended(终结残影)| 409 cwd_switch_unsupported(会话的内层装配未提供执行器重建面)
// 403 forbidden(归属不符)| 404 session_not_found | 405(非 POST,allow: POST)| 携第三段 404
```

- **策略闸与 ① 建会同律**:kernel `resolveSessionCwd` 四查 + 工厂 `cwdPolicy`(`{ allowedRoots }` realpath 子树 / `{ resolve(requested, { endUserId, fallback }) }` 映射,`fallback` = 会话**当前** cwd);**缺省无策略 = 恒 `400 cwd_not_allowed`**(D-6 A fail-closed)。请求 `cwd` 须为绝对路径(`~` 恒不展开)。
- **空闲期语义**:轮进行中(含空闲直压在飞)恒 `409 turn_running`,不排队、不隐式等待。
- **切换效果**(全部服务端就地完成,同一会话、同一 sessionId、历史不动):工具执行 cwd(`ToolContext.cwd`)与权限门路径锚(规则相对路径 / 工作区快路径 / 真实目标守卫)按新 cwd 重建(kernel `DispatchingToolExecutor.withCwd` 派生:registry / 已读文件表 / grep 聚合共享;权限引擎实例复用——会话级「总是允许」授权按记录时 cwd 锚定,收紧方向);权限桥 digest 锚改为新 cwd(**端侧自 `session.cwd_changed.to` 起以新 cwd 计 `server.permission.request` 回执 digest**);后续快照 `meta.cwd` = 新 cwd;store `meta.cwd` 改写为新 cwd 并追加 `meta.cwdHistory[{ at, from, to }]`(§九;resume 对账锚随之)。**不重跑**项目层配置发现(`loadConfig` 结果不随 cwd 变——权限规则 / hooks 仍为建会 cwd 的项目层;已知限界)。
- **事件**:成功即在事件流签发 KernelEvent `session.cwd_changed { from, to }`(contract-v0.24 预登记;带 id 进重放窗;`?exclude=session` 可滤);拒绝与「目标 = 当前」(200 且 `cwd === from`,零副作用)皆不签发。
- **注入形内层工厂**(`createAgentSessionFactory({ factory })`,cli `createServeSessionFactory` 重装配)须在 `ServeSessionDriver` 上 `bindCwdRebinder((cwd, current) => 新执行器)` 提供重建面,否则本端点恒 `409 cwd_switch_unsupported`(fail-closed:不做「只换 `ToolContext.cwd` 不换权限门」的半切换);平台内置形恒支持。**cli 内层工厂自 FX-C-46(2026-09-08)起缺省接线**:`createServeSessionFactory({ cwdRebinder? })`——缺省 = 权限门按新 cwd 重建(同一引擎;hooks / 事件账本 / 记忆读白名单随行)+ kernel `withCwd` 派生,与平台内置形同律;宿主可传自定义重建体;`cwdRebinder: false` 保留旧 409 形(C-6 续卡闭合,doc/117 §十之三)。
- **Kotlin/Swift 持份**:HTTP 方法加法;新 KernelEvent 一型按 Unknown 兜底(§三-1);端侧若要在切换后继续回执权限请求,须以 `session.cwd_changed.to` 更新本地 cwd 锚(结构化 API 候对表卡)。
- **TUI `/cd` 同律注记(十王修案 FX-B-17,2026-09-07;非契约位,无 CLI 形态标)**:TUI 切换效果与本端点同一清单——执行器 / 权限门 / hooks 引擎(载荷 `cwd` 与 command 执行体 cwd)/ Shell 沙箱写边界 / Task 子代理门与 cwd / 后续快照 `meta.cwd` / `/fork` 新会话 `meta.cwd` / `/restore` 对账,经 cli `assembly/cwd-rebind.ts` 一个收敛点完成(sdk `assembly.ts` `rebindCwd` 同形);`loadConfig` 同样不重跑。
### ⑰⑱ + ①-g fork 与快照导出/导入(doc/117 §十 F-3 加法,2026-09-04;代拍 D-2 B 二期落地)

> 恢复(⑮)是**同会话**就地换史;fork 是从一份快照**长出一条新会话**——源会话零改动,新会话以快照历史起步、独立演进。导出/导入让快照跨会话、跨部署可携(自包含字节,图像附件随行)。三者皆为快照面(工厂 `checkpoints` 缺席 → `409 checkpoints_not_wired`),归属校验 §一-3。

**①-g POST /v2/sessions { fork }** — 自源会话的一份快照 fork 出**新会话**并打开(源会话零改动):

```jsonc
{ "fork": { "sessionId": "<源会话>", "checkpointId": "<cid>" },   // 与 resume 互斥(同给 400 validation_failed);prompt / model / cwd / clientTools … 其余键按新建会话同律受理
  "prompt": "..." }
// 201 { "sessionId": "<新会话 id>", "resumed": false, "lastSeq": 1 }   // 全新会话(不是重灌);lastSeq 1 = 血缘帧已占 seq 1(客户端恒按响应值续订)
// 事件流:session.created(seq 0)→ session.forked{ sourceSessionId, checkpointId, sessionId }(seq 1,带 id 进重放窗,SSE 首连重放可见)→ 装配期提示帧(若有)
// 403 forbidden(源会话在册且归属他人;会话 id 恒非鉴权因子)| 404 fork_source_not_found(源会话或快照不在本租户快照根——他租户目标天然不可见)| 409 checkpoints_not_wired | 400 validation_failed(形状 / 与 resume 同给)
```

- 语义:新会话历史 = 快照 `messages`(配对治理后);历史面 ⑧ history 无参**字节等价**快照(经出流投影);源会话的历史、快照列表、事件流恒不因 fork 而变;新会话自己的快照面为空(快照归源会话)。store 在场时新会话落为一条**独立新卷**(首记录 `kind:'fork'` 携 `{ forkMode:'independent', sourceSessionId, checkpointId }` + `meta.forkedFrom { sessionId, checkpointId }` + 图像附件按 sha 复制;kernel `forkSessionFromCheckpoint`),随后 resume 与常规会话同律;store 缺席 = 仅内存新会话。源会话可为休眠会话(只读快照根,不 resume 源)。分片亲和 id(SC-32b)对 fork 出的新会话同律预生成。

**⑰ GET /v2/sessions/:id/checkpoints/:cid/export** → `200 application/octet-stream`(`content-disposition: attachment; filename="<sid>-<cid>.tansr-checkpoint.json"`):体 = 自包含 UTF-8 JSON `{ "format": "tansr-checkpoint/1", "checkpoint": <落盘形快照:头部字段 + messages,image 块为 { "t":"image", "mime", "$ref":"sha256:<hex>" }>, "attachments": { "<sha256hex>": "<base64>" } }`(图像字节内联进 attachments、同图去重;跨机器 / 跨会话可携,原样落文件即可);`404 checkpoint_not_found` | `409 checkpoints_not_wired` | `403 forbidden`。只读:运行中 / 终结残影皆可。

**⑱ POST /v2/sessions/:id/checkpoints/import[?label=<≤120 字>]** — 体为 ⑰ 的原字节(`application/octet-stream`;≤ **32 MiB**,超限 `413 payload_too_large` + `connection: close`,SC-07 同律停读):校验 `format` / schema / `messageCount` 与 `messages` 一致 / **每附件 sha256** / 每 `$ref` 皆有附件 / 每条消息 IRMessage 形——任一不过 `400 checkpoint_import_invalid`(message 携 detail;篡改即拒,零落盘)→ 身份改写:`sessionId` = 目标会话、`checkpointId` 重铸(新 ULID)、`trigger` 恒 `manual`、`createdAt` 现时、`label` 按 query 覆写或沿用导出体、`cwd / model / tokens` 沿用、`compactionId` 剥除 → `201 CheckpointMeta` + 事件 `session.checkpointed{ trigger:'manual' }`(每次快照落盘皆签发);`409 checkpoints_not_wired` | `409 session_ended`(残影)| `403 forbidden`。导入件归目标会话:可直接 ⑮ restore / ①-g fork(往返等价,测试锁);路径段 `import` 为保留字(与 `:cid` 同位;checkpointId 恒 ULID 不撞名)。

- 契约同笔:`contract.ts` `CreateSessionBodySchema.fork?`、`CHECKPOINT_IMPORT_MAX_BODY_BYTES`(32 MiB,端点专项,不入 `V2_LIMITS`)/ `CHECKPOINT_EXPORT_CONTENT_TYPE` / `CHECKPOINT_IMPORT_SEGMENT`;`V2_ERROR_CODE` +2 `fork_source_not_found` 404 / `checkpoint_import_invalid` 400;`AgentSessionHandle` 增可选 `exportCheckpoint? / importCheckpoint?`,`AgentSessionCreateInit.fork?`;store 双份同笔 `fork?(endUserId, input)` 可选方法 + `AgentStoredSessionMeta.forkedFrom?`(sdk `SessionStore.fork?` / `SessionRecordMeta.forkedFrom?`,§九);§三-1 KernelEvent 加一型 `session.forked`(contract-v0.24 预登记,与 W 泳道 `session.cwd_changed` 同 bump);sdk 份同笔 `session.fork()` / `exportCheckpoint()` / `importCheckpoint()` / `createSession({ fork })`(doc/90 §4.10)。**Kotlin/Swift 持份**:HTTP 方法加法(`CreateSessionRequest.fork` 可选键、⑰ 二进制下载、⑱ 二进制上传)+ 未知事件 type 忽略(`session.forked` 按 Unknown 兜底),零改动。

### ⑳ POST /v2/sessions/:id/audio/transcriptions · ㉑ POST /v2/sessions/:id/audio/speech — 音频直连(doc/123 §3.6 / D-A9 ②;加法,2026-09-05;暂不发版)

> 语音是**用户输入 / 助手输出的载体**(D-A9):只有工具链会把「说一句话」变成一次智能体工具调用。本两端点是托管拓扑下 SDK 令牌档直连方法 `session.platform.transcribe / speak` 的服务端类比——**服务端持 app token 代打平台 `/t1/asr` / `/t1/tts`**,受同一能力位/配额闸;**不触会话历史、不签发事件、协议包零触碰**(不新 KernelEvent、不新 IR 块,故不占 contract 号)。端侧自决是否把转写文本作为下一条 ② 输入、是否播放合成音频。

```jsonc
POST /v2/sessions/:id/audio/transcriptions       // body 同 doc/123 §3.1(白名单键代打;未知键忽略、恒不透传平台);≤ 32 MiB 专项体帽
{ "model": "whisper-1",                            // 可选:缺席 = 授权集顺位第一(bundle platformModels.speechToText 首行)
  "audio": "data:audio/mp4;base64,…" | "https://…",  // 必填:单文件;data:audio/<subtype>;base64 或 http(s) URL(mime 白名单由平台硬校验)
  "language": "zh", "diarize": false, "prompt": "…" }  // 可选
→ 200 TranscriptData { "model", "text", "language"?, "durationSec"?, "segments"?: [{ "start", "end", "text", "speaker"? }] }
   // 与 sdk TranscriptData 同型同源归一(usage / cost / currency 恒不下发端侧——终端恒无金额,doc/89 契约 3.2 同律)

POST /v2/sessions/:id/audio/speech                // body 同 doc/123 §3.2
{ "model": "qwen3-tts-flash", "input": "…", "voice": "Cherry", "format": "wav" | "mp3", "speed": 1.0 }   // input 必填非空白(≤ 20000 字符体面帽;平台按模型 maxChars 再钳,缺省 2000)
→ 200 SpeechData { "model", "audio": { "url"?, "b64"?, "mime", "format", "durationMs"?, "sampleRate"? }, "billedChars" }
   // URL 托管形(约 24h)原样透出、内联形以 b64 透出;端侧按 url ?? b64 取用;outputHosting / cost 不下发
```

- **闸序**(先拒再读,不为必拒的请求消费最多 32 MiB 体):会话在册(404 `session_not_found`)→ 归属(403 `forbidden`)→ 面在场(句柄无 `platformAudio` → 409 `platform_unavailable`:注入形内层工厂 / 自实现句柄;平台内置形恒有)→ 残影(409 `session_ended`)→ **能力位闸**:bundle 有效能力档 `platform.speechToText`(⑳)/ `platform.textToSpeech`(㉑)未开 → **403 `capability_disabled`**(message 携位名与控制台修复指引;两位独立施闸,D-A1)→ 体(413 `payload_too_large` 携 `connection: close`;400 `invalid_json` / `validation_failed`)→ 代打。
- **代打**:经工厂刷新 fetch(逐请求覆写 `x-tansr-app-token` 为该 endUser 现值令牌,与环3 工具装配同一枚;令牌恒不落工厂、恒不下发端)打平台**同步端点**(不走任务径——交互式 UX 以时延为要);客户端离场即中止代打(不为无人接收的响应烧上游计费面)。
- **上游错误码透传**:平台**业务 4xx** → **状态码与 `error.code` 原样**(`asr_not_configured` / `tts_not_configured` 409、`asr_quota_exceeded` / `tts_quota_exceeded` 429、`bad_request` 400(音频格式/时长、音色不在表内、字数超帽——message 携平台可解释人话,如可选音色表)、`insufficient_balance` 402 …;非 JSON 体 `http_<status>`),message 经 i18n 包装携上游人话;网络错 → 503 `upstream_unavailable` 携 `Retry-After`;平台 2xx 却无可归一产物 → 503 `upstream_unavailable`。
  - **改笔 2026-09-11(RF-05a,核心组件设计审查 RV-4-05 / V-C-09)**:平台 **401 / 403(含 `model_not_authorized`、套餐 403)/ 5xx(含 `upstream_error` 502、非 JSON 体)** 与网络错**不再原样透传**,一律归一 **503 `upstream_unavailable` 固定 i18n 词条 + `Retry-After`**;上游状态 / 码 / 人话只进结构化日志 `upstream.rejected{face,sessionId,upstreamStatus?,upstreamCode,message,status}`。理由:serve 客户端持的是 serve 令牌而非平台 key,平台 401 `unauthorized` 原样到端会误触 sdk / Kotlin / Swift `TOKEN_INVALIDATING_CODES`(本地令牌失效终态),403 亦属端侧越权终态;网络错原文携平台内网地址 / 超时细节。业务 4xx 透传形不变(端侧「未知 code 兜底 + 状态码」零改动)。
- **不做**:不进 ② 消息、不进 ⑧ history、不签发任何 SSE 帧;不缓存音频;不落盘不转存(D-A5)。`capabilitiesProfile`(①-c)不辖本两端点(直连不经工具白名单,受能力位闸)。
- **Kotlin/Swift 持份**:HTTP 方法加法 + 两码(`capability_disabled` 403 / `platform_unavailable` 409)+ 透传码按「未知 code 兜底 + 状态码」;core `AudioClient.transcribe(bytes, mime, model?)` / `speak(text, voice?, model?)` 归 C-1 / D-1 卡(doc/123 §3.7)。
- 契约同笔:`contract.ts` `AudioTranscriptionsBodySchema` / `AudioSpeechBodySchema` / `AUDIO_MAX_BODY_BYTES`(32 MiB,端点专项,不入 `V2_LIMITS`)/ `AUDIO_INPUT_PATTERN` / `AUDIO_SPEECH_FORMATS` / `AUDIO_ROUTE_SEGMENTS`;`V2_ERROR_CODE` +2 `capabilityDisabled` / `platformUnavailable`;`types.ts` `AgentSessionHandle.platformAudio?: AgentPlatformAudioFace`(`capabilities` 两位 + `request(face, body, signal)`)。

## 三、SSE 帧形(D2 终拍:KernelEvent 直下发)

1. **KernelEvent 帧**:`id: <seq>` + `data: <KernelEvent 完整 JSON>`,**恒无 `event:` 名**(SSE 缺省 message 事件;与 /v1 逐帧带 event 名刻意不同——/v2 客户端单一 message 监听 + type 判别,控制帧才走命名事件);包络(sessionId/turnId?/seq/ts/v/source)恒全保。
   - **session.checkpointed / session.restored 两型注记(增笔 2026-09-04,contract-v0.22 / doc/116 D-3 A / K-4 + V-1;加法)**:KernelEvent 族新增 `session.checkpointed { checkpointId, trigger, label?, messageCount }`(每次快照落盘签发:⑭ manual / ⑫ pre_compaction / ⑮ pre_restore,亦含 auto/reactive 压缩前的自动快照)与 `session.restored { checkpointId, fromMessages, toMessages }`(⑮ 换史签发);`session.compacted` 增可选 `checkpointId`(与 pre_compaction 快照对账)。直透规则不变,`?exclude=session` 可滤。**端侧解码核实(V-4,2026-09-04)**:tansr-android `KernelEventDecoder.kt:52` 词表外 type → `UnknownKernelEvent`(`malformed=false`,恒不抛),`SessionCompacted` 经 `ignoreUnknownKeys=true`(`:16`)忽略新键;tansr-ios `KernelEventDecoder.swift:46-47` 同律落 `UnknownKernelEvent`,`session.compacted` 解码只读具名字段(`:220-229`)——两端**零代码义务**,可选增两型 case 呈现「已建快照 / 已恢复」(对表卡随迁)。
   - **session.cwd_changed 一型注记(增笔 2026-09-05,contract-v0.24 预登记 / doc/116 §3.7 D-7 / W-2;加法)**:KernelEvent 族新增 `session.cwd_changed { from, to }`(⑲ 切换成功签发;`source: 'server'`;带 id 进重放窗)。端侧按未知 type 忽略恒安全;有则可呈现「工作目录已切换」并据 `to` 更新权限回执 digest 的 cwd 锚(§二-⑲)。
   - **session.forked 一型注记(增笔 2026-09-04,contract-v0.24 预登记 / doc/117 §十 F-2·F-3;加法)**:KernelEvent 族新增 `session.forked { sourceSessionId, checkpointId, sessionId }`——①-g 以快照 fork 并打开新会话时,在**新会话**事件流里紧随 `session.created` 签发(seq 1,带 id 进重放窗);`sessionId` = 新会话 id(与包络同值),`sourceSessionId / checkpointId` = 血缘;源会话事件流恒不变;载荷不含消息内容。直透规则不变,`?exclude=session` 可滤。端侧按未知 type 忽略恒安全(V-4 核实结论沿用:Kotlin/Swift 解码器 Unknown 兜底,零代码义务;可选增一型 case 呈现「自 … fork 而来」)。
   - **落卷失败帧注记(增笔 2026-09-07,十王修案 FX-C-07 / doc/130 R-03 采 B 形;既有型,词表加法)**:serve 轮末 / 改写 / 建行落 store 失败时,该会话事件流签发一条 **`turn.error { scope: 'server.persistence', message, recoverable: true, errorKind: 'store_commit_failed' | 'store_create_failed' }`**(`source: 'server'`;经驱动 `pushBody` 与内核帧同 seq 计数器,带 id 进重放窗)——与 sdk 进程内形态的 `turn.error { scope: 'sdk.persistence', recoverable: true }`(doc/90 §4.4)**逐字段位置同形**,只多 `errorKind` 两码作机器判别;`message` 为 i18n 固定词条(不携服务器路径 / 原始错误文本,原文只到宿主 `onStoreError` / 结构化日志 `store.commit_failed` / `store.create_failed`);计数 `tansr_serve_store_{commit,create}_errors_total` 照旧 +1。**刻意不新增** `server.platform.warning` 控制帧、不新增事件型:三端 reducer 已消费 `turn.error`,`recoverable: true` 帧按 R-05 归 `SessionView.notices[]` 不落 `lastError`(端侧 `TOLERATED_UNKNOWN_EVENT_TYPES` 不 +1)。补齐律:失败一轮无害——下一轮 commit 由 store 持久闩 `meta.pendingRewrite`(kernel 店核,FX-C-06)强制整卷轮换,增量径前缀校验兜底;客户端无需动作。锁测 `packages/server/test/concurrency-event-log-seam.test.ts`(V-C-14 / V-M06)。
   - **session.events_dropped 一型注记(增笔 2026-09-12,contract-v0.27 / RFC-RF-1 / DEC-RF-20;加法,serve 不签发)**:KernelEvent 族新增 `session.events_dropped { droppedCount, reason: 'count_limit' | 'bytes_limit', scope, droppedBytes?, firstDroppedSeq?, lastDroppedSeq?, maxEvents?, maxBytes? }`——**签发方唯一为 sdk 进程内 `AgentSession` 事件队列**(`scope: 'sdk.events'`,「从未有订阅者」的积压溢出通报,取代此前借 `turn.error { scope: 'sdk.events', errorKind: 'events_dropped' }` 的过渡形);**serve 恒不签发**(serve SSE 面的同类事实走 §三-5 `server.replay.gap` 控制帧,不同层),故本型**不进 /v2 事件流、不进重放窗、不进 journal**;sdk `contract/MANIFEST.json` 型表 54 → 55(revision 6 → 7)。端侧按未知 type 忽略恒安全(Kotlin/Swift 解码器 Unknown 兜底),**零代码义务**;MANIFEST 新鲜度 warning 一档随对表卡 RF-M1 消化。
   - **mcp.* 三型注记(增笔 2026-09-04,contract-v0.21 / RFC-SC-3 / SC-45;加法)**:KernelEvent 族新增 `mcp.reconnecting {server,connection,attempt,delayMs}` / `mcp.reconnected {server,connection,attempt,downtimeMs}` / `mcp.reconnect_exhausted {server,connection,attempts}`(MCP 连接池自动重连生命周期;`connection` = 池成员 0 基序号;恒不携失败原因/堆栈/命令行)。直透规则不变:服务端对 KernelEvent 按 type 零过滤零改写,`?exclude=mcp` 可按前缀滤除。**端侧按未知 type 忽略恒安全**(Kotlin/Swift 解码器 Unknown 兜底;有则可呈现「工具服务器恢复中」,无则零改动)。现状注记:serve 形态 MCP 附着为服务器作用域,生命周期事件走服务器日志、**尚不进会话流**(`mcp.connected` 同律);若日后按会话广播,帧形即本条。
2. **控制帧(四族七名;2026-09-02 增笔前三族六名)**:`event: server.<族>.<名>` + `id: <seq>` + `data: <载荷 JSON>`(载荷形 §三-3);**带 id 进环形缓冲**——业务往返断线必须重见(与 gap 帧刻意相反);seq 与 KernelEvent 共用同一会话计数器(打戳序 == 入队序 == 下发序)。
   - **seq 会话内单调 + 起点规则(改笔 2026-09-11,RF-05b / 核心组件设计审查 RV-4-02;修复计划 §3 代拍 #2 = B;wire 形零改动)**:此前「每纪元从 0 起」(resume 重灌的新驱动 seq 归零,旧游标 `Last-Event-ID` 只能靠 §三-5 纪元 gap 令客户端重建)改为 **seq 在同一 `sessionId` 的全部纪元内单调不回绕**:全新会话(含 fork 的新会话、①-g 导入)从 0 起;resume 起的新纪元起点 = **持久化水位 + 1**——服务端在每次轮末 / 改写检查点把当前 `lastSeq` 落 store 记录 meta(`lastSeq`,只增不减),正常关闭(`session.ended` 打戳后)再落最终值并**封印**(`seqSealed: true`)。播种规则(`seedInitialSeq`):**R1** 记录封印 → 起点恰 `lastSeq + 1`(跨纪元连续,旧游标 `Last-Event-ID = lastSeq` 续传恰得新纪元首帧 `session.created`,零 gap 零重建);**R2** 未封印(崩溃 / kill / 落盘失败 / 旧记录无水位)→ 起点 = `max(lastSeq + 1, now − createdAt 毫秒数)`——守卫地板保证新起点 > 旧纪元任何已下发 seq(单纪元平均事件率 < 1 条/ms 恒成立;`createdAt` 不可解析 → 地板 0)。**崩溃窄窗边界**:R2 依赖时间地板,理论失效面 = 单纪元平均 > 1 事件/ms 且客户端在新纪元已下发 ≥ 缺口条数事件之后才首次以旧游标续传;收紧手段(seq 租约 / 预写水位)为候拍,不在本笔。**gap 判据加法**:旧游标落在新纪元起点之前(`0 ≤ Last-Event-ID < 起点 − 1`,R2 跃升所致)→ 注册仓判 gap `reason: 'evicted'`(本日志没有那段事件),客户端 L2 重建;游标 = 起点 − 1 恰续;`Last-Event-ID` 缺席 / −1 恒全量重放本纪元。空日志水位 = 起点 − 1(早到的旧游标续传不误判 `ahead_of_log`)。端侧零义务:Kotlin `Long` / Swift `Int64` 游标按 `seq > cursor` 去重,L3 恒弃游标重建;仅注释「seq 新纪元(从 0 起)」归镜像卡。协议帧集 / `Last-Event-ID` 语法 / api-client 零改动。锁测 `packages/server/test/v2-seq-epoch.test.ts`(AC-05e)。**非活跃会话 `lastSeq`(改笔 2026-09-13,RF-S1 / DEC-RF-30;wire 形零改动)**:GET ⑥ / ⑦ / ⑧ 非活跃径的 `lastSeq` = 最近封印 / 落盘水位(即 store meta `lastSeq`;旧卷缺字段 → 0;活跃径不变),客户端 `Last-Event-ID` 续传起点据此——封印记录以该值续传恰得新纪元首帧,未封印记录仍可能落在 R2 起点之前而收 gap `evicted`(L2 重建);`lastSeq === 0` 自此不再等价于「无事件」,端侧恒按值续传(RF-M1 对表)。锁测 `packages/server/test/v2-stored-last-seq.test.ts`(AC-S1 S1a)。
3. **控制帧载荷表(⚠定稿)**:

| 帧 | data 形 |
|---|---|
| `server.tool.request` | `{ "callId", "name", "args", "deadlineAt", "ttlMs" }`(callId = 内核 toolCallId;deadlineAt = epoch_ms;args 为工具全量入参——客户端要执行,已过 zod 形状校验;**`ttlMs` 加法 2026-09-07(FX-C-22)**= 申报 `timeoutMs`,自签发起的存活期,端侧判期以 `ttlMs` 为准,`deadlineAt` 保留一版后删) |
| `server.tool.cancel` | `{ "callId" }` |
| `server.permission.request` | `{ "requestId", "name", "summary"?, "attribution": { "decision", "source", "matchedRule"?, "reason"? }, "digest", "expiresAt", "ttlMs" }`(**`ttlMs` 加法 2026-09-07(FX-C-22)**= `permissionTimeoutMs`,端侧判期以 `ttlMs` 为准,`expiresAt` 保留一版后删。**脱敏纪律:完整 args 恒不下发**——summary = summarizeCallTarget 目标摘要,载荷整体 redactDeep;T-K11 同律。**`attribution.reason` 加法增笔(doc/113 G-6,2026-09-04)**:`source = "classifier"` 时携带裁决人判为危险的一句话理由——端上呈现「裁决人判为危险:…」供终端用户终审(用户拒即 deny;机器恒不代批);其余来源缺席;旧端忽略未知字段。**行为增笔(同笔)**:平台内置形 bundle 生效裁决人档在场且符合咨询条件时,剩余 ask 可经裁决人;无人环×none、明确hook ask、预算/熔断等零咨询条件沿既定降级——判危险按姿态落地(缺省人在环:本帧携 reason 下发;宿主 `platform.adjudication.posture = 'headless'` 时改为 deny-and-continue 不下发本帧),判安全且资格半径覆盖(bundle `adjudicator.tier` ≥ medium,kernel F9 面)→ **不下发本帧直接执行**;bundle 无裁决人段(旧 api)行为现状。裁决人预算耗尽/熔断等开发者向告警按 FX-C-50 只经宿主 `onPlatformWarning` / 结构化日志 `platform.warning` 告知,不下发终端;`adjudicator_model_unavailable` 仍可经 `server.platform.warning` 告知。此处仅纠正旧投递说明,不新增帧或字段(SERVE-PERM-01,2026-09-14)。) |
| `server.permission.closed` | `{ "requestId" }` |
| `server.question.request` | `{ "requestId", "questions": [ { "id", "prompt", "options": [{ "id", "label" }], "allowMultiple"? } ], "expiresAt"?, "ttlMs"? }`(kernel Question 白名单拷贝;expiresAt 仅无订阅者宽限期激活时在场;**`ttlMs` 加法 2026-09-07(FX-C-22)**= `questionGraceMs`,与 `expiresAt` 同在场性) |
| `server.question.closed` | `{ "requestId" }` |
| `server.platform.warning` | `{ "code", "message" }`(S-ARCH P2 增笔 2026-09-02:平台 t.warn 提示帧投影——thinking_unavailable_on_face/balance_low 等**非致命降级结构化告知**,恒不代表请求失败,回答照常;端侧可渲染「为什么这次没有思考」,呈现策略归端上;无回执面,closed 语义不适用。**code 增笔 2026-09-04(doc/116 §3.3·§3.7 / V-1·V-3)**:`cwd_mismatch_on_resume`(resume 时 store `meta.cwd` ≠ 本次 cwd;seq 紧随 `session.created`)、`cwd_mismatch_on_restore`(⑮ 快照 `cwd` ≠ 会话 cwd;不改 cwd);code 为自由串,端侧按未知 code 兜底零改动) |

**closed 帧语义(⚠定稿)**:请求达到任何终局(有效回执/超时/中断/通道不可用)后恒下发对应 closed 帧——多设备场景他端收框;自答方收到幂等无害(platform.warning 系单向告知帧,无终局无 closed)。

**控制帧 `ts` 与时间基注记(增笔 2026-09-07,十王修案 FX-C-22 / doc/130 FX-C-S6 / 候拍 ⑨「A′ + B 合一」;加法)**:编码器对**每一枚控制帧**的 `data` 在上表载荷之外恒多写一键 **`ts: number`**(服务端墙钟 epoch_ms,= 帧入队时刻;内核帧包络本就携 `ts`)——此前控制帧 wire 不携 `ts`,端侧只收控制帧的时段无样本可估墙钟偏差(复审 E2-12)。端侧纪律:对 `expiresAt` / `deadlineAt` 类**绝对时刻恒不以本地墙钟直接比较**——三枚请求帧携相对时长 `ttlMs` 时恒优先 `receivedAtLocal + ttlMs` 判期;缺席才回落 `expiresAt − skew`,`skew` 由同流帧 `ts` 经 `ClockSkewEstimator`(滑动中位数,窗口 8,clamp ±5 min;首样本前 0)估得。「到达即过期」不静默(§二-⑩ `reason`)。`ts` 不属任一控制帧载荷键集,载荷键恒不被覆写;旧端忽略未知键。锁测 `packages/server/test/bridge-time-base.test.ts`(V-34 serve 半边);帧序金样 `packages/cli/test/fixtures/agent-v2-frames/frames.json` 同笔再生。

4. **订阅过滤 `?exclude=`(⚠词表定稿)**:逗号分隔 token,每个匹配 `^[a-z][a-z0-9_]*$`(≤16 个;非法 → 400);匹配规则 = KernelEvent `type === token` 或 `type` 以 `token + '.'` 开头;**控制帧与 gap 帧恒不受过滤**;被滤事件仍占 seq(id 空洞合法,Last-Event-ID 语义不受扰)。纯带宽优化,恒不改事件本体。
5. **gap 帧**:`event: server.replay.gap`,**恒不带 id**(不扰动游标);data = ReplayGapNotice(`{ type, sessionId, requestedAfterSeq, oldestRetainedSeq?, droppedEvents, reason? }`,/v1 既有形 + `reason` 加法键)。**契约门覆盖(增笔 2026-09-11,RF-05a / RV-4-06)**:事件名、载荷键集与 `reason` 词表单源于 `contract.ts`(`REPLAY_GAP_EVENT` / `REPLAY_GAP_REASON` / `ReplayGapNoticeSchema`,strict),进 `pnpm contract:check` MANIFEST `sha256` 事实源——改形不 `--bump` 即门红;端侧对表卷据此对 `reason` 字面。
   - **有界写(增笔 2026-09-11,RF-05a / RV-4-01;wire 零改动)**:§六 `maxSubscriberBufferBytes` 是**每次 socket 写的字节上界**,不只是 paused 期队列上界——attach 重放段惰性分片交付、同 tick 批量入队越界即刻分片写出、收流前亦分片;帧文本与顺序逐字节不变(SSE 按 `\n\n` 分帧,拼接边界不改语义)。socket 拒收后的残量才按下款慢消费者判据裁决。
   - **纪元 gap(增笔 2026-09-07,十王修案 FX-C-05 / S-02;加法)**:游标**超前于当前水位**(`Last-Event-ID > lastSeq`——他人 resume 起了新纪元、服务重启后 seq 从 0 起)**亦发 gap**(纪元不同):`requestedAfterSeq` 原值、`oldestRetainedSeq` 取日志最旧(空日志缺席)、`droppedEvents` 仍为累计逐出数(多为 0);随后直播照常续上,seq 恒不倒退地重新从水位起。判据在注册仓 `subscribe` 单点(`read.gap || afterSeq > log.lastSeq()`,覆盖外置日志实现),`MemorySessionEventLog.readAfter` 双保险。**改笔 2026-09-11(RF-05b,§三-2 起点规则)**:新纪元自持久化水位续起后,「他人 resume 起了新纪元」不再落本因(封印记录旧游标恰续;未封印记录旧游标落在起点之前 → `reason: 'evicted'`),`ahead_of_log` 退为伪造 / 越界游标或无 store 的重启;水位比较基 = `max(log.lastSeq(), 起点 − 1)`。`reason?: 'evicted' | 'ahead_of_log'` 区分两因(/v2 注册仓恒携;/v1 与慢消费者 drop-oldest 形缺席;端侧 Unknown 键容忍、处置同一 = §五 L2 弃投影重建)。旧码此形零帧等待 + 直播 seq 倒退(复审 R-6 三 ✗ 格),本笔闭合。
   - **字段语义澄清(增笔 2026-09-03,用户授权代拍 doc/118 §八 G4;wire 零改动,文档化)**:`droppedEvents` = 该会话缓冲**自建立以来累计**被逐出重放窗口的事件总数(单调累计,表征缓冲窗口整体够不够用),**不是**本次重连丢失的事件数;客户端判「这次丢了多少」恒用 **`oldestRetainedSeq − requestedAfterSeq − 1`**(`oldestRetainedSeq` 缺席 = 缓冲已空,此前全部事件均不可重放)。客户端处置恒按 §五-5.3 L2(弃投影 → history 重建),不依赖 `droppedEvents` 取值。
   - **慢消费者 gap(同笔增笔)**:服务端对写不动的连接(用户态待写队列超 `maxSubscriberBufferBytes` 或持续 paused 超 `slowSubscriberGraceMs`,§六)按部署策略处置——缺省 **`disconnect`**:服务端直接断连,客户端凭 `Last-Event-ID` 重连走 L1 重放(窗口外 L1 自然得到上款 gap 帧);可配 **`drop-oldest`**:保连接、丢该连接队列中最旧的帧,并在下次冲刷首部主动下发一帧 gap(**`requestedAfterSeq = -1`** 表「非重放请求」,`droppedEvents` = 本连接被丢弃的帧数,`oldestRetainedSeq` 缺席)。两形对客户端同一语义:投影已不完整,按既有 L1/L2 重建;客户端恒不得假定 gap 帧只在重连首帧出现。
6. 环形缓冲承 `eventBufferSize` 缺省 1024;心跳/重试参数承 /v1。
   - **定界注记(增笔 2026-09-03,G4-e 代拍)**:条数缺省 1024 **维持**;服务端另可配逐会话**帧字节上限** `eventBufferMaxBytes`(缺省**不设** = 仅条数定界,现状零漂移;部署建议 4 MiB/会话),设定时超字节亦逐出最旧事件、重放窗口前移,gap 帧语义(§三-5)不变;客户端零感知。

## 四、远程工具桥(服务半场契约)

### 4.1 clientTools 申报形制(⚠定稿)

```jsonc
{ "name": "searchOrders",                  // ^[A-Za-z][A-Za-z0-9_]{0,63}$;与装配集既有工具撞名 → 400
  "description": "...",                    // 1–2048 字符
  "parameters": {                          // 可选;参数表档(defineTool parameters 同形,非裸 JSON Schema)
    "keyword": { "type": "string", "description": "...", "optional": false }
    //           type ∈ string|number|boolean|array|object;array 带 items?;object 带 properties?
  },
  "readOnly": true,                        // 可选,缺省 false(保守按写工具)
  "effects": ["financial"],                // 可选(2026-09-05 加法,doc/optimize/37 §5):∈ irreversible|financial|external|affects-others,≤4 不重复
  "timeoutMs": 120000 }                    // 可选;整数 1000–600000,缺省 120000
```

- 申报数 ≤ 32/会话;`parameters` 序列化 ≤ 32 KiB,嵌套深度 ≤ 8(注入面定界,doc/96 §5.4);
- **`effects` 效应声明(加法,2026-09-05;doc/optimize/37 §5)**:开发者对本工具副作用性质的自述——`irreversible`(不可逆)/ `financial`(资金流动)/ `external`(触达外部系统或第三方)/ `affects-others`(影响其他用户或租户)。**信号不是闸门**:只进裁决人(应用协议 `<tool>` 段)语境与遥测标签,恒不参与权限判定与并发裁决(`readOnly` 位不受影响);词表外 / 重复 / 超四值 → 400 `validation_failed`;旧端侧不带即缺席,服务端零漂移。Kotlin/Swift defineTool 增可选参数(对表卡随下一批);
- **参数表档裁决**:wire 取 SDK defineTool 的 `ToolParameterSpec` 参数表形(非裸 JSON Schema)——转换先例同构(define-tool.ts specToZod L64-107 服务端同构实现),闭集可校验(申报注入面有界),服务端由此双生成 zod(执行校验)与 JSON Schema(IRToolDef 模型可见面);Kotlin defineTool(A2-1)申报同形出线;
- 申报载荷恒不拼进任何服务端执行语境(只进模型 tool schema),恒不据申报名做路由以外解释。

### 4.2 六项协议语义(双半场义务表,doc/96 §2.4 升格冻结)

| # | 语义 | 服务端义务 | 客户端义务 |
|---|---|---|---|
| 1 | 超时 | RemoteToolProxy.timeoutMs = 申报值(缺省 120s,帽 600s ⚠定稿);恒有限;超时走调度器既有 timeout 语义(`tool.failed` errorType='timeout');`deadlineAt` 随帧下发 | 按 deadlineAt 自弃过期任务;执行中越线即协程取消 |
| 2 | 重试/重发 | 恒不主动重发请求帧——SSE 重连 Last-Event-ID 重放天然重见未答帧(带 id 进缓冲的设计动机) | 对重放帧按 callId 幂等去重(in-flight/已答者忽略) |
| 3 | 幂等 | 回执第一份有效者终局(T-K11 同律);重复回执 409 无害 | 回执网络失败可安全重试 POST(同 callId 恒安全) |
| 4 | 取消 | 轮中断(interrupt/超时/budget)→ ctx.signal abort → 下发 `server.tool.cancel` → proxy 即刻按 aborted 收口(fail-fast,不等客户端确认);取消后到达的回执 410 | 收 cancel 帧 → 取消对应挂起函数 |
| 5 | 并发 | 多 in-flight callId 天然支持;并发次序恒由调度器裁决(isReadOnly 申报位透传),桥不自作主张 | pending map 按 callId 配对;handler 并发安全责任归开发者 |
| 6 | 二进制(图片) | 回执 content 直用 IRToolContent image 形;tool-results 端点体帽专项 20 MiB(§六) | 图片经 base64;超帽前置自检可读错 |

已答/已取消 callId 的幂等判别窗:服务端恒保留最近 256 个终局回执墓碑(⚠定稿;超窗后的重复回执按 404)。

## 五、askUser 双桥与三级恢复链

### 5.1 权限桥

- serve 装配注入 askUser 回调 → 下发 `server.permission.request`(素材脱敏 §三-3)→ 回执 ⑩;
- `expiresAt` = 签发时刻 + **120s**(⚠定稿,服务端可配 `permissionTimeoutMs`);帧同携 `ttlMs` = `permissionTimeoutMs`(FX-C-22 加法,端侧判期以此为准);**恒等 `permissionTimeoutMs` 到点**(改口 2026-09-07,doc/131 §七 #8 / 审计 06 D-6:实现只此一分支——无订阅者**不**提前收口,SSE 重连重放重见本帧仍可答)→ **deny-and-continue**(`tool.permission.decided` source 可观测,fail-closed);端侧判「到达即过期」时不静默自弃,回 ⑩ `deny` 携 `reason:'expired_on_arrival'`(服务端结构化日志 `bridge.permission_receipt` 可观测);
- digest 复述不符恒 409 不受理;requestId 第一份有效终局;**裁决语义恒在服务端**(PermissionEngine/规则/hooks 恒服务端跑,客户端 UI 只是取答面);中断(轮 abort)→ closed 帧 + 按 deny 落地。

### 5.2 提问桥

- RemotePromptChannel(kernel PromptChannel 实现)→ `server.question.request` → 回执 ⑪ 白名单归一 Answer[];
- signal abort → `server.question.closed` + reject AbortError 形(kernel 契约);
- **无订阅者宽限(⚠定稿)**:ask 起或运行中会话失去全部 SSE 订阅者 → 起 **60s** 宽限计时(`questionGraceMs` 可配);计时到点仍无订阅者且未答 → reject ChannelUnavailableError 形 → AskUser 工具结构化降级「自行决策并继续」恒不挂起;订阅者在场则等待恒以用户注意力为界(无服务端超时)。

### 5.3 三级恢复链(doc/96 §2.6 升格冻结)

| 级 | 触发 | 客户端步骤 | 服务端半场 |
|---|---|---|---|
| L1 | SSE 断连 | 自动重连携 `Last-Event-ID=<最后消费 seq>`;收帧按 seq 去重 | 环形缓冲重放断点之后 |
| L2 | 重连首帧收 `server.replay.gap` | ①弃投影 ②GET ⑥(status/live)③GET ⑧ history ④viewStateFromHistory 重建 ⑤自 **history.lastSeq** 续订 | history 快照与 lastSeq 同刻一致(§二-⑧) |
| L3 | ⑥ 404 或 `live:false`,或冷启动只持 sessionId | `POST /v2/sessions { resume: { sessionId } }` → `resumed:true, lastSeq:<新纪元水位>` → 弃旧游标按 L2 ②–⑤ 重建 | store 取回 → pairing 治理(repairHistoryPairing 同构:断尾 tool_call 补 isError 占位/孤儿 tool_result 截断到干净边界)→ initialMessages 重灌;seq 新纪元自持久化水位续起(§三-2 起点规则,RF-05b;此前从 0 起) |

运行中轮崩溃恒丢本轮(store 轮粒度提交:驱动轮末 `onTurnEnd → store.commit`,压缩改写轮携 rewritten=true 整卷轮换)。

### 5.4 轮末出站通知(onTurnEndNotify webhook 缝;A4-2 增笔,**D5 按建议值终拍 2026-08-31**)

D5 终拍:**SDK/会话服务恒不内置推送 SDK**——推送通道(FCM/厂商矩阵)选型属开发者域;服务端只留稳定最小承诺 = webhook 出站缝(doc/94 §5.4/§十 D5)。自接全链指引见本档附录 A。

- **触发判定**:会话轮终态(KernelEvent `turn.completed` / `turn.aborted` 抵达传输层)且该刻**无活跃 SSE 订阅者** → 向开发者配置的 webhook URL 出站 POST;有订阅者恒不发(用户正看着流,无唤起必要);判定与该终局帧对订阅者的可见性同刻一致;serve 优雅关闭期的批量中止恒不出站(非业务轮末,App 重连自续)。
- **防抖**:同会话同轮恒一裁(发/不发),`turn.started` 帧界复位——同轮重复终局帧恒不二发;重试重发恒同一载荷体(接收方可按体或 sessionId+turnId 去重)。
- **载荷(冻结最小面;脱敏纪律:恒不携消息内容、恒不携凭据)**:

```jsonc
POST <url>  content-type: application/json
{
  "sessionId": "...",
  "endUserId": "u_123",
  "turnId": "...",                   // 可选:内核包络 turnId(终局帧携带时在场)
  "status": "completed" | "aborted", // 轮终局词投影
  "reason": "client_gone",           // 可选(增笔 2026-09-04,contract-v0.21 / RFC-SC-1 R4):终局帧 TerminalReason 原值;缺席 = 此前载荷形
  "lastSeq": 1234,                   // 轮终局帧流水位(客户端比对本地水位判断追赶径)
  "ts": 1712345678901                // 签发时刻 epoch ms(接收方防重放/弃过期)
}
```

- **`reason` 可选位(增笔 2026-09-04,用户令「RFC-SC-1/2/3 开工」;契约 contract-v0.21,来源卡 SC-43;服务端出站面加法,接收方零改动)**:`status` 只投影 completed|aborted 两词,`reason` 携轮终局帧 `TerminalReason` 原值——接收方据此单帧区分 **`client_gone`**(订阅者全部离场后被 §六 孤儿策略 / 宿主 `onSubscribersGone` 钩子裁 interrupt 止损;此前与用户 ④interrupt 的 `aborted_streaming`/`aborted_tools` 同形)、**`internal_error`**(内核故障隔离层收口)、用户中断 `aborted_*`、资源上限 `max_turns`/`budget_exceeded` 等。终局帧携带即在场(内核终局帧恒携,实践中恒在场);词表 = `@tansr/protocol` `TerminalReasonSchema`,随 protocol 版本**加法**演进,接收方恒按「已知值专项 + 未知值兜底」消费、**恒不做封闭枚举校验**(RFC-R77-1 §3 同律)。`status` 语义不变;`client_gone` 的 `status` 仍为 `aborted`。

- **签名(可选;FX-C-43 双签一版,2026-09-07)**:配置密钥在场时携头 `x-tansr-signature`,按 `signatureVersions`(缺省 `['v2','v1']`)每版本一段、逗号分隔:`v2=<hex(HMAC-SHA256(secret, canonical))>`(canonical 与 contract-v0.13 `@tansr/protocol` `WEBHOOK_SIGNATURE_V2` 同源 = `"tansr-webhook-v2\n" + "POST\n" + <url 路径,不含 query> + "\n" + <x-tansr-timestamp> + "\n" + <x-tansr-nonce> + "\n" + hex(SHA-256(体))`,随行头 `x-tansr-timestamp: <epoch ms>` / `x-tansr-nonce: <每次投递随机 UUID>`,重试重发恒同一 ts / nonce / 体),`v1=<hex(HMAC-SHA256(secret, 原始请求体全文))>`(旧形 `sha256=<hex>` 的同义段)。接收方校**任一其接受的版本**(v2 在场恒只校 v2,不许降级;tansrd 入站缺省只接受 v2,`webhook.inbound.allowLegacySignature=true` 加收 v1,时间窗 `toleranceMs` 缺省 300 s)。**弃用时间表**:本版双签 → 下一 minor 缺省 `['v2']`(仍可显式加 v1)→ 再下一版删 v1。出站请求恒不带任何鉴权凭据头(webhook 真伪凭签名,不凭凭据外带)。
- **投递语义**:2xx 即成功;非 2xx/网络错/超时 → 指数退避重试,至多重试 3 次(初发 1 + 重试 3;`notifyMaxRetries` 可配);单次投递短超时 5 s(`notifyTimeoutMs`);退避基数 500 ms ×2 逐次(`notifyBackoffMs`)。投递全程 fire-and-forget,**恒不阻断会话主链**;终败落结构化日志(`server.v2.log.notify_failed`)恒不抛。
- **配置位**(`StartServerOptions.v2.onTurnEndNotify`,L3 注入契约;缺席 = 恒不出站,既有行为字节不变):`{ url, secret?, maxRetries?, timeoutMs?, backoffMs? }`(schema 持份 `packages/server/src/v2/notify.ts` `TurnEndNotifierOptions`;载荷形 `contract.ts` `TurnEndNotifyPayload` 同笔冻结)。**CLI 形态(FX-C-09/续,2026-09-08)**:`tansr serve --v2` 以 env `TANSR_SERVE_NOTIFY_URL`(绝对 http(s) URL;非法忽略并告警)/ `TANSR_SERVE_NOTIFY_SECRET`(只在 URL 生效时生效;取值恒不入日志 / 告警)接同一缝,其余键取库缺省;**无 URL = 不出站**。
- **演进纪律**:载荷加键循「可选位加法 + 接收方未知键忽略」同律(§一-5);本缝是服务端出站面,Kotlin 侧无持份义务(SDK 恒不消费 webhook)。
- **有界投递(增笔 2026-09-03,用户授权代拍 doc/118 §八 G4-c;服务端行为增笔,接收方零改动)**:投递机**有界并发**(在飞 ≤ `notifyMaxInflight` 32,其余 FIFO 排队)、**有界队列**(排队 ≤ `notifyMaxQueue` 1024,满则丢弃**最新到达者**)、**目标级熔断**(连续 `notifyBreakerFailureThreshold` 5 次终败 → 打开 `notifyBreakerOpenMs` 30 s,打开期恒不出网,到期半开只放行 1 个探测,成功闭合/失败重开)。被丢弃者立即走 `onDeliveryFailure`,`attempts = 0`,`reason` 为 **`queue_full`** 或 **`circuit_open`**(与出网终败的 `http_<status>`/网络错 message 同一通报面);优雅关闭期队列待发静默收口(关机不是投递终败)。配置位加四可选键 `{ maxInflight?, maxQueue?, breakerFailureThreshold?, breakerOpenMs? }`(缺省 = §六 冻结值)。
- **延后复判(同笔增笔,SC-19)**:轮终局帧抵达时**有**订阅者不再立即裁「不发」——登记 `notifyDeferMs`(2 s)后复判:窗内订阅者全部离场(半开连接被探活/心跳写触发的 close、断线事件与终局帧几乎同刻抵达)且会话未起新轮、未终结、未进入关闭期 → 出站(`ts` 取签发时刻);否则维持不发。「同轮恒一裁」防抖不变;`notifyDeferMs = 0` 回到即时裁决。

### 5.5 服务端生命周期与部署注记(增笔 2026-09-03,用户授权代拍 doc/118 §八 G4-h / G2;服务端行为增笔,端侧零改动)

1. **优雅关闭(drain)三阶段**:① 拒新——新建/resume 一律 `503 draining` 携 `Retry-After`(§七),同时向**全部在场 SSE** 下发一帧纯 `retry:` 字段帧(长间隔,缺省 10 s,数值属运维配置不冻结;无 `data`,不触发 message 事件)——**客户端必须按该值重连**(本机即将停监听,长间隔后重连自然落到别的副本);② 等在飞轮至终态或超时(缺省 30 s,不冻结)后 interrupt 剩余轮(轮末 store 提交照走,resume 可续);③ 落盘 flush(全部 fire-and-forget 的 store 提交 settle)→ 终结会话、停监听、掐连接。drain 期批量中止的终局帧恒不触发 §五-5.4 出站(非业务轮末);客户端靠回连 `Last-Event-ID` / history / resume 追赶。
2. **多副本部署注记**:会话运行态(注册仓/环形缓冲)在**进程内**,休眠态在**该副本私有**的存储根——多副本部署**必须**按路径中的 `sessionId` 做一致性哈希粘性路由(`/v2/sessions/:id/*` 恒落同一副本;创建请求无 `sessionId` 可落任意副本,之后客户端天然携 id),每副本私有存储根;**恒不共享存储目录**(NFS/EFS 等共享盘是**不安全配置**而非「慢一点」:会话锁按本机 PID 判活,跨容器/主机语义不成立,误判即并发写同一 journal → `500 store_corrupted`)。副本故障 = 其上会话不可迁移(私有存储),客户端按 L3 在别处 resume 得 `404 session_not_found` 后新建。客户端语义零变化:上述皆部署面义务。
   - **分片亲和 sessionId(服务端行为增笔;SC-32b)**:创建请求落到哪个副本由 LB 任意决定,而 sessionId 由该副本产生——纯随机 id 下,后续按 id 哈希的请求未必落回创建副本(`session_not_found`),按 Authorization 头哈希又随令牌刷新漂移。故副本可配**分片码**(0–255):在场时全新会话的 sessionId **前两位十六进制恒 = 分片码**,其余位仍为 UUID v4 随机位(version/variant 位不动,形制仍是 UUID,唯一性不受影响);反向代理按 id 前两位做静态前缀路由(nginx `map` / envoy 前缀匹配)即可把 `/v2/sessions/:id/*` 恒路由回创建副本,无 cookie、无共享存储、无端侧改动。resume/attach 沿用原 id。**客户端恒把 sessionId 当不透明字符串**,不得解析前缀、不得假定长度或字符集(单副本/未配分片时即纯 UUID)。副本序号(K8s StatefulSet ordinal)即分片码,扩缩容不改既有映射。
3. **SSE 首帧 `retry:` 可含抖动**:服务端可在缺省 3000 上叠加均匀抖动(部署可配,缺省 0)以打散重连惊群;客户端恒按帧内实际值重连,不得假定固定常量。
4. **过载 / drain 期 `retry:` 抬高**:服务端可在过载或 drain 期把此后新建流的首帧 `retry:` 抬高(既有流按第 1 条广播长间隔帧);客户端同上按实际值重连。

## 六、多会话治理与体帽表(⚠数值全表定稿)

| 参数 | 冻结缺省 | 语义 |
|---|---|---|
| `retentionMs` | 30 min | 终结会话内存记录保留窗,到点逐出(重放责任移交 store);恒只 /v2 会话仓生效,/v1 记录语义零触碰 |
| `idleTimeoutMs` | 24 h | 闲置(无输入/无订阅活动)会话自动 close 落 store,resume 可复活;与 doc/91 租约 30 天窗层次分明 |
| `maxSessionsPerEndUser` | 8 | per-endUser 并发活跃(未终结)会话帽,超限 429 `session_limit_exceeded` |
| `maxBodyBytes` | 1 MiB | /v2 缺省体帽(承 /v1) |
| `mediaMaxBodyBytes` | 20 MiB | ②messages 与 ⑨tool-results 两端点专项放宽(/t1 媒体 32MB 专项帽先例) |
| `toolTimeoutMs` 缺省/帽 | 120 s / 600 s | §四-4.2-1 |
| `permissionTimeoutMs` | 120 s | §五-5.1 |
| `questionGraceMs` | 60 s | §五-5.2 |
| `eventBufferSize` | 1024 | 逐会话环形缓冲(承 /v1 配置) |
| `notifyMaxRetries` | 3 | §五-5.4 轮末出站通知至多重试次数(初发 1 + 重试 3) |
| `notifyTimeoutMs` | 5 s | §五-5.4 单次投递超时(短超时恒不阻断主链) |
| `notifyBackoffMs` | 500 ms | §五-5.4 重试退避基数(指数 ×2:500/1000/2000) |
| `orphanGraceMs` | 60 s | **增笔 2026-09-03(G4-c 代拍)** 孤儿宽限:**曾有订阅者又全部离场**的会话,运行中的轮再跑多久被 interrupt(轮末 store 提交照走,resume 可续;webhook 按 `turn.aborted` 出站);从未订阅过的会话(REST-only 客户端)不适用,由 `idleTimeoutMs` 止损;宿主可经策略钩子覆写 |
| `idleAfterGoneMs` | 5 min | **增笔 2026-09-03(G4-c 代拍)** 订阅者离场后会话 idle 持续多久 close 落 store(resume 可复活);同上只对曾订阅又离场者生效 |
| `sweepIntervalMs` | 10 s | **增笔 2026-09-03(G4-c 代拍)** 治理扫描间隔(保留窗逐出/闲置回收/孤儿策略/寿命帽按到期项增量扫描;此前 60 s) |
| `notifyDeferMs` | 2 s | **增笔 2026-09-03(G4-c 代拍)** §五-5.4 轮末通知延后复判窗(有订阅者时延后一拍复判离场;0 = 即时裁决) |
| `maxSubscriberBufferBytes` | 2 MiB | **增笔 2026-09-03(G4-c 代拍)** 每条 SSE 连接用户态待写队列上限(`res.write` 返 false 后到达的帧在此排队),超界按慢消费者策略处置(§三-5:缺省 `disconnect`,可配 `drop-oldest`) |
| `slowSubscriberGraceMs` | 10 s | **增笔 2026-09-03(G4-c 代拍)** SSE 连接持续 paused(等对端读)超此时长判慢消费者(仅 `disconnect` 策略) |
| `notifyMaxInflight` | 32 | **增笔 2026-09-03(G4-c 代拍)** §五-5.4 出站通知在飞投递上限 |
| `notifyMaxQueue` | 1024 | **增笔 2026-09-03(G4-c 代拍)** §五-5.4 出站通知等待队列上限(满则丢最新到达者,`queue_full`) |
| `notifyBreakerFailureThreshold` | 5 | **增笔 2026-09-03(G4-c 代拍)** §五-5.4 连续终败多少次打开目标级熔断 |
| `notifyBreakerOpenMs` | 30 s | **增笔 2026-09-03(G4-c 代拍)** §五-5.4 熔断打开时长(到期半开放行 1 个探测) |

- **`idleTimeoutMs` 24 h 维持注记(2026-09-03,G4-d 代拍)**:`idleAfterGoneMs` 5 min 已覆盖「曾订阅又离场」的主流形态,24 h 只兜从未订阅过的 REST-only 会话,故不下调(原则 6 已有能力不退化)。
- **无冻结缺省的治理位(2026-09-03,G4-c 代拍)**:`maxRetainedSessions`(终结残影数量帽,LRU 最旧先逐)、`maxSessionLifetimeMs`(自纳管起的寿命帽)、`maxTurnsPerSession`(轮数帽)为**宿主配置**,缺省恒不设(Infinity),**不入本表、contract.ts 不设常量**——数值随租户与部署容量决定,部署参考值见 `packages/server/README.md`。上表全部数值 = `packages/server/src/v2/contract.ts` `V2_LIMITS` 同笔持份。
- **store 保留期(增笔 2026-09-07,十王修案 FX-C-09 / C2-04;服务端行为增笔,wire 零改动)**:治理扫描第 ⑤ 段「store 保留」按会话工厂 `sessions.retention{ maxAgeDays, maxPerEndUser, maxBytes }` 删**休眠态**会话记录(store `meta.updatedAt` 判龄;每 endUser 按 updatedAt 保留最新 N 条 / 字节帽自最旧起删;附件 / 同居快照 / 独立根快照随记录同删;**在册活跃会话恒不删**)。**缺省(`retention` 缺席)= 不扫描、零删除 = 现状永存**——机器恒不物理删除业务主数据,除非运维显式配置(L1);在场时 `maxAgeDays` 缺席按 **30 d**(与 api 会话租约窗 `SESSION_LEASE_WINDOW_DAYS` 同值;bundle owner 档 `governance.sessionRetentionDays` 到达 serve 后改读为缺省,M-05)。实扫间隔缺省 1 h(`sweepIntervalMs`,不入 `V2_LIMITS`);每轮有删除记结构化日志 `session.retention_swept{scanned,deleted,failures,byReason}`(恒不含会话内容)。被删会话此后 resume → `404 session_not_found`(客户端按 L3 新建;端侧零改动)。env 旋钮 `TANSR_SERVE_SESSION_MAX_AGE_DAYS` / `TANSR_SERVE_SESSION_MAX_PER_END_USER`(任一在场即启用)。

## 七、错误码表

| code | HTTP | 端点 | 语义 |
|---|---|---|---|
| `unauthorized` | 401 | 全部 | 开发者登录态缺席/无效(authenticate 缝 null) |
| `forbidden` | 403 | 一切 `:id` 面 + resume | endUser 归属不符(§一-3 统一 403) |
| `session_not_found` | 404 | `:id` 面 / resume | 会话不在内存也不在 store(本租户域) |
| `session_ended` | 409 | ② | 终态会话注入输入 |
| `session_limit_exceeded` | 429 | ① | per-endUser 活跃会话超帽 |
| `resume_unavailable` | 409 | ① | store 未接线时请求 resume |
| `store_corrupted` | 500 | ①⑧ | store 损坏结构化上抛(恒不静默回残缺历史;serve routes 500 映射在产) |
| `create_failed` | 400 | ① | 装配失败(未知别名/未知 profile 等,message 已本地化) |
| `validation_failed` | 400 | 全部 | 载荷/参数形状非法(含 exclude 非法/endUser 双写不符以外的一切形状错) |
| `payload_too_large` | 413 | 全部 | 超体帽(/v2 词面;/v1 的 body_too_large 恒不动) |
| `midturn_blocks_rejected` | 422 | ② | 运行中注入 blocks(D8 分野) |
| `call_not_found` / `call_already_resolved` / `call_expired` | 404/409/410 | ⑨ | 工具回执三态 |
| `request_not_found` / `request_already_resolved` / `request_expired` | 404/409/410 | ⑩⑪ | 权限/提问回执三态 |
| `digest_mismatch` | 409 | ⑩ | 复述不符 |
| `method_not_allowed` | 405 | 全部 | 方法不符(携 `allow` 头) |
| `invalid_json` | 400 | 全部 | 体非合法 JSON |
| `not_found` | 404 | — | 未知路径 |
| `internal_error` | 500 | 全部 | **增笔 2026-09-03(G4-a 代拍)** 路由层统一错误边界兜底:任何处理器内未捕获异常/拒绝收成本信封,**进程恒不退出**;响应头已发(SSE 流)则只收流让客户端重连。客户端按 5xx 退避重试 |
| `overloaded` | 503 | ①(会话帽/ELD/铸令牌闸)③(SSE 连接帽)②⑨⑩⑪(在飞体预算) | **增笔 2026-09-03(G4-a 代拍)** **本机**准入/过载拒绝,**恒携 `Retry-After`**:总活跃会话帽 / SSE 连接帽(总 + per-endUser)/ 在飞请求体总预算 / 事件循环延迟 p99 超阈 / 平台铸令牌闸队满或排队超时(缺省全部不设帽,部署可配)。语义「本机忙,稍后再来」。**同名不同层注记(SC-31)**:kernel MCP 连接治理层的 McpErrorCode 'overloaded'(写背压超帽拒收单条工具调用)与本码**无映射关系**——前者在桥接层作普通工具错误回模型,恒不上升为本 HTTP 信封 |
| `draining` | 503 | ① | **增笔 2026-09-03(G4-a 代拍)** 服务端优雅关闭期(§五-5.5)拒新建/resume,**恒携 `Retry-After`**;语义「正在关闭,换台再来」——在场 SSE 同时收到长间隔 `retry:` 帧 |
| `rate_limited` | 429 | ① | **增笔 2026-09-03(G4-a 代拍)** 宿主 rateLimit 缝拒绝(per-endUser / per-IP 策略归宿主),**恒携 `Retry-After`**。**注记 2026-09-06(PL-22,doc/124 三原则 3 / doc/125 结论 12;加法,不占 contract 号)**:平台内置形铸令牌撞**开发者套餐配额**(api 429 `plan_concurrency_exceeded` 并发硬帽 / `plan_end_users_exceeded` 测试期月度终端用户帽)亦映射本码——`Retry-After` 取 api 值(套餐 429 恒 30 s;负缓存窗内为剩余窗),信封加法 **`detail: { scope: 'plan' }`**(客户端据此区分「本机限流」与「套餐配额」;无 `detail` = 宿主限流现状);端上恒不见 `cap / hardCap / current / tier / upgradeUrl`(进服务端结构化日志 `session.rejected{ scope:'plan' }`);**不再译 503 `upstream_unavailable`**(那是上游坏,这是客户配额,重试无益于加压的判据不成立)。旧端按既有 429 + `Retry-After` 退避恒安全 |
| `upstream_unavailable` | 503 | ① | **增笔 2026-09-03(G4-a 代拍)** 平台侧铸令牌/配置拉取暂不可用(上游 429/5xx/网络错),**恒携 `Retry-After`**(取平台值或本地退避);与 `overloaded` 分开:一为**上游**坏(重试无益于本机加压),一为**本机**忙 |
| `turn_running` | 409 | ⑫⑭⑮⑲ | **增笔 2026-09-04(doc/116 V-1)** 轮进行中(含空闲直压在飞)的空闲期端点拒绝;不排队、不隐式等待,客户端空闲后重试(⑲ 增笔 2026-09-05,W-2) |
| `checkpoint_not_found` | 404 | ⑮⑯ | **增笔 2026-09-04(V-1)** 快照不存在(非法 id 形态同按不存在) |
| `session_mismatch` | 409 | ⑮ | **增笔 2026-09-04(V-1)** 快照归属他会话(跨会话恢复 = 越界;fork 二期另议) |
| `checkpoints_not_wired` | 409 | ⑫(仅显式 checkpoint 真值)⑬⑭⑮⑯ | **增笔 2026-09-04(V-1)** 服务端未接快照存储(工厂 `checkpoints` 缺席);可解释不静默 404 |
| `cwd_not_allowed` | 400 | ①⑲ | **增笔 2026-09-04(V-3;D-6 A)** body.cwd 无策略(fail-closed)/ 越界 / 策略拒(⑲ 同闸,W-2) |
| `cwd_invalid` | 400 | ①⑲ | **增笔 2026-09-04(V-3)** body.cwd 非绝对 / 不存在 / 非目录(message 携 reason)(⑲ 同查,W-2) |
| `cwd_unavailable` | 409 | ①(resume) | **增笔 2026-09-04(V-3)** 工厂 `cwdOnResume:'stored'` 而 store `meta.cwd` 目录已不存在;**改口 2026-09-07(FX-C-08 / R-04)**:存储值重过策略闸不过亦此码,`detail.reason` 加法键 `'not_allowed_on_resume'`(策略收窄 / 缺席)\| `'missing'`(目录已失 / 非目录)\| `'not_absolute'`;`'stored'` 自本笔起为配了 `cwdPolicy` 宿主的缺省(①-f) |
| `cwd_switch_unsupported` | 409 | ⑲ | **增笔 2026-09-05(W-2)** 会话的内层装配未提供执行器/权限门重建面(注入形内层工厂未接 `bindCwdRebinder`,或句柄无 `setCwd` 面);fail-closed 不做半切换,平台内置形恒支持 |
| `fork_source_not_found` | 404 | ①(fork) | **增笔 2026-09-04(F-3)** body.fork 的源会话或快照不在本租户快照根(他租户目标天然不可见;在册他人源会话为 403 forbidden) |
| `checkpoint_import_invalid` | 400 | ⑱ | **增笔 2026-09-04(F-3)** 导入体不是合法 `tansr-checkpoint/1` 导出件(format / schema / messageCount / 附件 sha256 / $ref 附件缺失 / 消息形任一不过;篡改即拒,零落盘;message 携 detail) |
| `capability_disabled` | 403 | ⑳㉑ | **增笔 2026-09-05(doc/123 B-3)** bundle 有效能力档 `platform.speechToText`(⑳)/ `platform.textToSpeech`(㉑)未开;message 携位名与控制台修复指引(与 SDK 装配期 `capability_disabled` 同名同义;直连不经工具白名单,受能力位闸) |
| `platform_unavailable` | 409 | ⑳㉑ | **增笔 2026-09-05(doc/123 B-3)** 会话无平台直连面(注入形内层工厂 / 自实现句柄未透出 `platformAudio`;平台内置形恒有);可解释不静默 404 |
| (透传)`asr_not_configured` `tts_not_configured` `asr_quota_exceeded` `tts_quota_exceeded` `bad_request` `insufficient_balance` … | 平台原状态码(业务 4xx) | ⑳㉑ | **增笔 2026-09-05(doc/123 B-3)** 平台 `/t1/asr` / `/t1/tts` 业务 4xx 错误码**原样透传**(非 v2 自有码,不入 `V2_ERROR_CODE`;非 JSON 体 `http_<status>`);message 经 i18n 包装携上游人话;网络错 / 2xx 无产物按既有 `upstream_unavailable` 503。**改笔 2026-09-11(RF-05a / RV-4-05)**:平台 401 / 403(`model_not_authorized` 等)/ 5xx(`upstream_error` 等)自此**不透传**,归一 503 `upstream_unavailable` 固定词条,原文只进 `upstream.rejected` 日志(§二-⑳㉑) |
| (透传)`plan_required` `plan_tier_insufficient` | 403 | ① | **增笔 2026-09-06(PL-22,doc/125 §7.2)** 平台铸令牌 403 套餐拒绝——该 App 的 owner 无有效套餐 / 档位不含能力,是**开发者装配错误**(不该到终端用户):原码透传(非 v2 自有码,不入 `V2_ERROR_CODE`;⑳㉑ 透传先例)+ `detail: { scope: 'plan' }`,message 经 i18n 包装携原码;不携 `Retry-After`(不是等一等能好的事);其余铸令牌 4xx 维持 400 `create_failed` |

- **`Retry-After` 注记(2026-09-03,G4-a 代拍)**:上表 `overloaded` / `draining` / `rate_limited` / `upstream_unavailable` 与既有 **`session_limit_exceeded`(429)恒携 `Retry-After`**(整秒,向上取整,至少 1;服务端可叠加抖动)。**客户端退避纪律**:429/503 携 `Retry-After` 时按其整秒值等待后重试;5xx 无 `Retry-After` 时指数退避 + 抖动;`draining` 期间 SSE 已收到的 `retry:` 帧值同为重连依据(§五-5.5)。既有端点状态码/响应形零改动;五码为**新增行**,旧端按「未知 code 兜底 + 按状态码退避」恒安全。
- **信封 `detail` 注记(2026-09-06,PL-22;加法,不占 contract 号)**:错误信封形 `{ error: { code, message, detail? } }`——`detail` 为**可选结构化位**,在场才落键(既有全部信封字节零变),值恒只述机器可判事实、恒不含 endUser 身份 / 令牌 / 内部 id;首个消费方 = ① 套餐配额 429 与套餐 403 的 `{ scope: 'plan' }`。旧端(Kotlin/Swift 未知键忽略,I-1a 同律)零改动恒安全;结构化持份候各端批次(可选:按 `detail.scope === 'plan'` 呈现「稍后重试」而非「本机限流」文案)。

## 八、安全红线(施工与集成双向义务,doc/96 §五承接)

1. /v1 serve Bearer token 恒不受理于 /v2;app_user 令牌/appkey 恒不下发设备;
2. 下行帧素材恒过 redactDeep + summarizeCallTarget(permission 帧);question 载荷逐字段白名单拷贝;tool.request 的 args 全量下发前恒过 zod 形状校验;
3. 工具回执/申报均为不受信输入:形状校验恒强制(zod/内容型白名单/体帽);语义面缓解 = 能力档收窄 + 权限桥 + 事件流归因;
4. digest 复述恒强制;requestId/callId 第一份有效终局;
5. 会话 id 恒不当鉴权因子(归属校验独立成立);SessionStore 按 endUserId 分域(路径物理分隔);/v2 恒无跨 endUser 读面。

## 九、serve 侧 SessionStore 持份纪律(A0-6 真接登记)

- serve /v2 store 接口 = doc/91 拍板③冻结形(`create/get/list/commit/delete`,SessionRecordMeta/SessionStoreCommitMeta 同形)——与 sdk `packages/sdk/src/sessions/store.ts`(bb9b1521)**同构双份**(pairing.ts 双份先例同律);实现 `packages/cli/src/serve/agent-session-store.ts` 复用 kernel journal 原语(JournalWriter 哈希链/readAll 崩溃截断修复/acquireSessionLock/整卷轮换),落盘记录 producer='serve',与 SDK/CLI 卷互认;
- **per-endUser 分域**:目录布局 `<root>/agent-sessions/<endUserKey>/sessions/<sessionId>/…`,endUserKey = endUserId 的 sha256 前 16 hex(路径安全 + 物理分隔);
- 接口/语义改动恒与 sdk 侧同笔 + 本档增笔登记(双仓提交号互登)。
- **分域索引为缓存非事实源(增笔 2026-09-03,用户授权代拍 doc/118 §八 G8;磁盘格式零改动)**:每 endUser 域旁挂 append-only 索引 `<endUserKey>/index.jsonl`(供 ⑦ 列表 / ⑥ 元信息 O(1) 读),**journal + meta.json 是唯一事实源**——索引缺失/损坏/失配恒可由目录扫描重建并原子重写,删掉即冷启动;任何消费方恒不得把索引当作会话存在性或内容的裁决依据。轮末 `commit` 改为**增量追加**(持久句柄 + 游标,不再整卷重读)且**不改磁盘格式**(记录形/哈希链/meta.json/目录布局逐字节同前,旧目录可读、新写旧读);sdk 侧 file-store 同笔。
- **历史只读共享契约(同笔增笔)**:宿主经 `onTurnEnd` 收到的 `history`(以及 TUI/acp 驱动同名回调)为**只读共享**的冻结数组——消费方恒不得改写其元素或数组本身(需私有副本自行 clone);数组增删抛 TypeError,元素改写为契约禁止项(不做运行时深冻结)。store/sink 序列化、标题扫描、记忆提取切片等既有消费方均为只读,零改动。
- **段与清单 / 分页 / 快照同居 / meta.cwd(增笔 2026-09-04,doc/116 §3.4·§3.5·§3.6·§3.7 / doc/117 V-1·V-2·V-3;sdk 份 file-store 同笔——S-3 冻结签名,server 份先落)**:① `createServeAgentSessionStore({ dir, segmentation? })`——卷内按阈值动态封段(kernel K-2 manifest + `segments/NNNNNN.jsonl`,`journal.jsonl` 恒为活段名,**无 manifest = 旧单卷**零迁移);store 层**缺省启用**(`{}` = 16 MiB / 5000 记录,D-5),`{ enabled: false }` 关;② **整卷轮换红线(D-K1)与 `.next` 暂驻协议(V-5 对齐 sdk 份 S-3,逐字同形)**:`rotateVolume` 新卷在临时目录以单活段写就 → rename 为 `<sessionDir>/journal.next.jsonl`(新卷完整耐久就位)→ `finishRotation`:`clearSegments`(manifest 写为无段 → 删 `segments/` → 删 manifest)→ rename `.next` 替换 `journal.jsonl`;旧段恒不随 manifest 留存(否则读侧复活压缩前消息 / 链断)。**崩溃安全**:`.next` 就位前崩溃 = 旧卷完好(rename 原子);就位后任一点崩溃,下次 `get` / `getHistoryPage` / `commit`(serve 份另含既有会话的 `create` 补录径)持锁后先补齐后半程——读回**自愈**为新卷,恒不停在「旧段已删、新卷未就位」的截断态,恒不 `store_corrupted`;③ 可选 `getHistoryPage(endUserId, sessionId, { offset, limit }) → { messages, total, offset } | null`(五方法承诺不变;签名与 sdk `SessionStore.getHistoryPage` 同笔冻结,首参多 endUserId;V-5 起返回形携对齐后起点 `offset`):自尾段反向装载(`readTail`)只读页位所需段,切片经 `alignHistoryPage` 配对边界对齐(起点落在被跳过段内或后缀起点非干净边界即多装一段重试),**卷不变量** = 本 store 卷只含 `producer='serve'` 的主干 model 可见消息记录(记录数 == 消息数),已装载后缀不符即退全量读;`limit=0` 只取 total;④ 可选 `sessionDirOf(endUserId, sessionId)`(纯路径):工厂据此把快照同居在 `<sessionDir>/checkpoints/<sessionId>/{<id>.json,<id>.meta.json,attachments/}`(随会话删除);⑤ `create(endUserId, { …, cwd? })` 首次建档写 `meta.cwd` = 实际会话 cwd(此前恒 `process.cwd()`;既有记录不改写),`SessionRecordMeta.cwd?` 随 meta 读出(resume 对账锚)。磁盘格式:记录形 / 哈希链 / `meta.json` 既有键零改动;新增文件 = `manifest.json` + `segments/` + `checkpoints/`,旧代码读旧单卷照常。
- **cwd 中途切换登记(增笔 2026-09-05,doc/116 §3.7 D-7 / doc/117 W-2;sdk 份 file-store 同笔——`SessionStore.recordCwdChange?` / `SessionRecordMeta.cwdHistory?`)**:⑥ 可选 `recordCwdChange(endUserId, sessionId, { at, from, to })`(五方法承诺不变;自实现 store 缺席 = 切换不落盘,⑲ 照常 200):`meta.json` 原文读-改-写——`cwd` 改写为 `to`(**`meta.cwd` 自此 = 会话当前 cwd**,创建期值可自 `cwdHistory[0].from` 追溯;resume 对账锚随之)、`cwdHistory[]` 追加 `{ at, from, to }`(原文 JSON 扩展键,与 `platformSessionId` 同法,kernel `SessionMetaSchema` 不认亦不剥);不触卷、不改 `updatedAt`;`AgentStoredSessionMeta.cwdHistory?` 随 meta 读出(坏形整体按缺席)。磁盘格式:仅 `meta.json` 加一可选键,旧代码忽略未知键照常。

- **fork 面(增笔 2026-09-04,doc/117 §十 F-3;sdk 份 file-store 同笔)**:可选 `fork(endUserId, { sourceSessionId, checkpoint, newSessionId?, cwd, title? }) → { sessionId }`——经 kernel `forkSessionFromCheckpoint` 落新会话为**独立新卷**(首记录 `kind:'fork'` 携 `{ forkMode:'independent', uuidPolicy:'remap', sourceBranchId:'main', sourceSeq:-1, sourceSessionId, checkpointId }`,internal 可见性;随后为配对治理后的快照 messages,`producer='serve'`;图像附件按 sha 复制到新会话 `attachments/`),`meta.json` 加可选 `forkedFrom { sessionId, checkpointId }`(kernel `SessionMetaSchema` 可选位;`AgentStoredSessionMeta.forkedFrom?` 读出,⑥⑦ wire 暂不携)。源会话目录零字节改动(不入源会话队列、不抢源锁)。工厂缺该法时回落 `create + commit` 全量历史(功能同在、无血缘记录;五方法承诺不变)。磁盘格式:记录形 / 哈希链既有键零改动,新增可选位皆向后兼容(旧代码读 fork 卷:`fork` kind 早在词表内,重建天然跳过)。

- **分层存储:热层 + 可挂载冷层(增笔 2026-09-05,doc/119 IO-21 / doc/120;sdk 份 file-store IO-22 同笔同形;wire 零改动、客户端零感知)**:① **热层 journal 仍是唯一事实源**(轮末提交仍先落本地盘 fsync + rename;耐久性地板不变),冷层只承接**封存后不可变的段**与派生的冷清单(protocol `SegmentManifest`,contract-v0.22 / RFC-SC-4),热清单 `manifest.json` 保留为本地分段真理;② 既有工厂**加可选项、不另起函数名、doc/91 冻结接口不改**:`createServeAgentSessionStore({ dir, segmentation?, cold?, index?, policy?, metrics?, logger?, onStoreEvent?, onStoreError? })`——`cold` 为一级 `SegmentBlobStore`(put/get/head/list/delete 五字节方法;S3 / OSS / COS / MinIO / GCS / 文件系统)或二级 `SessionHistoryStore`(store/load/listSessions/remove 自管布局);**`cold` 缺席 = 今日行为字节等价**(不创建分层核、不传钩子;`index` / `policy` / `metrics` 随之无效);一级冷层 `conditionalPut:false` 或 `list:'none'` 且无 `index` → 构造期 fail-fast(A15 / D-5);③ 接线 = kernel 段层两钩子:每会话 `createColdTier(...).bind({ sessionId, endUserKey, sessionDir })` 得 `onSegmentSealed`(封段完成 → 编码 → 上传 → 冷清单 CAS;异步、恒不阻塞轮末 ack)与 `segmentSource`(已提交段被热盘 LRU 逐出后读侧回源,解压后仍是 JSONL,链校验同律);冷层键形 `<tenant>/<endUserKey>/<sessionId>/<volumeId>/…`(`endUserKey` = 既有分域哈希,`tenant` 缺省 'default');④ **A10 热层全失仅凭冷层重建**:`get` / `getMeta` / `getHistoryPage` / `commit` 在热层**无该会话(meta.json 缺席)**时先 `tier.restore` 重建热清单骨架(全部 sealed、段文件缺席)并补 `meta.json` 展示位(createdAt / updatedAt / title / platformSessionId 取冷层会话元;cwd 取进程 cwd)+ 索引登记,再走既有读路径;`list` 合并冷层 `listSessions(endUserKey)`(热层已有者以热层为准);**刻意不以「热 manifest 缺席」触发 restore**——无 manifest 的热卷是合法单卷 / 刚轮换态,灌回旧卷清单 = 压缩前消息复活(D-K1 红线);⑤ 可选成员两枚(纯加法,五方法承诺不变;sdk `SessionStore` 同笔):`flush?(options?) → Promise<boolean>`(等在飞提交 settle + 冷层上传队列排空;SC-14 drain 第三阶段缝——工厂 flush 等热层落盘之后再等冷层;无冷层即 true)与 `readiness?() → { ready, reason? }`(热盘触顶 `policy.hotRetention.maxBytes` → `{ ready:false, reason:'store_hot_full' }`;形同 `ReadinessProbe` 返回值,`AgentStoreReader.readiness?` 同形,startServer 在场即接进 `/readyz`,D-4);⑥ `delete` **先**冷层 `remove`(段 + 清单 + index;幂等)再删热层目录(反序会让 list 复现 + restore 复活);整卷轮换(rewritten / 收缩)后解绑,下次访问重新 bind(新卷代际 `volumeId` 语义归 kernel IO-12,A11 旧卷独立过期);⑦ **失败语义**:上传 / 冷清单提交失败由分层核按预算重试(缺省 async / 并发 4 / 8 次 / 退避封顶 60 s),**热层已 ack 恒不回滚**——`store.commit_failed` 结构化事件(`gaveUp:true` 时另经 `onStoreError`)只告警不抛给调用方,未上传段恒不删、积压可见(`tansr_kernel_store_backlog_bytes`);读侧回源 / restore 的 `permanent` / `not_found` / `precondition_failed`(篡改 / 跳段 / 链断 / 清单被他副本接管)→ `AgentSessionStoreError`(`store_corrupted` 500 语义不变),`transient`(冷层暂不可达)原样上抛 `StoreError`(不冒充损坏;路由译码候登记);⑧ 结构化日志事件名沿用 `store.*` 族(`LOG_EVENT` 加法 `store.segment_sealed` / `store.segment_committed` / `store.hot_full` / `store.hot_recovered` / `store.evicted` / `store.orphan_gc`;`store.commit_failed` 既有名复用,字段加 `kind` / `attempts` / `gaveUp`),字段恒不含消息内容、`endUserKey` 为哈希非身份;⑨ **部署纪律**:热层仍禁共享盘(§五-5.5 第 2 条不变);冷层生命周期规则承接保留窗,加密 / 驻留地 / 访问控制归开发者(`policy.transform` 挂点);`@tansr/serve` 依赖面仍只有 zod(冷层适配器另包或 `examples/`)。磁盘格式:热层记录形 / 哈希链 / `meta.json` / 目录布局零改动;冷层对象格式 = protocol `SegmentManifest` + 段 JSONL(+gzip),契约见 RFC-SC-4。

## 十、增笔区

OBS-05/09（2026-09-11）：⑥/⑦新增可选 context/media，schema 为 `V2SessionMetaView`/`V2SessionMediaView`；Android/iOS 在 OBS-10A/I 同批对表，未完成各端原门前不得收编或发布该批。SDK 本地 `contextState`/`switchModel`/`sendBlocks` 不另造 HTTP 动作。protocol 冻结件零修改；原字段/端点语义不变，状态与验收见 SDK-OBS-01 计划。

| 日期 | 变更 | 提交 |
|---|---|---|
| 2026-08-31 | v1 冻结成文;D1/D2/D7/D8/D11 终拍登记(§〇);全 ⚠ 位定稿 | (本笔) |
| 2026-08-31 | A4-2 增笔(加法):§五-5.4 轮末出站通知 webhook 缝(载荷形/HMAC 签名头/投递语义)+ §六 三常量(notifyMaxRetries/notifyTimeoutMs/notifyBackoffMs)+ 附录 A 推送唤起自接指引;**D5 按建议值终拍 2026-08-31**(SDK 恒不内置推送)。服务端出站面,Kotlin 侧零持份义务 | tansr-cli(见 doc/49 A4-2 登记) |
| 2026-09-01 | S-TH2 增笔(加法,用户拍板确认「server wire 契约 thinking 键两项拍板」):§二-① 建会体增 `thinking:{budget}` 可选键(①-d 语义:三级取值恒定序/TWP 档位出线/与呈现档正交)+ 装配面旋钮(平台形工厂 `options.thinking`);Kotlin 持份 CreateSessionRequest.thinkingBudget 同笔(陈列件重拷至 3834bd3e,顺补 8-31 三常量对差 notify*) | tansr-cli `3834bd3e` / tansr-android `b4c83fe` |
| 2026-08-31 | 三方闭环鉴权增笔(加法;用户三层拍板 2026-08-31,全设计 = doc/102):附录 B 内置 token-server 端点(POST /v2/auth/tokens 签发缝 + `x-tansr-serve-token` 消费头 + `issueToken` 缝签名);contract.ts 增 SERVE_TOKEN_HEADER/SERVE_TOKEN_TTL_* / TokenIssueBodySchema/TokenIssueResult。Kotlin 侧持份候第三阶段 authProvider 卡随迁(A4-2 先例) | tansr-cli(serve-v2-auth 泳道) |
| 2026-08-31 | **附录 B 同日撤笔**(用户架构修正拍板 2026-08-31:serve = 裸 /v2 协议引擎发布为 npm 包,会话鉴权/token 形制完全开发者自定 = 最大放权,内置 token-server 完全移除):POST /v2/auth/tokens 端点、`x-tansr-serve-token` 头、`issueToken` 缝、SERVE_TOKEN_HEADER/SERVE_TOKEN_TTL_*/TokenIssueBodySchema/TokenIssueResult 全部退出契约与 contract.ts;§〇-2 鉴权模型随笔定盘。端点同日增删、零线上消费方(Android authProvider 第三阶段未开卡),非 v2 破坏;serve↔api 半边(appkey → per-endUser app_user 令牌)不受影响恒保留 | tansr-cli(serve 裸协议化泳道) |
| 2026-09-01 | **代拍 A 案 2026-09-01(用户授权代拍,原则:最优架构/最简集成/开发者优先)**:§二-⑧ 增 history 出流语义——history 出流恒不含内核 reminder 块(store 保全,出流滤除);过滤单点落 `packages/server/src/v2/history-projection.ts`,routes handleHistory 两分支(live 快照/store 取回)共用;resume 重灌径恒不滤。响应收窄型投影(剔系统元数据,恒不改 wire 形/字段/事件),非 v2 破坏;Kotlin 侧零随动(出流已滤,客户端无感,金样零 reminder 样本核查在案) | tansr-cli(reminder 出流语义泳道,本笔) |
| 2026-09-02 | **S-ARCH P2 增笔(加法,用户拍板「同意」2026-09-02)**:§三-2/3 控制帧四族七名——新增 `server.platform.warning {code,message}`(平台 t.warn 投影;非致命降级结构化告知,恒不代表请求失败;无回执无 closed;恒不受 ?exclude 过滤=控制帧既有律)。发射链 = 平台装配缝 SDK onWarn → per-会话沉降(assembleEndUserModel 入参 onWarning)→ 桥 emitPlatformWarning(带 id 进环形缓冲,断线重放可见)。旧端安全:Android UnknownControlFrame / iOS 兜底实证,未知帧恒不炸;Kotlin/Swift 结构化持份(渲染面)候各端批次随迁(A4-2 先例) | tansr-cli(本笔) |
| 2026-09-03 | **§七 错误码增笔(加法;2026-09-03 用户授权代拍,doc/118 §八 G4-a;来源卡 SC-01/02/13/14/23 → SC-33)**:增五码 `internal_error`(500)/ `overloaded`(503)/ `draining`(503)/ `rate_limited`(429)/ `upstream_unavailable`(503),后四者恒携 `Retry-After`;`session_limit_exceeded`(429)恒携 `Retry-After` 注记;客户端退避纪律一句。contract.ts `V2_ERROR_CODE` 五键旁注同笔改「增笔 2026-09-03(G4 代拍)」。**双仓对表**:Kotlin/Swift 持份候各端批次(doc/97 A4-8 / doc/108 S3-5 候办卡)——错误码五名 + `Retry-After` 消费;旧端按未知 code 兜底 + 状态码退避恒安全 | tansr-cli(本笔,SC-33) |
| 2026-09-03 | **§二-①-e 同 id 并发创建/恢复(服务端行为增笔;G4-b 代拍;来源卡 SC-02)**:后到者合并等待、成功按 attach 语义回 200 `{sessionId, resumed:false, lastSeq}`;归属先比对;resume 目标残影在 create 成功之后才逐出。端侧零改动 | tansr-cli(本笔,SC-33) |
| 2026-09-03 | **§三-5 gap 帧语义澄清 + 慢消费者 gap(文档化,wire 零改动;G4/G6 代拍;来源卡 SC-15/SC-11)**:`droppedEvents` = 自建立以来累计逐出数(非本次丢失数),丢失数 = `oldestRetainedSeq − requestedAfterSeq − 1`;`drop-oldest` 策略下服务端可主动发 gap(`requestedAfterSeq = -1`),缺省 `disconnect`;**§三-6 定界注记(G4-e 代拍;来源卡 SC-29)**:条数 1024 维持、`eventBufferMaxBytes` 缺省不设。端侧零改动(既有 L1/L2 处置不变) | tansr-cli(本笔,SC-33) |
| 2026-09-03 | **§五-5.4 有界投递 + 延后复判(服务端出站面增笔;G4-c 代拍;来源卡 SC-08/SC-19)**:有界并发/有界队列/目标级熔断,丢弃 reason `queue_full` / `circuit_open`(attempts 0);配置位加四可选键;`notifyDeferMs` 延后复判。接收方零改动 | tansr-cli(本笔,SC-33) |
| 2026-09-03 | **§五-5.5 新增:服务端生命周期与部署注记(服务端行为/部署面增笔;G4-h / G2 代拍;来源卡 SC-14/SC-12/SC-32)**:drain 三阶段语义(客户端必须按 `retry:` 帧值重连)、多副本 sessionId 一致性哈希粘性 + 副本私有存储根 + **恒不共享存储目录**、首帧 `retry:` 可含抖动、过载/drain 期 `retry:` 可抬高。端侧零改动(按帧内实际值重连即既有义务) | tansr-cli(本笔,SC-33) |
| 2026-09-03 | **§六 治理表增十行(加法;G4-c 代拍;来源卡 SC-08/11/17/19)**:`orphanGraceMs` 60 s / `idleAfterGoneMs` 5 min / `sweepIntervalMs` 10 s / `notifyDeferMs` 2 s / `maxSubscriberBufferBytes` 2 MiB / `slowSubscriberGraceMs` 10 s / `notifyMaxInflight` 32 / `notifyMaxQueue` 1024 / `notifyBreakerFailureThreshold` 5 / `notifyBreakerOpenMs` 30 s;`idleTimeoutMs` 24 h 维持注记(G4-d);`maxRetainedSessions` / `maxSessionLifetimeMs` / `maxTurnsPerSession` 无冻结缺省注记。contract.ts `V2_LIMITS` 同笔增十键(数值 = 各泳道原本地缺省逐字节等价,消费方改引单源;`test/v2-contract.test.ts` 钉死)。Kotlin/Swift 持份:数值表为服务端治理位,端侧无消费义务,对表卡随错误码一并核对 | tansr-cli(本笔,SC-33) |
| 2026-09-03 | **§九 存储增两条(加法;G8 代拍;来源卡 SC-27/SC-28)**:分域索引 `index.jsonl` 为缓存非事实源(journal + meta.json 唯一事实源;损坏/缺失恒可重建);commit 增量追加不改磁盘格式;`onTurnEnd` history 只读共享契约。磁盘格式零改动,sdk 侧同笔 | tansr-cli(本笔,SC-33) |
| 2026-09-03 | **刻意不写(G4-f / G4-g 代拍登记)**:观测端点 `/healthz` `/readyz` `/metrics` **不入 wire 契约**(运维面,免 Bearer、缺省仅回环;见 `packages/server/README.md` Operations);`turn.aborted.reason` 增 `client_gone` / `internal_error` **不加**(protocol 冻结 + 旧端枚举风险;`turn.error.errorKind` 已可解释)——RFC 草案留档 `doc/rfc/RFC-SC-1-TerminalReason-internal_error-client_gone.md`(状态「草案·不排期」) | tansr-cli(本笔,SC-33) |
| 2026-09-03 | **头注增服务端定名一句(文档化,wire 零改动;G1 代拍)**:服务端 = tansr **Agent 会话引擎**(`@tansr/serve`),「网关」只指 tansr-api,引擎恒不承担网关职责、准入帽/`rateLimit` 缝/上游治理为自保护面;同笔 `packages/server/README.md` 首段 + `package.json` description 定名并写明「本包不是网关」。**/v1 面同批注记(SC-18,G7 两级代拍;不属本档 wire)**:/v1 `SessionStore` 终结记录逐出——库缺省不逐出、CLI `tansr serve` 缺省 30 min / 10 000(文档落 doc/11 §6.3 与 README 主要出口);/v2 §六 `retentionMs` 30 min 语义不变。Kotlin/Swift 持份零随动 | tansr-cli(本笔,SC-W3-S) |
| 2026-09-03 | **§五-5.5 第 2 条增「分片亲和 sessionId」子条(加法;G2-a 代拍;来源卡 SC-32b)**:副本配分片码(0–255)时全新会话 sessionId 前两位十六进制 = 分片码、其余 UUID v4 位不动;反向代理按前缀静态路由回创建副本;客户端恒视 sessionId 为不透明字符串(不解析、不假定形制)。服务端 `AgentSessionsOptions.sessionIdShard` 缺省缺席 = 纯 UUID 零漂移 | tansr-cli(本笔,主线 W3-M `566552f8`);Kotlin/Swift 零改动(已按不透明串消费) |
| 2026-09-04 | **§三-1 mcp.* 三型注记(加法;contract-v0.21 / RFC-SC-3 / SC-45;用户令 2026-09-04「RFC-SC-1/2/3 开工」)**:KernelEvent 族新增 `mcp.reconnecting` / `mcp.reconnected` / `mcp.reconnect_exhausted`(MCP 连接池自动重连生命周期;字段以 kernel 回调形为准,恒不携失败原因/堆栈/命令行);SSE 直透规则零改动(按 type 零过滤、`?exclude=mcp` 可滤)。**端侧按未知 type 忽略恒安全**——Kotlin/Swift 解码器 Unknown 兜底已实证,零代码义务;可选增三型 case 呈现「工具服务器恢复中」(RFC-SC-3 R6,双仓对表卡随迁)。现状:serve 形态 MCP 生命周期事件走服务器日志、尚不进会话流(是否按会话广播候拍,见 SC-44/45 终报) | tansr-cli(本笔,K6 泳道 `bbba506f` + docs 提交);Kotlin/Swift 零改动 |
| 2026-09-04 | **§五-5.4 载荷增可选位 `reason?: TerminalReason`(服务端出站面加法;用户令 2026-09-04「RFC-SC-1/2/3 开工」;契约 contract-v0.21 `76b575e7`;来源卡 SC-43,RFC `doc/rfc/RFC-SC-1-TerminalReason-internal_error-client_gone.md` R4)**:终局帧 `TerminalReason` 原值随载荷出站,接收方单帧区分 `client_gone` / `internal_error` / 用户中断 / 资源上限;`status` 语义不变;缺席 = 此前载荷形。`contract.ts` `TurnEndNotifyPayload.reason?` 同笔;组装点 `registry.ts` `#emitTurnEndNotify`,`notify.ts` 原样透传。**同笔翻案登记**:2026-09-03 行「`turn.aborted.reason` 增 `client_gone` / `internal_error` 不加(G4-f)」自 RFC-SC-1 需求定稿 + 用户开工令起失效——两值已进 protocol `TerminalReasonSchema`(contract-v0.21),签发方:`internal_error` = kernel `run-query.ts` `events()` 隔离层(唯一);`client_gone` = 注册仓孤儿策略 / `onSubscribersGone` 钩子裁 interrupt 经 `AgentSessionHandle.interrupt({ source: 'client_gone' })` 标记;§三 SSE 帧内 `turn.aborted.reason` 自此可见两值,端侧 Kotlin/Swift `TerminalReason` 为 String 容忍**零改动**(RFC-SC-1 R7,对表卷登记两值字面) | tansr-cli(本笔,SC-43) |
| 2026-09-04 | **`client_gone` 签发口径澄清:治理 close 按来源二分(语义澄清,wire 字段零改动;主线代拍 SC-43 终报 §八-1,用户授权、六原则;来源卡 SC-43b)**:凡**因订阅者全部离场**而 close(§六 孤儿策略 `idleAfterGoneMs` 到点 close、宿主 `onSubscribersGone` 钩子裁 `'close'`)命中运行轮时,与 interrupt 同律经 `AgentSessionHandle.close({ source: 'client_gone' })` 标记,该轮 `turn.aborted.reason` 与 §五-5.4 webhook `reason` 同为 `client_gone`,随后紧随 `session.ended`——RFC-SC-1 R3「订阅者全部离场后被服务端策略中止」覆盖 interrupt 与 close 两种止损形;**寿命帽 `maxSessionLifetimeMs` / 轮数帽 `maxTurnsPerSession` / 24 h 闲置 / §五-5.5 drain 超时中止 / 宿主 ⑤ 显式 close / 用户 ④ interrupt 恒不标**(不是「客户端离场」,终局维持 `aborted_streaming` / `aborted_tools`;日后要专属值另走 RFC)。端侧零改动(取值仍在 contract-v0.21 词表内) | tansr-cli(本笔,SC-43b) |
| 2026-09-04 | **§三-3 `server.permission.request` 载荷 `attribution.reason?` 加法 + 行为增笔(doc/113 权限裁决姿态 G-6;用户拍板 2026-09-02 姿态模型 / 2026-09-04 半径 F9;来源 doc/115 G-6)**:`source = "classifier"` 时携裁决人判危险理由,端上呈现「裁决人判为危险:…」供终端用户终审;其余来源缺席。行为:平台内置形按 bundle 生效裁决人档(控制台「裁决人」任命)装配分类器,`PermissionEngine({ mode:'auto' })`——判危险缺省人在环(本帧携 reason;宿主 `platform.adjudication.posture='headless'` 改 deny-and-continue),判安全且 `adjudicator.tier ≥ medium`(kernel F9 面,tier-gates-v2)直接执行**不下发本帧**;bundle 无裁决人段行为现状。裁决人预算耗尽/熔断/绑定模型不可解析经 `server.platform.warning` 三码告知。**双仓对表**:Kotlin/Swift 权限卡可选渲染 `reason`(未知字段忽略恒安全;对表卡归各端批次) | tansr-cli(本笔,`lane/adjudication/G6-serve`);Kotlin/Swift 零改动义务、可选渲染 |
| 2026-09-04 | **会话持久化与上下文压缩(doc/116 / doc/117 Wave 1 V 泳道;代拍 D-1 A / D-2 A / D-3 A / D-4 A / D-5 / D-6 A / D-9 A / D-11 A;加法)**:§二 端点 11 → 16——⑫ `POST …/compact` / ⑬ `GET …/checkpoints` / ⑭ `POST …/checkpoints` / ⑮ `POST …/checkpoints/:cid/restore` / ⑯ `DELETE …/checkpoints/:cid`(体/响应/码逐字;编号自 ⑫ 起,doc/116 「⑨–⑫」系笔误);§二-① 增 `cwd` 键 + ①-f 语义(无策略 fail-closed / allowedRoots·resolve 策略 / resume 对账两策);§二-⑧ 增 `?offset&limit` 分页(无参字节等价;尾窗缺省;配对边界对齐;翻页纪律);§三-1 增 `session.checkpointed` / `session.restored` 两型注记 + `session.compacted.checkpointId?`(contract-v0.22)与 Kotlin/Swift 解码核实结论(Unknown 兜底,零代码义务);§三-3 `server.platform.warning` code 增 `cwd_mismatch_on_resume` / `cwd_mismatch_on_restore`;§七 增七码 `turn_running` 409 / `checkpoint_not_found` 404 / `session_mismatch` 409 / `checkpoints_not_wired` 409 / `cwd_not_allowed` 400 / `cwd_invalid` 400 / `cwd_unavailable` 409;§九 增段与清单 / 轮换清段红线 / `getHistoryPage` / `sessionDirOf` / `meta.cwd` 一条(sdk 份同笔)。contract.ts 同笔:`CompactBodySchema` / `CheckpointBodySchema` / `RestoreBodySchema` / `V2CheckpointMeta` / `V2CompactResult` / `V2RestoreResult` / `V2CheckpointList` / `CreateSessionBodySchema.cwd` / `V2_ERROR_CODE` 七键。**双仓对表**:Kotlin/Swift 持份候各端批次(五端点结构化 API + `getHistory(offset,limit)` + `CreateSessionRequest.cwd` + 七码 + 两事件 case);旧端零改动恒安全 | tansr-cli(V-1 `49d58b7f` / V-2 `dab02c86` / V-3 `2876a1f9` / V-4 本笔);Kotlin/Swift 零改动 |
| 2026-09-04 | **V-5 对齐卡(同构双份同笔纪律,主线裁定以 S 份为准;wire 加法零破坏)**:§二-⑧ 三处改笔——① `limit` 单独在场 = 自 0 起前 `limit` 条(撤「隐式尾窗」;尾窗取法 = 先 `?limit=0` 取 total 再 `?offset=total−N`),`offset` 缺省 0 / `limit` 缺省到末尾,与 SDK `normalizeHistoryPageOptions` 同律;② 配对对齐改为 sdk `alignHistoryPage` 同算法(起点向前、**终点向后**取干净边界,`limit` 为下限,页可比 `limit` 长;撤「终点收缩」),翻页以 `offset + messages.length` 推进;③ 休眠分支恒一次 `getHistoryPage`(返回形加 `offset` = 对齐后起点,对齐在 store 内)。§九 ②③ 同笔:整卷轮换改 `.next` 暂驻协议(`journal.next.jsonl` 就位 → 清段 → 替换;崩溃窗自愈而非 `store_corrupted`),`getHistoryPage` 返回携 `offset`。端侧:响应键集不变(`total/offset`),仅 `limit`-only 语义与页宽下限语义变化,V-2 未上产、零端侧影响 | tansr-cli(V-5 本笔) |
| 2026-09-04 | **I-1a 集成卡(主线;D-V11 裁定;wire 可选位加法)**:§二-⑧ 分页响应加可选键 `nextOffset`(下一页起点 = 原始下标空间的对齐终点;空页 = 回填 offset),翻页纪律改为「恒以 `nextOffset` 推进、终止看 `nextOffset ≥ total`」——出流投影剔除 meta-only reminder 后 `offset + messages.length` 不可靠。§九 `getHistoryPage` 返回形同笔加 `nextOffset`(server 份与 sdk 份 `HistoryPage`/`sliceHistoryPage`/两 store 结果构造点逐字同形)。端侧:旧端未知键忽略,零影响 | tansr-cli(I-1a 本笔) |
| 2026-09-05 | **cwd 中途切换(doc/116 §3.7 D-7 / doc/117 §十 W-2;二期,暂不发版;加法)**:§二 端点 16 → 17——⑲ `POST …/cwd { cwd }` → 200 `{ cwd, from }`(策略闸与 ① 同律:缺 `cwdPolicy` 恒 400 `cwd_not_allowed`;形态四查 400 `cwd_invalid`;运行中 409 `turn_running`;内层装配无重建面 409 `cwd_switch_unsupported`;同目录 200 零副作用);切换效果 = 执行器 + 权限门按新 cwd 重建(kernel `withCwd` 派生)、权限桥 digest 锚、快照 `meta.cwd`、store `meta.cwd` 改写 + `cwdHistory` 追加;不重跑 `loadConfig`。①-f「中途切换恒不支持」自本笔失效。§三-1 增 `session.cwd_changed { from, to }` 一型注记(contract-v0.24 预登记,头注条目由主线集成一笔写);§七 增一码 `cwd_switch_unsupported` 409 + `turn_running` / `cwd_not_allowed` / `cwd_invalid` 三行端点列增 ⑲;§九 增 ⑥ `recordCwdChange` / `meta.cwdHistory`。contract.ts 同笔:`SetCwdBodySchema` / `V2SetCwdResult` / `V2_ERROR_CODE.cwdSwitchUnsupported`;types.ts `AgentSessionHandle.setCwd?` + `AgentSetCwdResult`;serve-session.ts `ServeSessionDriver.setCwd` / `bindCwdRebinder`。**双仓对表**:Kotlin/Swift 持份候各端批次(⑲ 结构化 API + 一事件 case + 一码 + 权限回执 digest 的 cwd 锚随 `session.cwd_changed.to` 更新);旧端零改动恒安全(未知 type / 未知 code 兜底) | tansr-cli(W-2a `bb81f734` / W-2b `e389cc2d` / W-2c 本笔) |
| 2026-09-04 | **二期 F-3(doc/117 §十;代拍 D-2 B 落地;wire 加法零破坏)**:§二 ①-g 建会体 `fork?: { sessionId, checkpointId }`(与 resume 互斥;源归属 403 / 源缺失 404 `fork_source_not_found`);新增 ⑰ `GET …/checkpoints/:cid/export`(octet-stream 自包含件 `tansr-checkpoint/1`,附件 base64 内联)与 ⑱ `POST …/checkpoints/import[?label=]`(≤ 32 MiB,校验 format/schema/sha256,`checkpoint_import_invalid` 400);§三-1 KernelEvent 加一型 `session.forked{ sourceSessionId, checkpointId, sessionId }`(contract-v0.24 预登记,与 W 泳道 `session.cwd_changed` 同 bump);§七 加两码。sdk 份同笔 `fork/exportCheckpoint/importCheckpoint/createSession({fork})`。端侧:HTTP 方法加法 + 未知 type 忽略,零改动 | tansr-cli(F-3 本笔) |
| 2026-09-05 | **§九 增「分层存储:热层 + 可挂载冷层」一条(加法;doc/119 IO-21 / doc/120;用户令 2026-09-04「基本确定方案……」开工;S7 泳道)**:`createServeAgentSessionStore` 加可选项 `cold` / `index` / `policy` / `metrics` / `logger` / `onStoreEvent` / `onStoreError`(**缺席 = 今日字节等价**;doc/91 冻结接口不改、不另起函数名);接 kernel 段层两钩子 `onSegmentSealed` / `segmentSource`;A10 热层全失经 `tier.restore` 仅凭冷层重建;可选成员 `flush?` / `readiness?`(纯加法);`AgentStoreReader.readiness?` + startServer 自动接 `/readyz`(理由 `store_hot_full`);`LOG_EVENT` 加六个 `store.*` 事件名;`@tansr/serve` 再出口 kernel 调用契约面(`SegmentBlobStore` / `SessionHistoryStore` / `SessionIndexStore` / `StoreError` / `SegmentedStorePolicy` / `createMemoryBlobStore` …;`createFsBlobStore` / `runStorageConformance` 候 K7 IO-14 落地补出口)。**wire 零改动、客户端零感知**(热层仍是事实源;冷层承接封存段 / 清单;端侧恒只持投影缓存与游标,doc/112 §3.1)。sdk 份 `createFileSessionStore` IO-22 同笔同形(`endUserKey = 'local'`)。Kotlin/Swift 零改动 | tansr-cli(S7:IO-21 `c5658af1` / 本笔 docs) |
| 2026-09-05 | **音频直连两端点(doc/123 §3.6 / D-A9 ②;B 泳道 `lane/audio/W1-B`;暂不发版;加法,协议包零触碰)**:§二 端点 19 → 21——⑳ `POST …/audio/transcriptions`(body 同 doc/123 §3.1)→ 200 `TranscriptData`;㉑ `POST …/audio/speech`(body 同 §3.2)→ 200 `SpeechData`;闸序 在册 → 归属 → 面在场(409 `platform_unavailable`)→ 残影 → 能力位(403 `capability_disabled`)→ 体(32 MiB 专项帽)→ 经工厂刷新 fetch + app token 代打平台同步端点;上游错误码/状态码原样透传(message i18n 包装);网络错 503 `upstream_unavailable`;不触历史、不签发事件。§七 增两码 + 透传注记行。**行为增笔(同笔,doc/123 §3.6 装配)**:平台内置形 `assembleBuiltinTools` 按 `platform.speechToText / textToSpeech` 装 `SpeechToText` / `TextToSpeech`(装配序 … → ImageGen → VideoGen → SpeechToText → TextToSpeech → clientTools);`BUILTIN_CAPABILITY_PROFILES['mobile-default']` 不变(直连端点不经工具白名单)。contract.ts 同笔:`AudioTranscriptionsBodySchema` / `AudioSpeechBodySchema` / `AUDIO_MAX_BODY_BYTES` / `AUDIO_INPUT_PATTERN` / `AUDIO_SPEECH_FORMATS` / `AUDIO_ROUTE_SEGMENTS` / `V2_ERROR_CODE.capabilityDisabled` / `.platformUnavailable`;types.ts `AgentSessionHandle.platformAudio?` + `AgentPlatformAudioFace` / `AgentPlatformAudioResult`;i18n 四键 `server.v2.error.platform_{capability_disabled,unavailable,audio_rejected,audio_unexpected}`。**双仓对表**:Kotlin/Swift 持份归 C-1 / D-1(`AudioClient` 两方法 + 两码 + 透传码兜底);旧端零改动恒安全(新 HTTP 方法加法、无新帧) | tansr-cli(B-3 本笔) |
| 2026-09-05 | **§四-4.1 clientTools 申报加可选键 `effects`(应用表面裁决协议 v1,doc/optimize/37 §5;用户授权主线代拍 2026-09-05;来源 doc/121 AP-3)**:∈ `irreversible \| financial \| external \| affects-others`,≤4 不重复;词表外 / 重复 / 超四值 400 `validation_failed`;透传 kernel `Tool.effects`,只进裁决人应用协议 `<tool>` 段与遥测,**不参与权限判定与并发裁决**(信号非闸门)。**行为增笔(同笔)**:平台内置形裁决人自此装配**应用表面协议**(单阶段;指纹独立于 CLI 编码协议;载荷前置 `<app>`(bundle `app.name`/`app.purpose`)与 `<tool>`(描述/只读位/效应)段,`<engine-facts>` 引擎事实段随协议 v4 同入),`server.platform.warning` code 增 `adjudicator_evidence_drifted`(bundle `adjudicator.evidenceFingerprint` 与 sdk 编译内应用协议指纹不等;tier 仍按任命生效,RFC-U3)。端侧:Kotlin/Swift defineTool 增可选 `effects` 参数归对表卡,零改动义务 | tansr-cli(本笔) |
| 2026-09-06 | **§七 套餐配额映射注记(加法,不占 contract 号;用户分级与定价 doc/124 三原则 3 / doc/125 结论 12 · §7.2;D 泳道 PL-22)**:①既有 `rate_limited`(429)行注记——平台内置形铸令牌撞 api 429 `plan_concurrency_exceeded` / `plan_end_users_exceeded`(开发者套餐配额)亦映射本码,`Retry-After` 取 api 值,信封加法 `detail: { scope: 'plan' }`,**不再译 503 `upstream_unavailable`**;②新增透传行 `plan_required` / `plan_tier_insufficient`(403,原码透传 + `detail.scope`,不携 `Retry-After`);③新增「信封 `detail` 注记」——错误信封 `detail?` 可选结构化位在场才落键,既有信封字节零变。contract.ts `V2_ERROR_CODE.rateLimited` 旁注同笔;`http-util.ts sendError` 加可选 `detail` 参;工厂码 `AgentCreateErrorCode` +`plan_limited` / `plan_denied`(内部路由码,非 wire)。**双仓对表**:Kotlin/Swift 零改动恒安全(既有 429 + Retry-After 退避、未知 code / 未知键兜底);可选按 `detail.scope` 差异化文案候各端批次 | tansr-cli(`lane/plan/D-cli` PL-22 `eb174d98`) |
| 2026-09-07 | **进程间就绪帧与探针面注记(十王修案 FX-C-42;分册 C §三 S10 目标形态 1 / 候拍 ㉑「三形」/ O-5;运维面,不入 wire 契约,端侧零改动)**:① serve 在 listening 后**恒**额外写一行 `TANSR_READY {"v":1,"url":"http://127.0.0.1:1234","pid":4242,"readyz":"/readyz"}` 到 stdout——**它不是日志**(不经 `ServeLogger`、不受 `TANSR_SERVE_LOG_FORMAT` 影响、无 `ts` / `level`),是父子进程协议行;JSON 日志采集器按行严格解析时以 `TANSR_SERVE_READY_STREAM=stderr` 改道、`none` 不发(嵌入式宿主 `startServer({ readyFrame })`);帧形态源 `packages/server/src/ready-frame.ts`(`formatReadyFrame` / `parseReadyFrame` 同文件,字段加法不升 `v`);② tansrd 池就绪判定改 `readyFrom(line)` 三形逐行:①帧 / ②`server.listening` JSON 行 / ③文案行 `Tansr serve listening on …`(旧 serve 兼容一版),不再以「stdout 首个 URL」正则(S2-04 残片 → 熔断),FX-C-47 止血正则同笔撤除;③ 观测端点暴露:`TANSR_SERVE_EXPOSE_METRICS` 单独控 `/metrics`(优先于总开关 `TANSR_SERVE_EXPOSE_OBSERVABILITY`),CLI `tansr serve` 入口把 `/healthz` `/readyz` 钉为恒开(探针面无机密);tansrd 健康探针改 `GET /readyz`(旧 serve 404 / 401 回落 `/v1/status`);④ tansrd 建会撞 ⑤ 表 429 / 503 时读 `Retry-After`(秒或 HTTP-date):orchestrator 归 `AgentTurnFailed`(可重试;此前 429 误归 `SchemaValidation` 永久失败)并经 `lease.release({ retryAfterMs })` 让该实例冷却到点前不再派发 | tansr-cli(本笔) |
| 2026-09-07 | **§五-5.4 webhook 双签一版(十王修案 FX-C-43;S2-05 / 候拍 ⑱;服务端出站面加法,接收方零改动义务)**:serve 轮末 webhook 此前只发 v1(`sha256=<hmac(体)>`)而 tansrd 入站缺省拒 v1(contract-v0.13)= 同一产品两进程互不接受。自本笔起 `x-tansr-signature` 携 `v2=…, v1=…` 双段(`onTurnEndNotify.signatureVersions` 缺省 `['v2','v1']`;v2 canonical / 头名**逐字取 protocol `WEBHOOK_SIGNATURE_V2` 冻结常量**——分册 C 写的 `hmac(ts + "." + body)` / `x-tansr-signature-ts` 为草案形,采已冻结的 v0.13 形以免再造第二个 v2),tansrd / orchestrator 入站 `parseSignatureHeader` 校任一接受版本(v2 在场只校 v2,不降级);tansrd 接受集 = `['v2']` ∪ `allowLegacySignature ? ['v1'] : ∅`(等价分册所述 `acceptVersions` 缺省 `['v2']`,不另立键),300 s 窗 = 既有 `toleranceMs`。弃用时间表见 §5.4 | tansr-cli(本笔) |
| 2026-09-08 | **接线表:CLI `tansr serve --v2` 与 cli 内层工厂 ⑲ 重建缝(十王修案 FX-C-46;C-08 = O-9 / 06 D-10 / doc/117 C-6 续卡;宿主接线面,wire 契约零改)**:① `tansr serve --v2`(或 `TANSR_SERVE_V2=1`)挂本档 21 端点——**缺省关**(`tansr serve` 用户行为零变);doc/102「多租 /v2 鉴权归开发者 authenticate 缝,CLI 无处注入」的拍板仍成立,故 CLI 形是**单运维方形**而非多租平台形:鉴权 = 与 /v1 同一 Bearer token(同一信任锚),endUser 由调用方以 `x-tansr-end-user` 头自报(`[A-Za-z0-9._:-]{1,128}`,缺省 `local`;越形 401)只作 store 分域 / 治理计数 / 归属校验键,**不是身份断言**;store 落 `<sessionsDir>/agent-v2/`;`cwdPolicy = { allowedRoots: [启动 cwd] }`;SC-32 治理五键 / 分片码 / factory 组在 `--v2` 开时有消费点(不再提示「仅对 /v2 宿主生效」);② cli `createServeSessionFactory({ cwdRebinder? })`:缺省绑定执行器重建缝(权限门按新 cwd 重建 + kernel `withCwd` 派生,与平台内置形同律),⑲ 在注入形亦 200;`false` 保留 409 `cwd_switch_unsupported`(⑲ 段同笔改口)。接线表:`tansr serve`(/v1)· `tansr serve --v2`(/v1 + /v2 单运维方形)· `examples/serve-demo`(/v2 开发者自定 token 形)· 平台内置形(`createPlatformServeSessionFactory`) | tansr-cli(本笔) |
| 2026-09-08 | **serve 出站通知 CLI 两键(十王修案 FX-C-09/续;C-δ W5 终报 §五 1 / §七 1 敞口:`tansr serve --v2` 进程此前无 `onTurnEndNotify` 接线,serve 进程无法自发 webhook;宿主接线面 + 运维旋钮表加法,wire 契约零改)**:`SERVE_RUNTIME_ENV` 28 → 30 键,新组 `notify`——`TANSR_SERVE_NOTIFY_URL`(新 kind `string`,形制 = 绝对 http(s) URL;非法 → 既有 warnings 径忽略并告警,告警值剥 userinfo / 查询串)→ `v2.onTurnEndNotify.url`;`TANSR_SERVE_NOTIFY_SECRET`(新 kind `secret`:取值恒不入 warnings / 日志 / 生成物,只报「在场」;`requires` URL——单独在场 → 告警「无 url 被忽略」)→ `v2.onTurnEndNotify.secret`。`resolveServeRuntimeOptions` 产出 `v2.onTurnEndNotify?: { url, secret? }`,CLI `run-serve-entry` `v2Face` 透传进 `startServer({ v2 })` 既有缝(`TurnEndNotifier` 双签 `v2,v1` / `governor.fetch` / 终败结构化日志零改即得);**无 URL = 不出站**(行为字节不变);未开 `--v2` 时两键列入「仅对 /v2 宿主生效」告警;启动期 stdout 一行 `cli.serve.v2.notify_enabled_{signed,unsigned}` 只打 URL origin(SC-16 同律)。`.env.example` / README 旋钮表由表再生(30 键;secret 行注 `# sensitive`)。真进程实证归 `packages/tansrd/test/e2e/serve-tansrd.e2e.test.ts` 卷 ①(serve 真进程以两 env 自发 → tansrd 真进程 202) | tansr-cli(本笔) |
| 2026-09-11 | **RF-05a(核心组件设计审查 RV-4-01 / 05 / 06 / 08;修复计划 §2.5)**:①§三-5 增笔「有界写」——§六 `maxSubscriberBufferBytes` 是每次 socket 写的字节上界(attach 重放段惰性分片 / 同 tick 批量越界即刻分片 / 收流前分片;wire 零改动,帧文本与顺序逐字节不变);②§三-5 gap 帧形态(事件名 / 键集 / `reason` 词表)单源 `contract.ts` `REPLAY_GAP_EVENT` / `REPLAY_GAP_REASON` / `ReplayGapNoticeSchema`,进 `contract:check` MANIFEST 事实源(sdk MANIFEST revision +1;端侧 vendored 拷贝归镜像卡);③§二-⑳㉑ / §七 改笔:平台 401 / 403 / 5xx 与网络错不再原样透传,归一 503 `upstream_unavailable` 固定词条 + `Retry-After`,原文只进新日志事件 `upstream.rejected`(业务 4xx 透传形不变);④/v1 drain `retry:` 帧经 writer 记账(`http-server.ts` 直接 `res.write` 归零)。锁测 `packages/server/test/{sse-bounded-write,v2-audio-envelope,contract-sse-control-frames}.test.ts` | tansr-cli(本笔,RF-05a) |
| 2026-09-11 | **RF-05b(核心组件设计审查 RV-4-02;修复计划 §2.5 / §3 代拍 #2 = B)**:§三-2 增「seq 会话内单调 + 起点规则」——resume 新纪元不再从 0 起,起点 = 持久化水位 + 1(store meta `lastSeq` 逐检查点只增落盘、正常关闭封印 `seqSealed`;R1 封印 → 恰 `lastSeq + 1` 跨纪元连续;R2 未封印 → `max(lastSeq + 1, now − createdAt ms)` 守卫地板,崩溃窄窗边界登记);旧游标落在起点之前 → gap `evicted`;空日志水位 = 起点 − 1。§二-① resume 行 / §二-⑧ / §三-5 纪元 gap / §五-5.3 L3 改笔(响应 `lastSeq` 由恒 0 改为新纪元水位)。协议帧集 / `Last-Event-ID` 语法 / api-client 零改动;端侧零义务(注释归镜像卡)。锁测 `packages/server/test/v2-seq-epoch.test.ts` | tansr-cli(本笔,RF-05b) |
| 2026-09-13 | **RF-S1(核心组件设计审查 DEC-RF-30 / 48;验收单 AC-S1)**:§二-⑥ / ⑧ 与 §三-2 改笔——非活跃会话 GET ⑥ / ⑦ / ⑧ 的 `lastSeq` 自此 = store meta 持久化水位(RF-05b 落盘的封印值 / 最近检查点值;此前恒 0;旧卷缺字段 → 0;无 meta 记录仍 404;活跃径不变);⑧ 非活跃径为取水位在历史在场后另读一次 meta(历史缺席 404 先于之;meta 读抛错与既有读拒绝同码 500 `store_corrupted`)。wire 形 / 键集 / 键序 / 公共类型零变化(`StoredSessionMeta` 形不变,工厂读面以内部加法位回填;自实现 `AgentStoreReader` 不回填 = 仍 0);导出集零漂移;api-client 零改动。**端侧(RF-M1 对表)**:Android / iOS 若以 `lastSeq === 0` 判「无事件」须改为按值作 `Last-Event-ID` 续传起点。同笔宿主接线:`tansr serve --v2` 建店接 `onStoreWarning`,店核警告以 warn 级复用既有 `store.error` 日志事件出线(`LOG_EVENT` 不加键)。锁测 `packages/server/test/v2-stored-last-seq.test.ts` / `packages/cli/test/serve/run-serve-entry.test.ts` | tansr-cli(本笔,RF-S1) |

---

## 附录 A:推送唤起自接指引(FCM/厂商通道,开发者半场;A4-2/D5)

**边界申明(D5 终拍)**:tansr SDK 与会话服务**恒不内置任何推送 SDK**。国内设备 GMS 缺位使「内置 FCM」必然长出厂商通道矩阵(华为/小米/荣耀/OPPO/vivo…),维护面失控;服务端只承诺 §五-5.4 的 webhook 缝,推送通道选型、token 管理、通道商合规全在开发者域。以下为参考全链。

### A.1 全链时序

```
serve /v2                你的推送后端                 FCM/厂商通道        Android App
   │ 轮终态&无订阅者          │                           │                │(后台/已被杀)
   ├─ POST webhook ─────────▶│ ①验签+去重                 │                │
   │  {sessionId,endUserId,  │ ②endUserId→push token     │                │
   │   turnId,status,lastSeq}│ ③(可选)取预览材料          │                │
   │                         ├─ data 消息{sessionId} ───▶│─ 唤起/通知 ───▶│
   │                         │                           │                │ ④用户点开回前台
   │◀─ GET ⑥ meta /resume ①─┼───────────────────────────┼────────────────┤ ⑤SDK 三级链自动:
   │◀─ GET ⑧ history ────────┼───────────────────────────┼────────────────┤   L1 重连/L2 重建/
   │                         │                           │                │   L3 resume(冷启)
```

### A.2 服务端半场(你的推送后端)

1. **配置缝**(serve 装配处,`StartServerOptions.v2`):

```ts
v2: {
  authenticate, createSession, store,
  onTurnEndNotify: {
    url: 'https://push.example.com/hooks/tansr-turn-end',
    secret: process.env.TANSR_HOOK_SECRET,   // 强烈建议:HMAC 验签材料
  },
}
```

2. **收 webhook**:先验签——`x-tansr-signature` 头 = `sha256=<hex>`,hex = HMAC-SHA256(secret, 原始体全文),恒对原始字节验(先验签后 JSON.parse);再按 `sessionId+turnId`(或体全文)幂等去重(重试会重发同一体);尽快回 2xx(重活动异步做,serve 侧 5 s 超时);`ts` 过旧可弃(防重放)。
3. **定位设备**:`endUserId` → 你的用户体系 → 该用户的 push token(FCM registration token / 厂商 regId)。token 登记与刷新是你的 App ↔ 你的后端的既有半场,tansr 恒不经手。
4. **(可选)预览材料**:通知文案若需要「回答已就绪」以上的内容预览,可由你的后端(serve 宿主,天然可查)按 §二-⑧ history 取末条摘要——**注意**:预览内容一旦进推送通道即经第三方(Google/厂商)转手,脱敏取舍是你的合规决策;保守做法 = 通知恒只写「任务已完成,点击查看」,内容恒不出你的域。
5. **发通道消息**:FCM 恒发 **data 消息**(不带 `notification` 位——data-only 才稳定走 `onMessageReceived`,由 App 自建通知,点击意图可控);载荷建议只带 `{ sessionId }`(status/turnId 可选)。无 GMS 设备走厂商通道或聚合服务商,同一最小载荷纪律。

### A.3 端侧半场(Android App)

1. `FirebaseMessagingService.onMessageReceived`(或厂商回调)取 `sessionId` → 发本地通知(通知渠道自建,minSdk 26 NotificationChannel);
2. 点击 PendingIntent 携 `sessionId` 回 Activity;
3. 恢复恒走 SDK 既有三级链,零新 API:进程还在 → repeatOnLifecycle 回前台自动 L1 重连(Last-Event-ID),gap 自动 L2(history 重建);进程已死 → 冷启 `client.openSession(scope, CreateSessionRequest(resumeSessionId = sessionId))` 即 L3;
4. webhook 载荷的 `lastSeq` 若随通道透传到端,可与本地水位比对提前判断 L1/L2 径(纯优化,不透传也恒正确)。

### A.4 纪律清单

- 推送载荷恒最小面(sessionId 为锚),消息内容恒不进推送通道(A.2-4 的预览取舍除外,且那是你的域内决策);
- webhook 端点恒 HTTPS + 验签;凭据恒不出现在 webhook 体/头(serve 侧已恒不携带,你的后端也恒不回填);
- 通知点击后的会话访问仍走你的登录态鉴权(authenticate 缝)——推送只是唤起,恒不是鉴权因子;
- serve 终败重试用尽即放弃(结构化日志可观测):推送唤起是尽力而为的体验增强,事实源恒是 store/history——App 下次打开 resume 全链照常,恒不因漏发丢数据。

---

## 附录 B:(撤笔)内置 token-server 端点

**本附录已于同日撤笔**(用户架构修正拍板 2026-08-31;§十增笔区第四行为撤笔登记)。原内容 = POST /v2/auth/tokens 签发端点 + `x-tansr-serve-token` 消费头 + `issueToken` 缝(serve-v2-auth 泳道增笔,历史全文见 git)。

撤笔理由与替代形:serve 定盘为**裸 /v2 协议引擎**,以 npm 包嵌进开发者自己的 node 服务;会话鉴权/token 形制**完全开发者自定**(最大放权),serve 只暴露 `authenticate` 注入缝(§〇-2)——内置 token-server 与「token 形制恒不由 serve 规定」相悖,故整套退出契约。Kotlin 侧原候迁的 TokenIssueBody/TokenIssueResult 持份义务随之取消;第三阶段 Android `authProvider` 对接的是**开发者自设计**的换发面(形制由开发者定,SDK 只逐请求回调取头)。serve↔api 半边(appkey → per-endUser app_user 令牌,D7)不在本撤笔范围,恒保留(doc/102)。

---

> 落盘:Android P0 serve /v2 泳道(Fable 5 Max)· 2026-08-31。配套:doc/94(设计)/ doc/96(施工导读)/ doc/97(排程,勾格归主线);schema 持份 tansr-cli `packages/server/src/v2/contract.ts` 同笔。§五-5.4/附录 A:A4-2 泳道增笔 · 2026-08-31。附录 B:serve-v2-auth 泳道增笔 · 2026-08-31(doc/102 配套)。
