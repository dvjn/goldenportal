import { KeyRound, Users } from "lucide-react";
import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import type { Audience } from "@/features/dashboards";

// AudienceValue lists an audience's roles and groups, each marked as which
// it is, or none when there's no audience.
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
          <KeyRound />
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
