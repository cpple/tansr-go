# SDK2 档案同步与保留扩展

任务：SDK2-03。本文是 `SDK2-ext-v1-wire.md` 的加法合同；原 `sdk2-ext-v1`、SDK1、SessionStore、资金接纳与运行账本保持原语义。

## 1. 来源、角色与真实调用链

原核心是出版顺序、权限、代际及源 ACK 的裁决者。终端或开发者存储保管正文，不能自行产生新的核心记录。A 策略继续使用既有单来源工厂与原循环。B 的显式组合使用原 `createSdk2ReplicatedArchiveStore`：一个 primary 和一个开发者 replica 固定同组、同接收身份、同限额；原 input 及 request 必须在调用方耐久保留至双方确认。开发者端经 `createSdk2ArchiveSyncService` 提供存储端口，Serve 的 `startServeArchiveSync` 沿既有用户鉴权发布，终端 `createSdk2ArchiveSyncClient` 可直接作为原组合的 replica。

新接收介质使用 `openSdk2SqliteSyncArchiveStore`，固定 `sdk2-archive-sync-sqlite-v1` 文件族及 `syncRole`；不原地迁移旧 `sdk2-terminal-sqlite-v1`。`source` 可接受原批和确认原 ACK；`cache` 的 `receive`、`confirm` 在运行时拒绝，只能用 `receiveSync` 续接原出版链。缓存设备不加入 B 的固定确认人数，也不阻碍固定两副本完成源 ACK。

核心 `requiredReplica` 显式固定 policyId、policyGeneration、replicaId，保存在原 spool 的来源元数据中；恢复时不能删除或换策略。开发者接收完成后，可信宿主端口 `recordRequiredReplica` 核原完整出版范围及 ACK 并持久见证；终端不能通过公开 HTTP 自报这个事实。源 `acknowledge` 和 `releaseAcknowledged` 都要求原见证。见证写入与检查均在原 spool 串行域内，不在锁中出网。此记录不是另一套运行或资金账本。

## 2. HTTP 与原键恢复

新入口为 `POST /v3/sdk2/archive-sync/:bindingId`，独立信封 `{format:"archive-sync-v1",identity,method,value}`。客户端只向 HTTPS 或本机 loopback HTTP 发送 Bearer，禁止重定向。Serve 在读正文前、派发前和回包前使用原 authenticate 检查当前用户，解析器须返回同一当前授权绑定对象。applicationScopeId/endUserId/bindingId 与固定身份严格匹配；没有文件路径、任意 URL 或宿主工具入口。

支持原接收操作 receive/confirm/pending/head/coverage/identity/operation/records/body-chunk，新增 sync-page/retention/retention-page/apply-retention/reconcile。接收批仍保留原 BindingView、ArchiveStatus、ArchivePage、RequestIdentity 及正文哈希；开发者 `assertPublished` 必须逐记录与当前受信核心出版页相等。自行生成的摘要不能替代核心出版授权。

两副本部分写入沿既有 `recoverPending(loadOriginalInput)` 补齐；远端 COMMIT 或回包未知时重开原介质并续原请求。开发者已耐久但核心见证调用失回时，显式 `client.reconcile(originalAck)` 仅根据开发者已保存的原操作重试可信见证，不再写正文、不改键。其后才将原 ACK 交给原 `Sdk2Client.acknowledge` 并对两侧 confirm。没有自动降为单副本或换键。

每次同步只推进一个原批或一个删除修订，不无限追赶。元数据页最多原128条，独立响应帽1.5MiB；原控制编码器的256KiB帽不变。正文下载每段256KiB，新介质 `bodyChunk` 直接通过 SQLite `substr` 读该段，每段重核来源及当前保留证明；客户端末尾仍核原 SHA/字节及32MiB上限。没有逐段重新全读对象的默认回落，自建新介质须实现范围读取。接收批依既有限额整批 base64 传输，HTTP 的正文预留、32个并发及请求帽仍参与准入；不宣称已实现断点分块上传或独立物理故障域。离线、超限、未授权或未知结果明确拒绝；调用方保存原工作后择机续接。

## 3. 缓存同步与冲突

`archive-sync-v1` 页包含固定 identity、retentionRevision、原 checkpoint、原 ack/receipt，以及该批已授权墓碑。`receiveSync` 验证原顺序、前驱摘要、完整 ACK/receipt 及所有未删正文，一事务写记录、正文、操作、原确认、连续头；仅返回 `archive-sync-receipt-v1`，不能提交为源 ACK。对同键不同记录、不同 ACK/receipt、跨用户/跨代际、缺口、倒序或遗漏当前墓碑均拒绝。

