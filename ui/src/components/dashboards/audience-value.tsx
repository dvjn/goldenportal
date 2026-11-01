import { Users } from "lucide-react";
import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import type { Audience } from "@/features/dashboards";

// Everyone is an audience of all, as a chip like the roles and groups.
export function Everyone() {
  return <Badge variant="outline">Everyone</Badge>;
}

// AudienceValue lists an audience's roles and groups, groups marked with
// their icon, or none when there's no audience.
export function AudienceValue({
  audience,
  none,
}: Readonly<{ audience?: Audience | null; none: ReactNode }>) {
  const roles = audience?.roles ?? [];
  const groups = audience?.groups ?? [];
  if (roles.length === 0 && groups.length === 0) return none;
  return (
    <span className="flex flex-wrap gap-1">
      {roles.map((role) => (
        <Badge key={`role:${role}`} variant="outline" title="Role">
          {role}
        </Badge>
      ))}
      {groups.map((group) => (
        <Badge key={`group:${group}`} variant="outline" title="Group">
          <Users />
          {group}
        </Badge>
      ))}
    </span>
  );
}
