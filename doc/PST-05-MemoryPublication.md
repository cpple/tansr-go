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
- `Rekey(sourceOptions, freshTargetPath, newKey)`独占重开源、一次加密提交复制全部正文/暂存/终态，新路径必须不存在。核验后由宿主显式切路径；源保留。journal是另一介质，仍需原钥；没有自动journal换钥/清理或跨介质事务。当前版本不提供跨设备同步、离线副本删除、长期transfer压实或完整设备备份编排。

The file store uses an exclusive OS lock and encrypted sync/replace snapshots. It preserves original transfer owners, staging and permanent committed/conflict results across process restarts; unknown transfers are never recreated. Bounds default to 4096 transfers, 8 MiB staging and a 32 MiB encrypted file. Full capacity fails explicitly without dropping idempotency witnesses. This implementation rewrites the bounded snapshot on each chunk; custom transactional stores can serve larger workloads.

AES-GCM protects the publication, staging, owner/receipt metadata and temporary files. The separate encrypted journal protects full execution receipts, including read bytes. Wrong keys, identity/AAD mismatch, corruption and revoked access fail closed and preserve originals. No physical power-loss or network-filesystem SLA is claimed. Back up closed media together with the original host-managed keys; old backups are not current authorization or proof that later commits never happened. `Rekey` copies the publication to a fresh path while retaining all transfer facts; the separate journal still needs its original key. Automatic journal key migration, cross-device synchronization and transfer compaction remain outside this implementation.

## 本地候选验收记录 / local candidate evidence (2026-10-10)

本实现位于 `J:/tansr/worktrees/go-PST-05-20261010`，起点 main `60e6f6c385a05c3798931d35b5b763365d8b40f2`。只提交本地 `lane/go/PST-05-memory-publication`，固定主目录未切分支、未收编、未推送/发布。PST仍沿原6工程卡/36断言计数，本语言提交不独立关闭父卡。

实际 Windows、Go1.25.6、CGO=0：定向publication/journal/host/demo用例通过；真实本地Serve原UAPI链完成10次publication执行回执、设备密文落盘与冷重开一致。该夹具使用合成平台，无付费模型调用。原memory-only绑定closure少算专用管道由核心负责人修正，Go未移除closure、增加Read/Shell权限或改变冻结字段；Go夹具中的sessionContract误写也已改为原sdk1枚举。支持该链的Serve必须包含本轮核心修复；不冒称旧发行包已有修复。

集中全仓原门实际运行，**当前尚不能判全门通过**。首轮go test虽然exit0，JSON含原 `TestRealServeToolsDemo` 启动系统TEMP内go-tools.exe的Windows `Access is denied`，且integration输出不完整，因此未采信表面成功。只重跑该integration包，8个顶层测试中7个通过，唯一失败仍为相同cmd.Start拒绝；真实publication、原Archive两族/rebase、会话/执行器及chat/archive实际Demo均完整通过。以完整复验替换首轮integration后去重计856 pass（149顶层、707子项）、1 fail、0 skip、0 incomplete；三个无测试包不算测试跳过。未修改原断言或安全设置；只读Windows事件查询未能进一步归因，保留该环境执行门待恢复，不声称已排除产品相关性。

`go vet -p 1 ./...`、Windows/Linux/Darwin amd64 `go build -p 1 ./...`、39文件 `contractcheck -source`、`manifest2go -check`、gofmt及diff检查通过。CGO=0未跑race；跨编译不代Linux/macOS运行。本地 `go install ./examples/go-memory` 与安装后的.exe `-help` 均exit0，产物SHA256记录于local-install-receipt.json；没有对外发行。新Demo完整网络进程运行未单独验收，SDK相同公开装配已走真实Serve。

证据唯一目录：`J:/tansr/archive/PST-PLAN-20261009/dev-20261010-b2/go/`。`candidate-source-manifest.json`/最终清单登记源码，`serve-source-receipt.json`记录实际核心源hash，`go-test-final.jsonl`和`go-integration-recheck.jsonl`保留两轮原日志；`final-test-assessment.json`去重并保留红项，`final-gates.json`、`local-install-receipt.json`记录其他原门与本地安装物。后续恢复入口是同一候选下原 `TestRealServeToolsDemo`；排除启动拒绝并补齐原门之前不合并。Linux/macOS实际运行、race、正式发行及整个PST跨生态矩阵仍未代签。

The Windows candidate completed real local Serve publication (10 original execution receipts and encrypted cold reopen), but the full acceptance gate remains open: the existing go-tools demo executable was denied at Windows process startup twice. The complete integration rerun passed the other seven top-level cases. Deduplicated evidence is 856 pass, 1 fail, 0 skipped/incomplete. Vet, all three OS cross-builds, frozen contract/generated checks and local go-memory installation/help passed. No assertion or OS security setting was weakened. Linux/macOS execution, race (CGO unavailable), standalone new-demo network execution and release are not claimed.
