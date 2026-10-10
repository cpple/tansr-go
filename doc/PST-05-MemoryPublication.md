# Go MemoryPublication 持久化 / persistent publication

本页描述 PST-05 本地开发候选，尚未发布到 v0.3.0。从当前源码运行 `go run ./examples/go-memory -help`；不要用旧版本 `go install ...@v0.3.0` 查找新入口。Archive 保持原有协议与介质。MemoryPublication 只是 Serve 指定字节的保管宿主，不做记忆提取、选择、权限或接管决定。

This page describes the unpublished PST-05 candidate. Run `go run ./examples/go-memory -help` from this checkout. The existing Archive APIs and format remain unchanged. MemoryPublication stores bytes selected by Serve; it does not decide memory extraction, recall, authorization or takeover.

## 公共装配 / public composition

`memorypublication.OpenFileStore(Options)` 返回独立的加密介质。`memorypublication.NewHost(store, true)` 接 `executor.RunnerOptions.MemoryPublication`；注册 `executor.MemoryPublicationToolName` 与冻结的 `executor.MemoryPublicationDefinitionDigest`。授权操作名仍为 `MemoryPublication`，不可安装成普通模型业务工具。原 Runner 校验 scope、绑定、连接代际、租约、digest，原 journal 保存回执后才上传原 receipt。自定义介质实现 `Store.Identity/Capabilities/Execute`，声明 `AtomicDurablePublication` 必须真实提供原子正文+transfer结果；声明 `EncryptedAtRest` 必须覆盖正文、暂存、owner与回执元数据。显式要求加密时 Runner 同时要求 journal 的 `EncryptedAtRest()` 为 true，缺能力即拒绝。

Compose `OpenFileStore` → `NewHost(store, true)` → `RunnerOptions.MemoryPublication`. Register the frozen transport name/digest constants; the permission remains `MemoryPublication`. Custom `Store` providers must atomically publish body and transfer result. Encryption declarations cover staging and metadata as well as the published body. An encrypted publication host also requires an encrypted execution journal because read receipts contain body bytes.

```go
store, err := memorypublication.OpenFileStore(memorypublication.Options{
    Path: absolutePath, Mode: "reopen", Key: hostKey, Identity: hostIdentity,
    CurrentScope: readCurrentAuthenticatedScope,
})
// Check err; defer store.Close(). Do not replace unreadable data with an empty store.
host, err := memorypublication.NewHost(store, true)
journal, err := executor.NewEncryptedFileJournal(journalDirectory,
    executor.JournalEncryption{Key: hostKey, ApplicationScopeID: hostIdentity.ApplicationScopeID,
        EndUserID: hostIdentity.EndUserID, ExecutorID: executorID, CheckAccess: checkCurrentLogin})
// Check each error; defer journal.Close(). Pass host and journal to executor.NewRunner.
```

## Demo

可信宿主先建立允许设备记忆来源的现有 Serve 会话，提供与 `memoryPublicationFor` 对应的稳定 source/domain/generation 和当前授权 Scope。domainKey 是受信 Serve 提供的 publication key，不是自行推算的本地路径。`-access-file` 由认证服务维护，每次存储操作和 journal 操作重新读取；删除或撤销文件立即拒绝。它不是向 Serve 自报身份的渠道。请预先建立私有目录，Windows 配置当前账号 ACL。密钥模式沿用 Archive 的宿主提供 32 字节钥；示例从 `TANSR_MEMORY_KEY` 读取64位hex，真实应用使用OS密钥设施，不能把钥保存到这些文件中。

The trusted host supplies an existing authorized session, stable source/domain/generation and a live Scope file. `domainKey` comes from Serve's trusted publication identity, never a terminal pathname. The auth service maintains the access file; each storage/journal operation rechecks it. Provision a private directory/Windows ACL and a 32-byte host-managed key. The demo accepts it as `TANSR_MEMORY_KEY` (64 hex characters); production apps should use the host OS key facility. Keys are never persisted in these media.

Identity JSON (supplied by your host):
```json
{"applicationScopeId":"app","endUserId":"user","sourceId":"source","sourceGeneration":"1","domainKey":"host-supplied-publication-key"}
```
Access Scope JSON:
```json
{"applicationScopeId":"app","endUserId":"user","authorizationRevision":"1"}
```

