/**
 * UAPI-01 阶段三 — `/api` SSE 统一事件包络(RFC-UAPI-1 §1.5;显式协商,旧帧字节不变)。
 *
 * 协商(请求头 `tansr-event-envelope`):
 * - 缺席 → 本模块零介入:响应对象不打补丁,帧字节与旧前缀逐字节相同(对抗单 A10 字节纪律);
 * - `unified-v1` → 对 `ServerResponse` 装写拦截;**仅当**响应头以 `text/event-stream` 出线时生效,并回响
 *   `tansr-event-envelope: unified-v1`(客户端据此确认协商成功;无回响 = 未协商,不得自行假设);非 SSE 响应
 *   (JSON 体、错误信封)不受影响、不回响——头只表达“若为事件流则要包络”的意图,出现在非 SSE 路由上被忽略;
 * - 词表外的值 → 门面答 `400 invalid_request`(先鉴权再作答,与 `tansr-session-family` 同律;本模块只判定,不作答)。
 *
 * 包络(每个可解析为 JSON 对象的 `data:` 载荷 → 一行 `data:`;wire 定形 **7 键**、键序固定,方案 §0 D18,2026-10-01):
 * `{ contract:'unified-v1', eventId, domain, type, cursorSet:{ eventCursor, archiveCoverage, outputWatermark,
 *    materialConsumed, ackReceipt }, terminalStatus, raw }`
 * - `eventId` = 本帧 SSE `id:`(缺席 null;与 `cursorSet.eventCursor` 同源,前者是帧身份、后者是续订游标位);
 * - 不出 `seq`(流位置由 `cursorSet.eventCursor` 表达)、不出 `payload`(`raw` 恒为原事件对象,按 `type` 解析);
 * - `raw` = 原 `data:` 文本**原样拼入**(不 parse→stringify 往返,字节不改;未知事件类型照常入 raw,A12);
 * - `type` = SSE `event:` 字段,缺席取 `raw.type` / `raw.eventType`(字符串),再缺席 null;
 * - `cursorSet` 五位分离、不互换:`eventCursor` = 本帧 SSE `id:`(缺席 null,不继承上一帧——各位只在自身合同条件
 *   满足后推进);其余位只在该域合同已明确处填值,见 `deriveCursorSet`(取值来源逐项登记于接线报告);
 * - `terminalStatus` 仅当原事件明确表达终态时非 null;SSE EOF / 连接结束恒不产生终态帧;
 * - `id:` / `event:` / `retry:` / 未知字段行与注释(心跳)帧原样保留;无法解析为 JSON 对象的 `data:`(首帧
 *   `retry` 的空 data、非 JSON 文本、JSON 标量/数组)整帧原样透传,恒不丢帧。
 *
 * 传输:按 SSE 规范切帧(空行终止;行尾 LF / CRLF / CR 皆可),跨 `res.write` 边界安全——只暂存**一个不完整帧**
 * 的残留(不缓冲整个流;残留超过 `maxCarryBytes` 即原样透传放弃对该帧包络);`write` 返回值与 `drain` 语义原样
 * 透传(无可写字节时按 `writableNeedDrain` 如实作答);`end` 时把残留不完整帧原样冲出;不改变心跳节奏(注释帧
 * 直接透传)。
 */
import type { IncomingMessage, ServerResponse } from 'node:http';
import { UNIFIED_CONTRACT, type ApiDomain } from './route-table.js';

/** 请求头(协商)与响应头(回响)同名 */
export const EVENT_ENVELOPE_HEADER = 'tansr-event-envelope';
/** 协商词表(当前唯一值) */
export const EVENT_ENVELOPE_CONTRACT = 'unified-v1';

/** 包络的域(门面路由表所归的域;`discovery` 无事件流) */
export type EventEnvelopeDomain = Exclude<ApiDomain, 'discovery'>;

export type EventTerminalStatus = 'accepted' | 'completed' | 'aborted' | 'unknown';

