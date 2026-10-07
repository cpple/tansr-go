# RFC-SERVE-NET-1：终端增量工具与记忆协议

日期：2026-09-26。revision：v0.5。**当前同源合同为 candidate-7，代码已接线，集中门与跨端交付结算中，未发布。** 任务：SRV-01；消费者：NET-01/NET-04。本文沿原[USDK合同](RFC-USDK-1-运行端与终端执行契约.md)、[SDK1兼容合同](RFC-SDK2-1-兼容SDK1的扩展协议.md)补齐，不替换它们，也不修改冻结的`packages/protocol`。

本文是两套方案的新协议入口；精确字段以同源 schema/生成链为准。§11、§12保留历次候选事实，当前消费按§13，不从历史候选推导新增字段或路由。ACK恢复是独立、可选的新合同，见§14。

## 1. 兼容不变量

1. 旧/v1、/v2、sdk2-ext-v1既有消息及严格additionalProperties规则不变；不向旧receipt或旧事件联合偷偷增加字段。
2. 既有SDK2远端`process.exec`合同的120秒/64KiB限额不因服务升级静默提高，不能把这两个限额施加到CLI/Electron原本地Shell；旧SDK1存储默认、格式、五方法和原Electron完整SDK/IPC不变。
3. 新能力独立发现/绑定；未协商无新帧、无额外持久承诺。401/403、5xx、代理404、网络故障不当作可降级证据。
4. 工具执行/权限/运行代际/用量仍用原核心；新流仅传递已受理操作的通知/输出，不建立第二个执行身份。
5. 同一会话/同一operation身份及digest不因网络连接重建而重算。未知副作用只对账，不重执行；最多一次受理不能宣称永久exactly-once。
6. 记忆正文与治理簿记分权；终端平台/路径自报不授予权限，普通文件写工具不能修改治理元数据。原`pending-anchors.md`是模型按原权限写入的晋升候选正文；不能因文件名带pending就误禁它。锁、来源登记、consolidation及pending-invites仍属治理面。
7. 删除、迁移、源变更先通过可信代际和原权威边界；档案与记忆相互引用，不等同为一份Store。
8. 任何新增公共行为改变先修本文revision与schema/金样，再通知两个实现；不让各语言自行猜测。

## 2. 传输选择与候选路由

复用HTTP＋SSE。会话观察保持原SSE；执行器定向事件与工具输出观察是单独鉴权的流。上行进度用有界POST批次，可用HTTP连接复用；不为WebSocket新增网关要求。

以下路径已接入显式启用的终端监听；SDK1未启用时默认不变。候选包消费和正式发行分别记录：

| 候选接口 | 作用 |
|---|---|
| GET /v3/terminal/capabilities?contract=terminal-services-v1 | 已认证只读发现；精确版本、安装能力、有限值及可用性 |
| POST /v3/terminal/bindings | 在原执行绑定/会话上声明required/optional能力；返回实际交集 |
| GET /v3/terminal/executors/:id/events | 复用原USDK定向SSE提议：已受理操作可领取/取消/状态变化；不向普通观察者广播参数 |
| POST /v3/terminal/executors/:id/output-batches | 对已有operation提交有界输出批次；返回接收/耐久水位及缺口 |
| GET /v3/terminal/sessions/:id/tool-output | 授权观察者输出流；独立游标，不替代会话事件或工具终态 |
| GET /v3/terminal/sessions/:id/tool-output-status | 指定operation输出水位/保留窗/截断/工件信息，用于重连查账 |
| GET /v3/terminal/sessions/:id/memory | 当前授权记忆源、版本、状态；不暴露任意宿主路径 |
| POST /v3/terminal/sessions/:id/memory/commands | remember/pin/forget用户意图；核心裁定及调度，客户端不写权威摘要 |
| GET /v3/terminal/sessions/:id/memory/commands/:operationId | 查询原requestId/operationId回执，不重新调用模型 |
| GET /v3/terminal/sessions/:id/configuration | 当前model/thinking及耐久revision |
| POST /v3/terminal/sessions/:id/configuration | 版本化model/thinking更新；安全点生效、不重建会话；system仍走可信宿主入口 |
| GET /v3/terminal/executors/:id/operations/:operationId | 原操作状态，沿当前执行身份授权与原不可变回执 |

候选采用独立`/v3/terminal`装配，避免当前`/v3/sdk2/sessions`会被识别为source-required/offload族。绑定必须显式引用原`sessionContract`（sdk1或sdk2-offload-v1）及原sessionId，由可信registry核对真实family，后续使用受信绑定定位；不能猜测两个族、泄漏另一族存在性或为了新流重建会话。普通execution、旧兼容会话和offload会话均可在真实宿主支持时启用该能力。命名空间为新协议边界，不建立新运行/会话状态机。

后台任务的查询/输出/取消优先复用已有任务公开面；缺失部分在SRV-01一次定版，禁止实现者临时猜路径。记忆operation的派发使用同一执行器定向流与核心调度，定义独立操作联合类型，不把未知新操作塞进旧operations响应。

能力候选：`execution-stream-v1`、`execution-background-v1`、`memory-lifecycle-v1`、`session-configuration-v1`。服务支持、宿主已安装、租户已授权、设备可执行分别核验。必需能力缺失整体拒绝；可选能力只有显式允许且已确认不支持时不用，不能运行后暗降级。

配置/注册/绑定沿原空闲安全点及版本条件。C#新增高阶方法必须等待真实能力发现，不将stub存在当作生产可用。

## 3. 共同信封与安全

沿现有Scope、ExecutionBinding与canonical/digest规范引用，不新建租户身份。新信封至少关联：contract、requestId、applicationScopeId/endUserId/sessionId、原operationId/requestDigest、原binding/connection/workspace/authorization代际、事件/输出序号与内容见证。字段确切形状以会签schema为准；源自身份票的字段不得由JSON自报覆盖。