缓存不能写入本地独立会话分叉；多设备的会话写入仍由原 Serve 会话、运行准入和原 Store 决定。同步只复制已出版记录，不合并用户自行改写的档案。未确认的源批不得向新设备传播。应用卸载或关闭只关闭其自有连接，不删除核心绑定或自动解除固定副本责任；源策略退出须另循既有关闭流程。

## 4. 删除及当前保留证明

`archive-retention-v1` 固定 identity、requestId、revision、previousRevision 及1至128个 `{recordId,sequence,recordDigest}`；修订必须逐一连续，原请求及范围不得改释。可信 `readRetentionRevision` 来自当前核心授权，不能从旧备份自证；`authorizeRetention` 必须核核心已接受、材料及原派生改写已排空的完整原意图，单一 revision 数字不授权范围。实际 Serve 接线分别使用 `host.readRetentionRevision(subject)`、`host.readRetentionPage(subject, afterRevision)` 和 `host.authorizeRetention(subject, retention)`。

缓存落后当前保留修订时，正文和普通记录读取立即拒绝；只允许读取追赶所需的角色及修订元数据。先逐项 `applyRetention`，随后才允许新同步页。网络页不能独立引入墓碑，也不能省略本地已有墓碑。`applyRetention` 在同 SQLite 事务中核原意图，保存墓碑、修订和请求，删除不再被任何活记录引用的正文。原记录、原摘要链和历史 ACK 保留；共享附件若仍被合法记录引用则保留。未删除范围继续可读。旧备份没有当前修订或范围授权时不能恢复正文。

可信宿主现提供 `deleteSessionHistory(subject, { requestId, scope: 'entire-session' })`，须通过独立 `delete-history` 授权。它只接收整会话删除，要求原出版记录已获 ACK：先阻新运行和材料取用，在原控制 spool 固定请求/身份/完整原出版头，排空原 MaterialCoreLease；经 `withArchiveRestore`、原 rewrite 及 Store 确认提交空历史并清原上下文派生材料，随后走原 binding-close/退役并关闭会话。全部成功后才持久 `archive-session-deletion-v1` 完成回执、发布当前保留修订及按原记录派生的墓碑页。Store 提交前失败或写入后失回均保留屏障/原请求，拒绝墓碑与成功声明；不假定写失败就是未落盘。

整会话删除使用独立、不可回退的 `retentionRevision` 删除事实；原 target/generations 不伪造递增。旧来源及会话已永久关闭，不能按旧 Store、快照或材料恢复新运行。已接线的旧 `/v2` 会话列表、元信息、history/分页、checkpoints/导出及 events 重放共用原删除意图屏障，快照异步读取回包前再核。冷读按原命令定位原介质，以只读 SQLite 同快照点查原物理绑定、metadata 和固定意图/提交摘要，不领取 owner、不要求 resume、不创建 driver 或新预算项；未删除及未登记的原 Store 继续按原读权限可读。损坏的已登记介质保守拒读。

调用方直接绕过 SDK 读取磁盘、删除外部备份及介质安全擦除不在此逻辑删除合同内。新能力不自动迁移旧单来源文件。

### 4.1 显式同 binding 记录删除

部署者通过 `recordDeletion: { policyId, invalidateMemory? }` 显式启用后，可调用 `host.deleteArchiveRecords(subject, { requestId, expectedHead, recordIds })`。原 `delete-history` 授权、同用户与绑定、空闲运行、原出版头和已确认源 ACK 仍是前置；一次只选原 turn 记录，最多32条。原旧 Store 与未启用模式不采集来源旁带、不改变恢复或错误语义；启用前记录没有可靠映射时明确 `unsupported_capability`，不按相同文本猜测来源。

新旁带使用独立 `serve-archive-lineage-v1` 控制元数据，绑定原 Store 的实际 commitId、会话摘要和原捕获 workId。仅显式启用查询在回调前固定初始对象位置及正文摘要；同对象正文被改写不能继承旧来源，重复对象的全部位置仍保留。候选旁带不能单独证明 Store 已提交，热读与冷恢复均须核原已确认提交事实。未知改写及压缩产生的消息采用保守依赖范围；删除范围还按原 projection.coverage 计算派生闭包，无法拆分的相关摘要整体失效，不能保留含应删正文的派生内容。

