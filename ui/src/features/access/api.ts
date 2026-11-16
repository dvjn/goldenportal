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

// SubjectMatch is a person, with their email, or a group, with its member
// count, that a search found to grant a role to.
export type SubjectMatch = {
  type: SubjectType;
  id: string;
  name: string;
  email?: string;
  memberCount?: number;
};

export const accessKeys = {
  grants: ["access"] as const,
  subjects: ["subjects"] as const,
};

export async function listGrants(): Promise<Grant[]> {
  return (await api.get("settings/access").json<ApiDataResponse<Grant[]>>()).data;
}

export async function grantRole(type: SubjectType, id: string, role: Role): Promise<void> {
  await api.put(`settings/access/${type}/${id}`, { json: { role } });
}

export async function revokeGrant(type: SubjectType, id: string): Promise<void> {
  await api.delete(`settings/access/${type}/${id}`);
}

// searchSubjects finds the few people and groups closest to q by name or
// email, leaving out the signed-in admin.
export async function searchSubjects(q: string): Promise<SubjectMatch[]> {
  return (
    await api
      .get("settings/access/subjects", { searchParams: { q } })
      .json<ApiDataResponse<SubjectMatch[]>>()
  ).data;
}