短期token每次请求/重连复验；不把密钥放URL/SSE事件。认证身份变化使旧流失效。操作原始身份不可改写；重新连接可通过已验证授权取得对原操作输出/终局的重送资格，但不是新执行许可。

控制体、字符串、深度、数组、输出批次和所有buffer必须有限；schema严格拒绝未知安全字段。序号/代际复用原具名编码：现有`Sequence`为0～2^63−1十进制字符串，禁止经过IEEE754 Number；新输出seq/offset采用同一编码。原定义为JSON整数的有限长度/计数继续其边界，不能一刀切改编码。序号溢出不回绕或重新执行，按明确容量错误进入对账。二进制块以base64保真；标明通道及编码，客户端增量解码，不逐块错误解码UTF-8。不同平台编码由适配器显式声明/转换，不能假定所有Windows进程均UTF-8。

## 4. 工具流状态与确认

### 4.1 执行与输出两条状态

执行仍为原prepare/已派工/已接纳/结果未知/不可变终局体系。新输出只是：
`available -> receiving -> complete | truncated | gap | unavailable`。
进程完成≠输出全部持久，输出complete≠命令成功。退出码、取消及工具成功仍由原终局事实决定。

每个输出块绑定operation及通道，含单调seq、byteOffset、byteLength、payloadDigest、内容。块seq按operation统一编排，stdout/stderr标识分开；跨pipe只能承诺捕获观察序，不能伪称恢复OS内部绝对写时序。

终态关联最后输出seq/总字节/摘要与截断/缺口；缺块情况下UI显示不完整，禁止拼成成功的完整全文。最终结果仍按原ToolResult预算治理；流输出与最终回执不能双重追加模型历史。

复用ShellStreamEmitter会产生已有会话工具输出事件，新端不能同时追加两路文本。新绑定返回输出显示权威`session-events`或`tool-output`及原toolCallId到operationId的受信关联；选择新输出流的UI仅在该操作追加新流，旧会话流保留生命周期/审批/终局，旧消费者保持原事件。重连按operation/seq去重，最终回执不再次追加已经显示的全文；流失效显示gap，未经对账不得自行双路切换。关联信息只在独立扩展面给出，不修改旧事件schema。

### 4.2 重试与背压

同operation/seq同内容幂等，不同内容冲突；连续确认水位不能跨洞推进。事件Last-Event-ID只决定通知重放，不能当stdout持久水位。

POST返回区分`acceptedThrough`与`durableThrough`；未落声明介质不得填写耐久水位。端源可保留本地输出工件作为耐久来源，UI流可只是受限观察窗。重连先查询状态，重发原块/原回执，不能从输出缺失推导重跑进程。

候选资源预算沿原USDK§9有效交集：单原始输出块≤16KiB、批次≤64KiB、待发队列≤2MiB、默认观察保留≤8MiB，控制通知优先于数据。这些是新能力初始上界，需在SRV-01连同原核心预算会签；不能用于提高旧工具上限。写满后继续排空pipe并标截断或按政策取消，不阻塞子进程；无限原始输出不作耐久承诺。

关闭、撤权、租约失效时停止新派工，允许的迟到事实只用于既有对账，不能复活旧Promise。后台工具须有持久task身份、所属进程树见证及原语义的输出/取消/终结；不得用重启相同命令模拟恢复。

## 5. 记忆源与生命周期合同

### 5.1 来源及策略

历史/档案、memory、runtime三个策略分别从可信应用/宿主配置得出。memory来源候选：
- `server-managed`：Serve专用存储域，可接本地或开发者第三方后端；
- `client-managed`：已授权设备持久介质及可选备份；
- `developer-managed`：开发者共享存储，端侧可缓存。

旧默认不变；新增源需sourceId/sourceGeneration、删除代际、作用域/版本、能力与可用性承诺。sourceId不是路径也不证明介质存在。端源离线与空记忆是不同结果。组织记忆写权威保持组织，client-managed不能覆盖组织政策。

记忆域具有独立于sessionId/connectionId的稳定身份：可信app/user/project域，或明确的组织域；会话只是本次操作及费用归因。换会话/重连不换库、不重置revision/CAS，切用户不共库。正文、索引与派生物记录受信provenance，关联原材料/邀请/历史或档案记录及删除代际；不按文本相似猜来源。来源无法证实时保守隔离/失效并通知，不能自动升级为可信记忆。

### 5.2 三类操作

| 操作面 | 入口/权威 | 限制 |
|---|---|---|
| 用户意图 | remember/pin/forget/search及配置命令 | 核心执行原权限、秘密检测、晋升审批；非原始数据库任意CRUD |
| 正文读写 | 原Read/List/Write/Edit权限或新增受控记忆源适配 | 作用域内、原围栏/写后健康检查；普通模型工具不得写管理文件 |
| 宿主管理 | 提取邀请/固化/来源/索引修订/删除见证/锁及耐久事实 | 核心生成类型化命令；CAS、fence、幂等，不能接受模型构造管理操作 |

管理提交最少含scope/sourceGeneration、operationId、expectedRevision、lease/fence（需要排他时）、类型化变更及内容见证。提交结果为committed/revision、conflict、pending/unknown、denied、unavailable等可查询状态。请求接纳/执行完成/耐久保存/核心已消费分别记录，ACK丢失查询原键，不生成新operation逃避冲突。

当前只读MemorySource保持不变，新增管理/写侧接口为加法；旧五方法Store不强加CAS/lease。服务端存储的时钟/锁与端存储各按合同能力证明，不能用HTTP超时推定未提交。