原控制 spool 持久 `serve-archive-record-delete-v1` 意图，固定选中记录、派生闭包、原提交及候选摘要；一次原事务在改写前预留意图和完成槽。最多128次部分删除、每次闭包最多64条记录；超过容量在改写前拒绝，整会话删除不占该局部次数。原 rewrite 的排空检查只承认严格匹配同一意图、完整身份和摘要的完成预留，不放过其它未决工作或持有 lease。随后复用 `withArchiveRestore` 与原 Store 提交和发布，更新会话正文、上下文派生状态及来源旁带；原保留记录继续可读，同 binding 可继续新一轮。

显式删除取消旧 target 下原 reader 队列中尚未消费且未被任何 reader 持有的材料，再按原释放规则清正文；这是删除治理操作，不是因离线/超时自动取消，也不将 required 降成 optional。原 native guard 在拒绝前检查所有持久 lease，持有时保持原状态。失败或提交结果未知保留屏障和原请求，不能成功发布墓碑；需要材料的新轮必须重新签发。若配置原外部 memory，须提供幂等 `invalidateMemory`，按原 requestId 完成该应用的记忆失效确认；回调失败继续保持屏障，不宣称即时擦除外部备份。

只有原 Store 候选和派生处理确认后才写 `archive-record-deletion-v1` 完成回执、推进独立 retentionRevision，并沿原同步保留页传播墓碑。回执丢失按相同请求、原记录及原 expectedHead 续办，不换键、不重置原 ACK 或伪造原 target 代际。旧材料、落后保留修订的设备和旧备份不能重新供给被删内容；共享附件仍被其他合法记录引用时继续保留。

旧 `/v2` history、元信息和列表的异步返回绑定同次保留修订及已确认 Store 摘要，删除跨过读取时拒绝旧返回值。冷读继续只读原介质、不领取 owner/resume。旧检查点的来源不能证明当前修订时拒绝导出、导入与恢复；当前已确认安全快照可用，旧标题因缺少独立来源保守隐藏。显式删除首次推进 driver 事件可读起点，旧 SSE 连接在下一次送帧前关闭；重新连接使用原 gap 和 session.restored 事件，只重放新起点及其后的内容。未启用且没有删除事实的旧会话保留原行为。

## 5. 兼容与验收口径

旧单来源、旧 SessionStore 及旧 SQLite 文件照旧，Electron 继续原集成 SDK 模式。新同步端口没有放宽冻结协议，没有新增付款/运行账本；档案耐久与资金受理仍分别依据原链证据。定向覆盖真实核心 spool→HTTP→两 SQLite→原 ACK→新设备缓存、原键见证恢复、逐批补取、删除传播、旧页拒绝、实际 Store 删除失败和真进程冷启动反复活。8MiB 对象实测32次范围读取、读取总量8MiB、整对象查询0次。各原卡依其实际具名回执判断，不能以局部测试替代原完整门禁、实际压缩派生组合或各平台原生消费。

## 6. 可选密文介质、历史查看和迁移（第四批加法合同）

本节只增加 Node/PC 的存储工厂及组合入口，原 `sdk2-terminal-sqlite-v1`、`sdk2-archive-sync-sqlite-v1`、`sdk2-ext-v1` 和 SDK1 Store 不变。Android/iOS/Harmony 继续消费既有记录/ACK/同步语义，不因 Node 新工厂自动获得 OS 密钥生命周期能力；原平台门分别留证。

### 6.1 密文正文介质

`openSdk2EncryptedSqliteSyncArchiveStore(options)` 复用原同步接收器、事务、owner、ACK、墓碑、配额及有界读取，使用独立 `sdk2-archive-encrypted-sqlite-v1`。options 保留原同步字段，并增加 `key: { id, read: () => Uint8Array }`；固定 id 来自宿主可信配置，read 每次操作重新取得32字节 AES 密钥，不能写入数据库、日志或 wire。OS 密钥库由宿主注入，Node 模块不强制依赖任何特定 OS 密钥库。

正文与附件按256KiB独立 AES-256-GCM 段保存，每段固定12字节随机 nonce、16字节 tag、原长度 ciphertext；AAD 绑定格式、完整接收身份、key id、原 ArtifactRef、段位置和该段长度。原 metadata/记录/ACK/索引不作为正文密文，仍会暴露应用、用户、会话、源、绑定及请求 ID，target 的代际与快照摘要、记录类型/状态/轮号/前驱/正文摘要，ArtifactRef 的 ID/MIME/长度/SHA，原 ACK/回执/出版水位、保留请求与墓碑；不声称整盘或端到端加密。密文实际字节计入原 logicalBytes/maxStoredBytes，原 maxBatchBytes 继续约束明文批。每次 bodyChunk 只解密需要的有界段，不重新整读对象。库内单个认证 key-check 只用于检测空库错钥，不含原始密钥。读取、接收、确认沿原 reentrant/unknown 保护；close 不重取密钥，撤钥后仍释放原资源；密钥撤销或同 id 换值使后续操作失败，不能以空历史继续。模块持有的密钥副本在操作结束清零，不修改宿主原数组。