```sh
go run ./examples/go-memory -base http://127.0.0.1:8787 \
  -session EXISTING_SESSION -executor AUTHORIZED_EXECUTOR \
  -file /private/tansr/memory.bin -journal /private/tansr/journal \
  -identity-file /private/tansr/memory-identity.json \
  -access-file /private/tansr/current-scope.json \
  -binding-request ORIGINAL_BINDING_REQUEST_ID -mode create
```

首次明确 create；以后同文件、身份与钥用 `-mode reopen`（默认）。Demo 注册执行器、绑定执行、通过原 `terminal.binding.create` 协商 `memory-lifecycle-v1`，进入原 poll/receipt 循环；Ctrl+C等待Runner退出并关闭介质。它不创建会话、不调用模型、不自动重建档案或未知transfer。读写 memory 由已授权的 Serve 业务触发，输出 ready 只说明宿主已绑定。终端绑定失回保留原request ID和完整原请求；连接重建改变绑定后，先由受信控制器对账，不拿旧键提交异体。

Create only once; subsequent runs use the default `reopen` with the original files, identity and key. The demo negotiates the existing `memory-lifecycle-v1` feature and runs the normal poll/receipt loop. It does not create sessions or call models. Ctrl+C drains the runner before closing storage. Preserve original binding requests on response loss; a changed connection is not permission to reuse a request ID with different content.

## 介质保证、容量与恢复 / guarantees, capacity, recovery

- 固定上游83c64b2c、UAPI r7和39份锁定文件不变。head/read/begin/chunk/commit/query原字段不变，4MiB正文、12KiB分块，严格UTF-8与SHA256；ACK、canonical、digest原语义不变。
- FileStore 每次写入一个有界加密snapshot：OS独占文件锁、临时密文、File.Sync、原子替换（Unix再同步目录；Windows原MoveFileEx write-through）。它保证进程重开与原子可见性；物理掉电、网络文件系统不作未经实测承诺。
- begin保存完整owner（scope/session/binding）；同transferId异体、换连接/工作区/授权代际写入拒绝。只有宿主显式`AuthorizeRecovery`可允许新owner查询原结果，不能接管写。所有暂存与永久committed/conflict结果重开保留；未知chunk/commit返回unknown，不新建。
- 默认最多4096个transfer、8MiB暂存、32MiB密文文件。`Capacity`显示逻辑占用。终态没有TTL或自动删除；满额明确capacity_exceeded。整个snapshot每块重写，适合有界单设备；大规模应用用自定义事务介质。永久防重见证不能因满额换目录/删记录。
- AES-256-GCM保护publication正文、暂存、身份、owner、原transfer结果及临时文件。独立journal加密保护claim和read回执敏感副本，AAD绑定应用/用户/执行器/文件名，缺钥/错钥/篡改拒绝。应用日志、进程内存、磁盘备份中的钥不在此声明内；Demo不输出正文或钥。
- 错钥、身份变化、损坏、撤权不覆盖原件。提交后授权/IO失败按unknown，关闭后用同原介质对账。备份需停止写入/关闭后复制整snapshot与完整journal，保留原钥和身份；旧备份不包含后续提交见证，不能作为当前授权或安全回滚依据。
- `Rekey(sourceOptions, freshTargetPath, newKey)`独占重开源、一次加密提交复制全部正文/暂存/终态，新路径必须不存在。核验后由宿主显式切路径；源保留。journal是另一介质，可显式调用下节的 `RekeyEncryptedFileJournal`；没有自动换钥/清理或跨介质事务。当前版本不提供跨设备同步、离线副本删除、长期transfer压实或完整设备备份编排。

The file store uses an exclusive OS lock and encrypted sync/replace snapshots. It preserves original transfer owners, staging and permanent committed/conflict results across process restarts; unknown transfers are never recreated. Bounds default to 4096 transfers, 8 MiB staging and a 32 MiB encrypted file. Full capacity fails explicitly without dropping idempotency witnesses. This implementation rewrites the bounded snapshot on each chunk; custom transactional stores can serve larger workloads.

