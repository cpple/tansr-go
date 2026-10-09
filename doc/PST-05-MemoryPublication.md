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
