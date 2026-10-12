// 检测用户添加的 MCP 服务器：浏览器只提交服务器 id，命令、地址和密钥由后端从本机配置读取。
import { ensureAgentUiSession } from "@/services/api/agent-assistant";
import { http } from "@/services/api/request";

export type McpProbeTool = { name: string; title: string | null; description: string; readOnly: boolean };
export type McpProbeResult = { tools: McpProbeTool[] };

export async function probeMcpServer(serverId: string): Promise<McpProbeResult> {
    const token = await ensureAgentUiSession();
    const data = await http.post<McpProbeResult>("/assistant/mcp/probe", { serverId }, { headers: { "X-Beeftv-Ui-Session": token } });
    const tools = Array.isArray(data?.tools) ? data.tools : [];
    return {
        tools: tools
            .map((tool) => ({
                name: String(tool?.name ?? "").trim(),
                title: typeof tool?.title === "string" ? tool.title : null,
                description: String(tool?.description ?? ""),
                readOnly: tool?.readOnly === true,
            }))
            .filter((tool) => tool.name),
    };
}