轮钥不原地改 key id 或重写正在使用的库。创建新 id 的新介质，沿6.3完整复制和核对后显式切换应用引用；未完成时保留原介质和钥，原 pending ACK/未知状态先续原操作。迁移成功后宿主可撤销旧 id；旧备份仍需原钥和当前授权，不自动恢复已撤销钥。安全擦盘、用户绕开 SDK 直接读其自持密钥/备份不在逻辑合同内。

### 6.2 显式历史视图

`openSdk2SqliteArchiveHistory(options)` 只以 reopen 打开原 sync 或新密文介质，另要求 `current` 当前可信来源的 `head/readRecords/retentionRevision` 只读端口。返回对象只有 `readPage`、`readArtifact`、`close`，使用 `archive-history-view-v1` 结果，不提供 receive/ACK/confirm/coverage/material-source/恢复见证。旧备份自身的完整哈希只证明旧版本；每个返回记录均须与当前授权来源同 ID 的不可变记录逐项相等，并在返回前后核同一当前 head、retentionRevision 及原 app/user 授权。旧 head 可作为明确历史版本查看，不能覆盖当前 head。当前来源已删除/无权/未知/缺失的记录拒读，旧备份不能自行授权；共享附件也必须从一个当前仍授权的原记录取得。当前来源/治理推进跨越读取时拒绝旧返回值。该视图不是绕过原删除屏障的旧数据库打开开关。

### 6.3 同档迁移及轮钥

`copySdk2ArchiveStore(source, destination, {maxBatches})` 是有界开发者组合：source/destination 复用原 `Sdk2SyncArchiveStore` / HTTP sync client，同一完整接收身份，原 source 无 pending ACK。逐一先传播当前 retention，再复制原 syncPage 和精确正文，destination 只通过原 receiveSync 保存原 ACK/receipt，不产生新源 ACK。每次只在来源头与保留修订未变条件下继续，逐一核全部已复制前缀的原记录和正文，不能仅凭相同 head 跳过早期分歧；maxBatches 为1至1,000,000的本次验证/复制预算，旧前缀亦计数，预算不足须提高有界预算后重试。完成时两端 head/retention 一致；活动来源前移、缺块、坏摘要、超帽、错域和撤权原样失败，保留已验证目的前缀，调用者可有界重试。轮钥及跨 OS 迁移使用新物理库而非拷贝 inode 绑定文件；既有源、旧 Store 和备份不自动删除。目的新缓存迁移完成不等于已转换固定 B 副本角色，不代替源 requiredReplica 见证或部署者策略切换授权。


### 6.4 显式可信音视频附件策略

宿主来源 `trustedAttachmentSource.media: true` 仅为新运行租约固定 `sdk2-trusted-media-turn-attachments-v2`；旧 trusted v1 始终只处理外置图片，旧无策略租约保持原附件集合。prepared、冷完成及回收按原持久策略校验，不能从新来源能力猜测升级或降级。v2 只从原 typed image/video/speech artifact 取已登记引用：speech MIME 必须与原 artifact 一致；video 的原冻结类型没有 MIME，由可信注册表在读取前固定 MIME/字节数/SHA。图片沿原 png/jpeg/gif/webp；新增 audio/wav、audio/mpeg、audio/ogg、audio/flac、audio/mp4、video/mp4、video/webm，并校验对应文件签名及原 SHA。已有 imageIndex 字段只表示该 typed 产物的项目位置，私有租约加法不改冻结 wire。

外置来源只做可信注册表查找，不按 URL 自行联网。来源过期、对象变体、缺失、失败或总帽超限不能发布完整记录或成功完成原 fence；已持久附件可在无原来源时按原记录冷恢复。普通文件继续由可信宿主通过原 journal 的显式 ArtifactRef 附件提交，不从任意工具 data、路径或未来 artifact 猜测文件读取权限。图片、音视频和显式文件均复用原原文/附件 ACK、同步、墓碑和密文介质，不另建下载协议。

## 7. 三策略装配与单档只读授权（第七批加法合同）

原需求出处为主方案 §5 的“组织协作”场景、§5.1.4 共享来源及端缓存，以及该章安全约束中的组织成员变更复验。这里仅补当前成员读取明确授权档案的能力，不建立多人写入、成员自行确立来源或组织全域可读的新规则。