/** 五个游标位(RFC-UAPI-1 §1.5;不适用位为 null) */
export interface EventCursorSet {
  /** 事件流位置 = 本帧 SSE `id:`(Last-Event-ID 续订游标) */
  readonly eventCursor: string | null;
  /** 档案已覆盖序列:`archive.status.payload.acknowledgedCoverage`(Coverage 对象,含 headDigest) */
  readonly archiveCoverage: Readonly<Record<string, unknown>> | null;
  /** 终端输出水位:`output.block.block.seq`(tool-output 以 afterSeq 续订的序列) */
  readonly outputWatermark: string | null;
  /** 材料已被核心消费:`material.status.payload.state === 'core-consumed'` 时的 `materialRequestId`(≠ received) */
  readonly materialConsumed: string | null;
  /** ACK 回执:事件流内无回执载体(回执经 HTTP 响应 / operations 查询),恒 null */
  readonly ackReceipt: string | null;
}

/** wire 7 键(D18 定形;键序即 wire 序) */
export const EVENT_ENVELOPE_KEYS = Object.freeze(['contract', 'eventId', 'domain', 'type', 'cursorSet', 'terminalStatus', 'raw'] as const);

export interface UnifiedEventEnvelope {
  readonly contract: typeof UNIFIED_CONTRACT;
  /** 本帧 SSE `id:`(缺席 null) */
  readonly eventId: string | null;
  readonly domain: EventEnvelopeDomain;
  readonly type: string | null;
  readonly cursorSet: EventCursorSet;
  readonly terminalStatus: EventTerminalStatus | null;
  /** 原事件对象(wire 上为原 `data:` 文本原样拼入) */
  readonly raw: Readonly<Record<string, unknown>>;
}

export type EventEnvelopeNegotiation = 'absent' | 'unified-v1' | 'invalid';

/** 读协商头:缺席 / 词表内 / 词表外(重复头被 node 合并为 `a, b` → 词表外) */
export function negotiateEventEnvelope(req: IncomingMessage): EventEnvelopeNegotiation {
  const raw = req.headers[EVENT_ENVELOPE_HEADER];
  const value = Array.isArray(raw) ? (raw.length === 1 ? raw[0] : undefined) : raw;
  if (raw === undefined) return 'absent';
  return value === EVENT_ENVELOPE_CONTRACT ? 'unified-v1' : 'invalid';
}

