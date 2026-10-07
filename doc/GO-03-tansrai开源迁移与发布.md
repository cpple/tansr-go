# GO-03 tansrai 开源迁移与发布

2026-10-07。用户要求将 Go SDK / Demo 以 MIT 开源到 `tansrai`。本卡覆盖发布账号归属、Go Modules 路径、文档及公开消费，一张工程卡，不拆分原 GO-01 / GO-02 的完成口径。

## 范围与基线

- 原仓 `cpple/tansr-go` 已公开，起点 main `91f1fe9be3ce99120e1fce7e28ae1f1f29f77486`。
- 目标 `tansrai/tansr-go`；`tansrai` 是独立发布账号，与现有 .NET SDK 发布账号一致。
- 新模块为 `github.com/tansrai/tansr-go`，候选版本 `v0.3.0`。改变模块身份需要新版本；不覆盖旧 `v0.1.0` / `v0.2.0` 标签或重写历史。
- 本地开发树 `J:/tansr/worktrees/go-GO-03-tansrai`，分支 `lane/go/GO-03-tansrai`；证据统一在 `J:/tansr/archive/GO-03-tansrai-20261007`。固定 Go 仓保持 main。
- 沿用冻结 r7 / 81操作 / 11族 / 39文件，不改 API、运行行为、存储格式或冻结文件字节。
- Go SDK、Demo、测试、SDK 文档及已授权平层机器资产采用 MIT；`contract/reference/` 继续保留各自来源许可，详见 [NOTICE](../NOTICE.md)。本次不把 Serve/kernel 或专有参考实现改授 MIT，不上传私有 Serve 验收 bundle 或凭据。

## 执行清单

- [x] 核对目标账号、当前公开仓、许可范围和已有版本。
- [x] 将模块声明、Go imports、当前开发手册和 Demo 安装统一为新路径，保留历史版本说明。
- [x] 完成本地格式、vet、完整 race、原构建、generated、冻结合同及真实 Serve 集中门。
- [ ] 按用户明确选择转移原仓、保留历史；目标账号接受后完成公开仓与主线推送。仅保留一个仓库，旧地址跳转不视为副本，不对跳转地址执行删除。
- [ ] 创建不可覆盖的 v0.3.0 标签和发行说明，完成公开 Go Modules 下载、sumdb校验、无replace示例消费及pkg.go.dev展示。
- [ ] 回填证据，清理已合并推送且无活动依赖的工作树。

## 对抗验收

| 断言 | 完成条件 | 状态 |
|---|---|---|
| 模块身份一致 | go.mod、所有 Go import、生成器及当前教程相同；旧新类型不混用 | 通过；37 个 Go/go.mod 文件仅模块路径替换 |
| 行为与合同不回归 | 原测试/race/真实Serve与Demo通过，39份冻结字节不变 | 通过；134 个顶层测试、705 个子测试，失败/跳过/race 均为 0 |
| 许可与发布边界 | 根MIT及来源许可准确，公开文件不含私有bundle/密钥；旧标签不覆盖 | 待验 |
| 主线与远端 | 目标公开仓main精确SHA确认，仅推主分支，真实记录CI配置 | 待验 |
| Go Modules与文档 | 新路径固定版本在独立项目下载、校验、构建运行，Go文档可见 | 待验 |

## 当前进度

工程卡 **0/1 完成，剩余1，0%**；对抗验收 **2/5**。尚未发布新路径，不能把本地模块替换或原路径v0.2.0实证写成新渠道成功。

用户已明确授权“转移原仓到 tansrai，保留历史”，并要求只保留一份。GitHub 转移请求已受理，仓库 ID 为 `1399591375`；目标为个人账号，正在等待其确认邮件。当前查询仍属 `cpple`，目标地址尚未建立；没有创建副本，也没有删除仓库。

本地集中门证据位于 `J:/tansr/archive/GO-03-tansrai-20261007/gates/receipt.json`、`test-summary.json`。Go 1.25.6 / Windows amd64，`gofmt`、vet、完整 race、构建、生成校验、39 份冻结合同与上游对照均通过；真实本地 Serve 与三个 Demo 全部通过（合成模型和认证，不是付费模型验收）。Windows/Linux/macOS amd64 交叉构建通过；原 v0.2.0 三系统原生运行记录保留在 GO-02，本次不把交叉构建改写成新一轮原生执行。
