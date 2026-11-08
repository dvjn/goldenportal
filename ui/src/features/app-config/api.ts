import { type ApiDataResponse, api } from "@/lib/api";
import type { TracingConfig } from "@/lib/tracing";

export type Config = {
  name: string;
  logoUrl?: string;
  // tracing is there only when the server takes traces from the browser.
  tracing?: TracingConfig;
};

export const configKeys = {
  current: ["config"] as const,
};

export async function getConfig(): Promise<Config> {
  return (await api.get("config").json<ApiDataResponse<Config>>()).data;
}
