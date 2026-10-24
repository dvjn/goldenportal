import { type ApiDataResponse, api } from "@/lib/api";

export type ApiToken = {
  id: string;
  name: string;
  prefix: string;
  readOnly: boolean;
  expiresAt: string;
  lastUsedAt: string | null;
  createdAt: string;
};

export type NewToken = {
  name: string;
  readOnly: boolean;
  expiresInDays: number;
};

export type CreatedToken = {
  token: ApiToken;
  // The full token, shown once. The server keeps only its hash.
  secret: string;
};

export const tokenKeys = {
  all: ["tokens"] as const,
};

export async function listTokens(): Promise<ApiToken[]> {
  return (await api.get("me/tokens").json<ApiDataResponse<ApiToken[]>>()).data;
}

export async function createToken(input: NewToken): Promise<CreatedToken> {
  return (await api.post("me/tokens", { json: input }).json<ApiDataResponse<CreatedToken>>()).data;
}

export async function revokeToken(id: string): Promise<void> {
  await api.delete(`me/tokens/${id}`);
}
