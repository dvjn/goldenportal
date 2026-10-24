import type { Role } from "@/features/auth";
import { type ApiDataResponse, api } from "@/lib/api";

export type SubjectType = "user" | "group";

// Grant is a role given directly to a user or a group.
export type Grant = {
  subjectType: SubjectType;
  subjectId: string;
  name: string;
  email?: string;
  memberCount?: number;
  role: Role;
  grantedAt: string;
};

export type DirectoryUser = { id: string; displayName: string; email: string };
export type DirectoryGroup = { id: string; name: string; memberCount: number };

export const accessKeys = {
  grants: ["access"] as const,
  users: ["users"] as const,
  groups: ["groups"] as const,
};

export async function listGrants(): Promise<Grant[]> {
  return (await api.get("access").json<ApiDataResponse<Grant[]>>()).data;
}

export async function grantRole(type: SubjectType, id: string, role: Role): Promise<void> {
  await api.put(`access/${type}/${id}`, { json: { role } });
}

export async function revokeGrant(type: SubjectType, id: string): Promise<void> {
  await api.delete(`access/${type}/${id}`);
}

export async function listUsers(): Promise<DirectoryUser[]> {
  return (await api.get("users").json<ApiDataResponse<DirectoryUser[]>>()).data;
}

export async function listGroups(): Promise<DirectoryGroup[]> {
  return (await api.get("groups").json<ApiDataResponse<DirectoryGroup[]>>()).data;
}
