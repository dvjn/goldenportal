import { Badge } from "@/components/ui/badge";
import type { Role } from "@/features/auth";

export const roleLabels: Record<Role, string> = {
  member: "Member",
  maintainer: "Maintainer",
  admin: "Admin",
};

export function RoleBadge({ role }: { role: Role }) {
  return <Badge variant={role === "admin" ? "default" : "secondary"}>{roleLabels[role]}</Badge>;
}