AES-GCM protects the publication, staging, owner/receipt metadata and temporary files. The separate encrypted journal protects full execution receipts, including read bytes. Wrong keys, identity/AAD mismatch, corruption and revoked access fail closed and preserve originals. No physical power-loss or network-filesystem SLA is claimed. Back up closed media together with the original host-managed keys; old backups are not current authorization or proof that later commits never happened. `Rekey` copies the publication to a fresh path while retaining all transfer facts; the separate journal has its own explicit `RekeyEncryptedFileJournal` step below. Automatic migration, cross-device synchronization and transfer compaction remain outside this implementation.


## 显式保源换钥 / explicit source-preserving key rotation

先停止并排空 publication 与 execution journal 的所有写入，关闭 runner 与介质。备妥原身份、原钥及两个完整原件，再分别执行两步。`memorypublication.Rekey` 沿用既有完整 snapshot 入口；新增 `executor.RekeyEncryptedFileJournal` 只支持现有 encrypted-v1 journal 到**不存在的新目录及不同的32字节新钥**，不迁明文、不改应用/用户/执行器，也不重新签发 owner、operation、transfer 或 digest。它复制所有原文件键、claim digest 和永久 receipt（含 read 敏感正文）；没有 receipt 的 claim 仍为 pending，已有 unknown 回执仍为 unknown。换连接/授权代际不能用同原键重新执行。

Stop and drain both stores and close the runner before either step. Retain the original identity, keys and complete media. The existing publication `Rekey` is unchanged. The new encrypted journal migration requires an absent destination and a different 32-byte key. It preserves every original filename/key, claim digest and receipt, including sensitive read bytes. Pending claims and unknown outcomes never become permission to execute again. It cannot change identity, invent authorization or take over another owner's publication.

```go
// Writers have stopped; both directories have host-controlled private permissions/ACLs.
// Step 1: publication (check error and close the returned store).
next, err := memorypublication.Rekey(sourceOptions, newPublicationPath, newKey)
if err != nil { return err }
if err = next.Close(); err != nil { return err }
// Step 2: complete execution journal, using its original authenticated identity.
err = executor.RekeyEncryptedFileJournal(ctx, originalJournalDirectory,
    newJournalDirectory, executor.JournalMigrationOptions{
        Source: originalJournalEncryption, TargetKey: newKey,
        // Defaults: MaxFiles = 65536 claim/receipt files; MaxBytes = 1 GiB ciphertext.
    })
if err != nil {
    var failure *executor.JournalMigrationError
    if errors.As(err, &failure) {
        // Preserve failure.StagingDirectory and failure.TargetDirectory.
        // Published=true means a complete target was published before a later error.
        // Reconcile with current authorization; do not overwrite or auto-delete either.
    }
    return err
}
// Verify BOTH new media with the original identity and new key, then switch paths once.
// Keep original files and original key; never resume writers on both copies.
```

journal 迁移在新目标的父目录建立私有密文 stage，逐项验证原 claim/receipt 配对与文件键，完成全部复制、重读及冷开校验后，才用不覆盖原路径的目录发布原语使目标首次可见。已有空目录也拒绝，不用普通 rename 覆盖它。默认迁移上限为65536条 claim/receipt 文件和1GiB总密文字节，宿主可显式调整；超过上限拒绝，绝不丢永久见证。缺 marker、未知文件/格式、损坏、错主体/钥、撤权及取消均保留原件；失败 stage 不自动删除，内容若已写入均为密文。`JournalMigrationError` 的 `Published` 区分发布前失败与完整目标已发布后的失败；错误不代表目标不存在，也不允许换键重执行。原加密格式、原命名和原正文不变；首次打开旧版本 journal 可能增添空 `.journal-lock` 文件。

The first visible target is the complete snapshot: encrypted staging, validation and cold reopen all finish before an exclusive directory rename. Even an existing empty destination is refused. Default migration limits reject oversized sources without dropping witnesses. A missing marker, unknown file/version, corrupt record, wrong identity/key, revoked access or cancellation never reconstructs an empty source. Failed staging is retained and never contains plaintext record files. `JournalMigrationError.Published` distinguishes pre-publication failure from an error after a complete target became visible. Opening an older source can add only an empty `.journal-lock` file; existing record bytes and the encrypted-v1 format stay unchanged.