新管理提交包以一次明确的memory revision为可见性边界：正文/索引/治理元数据先准备，再经原存储原语的manifest CAS或等价本地事务出版，并保存原operation回执。中断读者只见旧完整版本，未出版内容后续回收；不得两次普通Write/Edit就声称整体原子。模型普通正文编辑仍沿原逐次耐久/部分成功语义，通过版本变更和索引失效/修复接入；SDK1不因新增管理事务被改写语义。第三方无法兑现该提交包时不宣告该能力。

### 5.3 跨运行、删除与迁移

提取/固化并发按记忆域fencing；端侧执行器重连身份与记忆source代际分开，不因换连接直接换记忆库。删除推进权威见证，按明确请求范围及provenance闭包使关联旧摘要/索引/回传材料/备份不能重新生效；跨副本删除尚未确认时保持pending和拒绝召回边界。普通会话删除不隐式删除无关联手写M0或组织M2正文，组织删除仍须其权威授权；旧delete合同不扩为全域级联。

共享数据库事务与远端记忆/第三方介质不是一个分布式原子事务。复用原prepare→派发→原键耐久回执→核心完成事实，DB行锁/事务中禁止等待设备或第三方网络。端已提交而核心COMMIT失回时续原operation查询/对账，不能假装回滚端写入，也不能重跑提取模型来覆盖；公开状态保留pending/unknown直到闭环。

迁移先明确清单/大小/摘要/源目标代际，复制校验后在安全点切默认来源；原源保留，失败原键续办。不得通过自动双写互相覆盖引入两个写权威。不在线的唯一来源不能许诺即时无损切换。

无需让C#执行提取模型或压缩算法；端侧search仅提供候选，最终准入由核心。代码引用模式不得绕开组织策略、用途预算和记忆模型计费。

## 6. Electron公共控制与扩展

动态配置采用expectedConfigurationRevision＋幂等requestId；闲置时提交，运行中显式排队至安全边界或返回busy，规则会签固定。不得用新建session达到切模型的表面效果。平台系统提示词与SDK覆盖/追加及平台保留开关沿原优先级，不引入新的优先级。

工具/Skills/MCP/Hooks/子代理/模型定制注册引用必须预先由可信宿主授权，或由已经绑定的客户端回调面按类型化合同执行。禁止上传任意JS/.NET程序集让Serve执行。C#的高级选项应对应实际可用公开行为；不支持的选项明确报缺口，不静默忽略。

已有meta中的contextState、图片块、compact/checkpoint/cwd/audio复用原端点；新增的是缺失动态控制/安全扩展桥，不重复制造同义API。

## 7. 错误与客户端动作

| 情况 | 行为要求 |
|---|---|
| unsupported capability / capability unconfirmed | 区分可识别不支持与发现失败；必需能力不启动副作用 |
| unauthorized / forbidden / stale binding | 停止读取/派工，不猜新主体、不回落宿主 |
| conflict / stale revision / fenced | 刷新状态后由调用者明确续办；不得覆盖或静默last-write-wins |
| output gap / truncated / unavailable | 显示不完整；允许授权读取有界工件，不重跑工具 |
| receipt unknown / commit unknown | 查原键，对账前不重复副作用/提取写入 |
| source offline / deleted / scope revoked | 与空结果区分，遵循可用性/删除边界 |
| capacity / timeout / cancelled | 有界停止、保全已受理事实及未决数据；不假报关闭/回滚成功 |

具体HTTP码及机器错误标识由SRV-01固定成schema金样，不能让C#靠中文消息匹配错误。新类型暂存内部适配时不得被标为稳定API。

## 8. 会签及并行交接

Serve会话为唯一协议写者；C#会话校核序列化/异步/恢复可实现性，提出反例。以下都完成才将本候选改为冻结版本：

- [x] 路径、feature名、完整schema、限值、错误码及操作状态机定版；明确需要现有生成链扩展的文件。见candidate-7同源交接及原生成门。
- [ ] 旧protocol/types不变的差量证据；新旧Serve/TS/.NET/原生四向混装金样。旧类型差量及TS真实安装包四向4/4已验；C#当前Serve实链、三原生fixture/parity已验，不能冒充跨语言四向。共同四向行为向量、消费映射与会签仍须补证，不附加全语言UI或正式发行要求。
- [x] seq/字节编码/摘要/CAS/终局竞争/删除等正反金样与schema SHA256。当前schema SHA见§13；后续实现缺陷继续按原卡修正，不改旧金样证据。
- [x] 输出/通知/派工/回执和memory来源各自的身份与恢复接缝，双方能独立实现。见§13.2及ACK恢复独立合同。
- [ ] 所有Electron差异逐项归现有API或新增公开合同，未解决项仍留缺口。
- [x] 参考Serve可启动入口、包/源码版本、旧兼容安装物及联调说明。已发布包与本地候选包区分见开发计划§7最终回执。

会签前C#可实现已有稳定协议及内部可替换接口；不能将本草案包装成正式NuGet API。联合验证只在一个冻结候选进行；后续修订需保留旧金样及迁移说明。

## 9. 事实源

体验要求已在v0.2补充至§10；不改变旧wire，不要求各语言API同形。

- `packages/server/src/v2/agent-execution.ts`、`agent-execution-routes.ts`、`platform-session-factory.ts`。
- `packages/api-client/src/sdk2/session-client.ts`、`pc-adapter.ts`、`node-process.ts`、`node-executor.ts`。
- `packages/kernel/src/tools/shell/progress.ts`、`shell-tool.ts`、`packages/kernel/src/tools/files/memory-health.ts`。
- `packages/cli/src/assembly/memory-extraction.ts`、`memory-consolidation.ts`、原Serve session factory。
- 现有SDK2 schema、公开宿主/档案/存储合同及SDK1锁定矩阵；旧协议定版事实优先于历史草案中的阶段描述。

