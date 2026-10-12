// 用户自行添加的 MCP 服务器 → 内置助手的外部工具来源。
// 客户端协议全部交给官方 @earendil-works/pi-mcp；这里只做配置校验、连接缓存、权限过滤与名称约束。
import crypto from 'node:crypto';
import { McpClient, StdioTransport, StreamableHttpTransport, toLlmContent } from '@earendil-works/pi-mcp';

export const CONNECT_TIMEOUT_MS = 15_000;
const TOOL_NAME_LIMIT = 64;
// 只放行运行命令所需的系统变量。宿主进程环境里带有宿主/桌面令牌，绝不能整体继承给 MCP 子进程。
export const STDIO_ENV_ALLOWLIST = ['PATH', 'HOME', 'USERPROFILE', 'APPDATA', 'LOCALAPPDATA', 'SystemRoot', 'TEMP', 'TMP', 'LANG', 'LC_ALL'];

function list(value) {
  return Array.isArray(value) ? value : [];
}

function keyValues(value) {
  return list(value)
    .map((item) => ({ name: String(item?.name ?? '').trim(), value: String(item?.value ?? '') }))
    .filter((item) => item.name);
}

function headerObject(value) {
  return Object.fromEntries(keyValues(value).map((item) => [item.name, item.value]));
}

// 无效条目返回 null，由调用方跳过；不会因为一条坏配置让整轮失败。
export function normalizeMcpServer(raw) {
  const id = String(raw?.id ?? '').trim();
  if (!id) return null;
  const transport = raw?.transport;
  const server = {
    id,
    name: String(raw?.name ?? '').trim() || id,
    enabled: raw?.enabled !== false,
    transport,
    disabledTools: list(raw?.disabledTools).map(String),
  };
  if (transport === 'stdio') {
    const command = String(raw?.command ?? '').trim();
    if (!command) return null;
    return { ...server, command, args: list(raw?.args).map(String), env: keyValues(raw?.env) };
  }
  if (transport === 'http') {
    let url;
    try { url = new URL(String(raw?.url ?? '').trim()); } catch { return null; }
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null;
    return { ...server, url: url.toString(), headers: keyValues(raw?.headers) };
  }
  return null;
}

// 环境白名单 + 本服务器自己配置的变量。不读取 process.env 的其他任何键。
export function buildStdioEnv(serverEnv = [], hostEnv = process.env) {
  const env = {};
  for (const name of STDIO_ENV_ALLOWLIST) {
    if (typeof hostEnv[name] === 'string') env[name] = hostEnv[name];
  }
  for (const item of keyValues(serverEnv)) env[item.name] = item.value;
  return env;
}

function fingerprint(server) {
  const normalized = JSON.stringify({
    transport: server.transport,
    command: server.command ?? null,
    args: server.args ?? null,
    env: server.env ? [...server.env].sort((a, b) => a.name.localeCompare(b.name)) : null,
    url: server.url ?? null,
    headers: server.headers ? [...server.headers].sort((a, b) => a.name.localeCompare(b.name)) : null,
  });
  return crypto.createHash('sha256').update(normalized).digest('hex');
}

function failure(reason, message, cause) {
  return Object.assign(new Error(message), { reason, ...(cause ? { cause } : {}) });
}

export async function defaultConnect(server) {
  const transport = server.transport === 'stdio'
    ? new StdioTransport({ command: server.command, args: server.args, env: buildStdioEnv(server.env), inheritEnv: false })
    : new StreamableHttpTransport({ url: server.url, headers: headerObject(server.headers) });
  const client = new McpClient({ name: 'beeftv-agent-host', version: '1.0.0', requestTimeoutMs: CONNECT_TIMEOUT_MS });
  let timer;
  try {
    await Promise.race([
      client.connect(transport),
      new Promise((_, reject) => { timer = setTimeout(() => reject(failure('mcp_connect_timeout', '连接 MCP 服务器超时')), CONNECT_TIMEOUT_MS); }),
    ]);
    return client;
  } catch (error) {
    await client.close().catch(() => {});
    throw error.reason ? error : failure('mcp_connect_failed', '无法连接 MCP 服务器', error);
  } finally {
    clearTimeout(timer);
  }
}

function slug(value, max) {
  return String(value).replace(/[^A-Za-z0-9_-]+/g, '_').replace(/^_+|_+$/g, '').slice(0, max) || 'x';
}

