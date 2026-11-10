export interface MCPTool {
    name: string;
    description: string;
    input_schema?: unknown;
    // Takes a search query, so it can be picked as the workspace's web search.
    search_capable?: boolean;
}

export interface MCPServer {
    id: string;
    org_id: string;
    name: string;
    url: string;
    auth_type: "none" | "bearer";
    enabled: boolean;
    discovered_tools: MCPTool[];
    last_error?: string;
    // The discovered tool the workspace uses as its web search; empty for none.
    search_tool: string;
    created_at: Date;
    updated_at: Date;
}

export interface CreateMCPServer {
    name: string;
    url: string;
    auth_type: "none" | "bearer";
    token?: string;
}

export interface UpdateMCPServer {
    name?: string;
    enabled?: boolean;
    token?: string;
    // One of the server's tools as the workspace's web search; "" clears it.
    search_tool?: string;
}