本版本 encrypted constructor、Claim、Complete 与迁移共享非等待 OS 排他锁；迁移期间另一同版本进程的开库/写操作失败，不能靠目录 hash 检查假装排他。原 plaintext journal 的 O_EXCL 并发行为不变。旧版本或绕过 SDK 的写入者不受新锁保护，必须由宿主先停写。成功后锁会释放，**SDK不会封存旧副本或自动切换两库**；两库之间没有事务，第一步成功、第二步失败时保留新第一库和两个原件，排查后只对尚未完成的介质使用新的空目标重试，不能重新覆盖第一库。双库验证完成才切换；开始在新库写入后，旧副本已落后，不能安全回滚或并行双写。AES-GCM 使用随机 nonce；本入口仍要求真正不同的新钥。

Encrypted constructors, Claim, Complete and migration now share a nonblocking OS lock. Same-version writers are excluded during migration; plaintext journal concurrency remains unchanged. Older or noncooperating writers must be stopped by the host. The lock is released afterward: **the SDK does not retire the old copy or transact across the two stores**. If only one step succeeds, retain that target and both originals, reconcile, and retry only the unfinished step into a fresh destination. Switch once after verifying both, never write to both copies. Once new writes begin, the old backup is stale and is not a safe rollback. GCM nonces are random, and this migration still requires a different key.

Demo 提供两个**离线模式**，均不连接 Serve、不创建会话或执行工具。离线取消仍以非零退出并报告保留目录，不能按普通 runner Ctrl+C 静默当成功。原钥仍放 `TANSR_MEMORY_KEY`，不同新钥放 `TANSR_MEMORY_NEW_KEY`（均为宿主提供64位hex；不要把钥写进命令行/文件）。复用原身份及实时授权文件；此时不需 session/binding-request。Windows使用同等绝对路径及私有ACL。

The demo exposes two offline steps without contacting Serve or invoking tools. Cancellation in either offline mode remains an error with a nonzero command exit and retained recovery information; only normal runner interruption is quiet. Supply the original `TANSR_MEMORY_KEY` and distinct `TANSR_MEMORY_NEW_KEY` through the host key facility, plus the original identity and live authorization file. No session or binding request is needed for these modes.

```sh
# Stop ALL writers before both commands; keep both original media and keys.
go run ./examples/go-memory -mode rekey-publication -executor AUTHORIZED_EXECUTOR \
  -file /private/tansr/memory.bin -journal /private/tansr/journal \
  -identity-file /private/tansr/memory-identity.json -access-file /private/tansr/current-scope.json \
  -target /private/tansr/memory-next.bin
go run ./examples/go-memory -mode rekey-journal -executor AUTHORIZED_EXECUTOR \
  -file /private/tansr/memory.bin -journal /private/tansr/journal \
  -identity-file /private/tansr/memory-identity.json -access-file /private/tansr/current-scope.json \
  -target /private/tansr/journal-next
# Verify both, then run normal -mode reopen using BOTH new paths and the new key.
```

目录发布使用Windows MoveFileEx（无覆盖标志）、Linux renameat2(RENAME_NOREPLACE)、Darwin renameatx_np(RENAME_EXCL)；不支持的系统/ABI/文件系统明确失败而不退化为覆盖式 rename。Linux实现当前支持amd64/arm64/riscv64/loong64；Darwin支持Go的amd64/arm64。此处列的是代码适配范围，实际本批只验Windows；跨编译不能代替Linux/macOS运行或物理掉电保证。

Directory publication uses OS no-replace primitives and fails explicitly when unsupported. The Linux adapter covers amd64/arm64/riscv64/loong64; Darwin covers Go's amd64/arm64. These are implementation targets, not runtime acceptance claims: this batch is tested on Windows only. Cross-builds do not prove Linux/macOS execution or physical power-loss durability.

## 本地候选验收记录 / local candidate evidence (2026-10-10)

本实现位于 `J:/tansr/worktrees/go-PST-05-20261010`，起点 main `60e6f6c385a05c3798931d35b5b763365d8b40f2`。只提交本地 `lane/go/PST-05-memory-publication`，固定主目录未切分支、未收编、未推送/发布。PST仍沿原6工程卡/36断言计数，本语言提交不独立关闭父卡。

实际 Windows、Go1.25.6、CGO=0：定向publication/journal/host/demo用例通过；真实本地Serve原UAPI链完成10次publication执行回执、设备密文落盘与冷重开一致。该夹具使用合成平台，无付费模型调用。原memory-only绑定closure少算专用管道由核心负责人修正，Go未移除closure、增加Read/Shell权限或改变冻结字段；Go夹具中的sessionContract误写也已改为原sdk1枚举。支持该链的Serve必须包含本轮核心修复；不冒称旧发行包已有修复。

