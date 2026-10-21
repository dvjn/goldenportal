import type { Role } from "@/features/auth";
import { type ApiDataResponse, api } from "@/lib/api";

export type TokenRole = Exclude<Role, "admin">;

export type ApiToken = {
  id: string;
  name: string;
  role: TokenRole;
  created_by: string;
  created_at: string;
  expires_at: string;
  last_used_at?: string;
};

export type NewApiToken = {
  name: string;
  role: TokenRole;
  expires_in_days: number;
};

export type CreatedApiToken = {
  token: ApiToken;
  secret: string;
};

export const apiTokenKeys = {
  all: ["api-tokens"] as const,
};

export async function getApiTokens(): Promise<ApiToken[]> {
  return (await api.get("access/tokens").json<ApiDataResponse<{ tokens: ApiToken[] }>>()).data
    .tokens;
}

export async function createApiToken(token: NewApiToken): Promise<CreatedApiToken> {
  return (await api.post("access/tokens", { json: token }).json<ApiDataResponse<CreatedApiToken>>())
    .data;
}

export async function revokeApiToken(id: string) {
  await api.delete(`access/tokens/${id}`);
}