// 工具名必须是 [A-Za-z0-9_-]{1,64}。服务端名与工具名都清洗；冲突时追加序号并再截断。
export function uniqueToolName(serverName, toolName, used) {
  const base = `mcp__${slug(serverName, 20)}__${slug(toolName, 40)}`.slice(0, TOOL_NAME_LIMIT);
  let candidate = base;
  for (let index = 2; used.has(candidate); index += 1) {
    const suffix = `_${index}`;
    candidate = `${base.slice(0, TOOL_NAME_LIMIT - suffix.length)}${suffix}`;
  }
  used.add(candidate);
  return candidate;
}

function toolParameters(schema) {
  const input = schema && typeof schema === 'object' ? schema : {};
  return { ...input, type: 'object', properties: input.properties ?? {} };
}

export function createMcpToolSource({ loadServers, connect = defaultConnect }) {
  const connections = new Map();
  const statuses = new Map();

  async function connectionFor(server) {
    const key = fingerprint(server);
    const existing = connections.get(server.id);
    if (existing && existing.fingerprint === key) return existing;
    if (existing) await closeConnection(server.id);
    const client = await connect(server);
    const connection = { fingerprint: key, client, toolsPromise: null };
    connection.toolsPromise = client.listTools();
    connections.set(server.id, connection);
    return connection;
  }

  async function closeConnection(id) {
    const connection = connections.get(id);
    connections.delete(id);
    if (connection) await connection.client.close().catch(() => {});
  }

  async function tools({ permissionMode = 'canvas' } = {}) {
    const servers = (await loadServers()).map(normalizeMcpServer).filter((server) => server?.enabled);
    const enabledIds = new Set(servers.map((server) => server.id));
    for (const id of [...connections.keys()]) {
      if (!enabledIds.has(id)) await closeConnection(id);
    }
    const readOnlyMode = permissionMode === 'read-only';
    const used = new Set();
    const result = [];
    for (const server of servers) {
      let listed;
      try {
        const connection = await connectionFor(server);
        listed = await connection.toolsPromise;
        statuses.set(server.id, { ok: true, toolCount: listed.length, error: null });
      } catch (error) {
        // 连接或列表失败只影响这一个服务器：记录原因，下一轮会重新连接。
        await closeConnection(server.id);
        statuses.set(server.id, { ok: false, toolCount: 0, error: plainError(error) });
        continue;
      }
      const connection = connections.get(server.id);
      for (const tool of listed) {
        if (server.disabledTools.includes(tool.name)) continue;
        const readOnly = tool.annotations?.readOnlyHint === true;
        if (readOnlyMode && !readOnly) continue;
        const name = uniqueToolName(server.name, tool.name, used);
        result.push({
          name,
          label: `${server.name} · ${tool.title || tool.name}`,
          description: `[${server.name}] ${tool.description || tool.title || tool.name}`,
          parameters: toolParameters(tool.inputSchema),
          readOnly,
          serverId: server.id,
          toolName: tool.name,
          execute: async (_toolCallId, args, signal) => {
            if (readOnlyMode && !readOnly) throw failure('scope_denied', '这一轮只能使用只读的 MCP 工具');
            const callResult = await connection.client.callTool(tool.name, args ?? {}, { signal });
            return { content: toLlmContent(callResult), details: undefined, isError: callResult.isError === true };
          },
        });
      }
    }
    return result;
  }

  function status() {
    return Object.fromEntries(statuses);
  }

  async function probe(raw) {
    const server = normalizeMcpServer(raw);
    if (!server) throw failure('mcp_server_invalid', 'MCP 服务器配置不完整');
    const client = await connect(server);
    try {
      const listed = await client.listTools();
      return {
        tools: listed.map((tool) => ({
          name: tool.name,
          title: tool.title ?? null,
          description: tool.description ?? '',
          readOnly: tool.annotations?.readOnlyHint === true,
        })),
      };
    } catch (error) {
      throw failure('mcp_probe_failed', plainError(error), error);
    } finally {
      await client.close().catch(() => {});
    }
  }

  async function close() {
    await Promise.all([...connections.keys()].map((id) => closeConnection(id)));
    statuses.clear();
  }

  return { tools, status, probe, close };
}

function plainError(error) {
  if (error?.reason === 'mcp_connect_timeout') return '连接超时，请检查服务是否在运行';
  if (error?.reason === 'mcp_connect_failed') return '无法连接，请检查命令或地址是否正确';
  return error instanceof Error ? error.message : String(error);
}