集中全仓原门实际运行，**当前尚不能判全门通过**。首轮go test虽然exit0，JSON含原 `TestRealServeToolsDemo` 启动系统TEMP内go-tools.exe的Windows `Access is denied`，且integration输出不完整，因此未采信表面成功。只重跑该integration包，8个顶层测试中7个通过，唯一失败仍为相同cmd.Start拒绝；真实publication、原Archive两族/rebase、会话/执行器及chat/archive实际Demo均完整通过。以完整复验替换首轮integration后去重计856 pass（149顶层、707子项）、1 fail、0 skip、0 incomplete；三个无测试包不算测试跳过。未修改原断言或安全设置；只读Windows事件查询未能进一步归因，保留该环境执行门待恢复，不声称已排除产品相关性。

`go vet -p 1 ./...`、Windows/Linux/Darwin amd64 `go build -p 1 ./...`、39文件 `contractcheck -source`、`manifest2go -check`、gofmt及diff检查通过。CGO=0未跑race；跨编译不代Linux/macOS运行。本地 `go install ./examples/go-memory` 与安装后的.exe `-help` 均exit0，产物SHA256记录于local-install-receipt.json；没有对外发行。新Demo完整网络进程运行未单独验收，SDK相同公开装配已走真实Serve。

证据唯一目录：`J:/tansr/archive/PST-PLAN-20261009/dev-20261010-b2/go/`。`candidate-source-manifest.json`/最终清单登记源码，`serve-source-receipt.json`记录实际核心源hash，`go-test-final.jsonl`和`go-integration-recheck.jsonl`保留两轮原日志；`final-test-assessment.json`去重并保留红项，`final-gates.json`、`local-install-receipt.json`记录其他原门与本地安装物。后续恢复入口是同一候选下原 `TestRealServeToolsDemo`；排除启动拒绝并补齐原门之前不合并。Linux/macOS实际运行、race、正式发行及整个PST跨生态矩阵仍未代签。

The Windows candidate completed real local Serve publication (10 original execution receipts and encrypted cold reopen), but the full acceptance gate remains open: the existing go-tools demo executable was denied at Windows process startup twice. The complete integration rerun passed the other seven top-level cases. Deduplicated evidence is 856 pass, 1 fail, 0 skipped/incomplete. Vet, all three OS cross-builds, frozen contract/generated checks and local go-memory installation/help passed. No assertion or OS security setting was weakened. Linux/macOS execution, race (CGO unavailable), standalone new-demo network execution and release are not claimed.

## 第四批候选门 / fourth-batch candidate checks

本批复用原 publication.Rekey，新增 journal 显式迁移及两个离线 Demo 步骤。新增局部反例覆盖原键 pending/completed/failed/unknown、变授权同键拒绝、同版本跨进程锁、缺marker/错钥/错主体/损坏/孤儿回执/容量拒绝、迁移取消/撤权、晚到空目标不覆盖，以及已发布后出错的具名恢复状态。只读审查发现 Demo 原取消策略会吞离线迁移错误，已实跑红→绿；只对正常 runner 保留静默 Ctrl+C，两个离线模式保留错误链及路径并失败退出。存储原语/冻结wire不因该修复变化。

集中原门一次，881 Test（158顶层+723子项）=874 pass/7 fail/0 skip/0 incomplete；7红为当时仍在编辑的核心 `archive-host.ts:1159` 中 await 非async语法错误，导致原3个Archive顶层及4子项在Serve ready前失败。原 `TestRealServeToolsDemo` 本次PASS，上一批 Access denied 未复现且根因仍未知；真实 publication/执行器/会话均PASS。只读事件核对未发现对应拒绝证据，没有修改安全设置或原断言。核心负责人确认相关实现冻结后，只补这3个Archive原测试，7/7通过，没有重跑全池。合并Demo取消修复的局部结果后，最终去重882/882（159顶层+723子项），0 fail/skip/incomplete。复验前后3306份保守源码清单中仅两处ColdMaterialCoverage类型导出变化；原始哈希和转译结果均存档，运行JavaScript完全相同，不声称源码字节完全未变。