## 10. 跨形态高维语义合同（v0.2增补）

用户确认形态和接入可以不同。协议共同描述能力、权威、生命周期和失败语义；具体包/API/初始化步骤由各端决定。高阶SDK负责编排必要协议交互，复用原核心；不能把传输自动化变成新的核心治理逻辑。

| 应用可观察点 | 跨形态必须一致的含义 |
|---|---|
| ready | 已声明必需能力/身份/必要存储及执行绑定已准备；进程存活不能假报业务ready |
| accepted | 输入/操作按指定身份受理；不表示模型完成或已经持久 |
| terminal | 同一运行或操作已达原合同终态；唯一终局，不因重连重启工作 |
| durable | 指定介质/范围确已提交；不是UI收到或HTTP200，未知状态保留 |
| settled/closed | 按公开合同完成应释放/保全的资源；不偷换成断开HTTP，超时保持未决 |
| disconnect/cancel/delete | 分别是观察链断开、终止相应工作、指定范围数据删除，不能互相隐式替代 |

上表是语义映射，不是给旧事件新增这些名字。SRV-01须将现有SDK方法/状态与Serve现有或新增入口逐项映射，C#用自然异步API表达；不能新增名称却不对应真实受理/提交事实。

能力发现与实际授权继续分离；配置有效值、能力不可用理由、存储来源/可用性、当前模型/上下文、用量均提供受授权且有界的可观察面。error需区分调用者取消、未授权、上游失败、设备/材料离线、容量、冲突、结果未知及安全的重试条件；已有错误面够用就复用，不仅为统一名词新增API。

业务扩展分客户端委托与可信核心宿主引用。注册版本、签名摘要、调用归属、取消/超时、回调重入及错误映射由原执行边界确定；观察类与控制类Hook不能混同。不同接入步骤合法，但权限/裁决控制回调不能退化为事后通知。未接通语义留缺口，不自动接受不支持的选项。

诊断关联可引用原request/session/turn/operation，并可附独立trace；trace不参与授权，不泄露票据/正文，不改变SDK1帧。共享资源关闭遵守所有权；会话清理不得停止其他租户共用服务。版本更新/排水按已有租约、事实账本和兼容协议执行，禁止后台自动换核心或重建会话掩盖不兼容。

冻结交接增加三项材料：进程内SDK↔Serve↔C#完成/失败语义映射；各形态可运行的同业务场景与比较器；允许的形态差异、环境前提及未完能力缺口登记。已有支持能力必须给正例，发现不支持的负例不能单独证明体验已对等。

## 11. v0.3内部候选与可执行边界

2026-09-26首段schema为[terminal-services-v1.schema.json](terminal-services-v1.schema.json)，revision=`2026-09-26.candidate-2`，SHA256=`3cbb311f568bdc6d85b816fafbdb95254664219fdbd625dcadc0dfbdf4df84ae`。它只覆盖发现、原身份绑定、输出批次/封口/水位、定向通知和错误；**不是完整合同**。记忆管理提交、后台任务和配置持久CAS尚未定版，不因为feature名称已在枚举就视为已实现。新类型暂为内部文件，不从包入口公开；原SDK2 schema及原生成输出不变。完整路由、参考宿主、跨端会签及SRV-01整卡仍待完成。candidate-1原schema/金样保存在本批archive的contract/candidate-1，不能拿旧摘要匹配新版。

### 11.1 首段序列化与输出语义

- `Scope`、`ExecutionBinding`、`Id`、`LegacyId`、`Sequence`与`Digest`逐值复用原SDK2定义；服务器scope来自身份与可信registry，BindingRequest不接受客户端scope。会话族来自真实handle，不从路径或body猜测。
- 每笔operation的首个输出seq及byteOffset均为字符串`"0"`；seq跨stdout/stderr按捕获观察序单调递增。offset是此前所有通道原始字节总长，不是字符数，也不是通道内局部位置。
- 单块`payloadDigest`为解码后的raw bytes的SHA256小写hex；base64须规范编码并与byteLength一致。单批raw bytes总长不超过maxBatchBytes；JSON体另受maxControlBytes限制。完整UTF-8字符可跨块，端侧按通道持续解码，不能逐块调用完整文本解码。
- 同一operation的同一通道encoding从首块固定，不能中途在utf-8/binary间切换；需转换编码的宿主从开始就选定转换方式。不支持的原生字节以binary呈现，不猜Windows代码页。金样seq0 stdout=`F0 9F`、seq1 stderr=`78`、seq2 stdout=`98 80`，全局offset分别0/2/3；stdout显示😀、stderr显示x，seal仍按全局raw次序`F0 9F 78 98 80`求SHA256。
- `seal.payloadDigest`为按seq串接全部已捕获raw bytes的SHA256；空输出为lastSeq=null、totalBytes=`"0"`、SHA256(empty)。seal.truncated指捕获本身截断；观察窗逐出另由retainedFrom/gap表达。封口不表示工具退出成功。
- `acceptedThrough`是已连续接纳的末seq；`durableThrough`只在声明介质实际提交后推进。未接纳/未耐久时为null。内存窗口永远不能填写耐久水位。seq/offset运算不得经过Number，溢出拒绝。
- 已接纳不等于源端可永久丢弃唯一原文。accepted=7、durable=null后服务丢失内存且发送端也已释放原块，返回gap/unavailable，不重跑命令。若连水位也已丢失，unavailable的acceptedThrough/durableThrough/retainedFrom/nextByteOffset均为null，不能以offset=0伪装从未输出。非unavailable必须有精确offset；已知accepted非空时offset也必须已知。eventId与Last-Event-ID不替代任何输出耐久水位；定向事件保留窗耗尽必须发reconcile-required并查原操作，不只继续等下一事件。
- 输出重连须保留对应operation/channel解码器的未完成字节与已观察seq；未保留时重放足够前缀，否则明确显示呈现缺口并重置两个通道解码器。retainedFrom落在UTF-8标量中部时保留raw字节，显示缺口及替换字符，不擅自补全字符或把剩余窗宣称完整。此处不改变模型最终工具结果。Int64.MaxValue之后不可传递nextSeq哨兵；受限运行时以显式容量状态终止递增。
- operation/requestDigest及请求的executorId/connectionId引用原操作身份；HTTP重连不能改写它。重送仍须当前身份票、原operation归属及受信对账许可；这些字段不是执行许可。
- 同seq仅在全部块字段一致时幂等，异内容冲突；批次含洞、损坏、错误封口时整体拒绝。已逐出且无法核对的旧块不能假报相同；客户端查原水位，不重跑进程。seal封口后仅允许相同内容重送，不能续写。
- `ExecutorEvent`只通知查询原已受理操作，不携带新命令，也不替代旧operations/receipts。`OutputCorrelation`表达受信toolCallId↔operationId关联及唯一显示来源，不能由客户端任意伪造关联。

