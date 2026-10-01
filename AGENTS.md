# tansr-go 协作入口

本仓是 tansr 统一 `/api` 合同的 Go SDK(UAPI-01 阶段四 U4-GO 骨架)。执行任务前读取 tansr-cli 仓的
`doc/工程工作纪律.md`、`doc/90-SDK技术手册.md` 第 16 章(§16.6 七条纪律)与 RFC-UAPI-1。

## 事实源(只读,路径以 tansr-cli 仓根为准)

| 本仓副本 | 事实源 | 用途 |
| --- | --- | --- |
| `contract/api-manifest.json` | `packages/server/contract/api-manifest.json` | **唯一**的操作目录事实源 → `api/operations_gen.go` |
| `contract/unified-v1.golden.json` | `doc/rfc/unified-v1.golden.json`(schema `doc/rfc/unified-v1.schema.json`) | `api` 类型形状与正负向量测试 |
| `contract/canonical-cross-vectors.json` | `scripts/api/fixtures/canonical-cross-vectors.json` | `canonical` 127 交叉向量 |
| `contract/sdk2-wire-v1.json` | `scripts/sdk2/fixtures/sdk2-wire-v1.json` | canonical 字节金样、路径段编码 |

来源提交与 sha256 登记在 `contract/PROVENANCE.json`;更新副本时同步更新该文件。

## 生成

```
go run ./internal/gen/manifest2go            # 重生成 api/operations_gen.go
go run ./internal/gen/manifest2go -check     # 校验已提交文件未过期
go test ./api -run TestOperationsGenerated   # 等价的 -check(go test 恒执行)
go test ./internal/manifestgen -update       # 宿主拒绝启动新编译可执行文件时的改写入口
```

- **禁止手写路径、方法、查询键、域名**:一切来自 manifest;`operations_gen.go` 不得手改。
- `api/schema.go` 的校验规则逐条对应 `unified-v1.schema.json` 的定义;改 schema 先改副本与 PROVENANCE,再跑 golden 测试。
- `EventEnvelope` 按 D18 七键出形并容忍过渡形(schema 九键 / 今日六键);U3-ALIGN 收口后收窄。

## 门禁

`gofmt -l .` 为空;`go vet ./...`;`go test ./...`(可用 CGO 时加 `-race`);
`GOOS=linux|darwin|windows GOARCH=amd64 go build ./...`。零第三方依赖(标准库;`golang.org/x/` 不引入除非必要)。

## 提交

格式 `type(scope): 具体变化 (UAPI-01)`;UTF-8、LF、无 BOM(`git commit -F`)。远端由主线在收编时创建;本仓不自行添加远端、不推送。