Demo取消窄修后，受影响整个Demo包5/5再次PASS（包括新增1项），vet及三OS编译通过；安装后的 go-memory.exe 实际完成两个离线换钥步骤，原新两介质分别以原/新钥重开通过。其余原门go vet、Windows/Linux/Darwin amd64全仓build、39文件源合同、生成、gofmt/diff及本地安装/help均通过。Linux/macOS实际运行、CGO=0的race、物理掉电/网络盘、新Demo完整Serve进程仍未验；不关闭父PST卡。

The fourth batch preserves the original publication migration and adds journal rotation plus offline demo steps. Review caught cancellation incorrectly being treated as command success; its new stage-created cancellation regression went red then green, and the full affected demo package passed 5/5. The one full run recorded 874 pass/7 fail out of 881 tests, with all seven failures coming from an in-progress core Archive host syntax error before Serve startup. The original go-tools real Serve case passed this time; the earlier Windows launch-denial cause remains unknown. Vet, all three OS cross-builds, frozen/generated checks and installed offline migration consumption passed. After the core correction, only the three original Archive cases were rerun: all seven top-level/subtest results passed. Combined with the affected demo rerun, the final deduplicated result is 882/882 (159 top-level, 723 subtests), with no failed/skipped/incomplete tests. Two type-only export additions appeared in the conservative 3306-file source audit; both versions transpiled to identical runtime JavaScript and the byte changes remain recorded. Linux/macOS runtime, race, physical power-loss and the parent PST acceptance are not claimed.

证据：`J:/tansr/archive/PST-PLAN-20261009/dev-20261010-b4/go/`，原全池日志 `go-test-final.jsonl`，取消红/绿 `demo-cancellation-red.log`/`demo-after-cancellation.jsonl`，安装消费 `installed-offline-consumer-final.log`，候选与完整归档摘要见 `candidate-source-manifest.json`、`report.md`、`manifest.json`。

## 第六批：执行事实与 Serve 受理分列

`Journal.Complete` / `Runner.Execute` 只证明原回执本地耐久，不代表 Serve 已受理。`Runner.Run` 先保存原回执再 `Submit`；发送失回后查询原 session/operation，只有远端 receipt 与本地完全一致才进入 `OnReceipt`。journal 当前没有单独持久化 ACK 位；它保存永久执行事实，不能从文件中的 completed 推导网络已确认。启动仅消费 Serve poll 返回的原操作，不遍历重提全部历史日志，也不自动恢复已关闭会话。

已关闭会话的 status 可能因原 runtime 不在而返回 source_unavailable；这不代表操作没有发生，也不能清 journal 或换 operation。显式恢复由可信控制端用原 session ID 调用原 Resume，再按原键查询；resume 不恢复旧 binding 的运行授权。没有合法恢复意图时不自动恢复全部历史会话，不以独立历史审计阻塞新会话。

本地停止与权限撤销不同。go-memory 在启动前仍拒绝已取消的 context；启动后 journal 的授权回调每次重读宿主授权文件并核对原 scope，局部停止不再阻止 Runner 使用原 WithoutCancel 提交已知永久回执。操作 context 仍控制新执行、轮询、迁移与网络；真实 Scope/authorizationRevision 变化、文件不可读或格式错误仍拒绝，不放宽身份/路径校验。

`Journal.Complete` and `Runner.Execute` establish a durable local fact, not Serve acceptance. `Runner.Run` submits that exact receipt and reconciles a lost response using the original session/operation status; only a matching remote receipt reaches `OnReceipt`. The current journal has no separate durable ACK marker. Startup follows Serve polling and does not sweep or automatically resume every historical session. A closed runtime's source_unavailable response is not proof of no execution. An authorized controller may explicitly resume the original session and reconcile original keys; resume does not restore binding authority.

Local cancellation stops new work but must not erase an already known outcome. The Demo now keeps journal authorization checks independent of cancellation after initialization, allowing the Runner's existing cancellation-independent journal commit. It still rereads and compares current authorization on each IO; changed authorization, malformed configuration and missing permission continue to fail closed. No wire, journal format or permanent receipt semantics changed.