const NULL_CURSORS: EventCursorSet = Object.freeze({
  eventCursor: null,
  archiveCoverage: null,
  outputWatermark: null,
  materialConsumed: null,
  ackReceipt: null,
});

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function stringOrNull(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

/**
 * 各域游标位与终态判定(只在该域合同已明确的位置填值;其余 null。规则逐项登记于接线报告“待会签”):
 * - session(sdk1 / 卸载族同帧形):`turn.completed` → completed,`turn.aborted` → aborted,`session.ended` → completed
 *   (观察的是事件流本身:会话结束 = 流完成;轮级完成只看 `type`);`turn.error{recoverable:false}` → aborted——
 *   /v2 契约里不可恢复错误即本轮终止(内核径随后还出 `turn.aborted`,二者同为终态、幂等;serve 驱动器
 *   `#onPumpFailure` 径只出这一帧,无此规则则统一客户端无终态可等);`turn.error{recoverable:true}` 与
 *   控制帧(server.*)不表达操作终态 → null;
 * - archive:`archive.status` → archiveCoverage = payload.acknowledgedCoverage;`material.status` → state
 *   received → accepted(只表示受理)/ core-consumed → completed + materialConsumed / rejected → aborted;
 *   `binding.status.payload.state === 'closed'` → completed;`archive.records-available` 的 publishedThroughSequence
 *   是发布水位而非覆盖,不填;
 * - terminal:执行器 `operation-cancelled` → aborted;输出 `output.block` → outputWatermark = block.seq;
 *   `output.status.status.state === 'complete'` → completed;`reconcile-required` / truncated / gap 等不裁定 → null。
 */
export function deriveCursorSet(
  domain: EventEnvelopeDomain,
  type: string | null,
  raw: Readonly<Record<string, unknown>>,
  eventId: string | null,
): { readonly cursorSet: EventCursorSet; readonly terminalStatus: EventTerminalStatus | null } {
  let cursorSet: EventCursorSet = eventId === null ? NULL_CURSORS : { ...NULL_CURSORS, eventCursor: eventId };
  let terminalStatus: EventTerminalStatus | null = null;
  const payload = isRecord(raw['payload']) ? raw['payload'] : undefined;
  switch (domain) {
    case 'session':
      if (type === 'turn.completed' || type === 'session.ended') terminalStatus = 'completed';
      else if (type === 'turn.aborted') terminalStatus = 'aborted';
      else if (type === 'turn.error' && raw['recoverable'] === false) terminalStatus = 'aborted';
      break;
    case 'archive':
      if (type === 'archive.status' && payload !== undefined && isRecord(payload['acknowledgedCoverage'])) {
        cursorSet = { ...cursorSet, archiveCoverage: payload['acknowledgedCoverage'] };
      } else if (type === 'material.status' && payload !== undefined) {
        const state = payload['state'];
        if (state === 'received') terminalStatus = 'accepted';
        else if (state === 'core-consumed') {
          terminalStatus = 'completed';
          cursorSet = { ...cursorSet, materialConsumed: stringOrNull(payload['materialRequestId']) };
        } else if (state === 'rejected') terminalStatus = 'aborted';
      } else if (type === 'binding.status' && payload !== undefined && payload['state'] === 'closed') {
        terminalStatus = 'completed';
      }
      break;
    case 'terminal':
      if (type === 'operation-cancelled') terminalStatus = 'aborted';
      else if (type === 'output.block' && isRecord(raw['block'])) {
        cursorSet = { ...cursorSet, outputWatermark: stringOrNull(raw['block']['seq']) };
      } else if (type === 'output.status' && isRecord(raw['status']) && raw['status']['state'] === 'complete') {
        terminalStatus = 'completed';
      }
      break;
    default:
      break;
  }
  return { cursorSet, terminalStatus };
}

// ———— 帧解析与改写 ————

const LF = 0x0a;
const CR = 0x0d;

interface SseLine {
  readonly text: string;
  /** 行终止符原文('\n' / '\r\n' / '\r';帧末空行亦带) */
  readonly eol: string;
}

/** 按 SSE 规范切行(LF / CRLF / CR),保留各行终止符原文;帧文本以空行(含其终止符)收尾 */
function splitLines(frame: string): SseLine[] {
  const lines: SseLine[] = [];
  let start = 0;
  for (let i = 0; i < frame.length; i += 1) {
    const code = frame.charCodeAt(i);
    if (code === LF) {
      lines.push({ text: frame.slice(start, i), eol: '\n' });
      start = i + 1;
    } else if (code === CR) {
      const crlf = frame.charCodeAt(i + 1) === LF;
      lines.push({ text: frame.slice(start, i), eol: crlf ? '\r\n' : '\r' });
      if (crlf) i += 1;
      start = i + 1;
    }
  }
  if (start < frame.length) lines.push({ text: frame.slice(start), eol: '' });
  return lines;
}

/** SSE 字段行 → (field, value):`:` 起首为注释;无冒号整行为字段名、值为空;值去掉恰一个前导空格 */
function parseField(line: string): { readonly field: string; readonly value: string } | 'comment' {
  if (line.startsWith(':')) return 'comment';
  const colon = line.indexOf(':');
  if (colon < 0) return { field: line, value: '' };
  const rawValue = line.slice(colon + 1);
  return { field: line.slice(0, colon), value: rawValue.startsWith(' ') ? rawValue.slice(1) : rawValue };
}

/**
 * 改写一帧(含终止空行):`data:` 可解析为 JSON 对象 → 以包络替换全部 data 行(落在首个 data 行原位、沿用其行尾);
 * 否则原字节透传。返回原 Buffer 本体即“未改写”。
 */
export function transformSseFrame(frame: Buffer, domain: EventEnvelopeDomain): Buffer {
  const lines = splitLines(frame.toString('utf8'));
  let eventName: string | undefined;
  let eventId: string | undefined;
  const dataLines = new Set<number>();
  const dataValues: string[] = [];
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index] as SseLine;
    if (line.text === '') continue;
    const parsed = parseField(line.text);
    if (parsed === 'comment') continue;
    if (parsed.field === 'data') {
      dataLines.add(index);
      dataValues.push(parsed.value);
    } else if (parsed.field === 'event') eventName = parsed.value;
    else if (parsed.field === 'id' && !parsed.value.includes('\u0000')) eventId = parsed.value;
  }
  if (dataLines.size === 0) return frame;
  const dataText = dataValues.join('\n');
  let raw: unknown;
  try {
    raw = JSON.parse(dataText);
  } catch {
    return frame;
  }
  if (!isRecord(raw)) return frame;
  const type = eventName ?? stringOrNull(raw['type']) ?? stringOrNull(raw['eventType']);
  const id = eventId ?? null;
  const derived = deriveCursorSet(domain, type, raw, id);
  // raw 以原 data 文本原样拼入(不往返编码);包络文本中的换行只可能来自多行 data 的拼接,重新拆回多行 data:
  // 键序 = EVENT_ENVELOPE_KEYS(contract, eventId, domain, type, cursorSet, terminalStatus, raw)
  const envelope = `{"contract":${JSON.stringify(UNIFIED_CONTRACT)},"eventId":${JSON.stringify(id)},"domain":${JSON.stringify(domain)},"type":${JSON.stringify(type)}` +
    `,"cursorSet":${JSON.stringify(derived.cursorSet)},"terminalStatus":${JSON.stringify(derived.terminalStatus)},"raw":${dataText}}`;
  const firstData = Math.min(...dataLines);
  const eol = (lines[firstData] as SseLine).eol === '' ? '\n' : (lines[firstData] as SseLine).eol;
  let out = '';
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index] as SseLine;
    if (index === firstData) {
      for (const part of envelope.split('\n')) out += `data: ${part}${eol}`;
    } else if (!dataLines.has(index)) {
      out += line.text + line.eol;
    }
  }
  return Buffer.from(out, 'utf8');
}

