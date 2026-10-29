import type { Role } from "@/features/auth";
import { type ApiDataResponse, api } from "@/lib/api";

export type TokenRole = Exclude<Role, "admin">;

export type ServiceToken = {
  id: string;
  name: string;
  role: TokenRole;
  created_by: string;
  created_at: string;
  expires_at: string;
  last_used_at?: string;
};

export type NewServiceToken = {
  name: string;
  role: TokenRole;
  expires_in_days: number;
};

export type CreatedServiceToken = {
  token: ServiceToken;
  secret: string;
};

export const serviceTokenKeys = {
  all: ["service-tokens"] as const,
};

export async function getServiceTokens(): Promise<ServiceToken[]> {
  return (
    await api.get("access/service-tokens").json<ApiDataResponse<{ tokens: ServiceToken[] }>>()
  ).data.tokens;
}

export async function createServiceToken(token: NewServiceToken): Promise<CreatedServiceToken> {
  return (
    await api
      .post("access/service-tokens", { json: token })
      .json<ApiDataResponse<CreatedServiceToken>>()
  ).data;
}

export async function revokeServiceToken(id: string) {
  await api.delete(`access/service-tokens/${id}`);
}
