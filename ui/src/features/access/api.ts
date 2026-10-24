import type { Role } from "@/features/auth";
import { type ApiDataResponse, api } from "@/lib/api";

export type User = {
  id: string;
  email: string;
  displayName: string;
  status: "active" | "deactivated";
  lastLoginAt: string | null;
  // The role bound to the user directly, or null when they only get roles from groups.
  role: Role | null;
  // The highest role from the user and their groups.
  effectiveRole: Role;
  groups: string[];
};

export type Group = {
  id: string;
  name: string;
  isTeam: boolean;
  memberCount: number;
  role: Role | null;
};

export type GroupUpdate = Pick<Group, "role" | "isTeam">;

export const accessKeys = {
  users: ["users"] as const,
  groups: ["groups"] as const,
};

export async function listUsers(): Promise<User[]> {
  return (await api.get("users").json<ApiDataResponse<User[]>>()).data;
}

export async function setUserRole(id: string, role: Role | null): Promise<void> {
  await api.put(`users/${id}/role`, { json: { role } });
}

export async function listGroups(): Promise<Group[]> {
  return (await api.get("groups").json<ApiDataResponse<Group[]>>()).data;
}

export async function updateGroup(id: string, update: GroupUpdate): Promise<void> {
  await api.put(`groups/${id}`, { json: update });
}