export interface SseEnvelopeTransformerOptions {
  readonly domain: EventEnvelopeDomain;
  /** 残留不完整帧的内存上限(字节);超出即原样透传放弃对该帧包络(不丢帧)。缺省 1 MiB */
  readonly maxCarryBytes?: number;
}

const DEFAULT_MAX_CARRY_BYTES = 1 << 20;
const EMPTY = Buffer.alloc(0);

/**
 * 跨 chunk 边界安全的 SSE 帧切分 + 改写:`push` 返回可立即写出的字节(可能为空),只暂存尾部不完整帧;
 * `flush` 冲出残留(原样)。行尾 LF / CRLF / CR 皆识别;chunk 末尾孤立 `\r` 留待下一 chunk 判定是否 CRLF。
 * 残留越上限进入透传态:本帧余下字节立即原样写出直至终止空行(不丢帧、不再暂存),下一帧恢复改写。
 */
export class SseEnvelopeTransformer {
  readonly #domain: EventEnvelopeDomain;
  readonly #maxCarryBytes: number;
  #carry: Buffer = EMPTY;
  /** 相对残留起点的当前行首(负值 = 本行始于已透传的字节之中)与已扫描位置(不重复扫描) */
  #lineStart = 0;
  #scanned = 0;
  #passthrough = false;

  constructor(options: SseEnvelopeTransformerOptions) {
    this.#domain = options.domain;
    this.#maxCarryBytes = options.maxCarryBytes ?? DEFAULT_MAX_CARRY_BYTES;
  }

  /** 当前残留字节数(诊断 / 测试) */
  get carryBytes(): number {
    return this.#carry.length;
  }

