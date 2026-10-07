# GO-03 tansrai 开源迁移与发布

2026-10-07。用户要求将 Go SDK / Demo 以 MIT 开源到 `tansrai`。本卡覆盖发布账号归属、Go Modules 路径、文档及公开消费，一张工程卡，不拆分原 GO-01 / GO-02 的完成口径。

## 范围与基线

- 原仓 `cpple/tansr-go` 已公开，起点 main `91f1fe9be3ce99120e1fce7e28ae1f1f29f77486`。
- 目标 `tansrai/tansr-go`；`tansrai` 是独立发布账号，与现有 .NET SDK 发布账号一致。
- 新模块为 `github.com/tansrai/tansr-go`，已发布版本 `v0.3.0`。改变模块身份需要新版本；不覆盖旧 `v0.1.0` / `v0.2.0` 标签或重写历史。
- 本地开发树 `J:/tansr/worktrees/go-GO-03-tansrai`，分支 `lane/go/GO-03-tansrai`；证据统一在 `J:/tansr/archive/GO-03-tansrai-20261007`。固定 Go 仓保持 main。
- 沿用冻结 r7 / 81操作 / 11族 / 39文件，不改 API、运行行为、存储格式或冻结文件字节。
- Go SDK、Demo、测试、SDK 文档及已授权平层机器资产采用 MIT；`contract/reference/` 继续保留各自来源许可，详见 [NOTICE](../NOTICE.md)。本次不把 Serve/kernel 或专有参考实现改授 MIT，不上传私有 Serve 验收 bundle 或凭据。

## 执行清单

- [x] 核对目标账号、当前公开仓、许可范围和已有版本。
- [x] 将模块声明、Go imports、当前开发手册和 Demo 安装统一为新路径，保留历史版本说明。
- [x] 完成本地格式、vet、完整 race、原构建、generated、冻结合同及真实 Serve 集中门。
- [x] 按用户明确选择转移原仓、保留历史；目标账号接受后完成公开仓与主线推送。仅保留一个仓库，旧地址跳转不视为副本，不对跳转地址执行删除。
- [x] 创建不可覆盖的 v0.3.0 标签和发行说明，完成公开 Go Modules 下载、sumdb校验、无replace示例消费及pkg.go.dev展示。
- [x] 回填发布证据并登记本批工作树退役；本提交收编并推送后执行，实际结果以 archive 中的 `cleanup.json` 为准。

## 对抗验收

| 断言 | 完成条件 | 状态 |
|---|---|---|
| 模块身份一致 | go.mod、所有 Go import、生成器及当前教程相同；旧新类型不混用 | 通过；37 个 Go/go.mod 文件仅模块路径替换 |
| 行为与合同不回归 | 原测试/race/真实Serve与Demo通过，39份冻结字节不变 | 通过；134 个顶层测试、705 个子测试，失败/跳过/race 均为 0 |
| 许可与发布边界 | 根MIT及来源许可准确，公开文件不含私有bundle/密钥；旧标签不覆盖 | 通过；既有公开内容迁移，原标签对象未变，未上传验收 bundle |
| 主线与远端 | 目标公开仓main精确SHA确认，仅推主分支，真实记录CI配置 | 通过；发行主线 `49f35ab`，远端工作流数量为 0，不宣称 CI 通过 |
| Go Modules与文档 | 新路径固定版本在独立项目下载、校验、构建运行，Go文档可见 | 通过；公开下载及校验、4份完整示例、真实Serve主例、3个Demo安装、pkg.go.dev MIT索引 |

## 当前进度

工程卡 **1/1 完成，剩余0，本轮新增关闭1，100%**；对抗验收 **5/5，通过率100%**。仅统计本卡，不重复累计 GO-01 / GO-02。

用户已明确授权“转移原仓到 tansrai，保留历史”，并要求只保留一份。目标账号已接受转移；新旧 API 地址均返回仓库 ID `1399591375` 与 `tansrai/tansr-go`。只存在一个仓库；未创建副本，未对旧地址执行删除。本地 origin 已统一为 `git@github.com:tansrai/tansr-go.git`。

本地集中门证据位于 `J:/tansr/archive/GO-03-tansrai-20261007/gates/receipt.json`、`test-summary.json`。Go 1.25.6 / Windows amd64，`gofmt`、vet、完整 race、构建、生成校验、39 份冻结合同与上游对照均通过；真实本地 Serve 与三个 Demo 全部通过（合成模型和认证，不是付费模型验收）。Windows/Linux/macOS amd64 交叉构建通过；原 v0.2.0 三系统原生运行记录保留在 GO-02，本次不把交叉构建改写成新一轮原生执行。

## 发行与公开消费结果

- [唯一源码仓](https://github.com/tansrai/tansr-go)；[v0.3.0 Release](https://github.com/tansrai/tansr-go/releases/tag/v0.3.0) 已于 `2026-10-07T14:33:41Z` 正式发布，无二进制附件、非预发行、非草稿。
- 发行提交 `49f35ab1a982c4dbf2a6176144c59ff18542a74a`；标签对象 `e00c2a32d81f7567e4d26412f1a663ceab201f12`。后续收口文档提交进入 main，不重打发行标签。
- `v0.1.0` 标签对象 `14b042e9e8bf4476ceba6587941c97c6250f18e1`，`v0.2.0` 标签对象 `609911dd4ba9f233dae64c3a8316fffbdd8c95ec`，转移前后不变。旧版本的旧模块身份继续作为历史保留；新路径应从 v0.3.0 开始消费。
- 独立临时项目使用 `GOPROXY=https://goproxy.io`、`GOSUMDB=sum.golang.org` 成功执行 `go get github.com/tansrai/tansr-go@v0.3.0`，没有本地 replace。下载 Origin 指向同一发行提交，`go mod verify` 通过。
- 模块校验和 `h1:boOwxuol2bo9TM6tgaVfg8yP+RXAq8Lfhji5nV52vPU=`，go.mod 校验和 `h1:QVoNYwexxKrrGj4M8sNc4+j34imBPOflTqrjk8stIAs=`。
- 4份完整 Markdown Go 示例针对新公开版本 build / vet 通过；主例连接本地真实 Serve，确认流式合成回复、轮完成及 Serve 干净退出。三个 Demo 均由新公开模块 `go install ...@v0.3.0` 安装，`-h` 启动通过。
- [pkg.go.dev](https://pkg.go.dev/github.com/tansrai/tansr-go@v0.3.0) 已显示新路径、v0.3.0、MIT、有效 go.mod 及包目录。最初索引前 404 和网络超时保留在原日志；不改写为通过。Demo 首次安装直连 sumdb 超时，改用已验证公开代理与同一 GOPATH 后通过，未关闭校验、未改产品。
- 证据：`release/transfer-receipt.json`、`remote-refs.txt`、`github-release.json`、`consumer-v0.3.0-fa801564c791450bac12e8447a484af1/`、`demo-install-r2/receipt.json`、`pkgsite-indexed.png`、`docs-check/examples/receipt.json`、`docs-check/runtime/receipt.json`。均位于本卡 archive 根；最终归档清单记录各文件大小、SHA256 和归属。