### 11.2 能力与错误

能力数组必须恰含四个不同feature，不能用重复项填满长度。installed不能在supported=false时为true。绑定required/optional各自去重且不得交叉；两组至少有一项。可选能力只有确认未支持/未安装才略过；已安装但撤权、离线、未确认分别拒绝，不用异常或代理404自动降级。执行流/后台执行必须引用已存在的受信执行binding。能力发现只读快照，不能代替每次请求授权。

结构校验与语义校验分开：共同金样的positive/negative验证JSON结构；semantic组另验durable≤accepted、retainedFrom≤accepted、seal.lastSeq/totalBytes与接纳事实一致、空seal与SHA256(empty)、输出状态与封口对应及未知水位。gap可保留完整seal；窗口逐出不抹掉已核实的捕获封口。服务端输出窗口验证批次全量再提交，客户端在结构通过后仍必须校验状态关系；不能因为Zod/JSON schema通过就当作可信完成。

HTTP候选映射：invalid_request/protocol_version_mismatch为400；unauthorized为401；forbidden为403；not_found为404；unsupported_capability/capability_unconfirmed/stale_generation/binding_conflict/revision_conflict/busy/output_gap/integrity_mismatch/commit_unknown为409；payload_too_large为413；capacity_exceeded为429；source_unavailable为503。retryAction按实际安全动作给出，绝不要求客户端重做结果未知的副作用。此映射随完整schema会签，不能靠本段候选匹配旧服务器中文错误。

### 11.3 宿主复用发现

原提取/固化算法、模型用途预算与权限继续复用kernel。原管理器本体及晋升运行器已迁入kernel的共享manager，CLI保留薄装配和原本地默认文件/锁；kernel构造必须显式给可信host。端口本身不证明远端manifest CAS、租约fence或耐久完成。远端savedCount必须来自验证过的正文耐久回执；不能沿用仅存在于本地ToolContext的onWrite计数，不能将HTTP200或模型声称保存当完成。

原事务运行事实尚未涵盖所有服务工具和MCP的耐久派发，archive wrapper也需透传transactionalExecution及其控制守卫。保留当前拒绝直至账本与接线完成；禁止删guard打通，禁止把网络等待包在owner.write事务中。动态model/thinking宿主接缝先保持安全空闲点及当前实例语义；未落持久修订及幂等事实前，不将其冒充configuration持久CAS接口。


## 12. v0.4 真实监听与配置候选（2026-09-26）

本轮沿六卡继续真实实现，不把内部金样视为接口交付。candidate-3 在 candidate-2 上增加配置读取/提交响应与三个机器错误；当前 schema SHA256 `086caaeeec0878c3805377f5833d2b7667b94ba104c56dba236dff99d84a521f`。完整协议仍待联合会签。

可信宿主在 `startServer` 显式设置 `terminal: { contract: "terminal-services-v1" }`，复用同一认证、准入、SSE writer 与实际 registry；不设置时旧行为不变。仅启用已实际接入的feature，生成类型不授予能力。SDK1与offload族可使用不同执行host，原会话family、endUser及原operation共同核验，不能从同名executor猜host。原SDK1没有执行host时沿其认证用户控制权；有执行host时必须再过原controller/executor裁决。

### 12.1 查询与输出身份

- 发现：`GET /v3/terminal/capabilities?contract=terminal-services-v1`。授权与设备可用性尚未绑定会话时返回unconfirmed是合法结果；客户端只可据supported/installed作静态判断，不能以unconfirmed代替绑定裁决。
- 定向通知：`GET /v3/terminal/executors/:id/events?contract=terminal-services-v1&sessionContract=sdk1|sdk2-offload-v1&connectionId=...`。通知续接仅用HTTP `Last-Event-ID`，不接受输出afterSeq冒充通知游标。
- 输出状态：`GET /v3/terminal/sessions/:id/tool-output-status?contract=terminal-services-v1&sessionContract=...&operationId=...&requestDigest=...`。
- 输出观察：同上将末段改为`tool-output`，可加`afterSeq`（原Sequence字符串）；不用`Last-Event-ID`充当输出确认水位。缺席从当前可用保留窗重放，客户端按status区分完整前缀与gap。
- 查询键不能重复或出现未知键，路径ID严格URL编码且解码后与body原值完全一致。POST没有查询参数；绑定、输出批次的session引用保留原候选。
- 每次请求/重连、SSE每次冲刷和至多10秒周期重新认证及裁决原权限；失效关闭，不继续排放私有正文。启动终端模式时SSE聚合/帧帽必须有限且不大于2MiB。服务内存输出窗不填写durableThrough；重启未知水位为null，绝不从0重新执行。

