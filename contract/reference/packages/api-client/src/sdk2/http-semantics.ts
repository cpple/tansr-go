/** 具名 schema 之外的有限交叉字段核对；真实权限及耐久事实仍归服务端／终端适配器。 */
import { clientFail, decodeBase64, encodeControl, equalControl, sha256, wireDateTime } from './wire-codec.js';
import { SDK2_LIMIT_RANGES } from './wire-limits-generated.js';
import type * as W from './wire-types-generated.js';
import { validTerminalToolInvocation } from '../terminal/background-profile.js';

const encoder = new TextEncoder();
const need = (condition: boolean): void => { if (!condition) clientFail(); };
const sequence = (value: string) => BigInt(value);
function limits(value: W.Sdk2Limits): void { need(value.inflightReserveBytes <= value.pendingBytes); }
function epoch(value: W.Sdk2EpochView | null, max?: number): void {
  if (!value) return; const duration = wireDateTime(value.expiresAt) - wireDateTime(value.issuedAt);
  need(duration > 0 && (max === undefined || duration <= max));
}
function unique(values: readonly string[]): void { need(new Set(values).size === values.length); }
function coverage(value: W.Sdk2Coverage): void { need(sequence(value.fromSequence) >= 1n && sequence(value.throughSequence) >= sequence(value.fromSequence)); }
/** 业务正文沿原 JSON 语义，不使用只允许非负整数的控制元数据编码器。 */
export function decodeExecutionToolObject(text: string): Record<string, unknown> {
  need(typeof text === 'string' && encoder.encode(text).byteLength <= 32768);
  let value: unknown;
  try { value = JSON.parse(text); } catch { return clientFail(); }
  need(value !== null && typeof value === 'object' && !Array.isArray(value));
  const pending = [{ value, depth: 0 }]; let nodes = 0;
  while (pending.length) {
    const next = pending.pop()!; need(next.depth <= 32 && ++nodes <= 32768);
    if (typeof next.value === 'number') need(Number.isFinite(next.value));
    else if (typeof next.value === 'string') encodeControl(next.value, 262144);
    else if (next.value && typeof next.value === 'object') {
      for (const [key, child] of Object.entries(next.value)) {
        encodeControl(key, 262144); pending.push({ value: child, depth: next.depth + 1 });
      }
    }
  }
  return value as Record<string, unknown>;
}
/** 零依赖端按原 /v2 ToolResultReceipt 的公开文本/图片白名单校验。 */
export function verifyExecutionToolReceipt(text: string): void {
  const value = decodeExecutionToolObject(text);
  if (value.status === 'error') { need(typeof value.message === 'string' && value.message.length >= 1 && value.message.length <= 4096); return; }
  need(value.status === 'ok' && Array.isArray(value.content) && value.content.length >= 1 && value.content.length <= 64 &&
    (value.isError === undefined || typeof value.isError === 'boolean'));
  for (const entry of value.content as unknown[]) {
    need(entry !== null && typeof entry === 'object' && !Array.isArray(entry));
    const item = entry as Record<string, unknown>;
    if (item.t === 'text') need(typeof item.text === 'string');
    else need(item.t === 'image' && typeof item.mime === 'string' && /^image\/(png|jpeg|webp|gif)$/.test(item.mime) && typeof item.data === 'string');
  }
}
function capRequested(input: W.Sdk2BindingCreateRequest): void {
  unique(input.requiredCapabilities); unique(input.optionalCapabilities);
  need(!input.requiredCapabilities.some(value => input.optionalCapabilities.includes(value)));
}
export async function verifyRequest(name: string | undefined, input: unknown, maximum: W.Sdk2Limits, check: () => void): Promise<void> {
  verifyExecutionRequest(name, input);
  if (name === 'BindingCreateRequest') capRequested(input as W.Sdk2BindingCreateRequest);
  if (name === 'ArchiveReadRequest') {
    const r = input as W.Sdk2ArchiveReadRequest; need(r.limit <= maximum.pageRecords && r.maxBytes <= maximum.pageBytes);
  } else if (name === 'ArtifactReadRequest') {
    const r = input as W.Sdk2ArtifactReadRequest; need(r.maxBytes <= maximum.chunkBytes);
  } else if (name === 'ArchiveAckRequest') {
    const r = input as W.Sdk2ArchiveAckRequest; coverage(r.coverage);
    need(sequence(r.coverage.throughSequence) - sequence(r.coverage.fromSequence) < 128n);
    unique(r.payloads.map(item => item.artifactId)); unique(r.attachments.map(item => item.artifactId));
    const hashes = new Map(r.payloads.map(item => [item.artifactId, item.sha256]));
    for (const item of r.attachments) if (hashes.has(item.artifactId)) need(hashes.get(item.artifactId) === item.sha256);
  } else if (name === 'MaterialUploadChunkRequest') {
    const r = input as W.Sdk2MaterialUploadChunkRequest, bytes = decodeBase64(r.base64);
    need(r.bytes <= maximum.materialChunkBytes && bytes.byteLength === r.bytes); check(); const hash = await sha256(bytes); check(); need(hash === r.chunkSha256);
  } else if (name === 'MaterialResponseRequest') {
    const r = input as W.Sdk2MaterialResponseRequest; unique(r.results.map(item => item.recordId));
    need(r.results.length <= maximum.materialCandidates);
  }
}
async function recordDigest(record: W.Sdk2ArchiveRecord, maximum: W.Sdk2Limits, check: () => void): Promise<void> {
  need(encoder.encode(encodeControl(record)).byteLength <= maximum.recordBytes);
  const { recordDigest: digest, ...metadata } = record;
  const bytes = encoder.encode('tansr.sdk2.record.v1\0' + encodeControl(metadata)); check(); const hash = await sha256(bytes); check(); need(hash === digest);
  if (record.sourceEventRange) need(record.sourceEventRange.firstSeq <= record.sourceEventRange.lastSeq);
  if (record.projection) coverage(record.projection.coverage);
}
export async function verifyResponse(name: string, output: unknown, input: unknown, scope: W.Sdk2Scope, maximum: W.Sdk2Limits, check: () => void): Promise<void> {
  await verifyExecutionResponse(name, output, input, scope);
  const request = input as Record<string, unknown>, result = output as Record<string, unknown>;
  if (typeof request.bindingId === 'string') need(result.bindingId === request.bindingId);
  if (request.generations && result.generations) need(equalControl(request.generations, result.generations));
  if (request.materialRequestId) need(result.materialRequestId === request.materialRequestId);
  if (name === 'CapabilitiesResponse') {
    const r = output as W.Sdk2CapabilitiesResponse; limits(r.limits); epoch(r.operationEpoch, r.limits.epochLifetimeMs);
    need(r.archiveAckFormats.length === (r.capabilities.includes('archive-transfer-v1') ? 1 : 0));
  } else if (name === 'BindingTargetView') {
    const r = output as W.Sdk2BindingTargetView; need(r.target.sessionId === request.sessionId); epoch(r.operationEpoch);
    if (r.bindingId === null) need(r.operationEpoch === null && r.revision === '0');
  } else if (name === 'BindingView') {
    const r = output as W.Sdk2BindingView;
    // 原创建重放保留旧授权修订；仅核当前 app/user，绝不将旧 revision 当当前权限。
    need(r.scope.applicationScopeId === scope.applicationScopeId && r.scope.endUserId === scope.endUserId);
    limits(r.limits); epoch(r.operationEpoch, r.limits.epochLifetimeMs);
    // 绑定只能收紧此前已发现/本地可兑现的帽，不能在协商后悄悄扩帽。
    for (const name of Object.keys(SDK2_LIMIT_RANGES) as (keyof W.Sdk2Limits)[]) need(r.limits[name] <= maximum[name]);
    unique(r.acceptedCapabilities); unique(r.rejectedCapabilities.map(item => item.capability));
    need(!r.rejectedCapabilities.some(item => r.acceptedCapabilities.includes(item.capability)));
    need(r.archiveAckFormat === (r.acceptedCapabilities.includes('archive-transfer-v1') ? 'split-receipts-v1' : null));
    if (request.target) {
      const create = input as W.Sdk2BindingCreateRequest; need(equalControl(r.target, create.target) && r.sourceId === create.archive.sourceId);
      need(create.requiredCapabilities.every(item => r.acceptedCapabilities.includes(item)));
      const asked = [...create.requiredCapabilities, ...create.optionalCapabilities];
      const decided = [...r.acceptedCapabilities, ...r.rejectedCapabilities.map(item => item.capability)];
      need(decided.length === asked.length && decided.every(item => asked.includes(item)));
    }
  } else if (name === 'ArchiveStatus') {
    const r = output as W.Sdk2ArchiveStatus;
    if (r.publishedThroughSequence !== null) need(sequence(r.publishedThroughSequence) >= 1n);
    if (r.releasableThroughSequence !== null) need(sequence(r.releasableThroughSequence) >= 1n);
    if (r.acknowledgedCoverage) {
      coverage(r.acknowledgedCoverage); need(r.publishedThroughSequence !== null && sequence(r.acknowledgedCoverage.throughSequence) <= sequence(r.publishedThroughSequence));
      if (r.releasableThroughSequence !== null) need(sequence(r.releasableThroughSequence) <= sequence(r.acknowledgedCoverage.throughSequence));
    } else need(r.releasableThroughSequence === null);
  } else if (name === 'ArchivePage') {
    const r = output as W.Sdk2ArchivePage, asked = input as W.Sdk2ArchiveReadRequest;
    need(r.records.length <= asked.limit && r.records.length <= maximum.pageRecords);
    let previous = asked.afterSequence === null ? 0n : sequence(asked.afterSequence), previousDigest: string | undefined;
    const objects = new Map<string, W.Sdk2ArtifactRef>(); const recordIds = new Set<string>();
    for (const record of r.records) {
      need(sequence(record.sequence) === previous + 1n && !recordIds.has(record.recordId)); recordIds.add(record.recordId);
      need(equalControl(record.target.generations, asked.generations));
      if (previous === 0n) need(record.predecessorDigest === '0'.repeat(64));
      if (previousDigest) need(record.predecessorDigest === previousDigest);
      for (const ref of [record.payload, ...record.attachments]) {
        need(ref.bytes <= maximum.attachmentBytes);
        const prior = objects.get(ref.artifactId); if (prior) need(equalControl(prior, ref)); else objects.set(ref.artifactId, ref);
      }
      await recordDigest(record, maximum, check); previous = sequence(record.sequence); previousDigest = record.recordDigest;
    }
    need(r.nextAfterSequence === (r.records.at(-1)?.sequence ?? asked.afterSequence));
    if (r.publishedThroughSequence === null) need(r.records.length === 0 && asked.afterSequence === null && r.complete);
    else {
      const published = sequence(r.publishedThroughSequence); need(published >= 1n && previous <= published);
      need(r.complete === (previous === published)); if (!r.complete) need(r.records.length > 0);
    }
  } else if (name === 'ArtifactChunk') {
    const r = output as W.Sdk2ArtifactChunk, asked = input as W.Sdk2ArtifactReadRequest, bytes = decodeBase64(r.base64);
    need(r.artifactId === asked.artifactId && r.offset === asked.offset && r.bytes <= asked.maxBytes && r.bytes <= maximum.chunkBytes &&
      r.totalBytes <= maximum.attachmentBytes && r.offset + r.bytes <= r.totalBytes && bytes.byteLength === r.bytes);
    check(); const hash = await sha256(bytes); check(); need(hash === r.chunkSha256);
    if (r.offset === 0 && r.bytes === r.totalBytes) need(r.sha256 === r.chunkSha256);
  } else if (name === 'MutationReceipt') {
    const r = output as W.Sdk2MutationReceipt; need(equalControl(r.request, request.request));
    const expected = request.operation ?? (request.coverage ? 'archive-ack' : 'binding-close'); need(r.operation === expected);
    if (!request.operation) {
      const { request: _identity, ...semantic } = request; void _identity;
      check(); const hash = await sha256(encoder.encode('tansr.sdk2.operation.v1\0' + encodeControl({
        scope: [scope.applicationScopeId, scope.endUserId], operation: expected, semantic,
      }, 1048576))); check(); need(r.semanticDigest === hash);
    }
  } else if (name === 'MaterialUploadStatus') {
    const r = output as W.Sdk2MaterialUploadStatus;
    need(r.artifact.artifactId === request.artifactId && r.artifact.bytes <= maximum.materialBytes && r.chunkBytes <= maximum.materialChunkBytes);
    if (request.sourceId) need(r.artifact.sourceId === request.sourceId);
    let sum = 0, previous = -1;
    for (const offset of r.receivedOffsets) {
      need(offset > previous && offset % r.chunkBytes === 0 && offset < r.artifact.bytes); previous = offset; sum += Math.min(r.chunkBytes, r.artifact.bytes - offset);
    }
    need(sum === r.receivedBytes && Math.ceil(r.artifact.bytes / r.chunkBytes) <= 16);
    need((r.state === 'committed') === (r.receivedBytes === r.artifact.bytes));
    if (typeof request.offset === 'number') {
      const asked = input as W.Sdk2MaterialUploadChunkRequest;
      need(asked.offset % r.chunkBytes === 0 && r.receivedOffsets.includes(asked.offset) &&
        asked.bytes === Math.min(r.chunkBytes, r.artifact.bytes - asked.offset));
      if (asked.offset === 0 && asked.bytes === r.artifact.bytes) need(r.artifact.sha256 === asked.chunkSha256);
    }
  } else if (name === 'MaterialReceipt') {
    const r = output as W.Sdk2MaterialReceipt; unique(r.acceptedRecordIds);
    if (r.state === 'pending') need(r.revision === '0' && r.acceptedRecordIds.length === 0);
    if (request.results) {
      const selected = (input as W.Sdk2MaterialResponseRequest).results;
      need(r.state === 'received' && r.revision !== '0' && r.acceptedRecordIds.length === selected.length &&
        r.acceptedRecordIds.every(id => selected.some(record => record.recordId === id)));
    }
  }
}