  push(chunk: Buffer): Buffer {
    if (chunk.length === 0) return EMPTY;
    const buffer = this.#carry.length === 0 ? chunk : Buffer.concat([this.#carry, chunk]);
    const out: Buffer[] = [];
    let frameStart = 0;
    let lineStart = this.#lineStart;
    let i = this.#scanned;
    while (i < buffer.length) {
      const byte = buffer[i];
      let next: number;
      if (byte === LF) next = i + 1;
      else if (byte === CR) {
        if (i + 1 >= buffer.length) break; // CRLF 跨 chunk:留待下一 chunk 判定
        next = buffer[i + 1] === LF ? i + 2 : i + 1;
      } else {
        i += 1;
        continue;
      }
      const emptyLine = i === lineStart;
      lineStart = next;
      i = next;
      if (emptyLine) {
        const frame = buffer.subarray(frameStart, next);
        out.push(this.#passthrough ? frame : transformSseFrame(frame, this.#domain));
        this.#passthrough = false;
        frameStart = next;
      }
    }
    const rest = buffer.subarray(frameStart);
    if (this.#passthrough || rest.length > this.#maxCarryBytes) {
      // 透传态:残留立即原样写出;只保留尾部孤立 CR(CRLF 跨 chunk 待判)
      this.#passthrough = true;
      const keep = buffer.length - i;
      const flushed = rest.length - keep;
      if (flushed > 0) out.push(rest.subarray(0, flushed));
      this.#carry = keep > 0 ? Buffer.from(rest.subarray(flushed)) : EMPTY;
      this.#lineStart = lineStart - frameStart - flushed;
      this.#scanned = 0;
    } else {
      // 复制残留:不持有调用方 chunk 的底层内存
      this.#carry = rest.length === 0 ? EMPTY : Buffer.from(rest);
      this.#lineStart = lineStart - frameStart;
      this.#scanned = i - frameStart;
    }
    return out.length === 0 ? EMPTY : out.length === 1 ? (out[0] as Buffer) : Buffer.concat(out);
  }

  /** 收流:残留不完整帧原样冲出 */
  flush(): Buffer {
    const rest = this.#carry;
    this.#carry = EMPTY;
    this.#lineStart = 0;
    this.#scanned = 0;
    this.#passthrough = false;
    return rest;
  }
}

// ———— ServerResponse 写拦截 ————

type WriteCallback = (error?: Error | null) => void;

function toBuffer(chunk: unknown, encoding: string | undefined): Buffer | undefined {
  if (typeof chunk === 'string') return Buffer.from(chunk, (encoding as BufferEncoding | undefined) ?? 'utf8');
  if (Buffer.isBuffer(chunk)) return chunk;
  if (chunk instanceof Uint8Array) return Buffer.from(chunk.buffer, chunk.byteOffset, chunk.byteLength);
  return undefined;
}

/** writeHead 入参 / setHeader 面上的 content-type(头未发出前判定,顺序:显式入参 → setHeader) */
function contentTypeAtWriteHead(res: ServerResponse, args: readonly unknown[]): string | undefined {
  const candidate = typeof args[1] === 'string' ? args[2] : args[1];
  if (Array.isArray(candidate)) {
    for (let i = 0; i < candidate.length; i += 1) {
      const item: unknown = candidate[i];
      if (Array.isArray(item)) {
        if (String(item[0]).toLowerCase() === 'content-type') return String(item[1]);
      } else if (typeof item === 'string' && item.toLowerCase() === 'content-type') {
        return String(candidate[i + 1]);
      }
    }
  } else if (candidate !== undefined && candidate !== null && typeof candidate === 'object') {
    for (const [key, value] of Object.entries(candidate as Record<string, unknown>)) {
      if (key.toLowerCase() === 'content-type') return Array.isArray(value) ? String(value[0]) : String(value);
    }
  }
  const own = res.getHeader('content-type');
  if (typeof own === 'string') return own;
  if (Array.isArray(own)) return own[0];
  return undefined;
}

function isEventStream(contentType: string | undefined): boolean {
  return contentType !== undefined && /^\s*text\/event-stream(?:\s*;|\s*$)/i.test(contentType);
}

/**
 * 对一条即将委托域 handler 的响应装拦截(仅协商成功时调用):头出线瞬间识别 `text/event-stream` → 回响协商头并启用
 * 帧改写;其它响应零介入。实例级补丁叠在观测层的 writeHead 补丁之上(链式调用,互不感知)。
 */
export function installEventEnvelope(res: ServerResponse, domain: EventEnvelopeDomain): void {
  const originalWriteHead = res.writeHead;
  const originalWrite = res.write;
  const originalEnd = res.end;
  let decided = false;
  let transformer: SseEnvelopeTransformer | undefined;
  const decide = (args: readonly unknown[]): void => {
    if (decided || res.headersSent) return;
    decided = true;
    if (!isEventStream(contentTypeAtWriteHead(res, args))) return;
    res.setHeader(EVENT_ENVELOPE_HEADER, EVENT_ENVELOPE_CONTRACT);
    transformer = new SseEnvelopeTransformer({ domain });
  };
  res.writeHead = function envelopeWriteHead(this: ServerResponse, ...args: Parameters<ServerResponse['writeHead']>): ServerResponse {
    decide(args);
    return (originalWriteHead as (...a: unknown[]) => ServerResponse).apply(this, args);
  } as ServerResponse['writeHead'];
  res.write = function envelopeWrite(this: ServerResponse, chunk: unknown, encodingOrCallback?: unknown, callback?: unknown): boolean {
    if (!decided) decide([]);
    const write = originalWrite as (...a: unknown[]) => boolean;
    if (transformer === undefined) return write.call(this, chunk, encodingOrCallback, callback);
    const encoding = typeof encodingOrCallback === 'string' ? encodingOrCallback : undefined;
    const done = (typeof encodingOrCallback === 'function' ? encodingOrCallback : callback) as WriteCallback | undefined;
    const bytes = toBuffer(chunk, encoding);
    if (bytes === undefined) return write.call(this, chunk, encodingOrCallback, callback);
    const out = transformer.push(bytes);
    if (out.length === 0) {
      // 本次无完整帧可写:不向 socket 写空块;背压状态按响应当前水位如实作答
      if (done !== undefined) process.nextTick(done);
      return !this.writableNeedDrain;
    }
    return done === undefined ? write.call(this, out) : write.call(this, out, done);
  } as unknown as ServerResponse['write'];
  res.end = function envelopeEnd(this: ServerResponse, chunkOrCallback?: unknown, encodingOrCallback?: unknown, callback?: unknown): ServerResponse {
    if (!decided) decide([]);
    const end = originalEnd as (...a: unknown[]) => ServerResponse;
    if (transformer === undefined) return end.call(this, chunkOrCallback, encodingOrCallback, callback);
    const chunk = typeof chunkOrCallback === 'function' ? undefined : chunkOrCallback;
    const done = (typeof chunkOrCallback === 'function' ? chunkOrCallback : typeof encodingOrCallback === 'function' ? encodingOrCallback : callback) as
      | WriteCallback
      | undefined;
    const encoding = typeof encodingOrCallback === 'string' ? encodingOrCallback : undefined;
    const parts: Buffer[] = [];
    if (chunk !== undefined && chunk !== null) {
      const bytes = toBuffer(chunk, encoding);
      if (bytes === undefined) return end.call(this, chunkOrCallback, encodingOrCallback, callback);
      parts.push(transformer.push(bytes));
    }
    parts.push(transformer.flush());
    transformer = undefined;
    const out = Buffer.concat(parts);
    if (out.length === 0) return done === undefined ? end.call(this) : end.call(this, done);
    return done === undefined ? end.call(this, out) : end.call(this, out, done);
  } as unknown as ServerResponse['end'];
}

/**
 * 门面委托前调用:读协商头并按需装拦截。返回 `'invalid'` 时由门面答 400(本模块不作答);`'absent'` 零介入。
 */
export function armEventEnvelope(req: IncomingMessage, res: ServerResponse, domain: EventEnvelopeDomain): EventEnvelopeNegotiation {
  const negotiation = negotiateEventEnvelope(req);
  if (negotiation === 'unified-v1') installEventEnvelope(res, domain);
  return negotiation;
}