### 12.2 配置读取与持久CAS

`GET /v3/terminal/sessions/:id/configuration?contract=terminal-services-v1&sessionContract=...`返回ConfigurationResponse。`POST`同路径使用ConfigurationRequest（contract/requestId/session/expectedRevision/changes），变化仅含授权模型别名model和thinking，至少一项。禁止上传可执行client代码、凭据、任意系统配置。

配置revision是0至JavaScript安全整数上界的JSON整数，和工具输出的Int64十进制Sequence不混用；超界必须拒绝。thinking=null恢复模型默认；对象budget缺席沿默认思考开关，非负预算须由原模型门继续约束。model=null仅为读取旧默认，修改模型必须明确受权别名。

成功提交返回200 ConfigurationCommitResponse，status为changed或同键同意图重放replayed，携原成功revision。原owner及Store完成同次耐久写入后才发布当前运行配置；resolver不在Store锁中联网。requestId同键异意图返回409 request_conflict；期望修订不匹配409 revision_conflict；正在运行409 busy；新模型上下文窗无法容纳现有合法上下文409 context_transition_required（先由调用者压缩/处理，不自动删历史）。幂等保留额度耗尽429 request_limit，不能悄悄遗忘旧键。原Store未装可选CAS返回unsupported_capability，不能假装内存setter已持久。

配置不新建session、不换计费身份，不改系统提示词平台/宿主覆盖与保留开关优先级。连接断开后已提交事实保留，用原requestId对账，不能重算新键躲避冲突。此版本只定义model/thinking，其他公开控制仍按原六卡实现及会签。

## 13. candidate-7 同源交接

当前 schema revision 为 `2026-09-26.candidate-7`，SHA256 为 `8cd8c7c55a84c5700373aed75d5653a0737d718bfe0546641be367bda1a11896`。对应 `terminal-services-v1.golden.json`、生成的服务端合同和客户端 schema/types；旧 SDK2 DTO 及生成物不加入这些字段。C#、Android、iOS、Harmony 按这个精确候选消费，源码或生成物变动须重新登记，不以聊天中的版本号代替文件哈希。

### 13.1 明确的接入与权限边界

公开 Node/TS 入口为 `@tansr/api-client/terminal` 与 `@tansr/api-client/terminal/node`，通过 `TERMINAL_CANDIDATE` 显式选择。原 `@tansr/api-client/sdk2` 不自动协商或降低旧协议要求。完整可执行包夹具在 `scripts/sdk2/fixtures/terminal-services`，实际构建包与运行结果另记验收，不将源码 import 冒充独立包消费。

具有执行宿主的会话复用原 controller/executor、policy 和绑定代际。无执行宿主也可只使用配置/服务端记忆控制：可信部署者须提供 `terminal.scopeFor`，返回实际应用/用户/授权修订。每次控制读取及写入最终提交前重读映射；缺失、无效或撤销映射明确拒绝。它不来自客户端 JSON，不创建第二个 session。缺少设备时必需的 stream/background 能力拒绝，不回落到服务端机器。

`TerminalControlClient` 提供 configuration/configure、memoryState/memoryCommand/memoryCommandStatus。记忆管理动作当前为 pin、remember、forget；SearchMemory 继续为核心工具。配置失回使用完整原请求的同键 POST 重放；当前 GET 返回当前配置，不是按 requestId 的只读状态查询。system 动态值走可信 `platform.systemFor`，不在 ConfigurationChanges 偷加字段。原条款没有要求新增配置专用只读路由或完整动态预算来源API，不把建议追加为原卡阻塞，也不谎称已有路由；context输入空间budget与运行费用限制是不同事实。原P05提示词有效来源仍为真实补齐范围，使用下述显式metadata观察，candidate-7 schema不变。

原会话metadata GET（SDK1及offload各自原family）新增显式查询include=applicationPrompt。缺参保留旧返回体；选入时仅在当前活跃system来源可证明的情况下返回applicationPrompt双字段：policy为fallback/prepend，source为none/platform/sdk/platform+sdk。值直接来自SDK共享resolveApplicationSystem，不二次推断正文；sdk沿原词表指可信开发者/Serve宿主传入的S段，不是让远端客户端上传代码或越权更改system。正文、路径、凭据不返回。休眠/结束、旧服务、取消准备未实际应用或恢复system无法对应已证来源时缺席，客户端显示unknown；未知枚举、额外字段、fallback+platform+sdk矛盾组合不得假报来源。该读取不刷新配置、不起模型轮、不改变缓存或用量身份。

### 13.2 后台与设备记忆的独立 profile

后台仍沿原 Shell 权限、调度和任务 registry；新 profile 名 `TansrTerminalBackground`，定义摘要 `03848c0c75af6e2e358ac64f81a61c79f595d8fc70442d06ae3c0576e39157b8`。原操作终态、输出水位、后台存活与工件耐久分别表达；恢复中未知总长度为 null，不伪造为零。

设备记忆通过原 `tool.invoke` 的私有 profile `TansrTerminalMemoryPublication`，定义摘要 `8532a582d40d2d8993a80db59412a89671670ed7f994eaf9bf972bd544a111b0`。固定动作 head/read/begin/chunk/commit/query；单出版物最大 4 MiB、单块最大 12 KiB，不改变旧参数/回执 32 KiB 帽。该能力不注册为模型工具，也不靠打开 Shell 绕过授权。sourceId/sourceGeneration/domainKey、原执行主体和当前绑定共同裁决；CAS 与端侧 transfer 回执同一 SQLite 事务完成。

