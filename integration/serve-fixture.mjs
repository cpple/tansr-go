// GO-01: 只替换平台/模型响应；使用参数指定的 CLI 源码中的真实 Serve、内核、HTTP 和 SSE。
// 本文件仅为集成验收宿主，Go SDK 和示例不依赖 Node。
import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { createInterface } from 'node:readline';

const cliRoot = resolve(process.argv[2] ?? '');
const directory = resolve(process.argv[3] ?? '');
const mode = process.argv[4] ?? 'session';
if (!process.argv[2] || !process.argv[3]) throw new Error('usage: serve-fixture.mjs CLI_ROOT TEMP_DIRECTORY [session|execution|execution-demo|archive|archive-offload|publication|persistence]');
const load = relative => import(pathToFileURL(join(cliRoot, relative)).href);
const contract = JSON.parse(await readFile(join(cliRoot, 'packages/server/contract/api-manifest.json'), 'utf8'));
let cleanup;
let info;
let control;

if (mode === 'archive' || mode === 'archive-offload') {
  const [{ createServeArchiveHost }, { createServeOffloadArchiveHost }, { createMemoryBlobStore },
    { serveTestAgent, serveArchiveLimits }, { MockModelClient, textTurn }] = await Promise.all([
    load('packages/server/src/extensions/archive-host.ts'),
    load('packages/server/src/extensions/offload.ts'), load('packages/kernel/src/index.ts'),
    load('tests/sdk2/serve-archive-host.helper.ts'),
    load('packages/testkit/src/index.ts'),
  ]);
  const agent = serveTestAgent(directory, () => new MockModelClient([textTurn('go-archive-answer'), textTurn('go-archive-second')]));
  const cap = { bytes: 64 * 1048576, records: 4096 }, total = { bytes: 512 * 1048576, records: 32768 };
  const issuedAtMs = Date.now() - 1000;
  const options = {
    agent: agent.agent, applicationScopeId: 'go-app', security: 'trusted-single-application',
    archive: { directory: join(directory, 'archive'), mode: 'create', hostId: 'go-archive-host',
      quota: { host: total, application: total, endUser: total, capture: cap }, maxSessions: 4,
      commandBytes: 1048576, commandDatabasePages: 1024, spoolDatabasePages: 32768, limits: serveArchiveLimits,
      epoch: { id: 'go-integration-epoch', issuedAtMs, expiresAtMs: issuedAtMs + serveArchiveLimits.epochLifetimeMs } },
    events: { storeId: 'go-events', key: createHash('sha256').update('synthetic-go-events-key').digest(), maxQueuedOperations: 128, maxSubscriptions: 8 },
    sourceFor: subject => ({ sourceId: mode === 'archive-offload' ? 'go-offload-source' : `source-${subject.sessionId}`, sourceGeneration: 'go-source-generation' }),
    authorize: () => ({ authorizationRevision: '1' }),
  };
  const { store: _legacyStore, ...offloadAgent } = agent.agent;
  const host = mode === 'archive-offload' ? createServeOffloadArchiveHost({ ...options, agent: offloadAgent,
    persistence: { directory: join(directory, 'offload'), mode: 'create', sourceId: 'go-offload-source',
      sourceGeneration: 'go-source-generation', cold: createMemoryBlobStore(),
      policy: { codec: 'jsonl', hotRetention: { maxBytes: 65536, keepRecentSegments: 0 }, upload: { mode: 'sync' } },
      segmentation: { maxBytes: 4096, maxRecords: 1 }, maxRollbackBytes: 1048576 } }) : createServeArchiveHost(options);
  const server = await host.start({ host: '127.0.0.1', port: 0, token: 'unused-legacy', readyFrame: 'none', heartbeatMs: 0,
    v2: { authenticate: request => request.headers.authorization === 'Bearer go-integration-token' ? { endUserId: 'go-user' } : null } });
  cleanup = async () => { await server.close(); await server.settleResources?.(); await host.dispose(); };
  info = { baseURL: server.url };
} else if (mode === 'session' || mode === 'execution' || mode === 'execution-demo' || mode === 'publication' || mode === 'persistence') {
  const [{ startServer, createAgentSessionFactory }, { createServeAgentSessionStore }, { createFakePlatform, FAKE_API_BASE },
    { openSqliteArchiveSpool, readArchiveSpoolOperationFacts }, { defaultAppCapabilities }, { clientToolDefinitionDigest }] = await Promise.all([
    load('packages/server/src/index.ts'), load('packages/server/src/v2/agent-session-store.ts'),
    load('packages/server/test/fake-platform-fetch.ts'), load('packages/kernel/src/index.ts'),
    load('packages/sdk/src/index.ts'), load('packages/server/src/v2/agent-execution.ts'),
  ]);
  const limit = { bytes: 16 * 1048576, records: 512 };
  const spool = await openSqliteArchiveSpool({ path: join(directory, 'execution.sqlite'), mode: 'create', storeId: 'go-execution',
    bindings: [{ bindingId: 'go-control', applicationScopeId: 'go-app', endUserId: 'go-user', limits: { binding: limit, application: limit, endUser: limit } }],
    globalLimit: limit, maxReservations: 128, maxEntries: 512, maxOperations: 512, maxDatabasePages: 8192 });
  const caps = defaultAppCapabilities('desktop');
  const executionEnabled = mode !== 'session';
  const publicationIdentity = { kind: 'client-managed', domain: 'go-app/go-user', applicationScopeId: 'go-app', endUserId: 'go-user', sourceId: 'go-memory-source', sourceGeneration: '1' };
  const publicationEnabled = mode === 'publication' || mode === 'persistence';
  let publicationMode = 'create', transitioning = false, pendingCreates = 0;
  const handles = new Map();
  const publication = publicationEnabled ? await load('packages/server/src/v2/memory-management.ts') : undefined;
  const lifecycleAccess = publicationEnabled ? await load('packages/server/src/v2/memory-lifecycle.ts') : undefined;
  const fake = createFakePlatform({ bundleExtra: { capabilities: { ...caps, tools: { ...caps.tools, customTools: true },
    ...(executionEnabled ? { execution: { version: 'bound-device-v1', boundDevice: { tools: { customTools: true } } } } : {}) }, app: { platform: 'desktop' } } });
  // Same public declaration as examples/go-tools. A mismatch is caught by the
  // actual definitionDigest handshake, not hidden by a private route fixture.
  const declaration = mode === 'execution-demo'
    ? { name: 'DemoOrderStatus', description: 'Read the status of sample order DEMO-001; this is demonstration data.',
      parameters: { orderId: { type: 'string', description: 'Sample order ID: DEMO-001' } }, readOnly: true }
    : { name: 'BusinessLookup', description: 'Read one synthetic business fact', readOnly: true };
  const frame = (event, data) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
  let exchange = 0;
  const fetchImpl = async (url, init) => {
    const path = new URL(typeof url === 'string' ? url : url instanceof URL ? url.href : url.url).pathname;
    if (path !== '/t1/exchange') return fake.fetchImpl(url, init);
    const body = JSON.parse(String(init?.body));
    const number = ++exchange;
    if (JSON.stringify(body).includes('GO-BLOCK')) {
      return new Response(new ReadableStream({ start(controller) {
        controller.enqueue(new TextEncoder().encode(frame('t.open', { exchangeId: `go-${number}`, model: body.model, protocol: 'twp/1' }) +
          frame('t.delta', { i: 0, t: 'text', v: 'go-waiting' })));
        const stop = () => { try { controller.close(); } catch {} };
        init?.signal?.addEventListener('abort', stop, { once: true });
      } }), { headers: { 'content-type': 'text/event-stream' } });
    }
    const hasTool = body.tools?.some(tool => tool.name === declaration.name);
    const requested = JSON.stringify(body).includes('GO-TOOL');
    const answered = JSON.stringify(body).includes(mode === 'execution-demo' ? 'awaiting shipment' : 'go-terminal-fact');
    const content = hasTool && requested && !answered
      ? frame('t.delta', { i: 0, t: 'tool_use', id: `go-tool-${number}`, name: declaration.name,
        vJson: mode === 'execution-demo' ? '{"orderId":"DEMO-001"}' : '{}' }) + frame('t.close', { stop: 'tool_use' })
      : frame('t.delta', { i: 0, t: 'text', v: answered ? 'go-tool-complete' : 'go-real-serve-answer' }) + frame('t.close', { stop: 'end_turn' });
    return new Response(frame('t.open', { exchangeId: `go-${number}`, model: body.model, protocol: 'twp/1' }) + content,
      { headers: { 'content-type': 'text/event-stream' } });
  };
  const build = createAgentSessionFactory({ cwd: directory, store: createServeAgentSessionStore({ dir: join(directory, 'sessions'), ...(publicationEnabled ? { ownership: {} } : {}) }),
    platform: { apiBaseUrl: FAKE_API_BASE, appId: 'go-app', appKey: 'synthetic-fixture-key', fetchImpl,
      ...(publicationEnabled ? { memoryPublicationFor: () => ({ identity: publicationIdentity, mode: publicationMode, ...(mode === 'persistence' ? { profile: 'terminal-persistence-v1' } : {}), enabled: () => true, balance: () => null, recallSelector: false }) } : {}) },
    ...(executionEnabled ? { execution: { applicationScopeId: 'go-app',
      authorize: request => ({ controller: request.headers.authorization === 'Bearer go-integration-token',
        ...(request.headers.authorization === 'Bearer go-integration-token' ? { executorId: 'go-executor' } : {}) }),
      readPolicy: async () => ({ authorizationRevision: '1', tools: publicationEnabled ? ['SearchMemory'] : [declaration.name] }),
      spoolFor: () => ({ spool, bindingId: 'go-control' }) } } : {}) });
  const createSession = build.factory.create.bind(build.factory);
  build.factory.create = async init => {
    if (transitioning) throw new Error('publication transition in progress');
    pendingCreates++;
    try { const value = await createSession(init); handles.set(value.handle.sessionId, value.handle); return value; }
    finally { pendingCreates--; }
  };
  const facts = () => {
    const batch = readArchiveSpoolOperationFacts(spool, { bindingId: 'go-control', operationKeyPrefix: 'execution-', maxBodyBytes: 262144 });
    if (!batch) throw new Error('real execution facts unavailable');
    const read = id => {
      const entry = batch.entries.find(value => value.entryId === id);
      if (!entry) return null;
      if (!entry.complete || entry.bodyState !== 'retained' || !entry.body) throw new Error('execution fact body unavailable');
      return JSON.parse(Buffer.from(entry.body).toString('utf8'));
    };
    const operations = batch.operations.filter(value => value.reservation.operationKey.startsWith('execution-request-')).map(value => {
      const key = value.reservation.operationKey, operation = read(key);
      if (!operation) throw new Error('execution request unavailable');
      const args = operation.request.operation === 'tool.invoke' ? JSON.parse(operation.request.args.argsJson) : null;
      const receipt = read(`execution-result-${operation.operationId}`);
      const result = batch.operations.find(item => item.reservation.operationKey === `execution-result-${operation.operationId}`);
      return { operationId: operation.operationId, sessionId: operation.sessionId, digest: operation.digest, expiresAt: operation.expiresAt,
        toolName: operation.toolName, tool: operation.request.args.name ?? null, action: args?.action ?? null,
        transferId: args?.transferId ?? null, intentSha256: args?.intentSha256 ?? null,
        resultState: result?.state ?? null, receiptStatus: receipt?.status ?? null,
        receiptBodySha256: batch.entries.find(item => item.entryId === `execution-result-${operation.operationId}`)?.witness.bodySha256 ?? null };
    });
    return { mode, publicationMode, modelExchanges: exchange, operations,
      sessions: [...handles.values()].map(handle => ({ sessionId: handle.sessionId, status: handle.status() })),
      spool: spool.snapshot() };
  };
  control = async value => {
    if (value.command === 'facts') return facts();
    if (!publicationEnabled || transitioning || pendingCreates !== 0) throw new Error('publication lifecycle is unavailable or in flight');
    transitioning = true;
    try {
      if (value.command === 'archive-receipts') {
        const handle = handles.get(value.sessionId);
        if (mode !== 'persistence' || !handle || handle.endUserId !== 'go-user' || handle.status() !== 'idle' ||
            !Number.isSafeInteger(value.limit) || value.limit < 1 || value.limit > 256 ||
            !Array.isArray(value.operationIds) || value.operationIds.length < 1 || value.operationIds.length > value.limit ||
            new Set(value.operationIds).size !== value.operationIds.length ||
            value.operationIds.some(id => typeof id !== 'string' || [...id].length < 1 || [...id].length > 256 || /[\u0000-\u001f\u007f]/.test(id)) ||
            value.consume !== undefined && typeof value.consume !== 'boolean') throw new Error('invalid bounded archive control');
        const lifecycle = lifecycleAccess.getPlatformMemoryLifecycle(handle), source = lifecycle?.management;
        const check = () => {
          if (!source || handles.get(value.sessionId) !== handle || handle.status() !== 'idle' ||
              lifecycleAccess.getPlatformMemoryLifecycle(handle) !== lifecycle || lifecycle.management !== source || !lifecycle.status().enabled ||
              source.identity.applicationScopeId !== 'go-app' || source.identity.endUserId !== 'go-user' ||
              source.identity.sourceId !== publicationIdentity.sourceId || source.identity.sourceGeneration !== publicationIdentity.sourceGeneration)
            throw new Error('archive publisher changed');
          source.assertCurrent();
        };
        check();
        // Only original durable terminal facts qualify. No historical receipt is fabricated.
        for (const id of value.operationIds) {
          const original = await source.status(id); check();
          if (!original || !(original.status === 'failed' || original.status === 'committed' && original.durable &&
              (original.consumed || value.consume === true))) throw new Error('original receipt is not archivable');
        }
        if (value.consume === true) for (const id of value.operationIds) {
          const original = await source.status(id); check();
          if (original.status === 'committed') { await source.consume(id); check(); }
        }
        const archived = await source.archiveReceipts(value.operationIds); check();
        return { archived, explicitlyConsumed: value.consume === true, ...facts() };
      }
      if (value.command === 'settle-publication') {
        const handle = handles.get(value.sessionId);
        if (!handle || handle.endUserId !== 'go-user' || handle.status() !== 'idle') throw new Error('explicit idle publisher is required');
        // The executor must keep polling; this does not close a session or manufacture an execution receipt.
        await handle.settleResources();
        if (handles.get(value.sessionId) !== handle || handle.status() !== 'idle') throw new Error('publisher changed during settlement');
        return { liveSettledSessionId: value.sessionId, ...facts() };
      }
      if (value.command === 'set-publication-mode' && value.mode === 'reopen') {
        if (handles.size === 0) throw new Error('no prior publisher');
        for (const handle of handles.values()) {
          if (handle.endUserId !== 'go-user' || handle.status() !== 'ended') throw new Error('prior publisher has not ended');
          await handle.settleResources();
          if (handle.status() !== 'ended') throw new Error('publisher changed during settlement');
        }
        publicationMode = 'reopen';
        return { quiescentSessions: [...handles.keys()], ...facts() };
      }
      throw new Error('unknown fixture control');
    } finally { transitioning = false; }
  };
  const server = await startServer({ ...(publicationEnabled ? { terminal: { contract: 'terminal-services-v1' } } : {}), host: '127.0.0.1', port: 0, token: 'unused-legacy', heartbeatMs: 0, readyFrame: 'none',
    createSession: { create() { throw new Error('legacy SDK1 entry is unused'); } },
    v2: { authenticate: request => request.headers.authorization === 'Bearer go-integration-token' ? { endUserId: 'go-user' } : null,
      createSession: build.factory, store: build.storeReader, governance: { sweepIntervalMs: 0 } } });
  cleanup = async () => {
    const failures = [];
    for (const close of [() => server.close(), () => server.settleResources(), () => build.flush(), () => spool.close()]) {
      try { await close(); } catch (error) { failures.push(error); }
    }
    if (failures.length) throw new AggregateError(failures, 'fixture cleanup failed');
  };
  info = { baseURL: server.url, ...(publicationEnabled ? { profile: mode === 'persistence' ? 'terminal-persistence-v1' : 'terminal-services-v1', controls: ['facts', 'settle-publication', 'set-publication-mode', ...(mode === 'persistence' ? ['archive-receipts'] : [])] } : {}), declaration, definitionDigest: clientToolDefinitionDigest(declaration),
    ...(publication ? { publicationIdentity: { applicationScopeId: 'go-app', endUserId: 'go-user', sourceId: publicationIdentity.sourceId, sourceGeneration: publicationIdentity.sourceGeneration, domainKey: publication.memoryPublicationKey(publicationIdentity) } } : {}) };
} else {
  throw new Error('unknown fixture mode');
}

