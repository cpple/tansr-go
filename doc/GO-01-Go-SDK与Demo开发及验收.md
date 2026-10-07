# GO-01 Go SDK 与 Demo 开发及验收

2026-10-07。承接用户“SDK2.0 & UAPI 开发完毕，继续 Go SDK / Go Demo”和“首先冻结合同”。本轮在已有 Go UAPI 骨架上实现可用的高层消费链，复用 r7 路由、canonical、SSE 与错误模型；不重写核心，不展开 Go WebUI、供应商模型、任意系统 shell 或正式发行。

## 固定交付范围

沿用一张工程父卡 **GO-01**，不按内部步骤不断增卡。目标是三类 Go 消费示例与其 SDK 支撑闭环：多轮会话；显式注册的业务工具；单端加密档案保存、校验及 ACK/材料交接。UAPI 已有 81 操作的通用调用能力继续保留；便捷 API 尚未覆盖的同步、保留策略、备份和高级缓存管理不宣称已达到 Node/Electron 完整体验。

| 写区 | 实施内容 | 完成条件 |
|---|---|---|
| contract、internal/contractlock、internal/wire | 冻结基线、漂移门、共用严格 schema 校验 | 与主线已完成合同一致，篡改/非法控制请求拒绝 |
| api、sse | 注入 HTTPClient 的重定向边界；流并发关闭与资源释放 | 不跨源携凭据跟随；取消可打断读取，Close 幂等 |
| session | 创建/附着/恢复、closure、消息、流、取消与人工交互 | 200/201及sessionId正确；成功、失败、取消、EOF区分 |
| executor | 注册/心跳/绑定/轮询、验证、执行与回执、持久执行记录 | 验证在执行前，未知结果不自动重做，无默认shell |
| archive | 页和对象完整性、加密本地保存、耐久后ACK、待决恢复、材料上传 | ACK不领先持久化，失回不改键、不重编码正文 |
| examples、integration | 会话、订单工具、档案三个命令行示例；真实Serve路由联调 | 新开发者可按README运行，失败不打印成功 |

环境：起点 `tansr-go main f0f7718`；开发树 `J:/tansr/worktrees/go-GO-01-sdk-demo`，分支 `lane/go/GO-01-sdk-demo`。共同证据目录 `J:/tansr/archive/GO-01-sdk-demo-20261007`；Go 构建缓存也放该任务目录（系统默认缓存访问失败，不改用户全局配置）。固定主仓继续跟踪 main。

## 执行顺序

- [x] 冻结合同并通过指纹/漂移检查，随后恢复产品实现。独立提交 `2ee3a90`；39份副本、原提交 Git blob 及上游源码一致，篡改反例通过。
- [x] 按上述独立写区并发实施，在 SDK 层共享传输、严格校验和语义，Demo 不复制内核。
- [x] 冻结一份 Go 实现候选，集中完成 Go 本地门和真实 Serve 合成场景；辅助 CLI 的全量门另行结算。
- [ ] 按下表回填实际证据、余缺与工程卡状态，再收编主线；远端推送及正式 Go tag 另按发布事实记录。

## 对抗式验收

| 编号 | 验收条件 | 状态/证据 |
|---|---|---|
| A01 | 锁定全部11族、81操作及源指纹；篡改/缺文件/路径越界拒绝 | 通过：contractlock 测试与 `contractcheck -source` |
| A02 | 原165统一金样、127canonical交叉向量、generated check不退化 | 通过：原金样测试及 manifest2go 检查 |
| A03 | schema约束、重复键、非法UTF-8、摘要不匹配拒绝 | 通过：internal/wire、canonical、api 与档案反例 |
| A04 | 注入HTTP客户端不跟随重定向，不改调用者客户端 | 通过：api/stream_lifecycle_test.go |
| A05 | 流并发关闭、取消阻塞读取、EOF/解析失败释放连接 | 通过：api 流生命周期与 SSE CR 开放连接回归；未运行 race |
| A06 | 创建/附着/恢复、closure变化拒绝越权，未知请求不自动重发 | 通过：session/api 反例及真实 Serve 多轮/恢复 |
| A07 | 多轮流式结果、审批/问题/中断；失败/取消/断流不当成功 | 通过：session、go-chat UI 测试与真实 Serve 取消/审批 |
| A08 | 工具scope/绑定/代际/digest/租约验证；重放与未知副作用安全 | 通过：executor 反例、受限状态回调与真实 Serve 工具只执行一次 |
| A09 | 输出分块与ACK/重传边界；无默认shell和宿主回落 | 通过：executor/output_test.go 合同级受控 HTTP；未宣称真实 shell 流联调 |
| A10 | 档案/附件字节及链校验，未落盘不得ACK | 通过：archive 反例与真实 Serve 两会话族 |
| A11 | 加密存储、错密钥/损坏拒绝、失回原ACK恢复、材料非自动consumed | 通过：archive 测试；真实 Serve 在提交后丢弃 ACK 响应，重开同加密文件恢复原请求 |
| A12 | 三个Demo可构建；真实Serve公开路由合成业务联调 | 通过：integration 实际启动三个 Go 命令二进制 |
| A13 | gofmt/vet/test/generated/lock及Windows/Linux/macOS编译；有CGO时race | 通过：下述集中门；本机无受支持 C 编译器，条件 race 门未运行 |