设备适配只存储与搬运，提取、固化、召回、SearchMemory、晋升、预算和模型费用仍在核心。建会与协商不提前向未登记设备派工；首次实际需要时懒准备，离线拒绝，不切换来源。MemoryIdentifier 为至多 256 个 Unicode code point，允许合法路径分隔字符；pin 最大 4096 个 code point。原 LegacyId 的 opaque 字节语义不被新设备 ID 规则缩窄。

终端 SQLite 要求显式 maxTransfers；永久旧键账不自动逐出。COMMIT 未知须重开原介质并查原 transfer，不能新建键重做。可信维护入口从原执行 journal 核完整 begin/chunk/commit 见证，允许新的实际连接查询旧结果；维护态不能开始新写入、主模型、摘要或子代理。恢复必须先经原可信 reconcileObservation 追加事实，关闭维护实例，再按原 owner/绑定重建。普通 HTTP 或模型不能提交“我已验证”来获得维护权。

## 14. 独立 ACK 恢复合同

见 [RFC-SDK2-ACK-recovery-v1](RFC-SDK2-ACK-recovery-v1.md)、[schema](sdk2-archive-recovery-v1.schema.json)、[真实金样](sdk2-archive-recovery-v1.golden.json)及[SQLite 增量 DDL](sdk2-archive-recovery-v1.sqlite.sql)。这是显式新 API 和新介质格式，不改旧 ACK DTO/端点或旧六方法。

`POST /v3/sdk2/bindings/:bindingId/archive/ack-rebases` 在原 idle lease/同事务边界内恢复已耐久接收但被 finish-run 推进 revision 的 ACK。请求身份先在终端耐久固定，服务端与终端各保留 previous→next 永久映射；失回只重放原键。只支持原仍活跃 epoch；明文 source/sync 恢复工厂与旧加密/cache 能力并存，不宣称后者已具恢复格式。

schema SHA256 `f530de1096b4f5d56ea688b7f2cec9ae66deb7d3719db85ab1f48287d3bd7ad4`；金样 SHA256 `614adf2088585a6213d010acde29598d40757d0f2d7b4608f1670ccbde3a5553`；DDL SHA256 `a6fcfef2458cb0dcf9788af2385573c85b238dc875dee7ffc144f27dcc28028b`。规范请求帽为 controlBytes+1024、响应帽为 2×controlBytes+4096，原单 ACK/receipt 帽不变。

## 15. 原六卡补齐：只读观察与设备沙箱（2026-09-27）

两项均为独立显式合同，旧 SDK1、SDK2 和 candidate-7 严格 DTO 不增加字段。实现仍复用原 registry、执行账本、身份、权限与输出队列；合同定稿不等于所有平台已消费或正式发布。

### 15.1 真实资源完成与工具输出关联

[terminal-observation-v1.schema.json](terminal-observation-v1.schema.json) SHA256 `b6668463458e78b2cfe23baa72d9d9ac6263a248467dd8aeb60387fc2f67c28a`；[金样](terminal-observation-v1.golden.json) SHA256 `2148eb11fb4538736cd244b17e5ba135bc26919081105fd1018ff884ab364e1a`。入口沿可信宿主显式启用的 terminal 模式：

- `GET /v3/terminal-observation/sessions/:id/resources?contract=terminal-observation-v1&sessionContract=sdk1|sdk2-offload-v1`
- `GET /v3/terminal-observation/sessions/:id/output-correlations?contract=terminal-observation-v1&sessionContract=...`，可选 `toolCallId`。

旧 DELETE 仍返回 202 接纳；`session.ended` 只证明执行结束。resources 读取原泵和原 `settleResources` 的真实结果，明确 `active/accepted/draining/completed/failed/unknown`、`closeRequested`、`executionEnded`、安全错误码和原 `epochStartSeq`。没有完成接口的旧句柄为 unknown；超时、断线、观察取消不制造 completed。GET 不触发关闭、恢复或新的排空；原 registry 保留期过后 404，不能从磁盘猜完成。同 ID 恢复后的新纪元不能冒充旧资源回执。

读侧前后复验当前认证、controller 权限、真实 family/endUser 及原删除/来源读闸。已正常结束的保留记录允许只读观察，不重用已释放的可写 lease。输出关联来自原持久 prepare 成功后的 `toolCallId/operationId/requestDigest/binding`，不从文本或 HTTP 请求猜测。内部至多保留 1024，公开返回最近 32 条及 `truncated`；同调用可关联多个真实操作，按 operation 身份去重。`outputAuthority` 沿原协商结果，呈现层逐调用选择显示来源；原事件日志不被全局抹除。

公开 TS `TerminalObservationClient` 复用已认证 `TerminalClient`，只读观察失败不自动重发关闭或执行请求。未启用新合同的服务按真实 404/不支持处理，不自动切换执行位置。

### 15.2 原远端 Shell 的受控沙箱

[terminal-shell-sandbox-v1.schema.json](terminal-shell-sandbox-v1.schema.json) SHA256 `be3ebcf6dc650844319a18a15f6abbefb9316f704410b591f9ba6dcab7620fe1`。固定工具 `TansrTerminalShellSandbox`，definitionDigest `058f31335cbb45a6ffe1b5ede8466013462c544c8108d687f41fa18447d51b96`，承载于原 `tool.invoke`。可信执行宿主显式 `shellSandbox: true`，设备精确登记工具摘要和解释器，应用 Shell 与当前绑定均允许时才生效。默认旧路径不变；只安装 profile 的设备不假宣告裸 `process.exec`。此前 4 KiB 总捕获候选 `4d217380…` 未交付，不与本候选混用。

普通执行及审批后的旁路共用此 profile。核心真实人审签发一次性不可伪造许可，绑定最终参数、turn、toolCall、完整执行目标及取消信号；缓存答复不能充当这次人审。Serve 与设备还读取原持久操作/回执，证明同轮同命令、cwd、解释器、作用域与绑定的先前真实隔离拒绝。未知结果、普通 EACCES 或任意 stderr 不能制造旁路证据。重复旁路仍按原 journal 和旧拒绝消费围栏拒绝，不建立第二套执行账。

