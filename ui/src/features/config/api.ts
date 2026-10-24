import { type ApiDataResponse, api } from "@/lib/api";

export type Config = {
  name: string;
  logoUrl?: string;
};

export const configKeys = {
  current: ["config"] as const,
};

export async function getConfig(): Promise<Config> {
  return (await api.get("config").json<ApiDataResponse<Config>>()).data;
}
