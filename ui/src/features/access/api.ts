import type { Role } from "@/features/auth";
import { type ApiDataResponse, api } from "@/lib/api";

export type GrantRole = Exclude<Role, "member">;

export type SubjectType = "user" | "group";

export type Subject = { type: SubjectType; id: string };

export type Grant = {
  id: string;
  subject: Subject & { name: string; email?: string; member_count: number };
  role: GrantRole;
  granted_by: string;
  created_at: string;
  updated_at: string;
  last_admin: boolean;
};

export type SubjectOption = {
  id: string;
  name: string;
  email?: string;
  active: boolean;
  role?: GrantRole;
};

export type Subjects = {
  groups: SubjectOption[];
  users: SubjectOption[];
};

export type UserCheck = {
  user: { id: string; name: string; email: string; active: boolean };
  role: Role | "";
  bootstrap: boolean;
  carried_by: (Subject & { name: string; role: GrantRole })[];
  groups: { id: string; name: string }[];
  last_sign_in_at?: string;
};

export const accessKeys = {
  all: ["access"] as const,
  grants: ["access", "grants"] as const,
  subjects: ["access", "subjects"] as const,
  user: (id: string) => ["access", "users", id] as const,
};

type GrantList = { grants: Grant[] };

function grantPath({ type, id }: Subject) {
  return `access/grants/${type}/${id}`;
}

export async function getGrants(): Promise<Grant[]> {
  return (await api.get("access/grants").json<ApiDataResponse<GrantList>>()).data.grants;
}

export async function setGrant({ subject, role }: { subject: Subject; role: GrantRole }) {
  return (await api.put(grantPath(subject), { json: { role } }).json<ApiDataResponse<GrantList>>())
    .data.grants;
}

export async function removeGrant(subject: Subject) {
  return (await api.delete(grantPath(subject)).json<ApiDataResponse<GrantList>>()).data.grants;
}

export async function getSubjects(): Promise<Subjects> {
  return (await api.get("access/subjects").json<ApiDataResponse<Subjects>>()).data;
}

export async function getUserCheck(id: string): Promise<UserCheck> {
  return (await api.get(`access/users/${id}`).json<ApiDataResponse<UserCheck>>()).data;
}

export async function revokeSessions(id: string) {
  await api.delete(`access/users/${id}/sessions`);
}
