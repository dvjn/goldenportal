import { type ApiDataResponse, api } from "@/lib/api";

export type Role = "member" | "maintainer" | "admin";

export type Permission =
  | "settings.read"
  | "access.read"
  | "access.write"
  | "audit.read"
  | "catalog.read"
  | "catalog.write"
  | "catalog.model"
  | "integrations.sync"
  | "integrations.tokens";

export type Me = {
  user: { id: string; email: string; name: string };
  groups: { id: string; name: string }[];
  role: Role;
  permissions: Permission[];
  bootstrap: boolean;
};

export type Logout = {
  redirect_to: string;
};

export const authKeys = {
  me: ["auth", "me"] as const,
};

export async function getMe(): Promise<Me> {
  return (await api.get("auth/me").json<ApiDataResponse<Me>>()).data;
}

export async function logout(): Promise<Logout> {
  return (await api.post("auth/logout").json<ApiDataResponse<Logout>>()).data;
}

export function loginUrl(returnTo: string) {
  return `/api/v1/auth/login?return_to=${encodeURIComponent(returnTo)}`;
}

export const signoutUrl = "/api/v1/auth/signout";
