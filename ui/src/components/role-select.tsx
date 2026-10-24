import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { Role } from "@/features/auth";

export const roleLabels: Record<Role, string> = {
  admin: "Admin",
  builder: "Builder",
  member: "Member",
};

const none = "none";

type RoleSelectProps = {
  value: Role | null;
  onChange: (role: Role | null) => void;
  // noneLabel names the "no role bound" choice, e.g. "From groups".
  noneLabel: string;
  label: string;
  disabled?: boolean;
};

export function RoleSelect({ value, onChange, noneLabel, label, disabled }: RoleSelectProps) {
  return (
    <Select
      value={value ?? none}
      onValueChange={(next) => onChange(next === none ? null : (next as Role))}
      disabled={disabled}
    >
      <SelectTrigger size="sm" className="w-36" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={none}>{noneLabel}</SelectItem>
        {(Object.keys(roleLabels) as Role[]).map((role) => (
          <SelectItem key={role} value={role}>
            {roleLabels[role]}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