本批起点 `e55bbeca2d119a952b508e26fad394726fd70f60`；固定 main `60e6f6c385a05c3798931d35b5b763365d8b40f2` 不动。取消真红保存在 `go-cancel-red.log`：原回调令已知回执的 Complete 返回 context canceled。修复后受影响 go-memory/executor 两包去重 **40 主项、45 子项通过，0失败/跳过**；随后只对加强了关卷/原路径原钥重开的 Demo 包补跑6项全部通过，完整 receipt 深比较一致且实际 authorizationRevision 变化仍拒绝。局部 vet、gofmt、diff-check 通过，未重复 B4 全池。B6 同候选封存 Serve 消费另记，不以本地门代签。证据归 `J:/tansr/archive/PST-PLAN-20261009/dev-20261010-b6/python-go/`，检查点见 `checkpoint.json`；无推送、合并或发行。

The cancellation regression was observed before the fix. The affected packages passed 40 top-level tests and 45 subtests with no failures or skips. The six Demo tests were then rerun after strengthening the regression to close/reopen the encrypted journal and compare the entire original receipt. Actual authorization changes still reject access. Local vet/format/diff checks passed; full B4 gates and platform execution were not repeated. Same-candidate B6 Serve consumption is recorded separately.

B6 最终封存包消费：原 `TestRealServeEncryptedMemoryPublication` **1 passed、0 failed/skipped**，8条真实 receipt、原加密介质冷重开一致，Runner 静止与 Host 自然退出。首轮在存储创建前 request_conflict：共享 Host 的 publicationIdentity 使用 nested scope，旧 Go 夹具只读 flat app/user。仅在 integration 夹具显式解码并核对两种布局，冲突仍拒绝；不改共享 Host、产品身份校验或冻结协议。首轮红与修正后绿分存 `go-publication-b6.jsonl` / `go-publication-b6-r2.jsonl`，局部 integration vet 通过。消费使用根 `sealed-packages.json` 的 Serve `0cba9f28bf6cf6ef86f83da62302aee56e10a3be2d819cc50e51d1bd6e23f88d`、SDK `048ca882b055ec8793deb9ea3e5ca2f6549751c098f65dbe79c39d5291611d0c` 及原 API-client 包；工装 SHA256 `523660803c7fe9f878f0de64f3adf68db0089c0976560b40221e7e704443b053`，共享安装只读、数据用本次系统 tmp。无模型提示请求；未运行 go-memory 真二进制的新 Serve 闭会话恢复或多OS，不以本组代签这些边界。

Final sealed B6 consumption passed the original publication integration: one test, eight actual receipts, identical encrypted storage after cold reopen, and graceful resource shutdown. The first run failed before storage creation because the old test fixture decoded only flat identity fields. The fixture now explicitly accepts the shared host's nested scope and rejects conflicting ownership; product validation and the frozen wire are unchanged. Both logs are retained. This does not claim a new real-process go-memory Demo scenario, closed-session recovery, or multi-OS execution.


## 第八批：永久回执孤儿保护与加密安装消费

本批从 `dccd25be4e426601e0ed121d93d4517f1fb6cdac` 接续，仅 `executor/journal.go` 有产品差量。原 `Claim` 在原 `.receipt` 仍存、对应 `.claim` 缺失时会重新创建 claim 并返回 Claimed=true，可能把永久 unknown/终态当作未执行。新增明文/加密两格先实跑原红；修复在创建前检查同键永久 receipt，存在、不可读或损坏均保守返回 outcome unknown，不重建 claim、不改 receipt。原并发 O_EXCL、原键摘要、当前 scope、owner、wire 和永久事实格式不变。损坏/错 AAD/截断/plaintext envelope 拒绝及实际临时快照密文扫描同时补入原测试。

本批原本地门一次顺序执行：`go test -p 1 ./... -count=1 -json` 为 **157 主项、726 子项通过**，0失败；原真实 Serve 环境未提供的 **6主项、4子项跳过**单列，不当通过。vet、Windows/Linux/Darwin amd64 build、39份冻结合同对照、生成检查、gofmt均exit0；CGO=0，未跑race。三OS build不是三OS运行。