process.stdout.write('TANSR_GO_FIXTURE ' + JSON.stringify({ ...info, mode, manifestRevision: contract.revision,
  token: 'go-integration-token', applicationScopeId: 'go-app', endUserId: 'go-user', authorizationRevision: '1' }) + '\n');
const lines = createInterface({ input: process.stdin });
try {
  for await (const line of lines) {
    if (line.trim() === '' || line.trim() === 'stop') break;
    let request;
    try {
      if (Buffer.byteLength(line) > 4096) throw new Error('fixture control exceeds bound');
      request = JSON.parse(line);
      const fields = Object.keys(request).sort().join(',');
      if (!request || typeof request !== 'object' || Array.isArray(request) ||
          typeof request.requestId !== 'string' || !/^[A-Za-z0-9_-]{1,80}$/.test(request.requestId) ||
          !['command,requestId', 'command,requestId,sessionId', 'command,mode,requestId',
            'command,limit,operationIds,requestId,sessionId', 'command,consume,limit,operationIds,requestId,sessionId'].includes(fields) || !control) throw new Error('invalid fixture control');
      const result = await control(request);
      process.stdout.write('TANSR_GO_CONTROL ' + JSON.stringify({ requestId: request.requestId, ok: true, result }) + '\n');
    } catch (error) {
      process.stdout.write('TANSR_GO_CONTROL ' + JSON.stringify({ requestId: request?.requestId ?? null, ok: false, error: String(error?.message ?? error) }) + '\n');
    }
  }
} finally { lines.close(); await cleanup(); }
