import { type ApiDataResponse, api } from "@/lib/api";
import type { TracingConfig } from "@/lib/tracing";

export type Config = {
  name: string;
  logoUrl?: string;
  // tracing is there only when the server takes traces from the browser.
  tracing?: TracingConfig;
  // mcp are the MCP servers' URLs, once the portal knows its public URL.
  mcp?: { user: string; builder: string };
};

export const configKeys = {
  current: ["config"] as const,
};

export async function getConfig(): Promise<Config> {
  return (await api.get("config").json<ApiDataResponse<Config>>()).data;
}