`mode=required` 始终不可旁路；无 OS 沙箱能力如实 `capability=none`，不虚报 escalated。设备可信宿主注入原 SandboxRuntime 结构和明确的包装器绝对路径；固定程序白名单不等于 OS 隔离。此能力不授予 UAC/root 权限，也不在 Serve 宿主执行命令。

前台继续原双 pipe 捕获和输出流，原 120 秒/64 KiB 捕获上限不变。profile 前台回执受旧 tool.invoke 32 KiB 帽，仅容纳合计 4096 字节的 UTF-8 安全预览；必需 `output` 携 `previewTruncated` 及 stdout/stderr 各自完整 `totalBytes/payloadDigest`。预启动拒绝时 result/output 均 null。摘要以原严格 UTF-8 解码并保留 BOM 的完整输出为准，非法编码仍拒绝，不补造替换正文。

需要超过预览上限时，终端必须在 spawn 前接上原分块输出通道。核心仅在原同 operation 的输出窗连续、无缺口、已完整 seal 后，复验两通道完整字节数和摘要，重组原工具结果给模型一次。预览不能冒充完整正文，ACK 不冒充耐久；等候有界且响应取消，丢块、逐出、冷恢复失窗或撤权明确拒绝并保留原执行事实，不重跑命令。没有截断的完整小结果可以由回执自身校验，无需多一次模型调用。后台沿原 task/runtime/process/artifact 身份与查询、取消、读取生命周期，不把后台工件重复送入前台输出流。原权限、费用和模型最终结果仍只结算一次。

## 16. 原 P01 平台只读投影（2026-09-27）

独立 `terminal-profile-v1`，不改旧 metadata、candidate-7 或 SDK1 返回体。[schema](terminal-profile-v1.schema.json) SHA256 `560ad136f0619a2a0592151d56e10871c3799ca438163c4e2872381bf5f5159a`；[金样](terminal-profile-v1.golden.json) SHA256 `6ecfea5872a8cb2052d35cb908839c7ab07c11d77f65dc7e9a230494a2cf8ea4`。

- `GET /v3/terminal-profile/sessions/:id/catalog?contract=terminal-profile-v1&sessionContract=sdk1|sdk2-offload-v1`：同一 app_user bundle 的授权 `models`、`aliases` 与已知 `capabilities` 白名单。模型只含 handle/displayName/manufacturer/family；不含上游模型 ID、计价、凭据、owner 或原始 bundle。
- 同路径 `/usage`：固定当前认证用户的原 `/v1/my-usage?window=1d`，只返回本人 requests/inTokens/outTokens/cacheRTokens/cacheWTokens 五计数、window/endUserId；不允许终端选择其他用户或窗口，不暴露金额。

两响应均带 `contract/session/quota`，quota 固定 `{state:'not_exposed',enforcement:'gateway'}`。原 Electron 没有获知具体 rpm/日 token 上限；这里不把未知解释为无限，不推算账户余额。应用能力位说明应用配置，不承诺设备已绑定或已获得当前执行权；工具实际调用仍走原应用、用户政策、能力、绑定和权限交集。

读取沿原逐用户刷新令牌、401 恰一次重铸及 ETag 条件确认，不以旧目录掩盖来源失败；只读不建会、不调用模型、不切换配置。会话 family、controller、真实用户与同 handle 及删除/来源读闸在异步前后复验。缺源明确不支持，断线及超时不假报底层请求完成。公开 `TerminalProfileClient(existingTerminalClient).readCatalog/readUsage` 复用原认证。

## 17. 原 SRV-05 本地可信宿主与 P02 定制

CLI/SEA 显式 `serve --v2 --host-module <absolute.cjs> --host-module-sha256 <sha256>` 加载部署者预先安装的模块。入口通过同一文件描述符读取、校验后执行这份字节；不由模型或 HTTP 提供模块路径，不改变旧启动默认和 SEA code cache。模块与部署者本机代码同级受信，入口摘要不签署依赖图，也不是代码沙箱。

context 提供原公开 serve/extensions、cwd、只读 env 快照及 own(resource)；模块返回原 agent 工厂配置、真实 authenticate 和可选 terminal/v2/resource。CLI 最后固定监听、随机 Bearer 和 cwd；先验证 Bearer 再委托用户认证。官方多用户示例使用独立高熵用户票及哈希映射，不相信客户端自报 endUserId；样例及接法见 `examples/serve-demo/TRUSTED-HOST.md`。关服沿原生命周期 drain、真实 settleResources、最后 factory.flush，再逆序关闭 owned 资源；结束封印仍在飞时不能提前关介质。

平台宿主支持原 query 的 temperature/stopSequences/maxToolRounds/maxConsecutiveErrors 配置；它们不是新远程 JSON 字段。可信 `platform.trustedExtensionsFor(scope).decorateModelClient(client)` 在初建及切模装配中生效，外层仍保留原执行/记忆请求守卫、资源与费用追踪。正常委托原 client 保留原平台链；主动替换上游行为的可信开发者须对返回协议和用量真实性负责，不能把此接缝宣传为防止恶意宿主伪造计费的安全边界。

工具定制沿已授权 toolsFor/clientTools 注册及原 DispatchingToolExecutor；beforeTool 只能收紧。不能通过替换整个 executor 或 runQueryImpl 测试缝绕过权限和设备定向。Electron 继续完整 SDK/IPC，不改依赖生态。以上实现及合同登记不代表已完成精确包/SEA 消费、主线收编或发行；验收结果回填原六卡与24断言，不新增统计分母。