本地模块 ZIP `v0.0.0-pstb8` 经 file GOPROXY 导入独立 GOMODCACHE，无 replace、无网络 registry；公开消费者实际完成六动作、敏感执行回执加密、原路径原钥重开及孤儿receipt拒绝重执。它是未发布内部候选包，不是公开发行。另复用根B8统一安装物 `serve-demo-candidate-UCKa95`，原 `TestRealServeEncryptedMemoryPublication` **1/1通过**、8条真实回执、加密冷开一致、模型调用0；原cleanup等待Host自然exit0并释放自有临时根。首次工装误用了不存在的Node路径，保留启动红，改为已安装 `C:/nvm4w/nodejs/node.exe` 后同一原单例通过；SDK/共享Host未为此改动。

完整证据在 `J:/tansr/archive/PST-PLAN-20261009/dev-20261010-b8/languages/go/`：`orphan-receipt-red.jsonl`、`journal-green.jsonl`、`unit.log`、`unit-summary.json`、`gates.json`、`package-manifest.json`、`installed-module.log`、`serve-publication-b8-r2.jsonl`。原A25—30逐格索引在同级 `pst05-functional-assessment.json`，Archive页链/附件/pending ACK/coverage沿未改实现的既有真实回执独列，不以publication回执代签。只本地提交原PST分支，不合main、不push、不改冻结schema/lock；九端总断言由根唯一结算。Linux/macOS实机、物理掉电和正式发行未在本批执行。

The B8 fix prevents a surviving permanent receipt from authorizing execution again when its claim is missing. Both plaintext and encrypted red cases are retained. Local gates, an offline module-cache consumer, and the shared sealed B8 Serve publication test passed. The ordinary local gate's ten environment-dependent Serve entries remain explicitly skipped; they are not added to the dedicated real-Serve result. No wire, owner policy, or receipt format changed.

## 第九批：显式 TerminalPersistence v1

新增 `terminalpersistence`（Go）/`tansr_sdk.terminal_persistence`（Python）独立布局，只在可信宿主显式选择 `terminal-persistence-v1` 时装配。旧六动作、原 39 项锁、terminal-services-v1 外层与原 owner/unknown 语义不变。新工具名为 `TansrTerminalPersistenceV1`，摘要 `33029a264edf81f3fda2a13fc382403d0cd7ffefa1f38088bb9366f387a13587`；schema/golden 是经批准的独立 sidecar。正文是任意字节，Root 身份只含 app/endUser/source/generation/domain 五字段。

新 FileStore 用一次 AES-GCM 快照与原原子 replace/介质锁共同提交 Root、永久双键索引、transfer 原键结果和计数。只回收当前 Root、全部 staging base/已收对象均不再引用的正文块及描述页；永久索引 value、transfer 结果不回收，旧 Root 的 query 不承诺历史正文仍可 read。读和 lookup 必须固定当前 commitRoot，根变返回原冲突。原 owner 不符只可由可信 query-only 恢复回调准许原键观察，不授 put/commit。提交开始后取消、撤权或回执失回保持 unknown，需原路径/钥重开和原键 query；不自动重试 begin、换 ID 或清未决。

实际默认/最高宿主配额为 active=8、staging=16MiB、receiptEntries=8192、transferFacts=4096、objects=16384、retainedBytes=32MiB；用户只能降低，head 返回真实值。独立密文快照帽为 128MiB，另留原临时文件空间，不宣称逻辑配额等于磁盘预留。每次写仍重写整个快照、冷开审计整个布局，未声称百万项/O(1)/性能达标。原有限钥次数/字节帽和未决必要写预留保留，帽耗尽拒新接纳并保原事实；新格式尚未提供 copy/轮钥或旧格式自动转换，不能套用旧六动作的迁移入口。无受支持备份、任意 delete、跨机接管、回滚检测或掉电保证。

Demo 保留旧 profile 默认值，新增 `--profile terminal-persistence-v1` 显式路径；新介质与 encrypted execution journal 使用两把独立 32 字节钥。配置 source/domain 来自可信宿主，不能采信网络正文来授恢复权。新格式不接受旧 copy/rekey Demo 模式。终端只实现机械存储，消费/归档业务策略仍在 Serve。

本批原门与安装消费回执归 `archive/PST-PLAN-20261009/dev-20261010-b9/python-go`；Go 的新/旧真实 HTTP 各 1/1 属已提交集成 `4d7a8d3`（`b9/go-integration/report.md`），Python 新 profile 实际 HTTP、Linux/macOS 运行、跨平台钥托管尚未验证。交付只本地提交，不推送、合并或发布；父卡与36断言仍由根统一结算。
