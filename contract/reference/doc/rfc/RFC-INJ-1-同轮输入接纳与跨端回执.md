# RFC-INJ-1：同轮输入接纳与跨端回执

日期：2026-09-10。状态：本批宿主扩展实施合同；L0 协议变更仍为草案，不修改 packages/protocol。

## 动机

同一轮运行期间增加用户输入，需要把接纳回执与原 prompt 的完成结果分离。旧 SDK send:void、serve messages 202、ACP prompt 和 headless text stdin 保持原合同，新增入口明确协商，不隐式改变旧客户端行为。

## 决策

- 内核在 QueryHandle 增加可选 reserveUserInput/inputReceipts，绑定原 turn；内存接纳同步完成。状态 consumed 仅表示正文进入历史。
- SDK/serve 对外提供 submitInput、目标查询和状态查询；inputId 与目标 {turnId,historyEpoch} 分工明确。重复同体回原票，异体 conflict；鉴权和 owner 继续沿原会话能力执行。
- 本批 content 支持 text 和全部为 text 的 blocks；ack 默认 memory，durable 明确 unsupported。能力位必须反映真实支持。
- 回执是宿主扩展数据，不伪装 KernelEvent。终态原因不新增冻结枚举，宿主错误原因使用自己的联合类型。
- ACP 私有命名空间 `_tansr.com/session/` 提供 steer、input_status、input_capabilities；仅向协商该能力的客户端开放。标准 prompt busy、权限请求及原 prompt 的唯一完成应答保持。
- headless `--input-format ndjson` 仅配 `-p` 与 stream-json；控制帧 v=1，独立请求 id，method 为 steer/input_status/input_capabilities/cancel，params 遵守对应输入合同。ready 公布目标和能力；控制回执使用独立命名空间，不加 protocol 事件。EOF 不取消 query，终态后不新建 query。默认 text 模式不改变。
- 去重范围为 `(sessionId, historyEpoch, turnId, inputId)`，仅保留当前及最近结束轮；不是永久去重服务。相同目标/ID 的相同归一化正文返回原票，异体拒绝；不把旧票绑定到新轮。

## 已实施宿主合同

SDK/serve session 提供 `inputCapabilities()`、`getInputTarget()`、`submitInput()`、`getInputStatus(inputId,target)`。提交结构为 `{inputId,target:{turnId,historyEpoch},content:{text}|{blocks:[{t:'text',text}]},ack?:'memory'|'durable'}`；文本块以换行连接。回执包含 session/epoch/turn/input 身份、ordinal、revision、source、state、durability；consumed 仅说明入史。

serve v2 新增 `GET /v2/sessions/:sid/input-capabilities`、`POST /v2/sessions/:sid/inputs`、`GET /v2/sessions/:sid/inputs/:inputId?historyEpoch=...&turnId=...`。沿用原鉴权/owner，接纳 202；closed 或 input_conflict 为 409，injection_limit 为 429，其余业务拒绝 422，schema 错误 400。查询不存在回执为 404。旧 v1 和旧 v2 messages 形状保持，新回执路由只在 v2。

capabilities.version=1，包含 text/textBlocks/image/memoryAck/durableAck、receiptRetention='current-and-last-turn' 和 target。不支持该扩展的旧 handle 如实返回关闭能力；durable_unsupported 与 content_unsupported 不预留票、不降级。

strict追加作为用户消息进入原查询的后续历史，不覆盖或刷新系统提示词。平台P、宿主system（S）与systemAppend（A）的fallback/prepend组合及快照边界见[开发方案第4节](../report/同轮追加输入-开发方案-2026-09-10.md)：S/A在会话装配时固定，P及策略在下一显式新轮按原预检刷新；同轮补充不重装系统指令，也不构成权限授权。配置SessionStore不会把memory回执升级为durable。

headless 还提供 input_capabilities 方法；ready 披露 maxLineCodeUnits（不是 UTF-8 字节数），输出使用 `tansr.input.ready` 和 `tansr.input.response`。最终源码的 d.ts/exports 和双语手册作为可调用合同；发布前须验证配套版本，不据本 RFC 宣称已发布。

## 不采纳及后继

本批不修改 IR 原生音视频，不承诺模型看到/处理成功或工具副作用 exactly-once，不承诺进程重启恢复原 generator。durable 需后继原子存储 RFC。无 -p 的首帧 start 后继处理。

若未来要把此宿主扩展纳入 L0，按 AGENTS 的 RFC 提案、受影响泳道会签和版本流程另行处理；本批不能直接修改冻结协议。

## 验收和迁移

详见[20 项对抗矩阵](../report/同轮追加输入-对抗验收与问题台账-2026-09-10.md)。旧服务没有 capability 时客户端不能默默 cancel+重发模拟同轮追加。公开发布前确认新增 API 所属版本、双语示例与实际安装包一致。INJ产品修复已随cfe进入20abb；803d普通CI成功/Full Windows失败及8f本地首步失败作为历史保留，不代签后继。

当前196f1532已通过[原九步本地验收](../report/midturn-input-evidence-20260910/windows-resource-196f1532/final-host/verified-local.json)，并以[同一文件树](../report/midturn-input-evidence-20260910/main-integration-77361f1f.json)收编、只推送main77361f1f7dd90c3310b12bd968213130acb7fe30。普通CI34454773128为1/1、FullCI34454773109为7/7，[精确主线回执](../report/midturn-input-evidence-20260910/main-ci-77361f1f/verification-report.md)已核实；收编及自动化验收不等于npm/安装包公开发布。此次测试资源分区未修改本RFC的产品合同，durable/多媒体/真机及低阶取消预约保留量仍按各自后继卡处理。