A 继续原单一终端来源、原 receiver receive→核心 ACK→confirm；B 继续原两个 source 角色、requiredReplica 见证和 recoverPending 原键恢复；C 使用开发者 source 与原 syncRole cache，同 owner 跨设备无额外许可。策略不能由请求体切换，B 不能在副本离线时降级，C 离线不能把缓存当作当前授权来源。冲突、原批缺失、未确认及坏摘要沿现有码拒绝，单批补取与同 binding 墓碑传播沿 §2～4，不另建账本。来源选择和默认/叠加/offload 的持久关系相互独立。

### 7.1 actor 与 owner 分离

`Sdk2ArchiveSharedReadGrant` 为宿主配置回调的固定数据 `{actor: Scope, owner: ReceiverIdentity, grantRevision: Sequence}`。actor 是当前受信登录者，owner 包括原 applicationScopeId/endUserId、bindingId、target.sessionId、historyEpoch/deletionGeneration/projectionRevision、sourceId/sourceGeneration；actor 与 owner 必须同应用，不能仅凭同组织放行。`sharedReadAccess()` 必须从可信当前 ACL 取得这个精确范围，不能直接返回 wire、自报组织、旧缓存或待打开备份内的数据；无权或当前证明不可得时抛错。该回调只在本地限制访问，不替代远端授权。

原 `ServeArchiveSyncHost` 加可选 `resolveReadOnly(actorEndUserId,bindingId)`，仅原 owner resolve 未命中时解析，返回 `{binding,grant}` 或 null。binding 必须是原稳定同步服务对象，grant.owner 与其实际完整 identity 逐字段相同，grant.actor 与原 authenticate 的当前主体一致。入口、读 body 后、dispatch 前后均复验当前认证、binding 对象及整个 grant 快照；撤销、grantRevision、授权修订或来源代际跨 await 变化，拒绝旧结果。请求体不能指定新的 ACL 范围。

只读 HTTP 方法限定 head/coverage/identity/records/body-chunk/sync-page/retention/retention-page；receive/confirm/pending/operation/reconcile/apply-retention 均拒绝。sync-page 携带原不可变 ACK/receipt 作为复制校验，不产生新源 ACK 或副本见证。服务端独立拒绝写操作，绕过客户端检查不获得权限。错误仅保留原代码，不回传令牌、ACL 明细或正文。

### 7.2 共享缓存与换票

原同步及密文工厂仅在 `syncRole:'cache'` 接受 `sharedReadAccess`；source 与旧单来源工厂不接受。readContext 始终返回真实 actor，不伪造 owner。新建缓存沿原 metadata 固定 sharedReadActor 的应用/用户，仍保存精确 owner identity；不同成员、省略 sharedReadAccess、换来源/会话代际、改为 source 都无法重开该库。合法授权修订更新不改持久 actor，可重新取得当前许可后续读。每次原事务、正文、附件、索引、syncPage、历史查看及导出均核真实 actor、固定 owner、完整 grant 和当前保留修订，跨 await 再核。close 不需取授权或钥。

共享缓存不能 receive/confirm，且 pending/replicaOperation 明确拒绝，不能包装为 B 的必需副本。receiveSync 只保存当前核准的原出版批；本地 applyRetention 仍须原 authorizeRetention 精确核核心删除事实，只清缓存，不授予源删除权。同步先追当前墓碑，再补未删正文；撤权或来源不可用不能以缓存回落。历史视图仍须原 current 逐记录证明，不能用本地旧页授权自己。

共享只读缓存的轮钥或新设备重建使用新物理缓存及当前已授权远端的原 synchronize，一次一批直到追平后由宿主显式切换；完成前保留旧缓存和钥。原 copySdk2ArchiveStore 要求 source pending 权限，只用于原有合法源迁移，不放宽给共享缓存或其只读 HTTP 客户端。离线不能为完成迁移而提升缓存权限。

`createSdk2ArchiveSyncClient` 加可选 `readToken`，与原 apiKey 二选一。每个请求及整个 synchronize/body 调用固定当前 token、actor/owner 许可和授权修订；本地 cache 等待、HTTP 等待、块读取及缓存写入前后均检查。途中变化返回 context_changed，不自动换票重放、改 requestId、重建 binding 或 session。恢复由宿主取得当前授权后显式重试原键。原静态 apiKey 与无共享选项的行为保持兼容。

本节复用原 152b2e8b/91db5ee1 的 HTTP 双副本、原同 binding 删除/离线回流回执；新增实红和跨语言共同原 schema 夹具登记在 `archive/20260924-SDK2-seventh-batch/storage`。当前单档授权测试不替代各平台 OS 密钥、安装物消费或发行验收。
