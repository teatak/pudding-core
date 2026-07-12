import { useEffect } from "react";

export type ToolDefinition = {
  name: string;
  description: string;
  inputSchema: Record<string, unknown>;
  capability?: "chat" | "work" | "code";
  appID?: string;
  handler: (args: unknown) => unknown | Promise<unknown>;
};

export type RuntimeAppSkillDefinition = {
  id: string;
  name: string;
  description?: string;
  path: string;
  content: string;
};

export type RuntimeAppDefinition = {
  id: string;
  name: string;
  version?: string;
  description?: string;
  requiredMode: "chat" | "work" | "code";
  defaultSkillID?: string;
  skills?: RuntimeAppSkillDefinition[];
};

type BrowserMCPOptions = {
  endpoint: string;
  enabled: boolean;
  tools: ToolDefinition[];
  apps?: RuntimeAppDefinition[];
  runtimeInfo: { id: string; type: string };
  serverInfo?: { name: string; version: string };
  onRegistryChanged?: () => void;
};

type JSONRPCMessage = {
  jsonrpc?: string;
  id?: string | number | null;
  method?: string;
  params?: unknown;
  result?: unknown;
  error?: { code: number; message: string };
};

export function useBrowserMCP({ apps, endpoint, enabled, onRegistryChanged, runtimeInfo, serverInfo, tools }: BrowserMCPOptions) {
  useEffect(() => {
    if (!enabled || !endpoint) {
      return;
    }
    const server = new BrowserMCPServer({
      endpoint,
      apps: apps ?? [],
      runtimeInfo,
      serverInfo: serverInfo ?? { name: "pudding-ui", version: "1.0" },
      tools,
      onRegistryChanged,
    });
    let cancelled = false;
    queueMicrotask(() => {
      if (!cancelled) {
        server.connect();
      }
    });
    return () => {
      cancelled = true;
      server.close();
    };
  }, [apps, enabled, endpoint, onRegistryChanged, runtimeInfo, serverInfo, tools]);
}

class BrowserMCPServer {
  private socket: WebSocket | null = null;
  private closed = false;
  private reconnectTimer: number | null = null;
  private reconnectDelay = 300;
  private readonly tools = new Map<string, ToolDefinition>();

  constructor(
    private readonly options: {
      endpoint: string;
      apps: RuntimeAppDefinition[];
      runtimeInfo: { id: string; type: string };
      serverInfo: { name: string; version: string };
      tools: ToolDefinition[];
      onRegistryChanged?: () => void;
    },
  ) {
    for (const tool of options.tools) {
      this.tools.set(tool.name, tool);
    }
  }

  connect() {
    if (this.closed) {
      return;
    }
    const socket = new WebSocket(this.options.endpoint);
    this.socket = socket;
    socket.onopen = () => {
      this.reconnectDelay = 300;
    };
    socket.onmessage = (event) => {
      void this.handleMessage(socket, event.data);
    };
    socket.onclose = () => {
      if (this.socket === socket) {
        this.socket = null;
      }
      this.options.onRegistryChanged?.();
      this.scheduleReconnect();
    };
    socket.onerror = () => {
      socket.close();
    };
  }

  close() {
    this.closed = true;
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.socket?.close();
    this.socket = null;
  }

  private scheduleReconnect() {
    if (this.closed || this.reconnectTimer !== null) {
      return;
    }
    const delay = this.reconnectDelay;
    this.reconnectDelay = Math.min(this.reconnectDelay * 2, 3000);
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  private async handleMessage(socket: WebSocket, data: unknown) {
    const message = parseMessage(data);
    if (!message?.method) {
      return;
    }
    const hasID = Object.prototype.hasOwnProperty.call(message, "id") && message.id !== undefined && message.id !== null;
    try {
      const result = await this.handleRequest(message.method, message.params);
      if (hasID) {
        this.send(socket, { jsonrpc: "2.0", id: message.id, result });
      }
      if (message.method === "apps/list") {
        window.setTimeout(() => this.options.onRegistryChanged?.(), 100);
      }
    } catch (error) {
      if (hasID) {
        this.send(socket, {
          jsonrpc: "2.0",
          id: message.id,
          error: { code: -32000, message: error instanceof Error ? error.message : String(error) },
        });
      }
    }
  }

  private async handleRequest(method: string, params: unknown) {
    if (method === "initialize") {
      return {
        protocolVersion: "2024-11-05",
        serverInfo: this.options.serverInfo,
        runtimeInfo: this.options.runtimeInfo,
        capabilities: { apps: {}, tools: {} },
      };
    }
    if (method === "tools/list") {
      return {
        tools: this.options.tools.map(({ name, description, inputSchema, capability, appID }) => ({
          name,
          description,
          inputSchema,
          capability,
          appID,
        })),
      };
    }
    if (method === "apps/list") {
      return {
        apps: this.options.apps.map(({ skills, ...app }) => ({
          ...app,
          skills: skills?.map(({ content: _content, ...skill }) => skill),
        })),
      };
    }
    if (method === "apps/skills/read") {
      const request = asRecord(params);
      const appID = stringValue(request?.appID);
      const skillID = stringValue(request?.skillID);
      const app = this.options.apps.find((item) => item.id === appID);
      const skill = app?.skills?.find(
        (item) => item.id === skillID || item.name === skillID || item.path === skillID,
      );
      if (!skill) {
        throw new Error(`unknown runtime app skill: ${appID}/${skillID}`);
      }
      return skill;
    }
    if (method === "tools/call") {
      const call = asRecord(params);
      const name = stringValue(call?.name);
      const tool = name ? this.tools.get(name) : undefined;
      if (!tool) {
        throw new Error(`unknown tool: ${name || ""}`);
      }
      return tool.handler(call?.arguments);
    }
    throw new Error(`unknown method: ${method}`);
  }

  private send(socket: WebSocket, message: JSONRPCMessage) {
    if (socket.readyState !== WebSocket.OPEN) {
      return;
    }
    socket.send(JSON.stringify(message));
  }
}

function parseMessage(data: unknown): JSONRPCMessage | null {
  try {
    const text = typeof data === "string" ? data : data instanceof Blob ? null : String(data);
    if (!text) {
      return null;
    }
    const value = JSON.parse(text);
    return (asRecord(value) as JSONRPCMessage | undefined) ?? null;
  } catch {
    return null;
  }
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}