没有真实运行的项目不记通过。开发阶段仅做对应局部验证，最后共用一次全量池；不得以模拟HTTP夹具通过冒充真实Serve或真实模型费用验证。本批不需付费模型调用。

## 当前进度

工程卡：总数 **1**，完整完成 **0**，剩余 **1**，进度 **0%**。冻结为准备条件，不计作额外产品卡。原 SDK2/UAPI 卡片不重开、不改变分母。此数在集中验收与收编后更新。

## 联调发现与修复记录

| 问题 | 原因与处理 | 验收状态 |
|---|---|---|
| 首次设备绑定被拒 | 真实 Serve 的 UAPI closure 用“设备工具已 available”判断可否建立绑定，而 available 又要求已有绑定。只在内部增加基于当前应用/用户策略的绑定资格，保留原授权/执行器/工作区校验，不改冻结合同。CLI 修复树 `J:/tansr/worktrees/cli-GO-01-serve-bind`，起点同为 `83c64b2c` | 原始真 Serve 红例已存 `real-serve-integration-before-binding-fix.log`；73项受影响回归及全部Go实际联调通过，CLI全仓门另见其报告 |
| 流关闭并发与重定向 | 原底座的 injected HTTPClient 没有保持拒绝重定向；done/游标无同步且错误后未释放。已作最小修补 | api/sse局部通过 |
| CR 完整帧延迟 | 对 CR 结尾执行阻塞 Peek；改为下一行吞可选LF，完整帧立即可读 | 已复现红例并通过开放连接回归 |
| Executor 公共 Execute 借错租约 | 和Run并发时曾借用Run续租判断，却不受其失败取消；已分离监控调用并传播取消 | 并发租约失败反例通过 |
| History count-only | Go高层初稿拒绝Serve合法的limit=0 | 已修正并通过边界用例 |

辅助 CLI 树只处理完成 GO-01 必需的上游实现阻塞，不重开 SDK2/UAPI 原卡或更改协议状态标签。收编前仍须其原本地门，不绕开 UAPI 或启用旧路径求绿。

## 集中验收证据

Windows 主机、Go 1.25.6、Node 22.22.1、pnpm 11.3.0。Go 运行时和三个 Demo 均为标准库实现。实际 Serve 联调才使用本地 Node / CLI 源码；这不是 Go 产物运行依赖。日志均在上述唯一证据目录，归档指纹见 `evidence-manifest.json`。

| 命令/范围 | 结果 | 证据 |
|---|---|---|
| `go vet ./...` | exit 0 | go-vet.log |
| 含 `TANSR_GO_SERVE_CLI_ROOT` 的 `go test -json ./...` | exit 0；122 顶层、673 子测试通过；失败/测试跳过 0。另有3个包无测试文件，不算通过用例 | go-test-final.jsonl、go-test-summary.json |
| 候选最后两处校验调整后的 `go vet ./executor ./archive` 与 `go test ./executor ./archive -count=1` | exit 0；不与全仓重叠累加数量 | go-late-targeted.log |
| `CGO_ENABLED=0 go build ./...` | Windows amd64、Linux amd64/arm64、Darwin amd64/arm64 均 exit 0 | go-build-receipt.json |
| gofmt / diff / generated / frozen lock | 空格式差异，生成物最新，39份字节与原Git对象及实际Serve源码一致 | go-static-receipt.json |
| 实际 Serve 单组集成 | 全部通过；实际内核/路由，合成模型/认证/平台 | real-serve-integration-final.log |

本机缺少支持 `-race` 的 C 工具链，所以未运行 race；跨编译不冒充 Linux/macOS 系统运行验收。Go 仓当前没有 GitHub workflow，不把主线推送写成 CI 通过。真实业务工具全链已验；输出 writer 仅完成合同级验证，未实现 Go 原生任意 shell。档案冷存储夹具在进程内，不冒充生产存储或 Serve 进程重启恢复。

## 后续能力与发行边界

本轮 13 项验收通过只覆盖固定 GO-01 范围。后续的完整记忆发布、双副本/跨设备同步、retention/备份、自动 ACK rebase、Node SQLite 介质互通、原生系统资源工具和高级缓存编排未承诺实现；通用81操作入口保留。`archive.Store` 可以接开发者持久存储，但其原子提交/授权语义由适配者实现并验收。执行记录在Windows的掉电耐久性没有实证。

已有 `v0.1.0` 标签仍是历史骨架；本轮不创建新 tag、不发布、不部署。主线收编状态在最终结算回填，不能用候选通过冒充已发布。
