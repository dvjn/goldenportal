import { type ApiDataResponse, api } from "@/lib/api";

export type Settings = {
  name: string;
  public_url: string;
  oidc_issuer_url: string;
  oidc_client_id: string;
  oidc_client_secret_set: boolean;
  session_max_age_seconds: number;
  session_idle_timeout_seconds: number;
  bootstrap: { admins: string[]; admin_granted: boolean };
};

export type Scim = {
  endpoint: string;
  last_push_at?: string;
  users: number;
  groups: number;
};

export type RotatedScimToken = {
  token: { id: string; created_at: string };
  secret: string;
};

export const settingsKeys = {
  settings: ["settings"] as const,
  scim: ["settings", "scim"] as const,
};

export async function getSettings(): Promise<Settings> {
  return (await api.get("settings").json<ApiDataResponse<Settings>>()).data;
}

export async function getScim(): Promise<Scim> {
  return (await api.get("scim").json<ApiDataResponse<Scim>>()).data;
}

export async function rotateScimToken(): Promise<RotatedScimToken> {
  return (await api.post("scim/token").json<ApiDataResponse<RotatedScimToken>>()).data;
}