/** 新运行端合同仍在同一语义核验层，不改变旧档案合同的任何分支。 */
export function verifyExecutionRequest(name: string | undefined, input: unknown): void {
  if (name === 'SessionInitializeRequest') { const r = input as W.Sdk2SessionInitializeRequest; if (r.requestedTools) unique(r.requestedTools); }
  if (name === 'ExecutorRegistrationRequest') {
    const r = input as W.Sdk2ExecutorRegistrationRequest; unique(r.operations); unique(r.workspaces.map(x => x.workspaceId));
    unique((r.tools ?? []).map(tool => tool.name)); need(r.operations.includes('tool.invoke') === ((r.tools?.length ?? 0) > 0));
  }
  if (name === 'ExecutionReceiptRequest') {
    const r = input as W.Sdk2ExecutionReceiptRequest;
    need(r.status === 'completed' ? r.result !== null && r.errorCode === null : r.result === null && r.errorCode !== null);
    if (r.result) executionResourceResult(r.result);
  }
}
function executionResourceResult(result: W.Sdk2ResourceResult): void {
  if (result.operation === 'tool.invoke') verifyExecutionToolReceipt(result.args.resultJson);
  if (result.operation === 'fs.read') need(decodeBase64(result.args.bytesBase64).byteLength <= 65536);
  if (result.operation === 'fs.list') { unique(result.args.entries.map(x => x.name)); for (const entry of result.args.entries) need(!/[\\/\x00]/.test(entry.name) && !['.', '..'].includes(entry.name)); }
  if (result.operation === 'process.exec' && result.args.exitCode !== null) {
    const code = BigInt(result.args.exitCode); need(code >= -2147483648n && code <= 2147483647n);
  }
}
export async function verifyExecutionResponse(name: string, output: unknown, input: unknown, scope?: W.Sdk2Scope, historicalFacts = false): Promise<void> {
  const request = (input ?? {}) as Record<string, unknown>;
  if (name === 'SessionExecutionCapabilities') {
    const r = output as W.Sdk2SessionExecutionCapabilities; need(r.sessionId === request.sessionId); unique(r.effectiveTools.map(x => x.name));
    if (request.platform) need(equalControl(r.platform, request.platform));
    if (request.executorId) need(r.binding?.target.executorId === request.executorId && r.binding.target.connectionId === request.connectionId && r.binding.target.workspaceId === request.workspaceId);
  } else if (name === 'ExecutionBoundary') {
    const r = output as W.Sdk2ExecutionBoundary;
    need(r.sessionId === request.sessionId);
    unique(r.application.policyTools); unique(r.application.platformCapabilities);
    unique(r.effectiveTools.map(x => x.name));
    if (r.requestedTools !== null) unique(r.requestedTools);
    if (r.executor !== null) {
      unique(r.executor.operations); unique(r.executor.workspaces.map(item => item.workspaceId));
      need(r.binding !== null && r.binding.target.executorId === r.executor.executorId &&
        r.binding.target.connectionId === r.executor.connectionId &&
        r.binding.target.connectionRevision === r.executor.connectionRevision);
    }
  } else if (name === 'ExecutorConnection') {
    const r = output as W.Sdk2ExecutorConnection; need(r.executorId === request.executorId); if (request.connectionId) need(r.connectionId === request.connectionId);
  } else if (name === 'ExecutionBatch') {
    const r = output as W.Sdk2ExecutionBatch; need(scope !== undefined && r.executorId === request.executorId && r.connectionId === request.connectionId); unique(r.operations.map(x => x.operationId));
    for (const op of r.operations) {
      need(equalControl(op.scope, scope) && op.binding.target.executorId === r.executorId && op.binding.target.connectionId === r.connectionId && op.binding.target.connectionRevision === request.connectionRevision);
      await executionDigest(op);
    }
  } else if (name === 'ExecutionStatus') {
    const r = output as W.Sdk2ExecutionStatus;
    if (request.sessionId) need(r.operation.sessionId === request.sessionId);
    if (request.operationId) need(r.operation.operationId === request.operationId);
    await executionDigest(r.operation);
    if (request.digest) need(r.operation.digest === request.digest && equalControl(r.receipt, input));
    if (scope) {
      // 查账和原回执补投保留当时授权代；不能把历史事实转成当前派工权限。
      if (historicalFacts) need(r.operation.scope.applicationScopeId === scope.applicationScopeId && r.operation.scope.endUserId === scope.endUserId);
      else need(equalControl(r.operation.scope, scope));
    }
    if (r.status === 'pending' || r.status === 'unknown' && r.receipt === null) need(r.receipt === null);
    else {
      need(r.receipt !== null); const receipt = r.receipt!; verifyExecutionRequest('ExecutionReceiptRequest', receipt);
      need(receipt.status === r.status && receipt.operationId === r.operation.operationId && receipt.digest === r.operation.digest &&
        receipt.executorId === r.operation.binding.target.executorId && receipt.connectionId === r.operation.binding.target.connectionId);
      if (receipt.result) {
        const asked = r.operation.request, result = receipt.result;
        need(result.operation === asked.operation);
        if (asked.operation === 'fs.read' && result.operation === 'fs.read') need(decodeBase64(result.args.bytesBase64).byteLength <= asked.args.length);
        if (asked.operation === 'fs.write' && result.operation === 'fs.write') need(await sha256(decodeBase64(asked.args.bytesBase64)) === result.args.hash);
        if (asked.operation === 'process.exec' && result.operation === 'process.exec') need(encoder.encode(result.args.stdout).byteLength + encoder.encode(result.args.stderr).byteLength <= asked.args.maxOutputBytes);
      }
    }
  }
}
async function executionDigest(operation: W.Sdk2ExecutionOperation): Promise<void> {
  const { digest, ...payload } = operation;
  need(await sha256(encoder.encode('tansr.sdk2.execution.v1\0' + encodeControl(payload, 1048576))) === digest);
  if (operation.request.operation === 'tool.invoke') {
    need(validTerminalToolInvocation(operation));
    decodeExecutionToolObject(operation.request.args.argsJson);
  } else if (operation.request.operation === 'process.exec') {
    need(operation.binding.target.interpreter !== undefined &&
      equalControl(operation.binding.target.interpreter, operation.request.args.interpreter));
  }
}
