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

type RoleSelectProps = {
  value: Role;
  onChange: (role: Role) => void;
  label: string;
  id?: string;
  disabled?: boolean;
  className?: string;
};

export function RoleSelect({ value, onChange, label, id, disabled, className }: RoleSelectProps) {
  return (
    <Select value={value} onValueChange={(next) => onChange(next as Role)} disabled={disabled}>
      <SelectTrigger id={id} size="sm" className={className ?? "w-32"} aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {(Object.keys(roleLabels) as Role[]).map((role) => (
          <SelectItem key={role} value={role}>
            {roleLabels[role]}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
