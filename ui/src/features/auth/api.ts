import { type ApiDataResponse, api } from "@/lib/api";

export type Role = "member" | "builder" | "admin";

const roleRank: Record<Role, number> = { member: 1, builder: 2, admin: 3 };

// hasRole reports whether role includes required. Roles nest, as on the server.
export function hasRole(role: Role, required: Role) {
  return roleRank[role] >= roleRank[required];
}

export type Me = {
  id: string;
  email: string;
  displayName: string;
  role: Role;
  // groups are the names of the identity provider's groups they're in.
  groups?: string[];
};

export const authKeys = {
  me: ["me"] as const,
};

export async function getMe(): Promise<Me> {
  return (await api.get("me").json<ApiDataResponse<Me>>()).data;
}

export async function signOut(): Promise<void> {
  await api.post("auth/logout");
}

// signInUrl starts single sign-on. It is a full-page navigation, not an API call,
// because the identity provider takes over the browser.
export function signInUrl(redirect: string) {
  return `/api/v1/auth/login?${new URLSearchParams({ redirect })}`;
}
