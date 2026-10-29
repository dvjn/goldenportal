import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { EntityFilter } from "../api";
import { listFilter } from "../search";

const anyValue = "any";

export function BooleanFilter({
  label,
  filter,
  onChange,
}: Readonly<{
  label: string;
  filter: EntityFilter | undefined;
  onChange: (filter: EntityFilter | undefined) => void;
}>) {
  const value = filter?.in?.[0] ?? anyValue;

  return (
    <Select
      value={value}
      onValueChange={(next) => onChange(next === anyValue ? undefined : listFilter([next]))}
    >
      <SelectTrigger aria-label={label} className="bg-background">
        <span className="text-muted-foreground">{label}</span>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={anyValue}>Any</SelectItem>
        <SelectItem value="true">Yes</SelectItem>
        <SelectItem value="false">No</SelectItem>
      </SelectContent>
    </Select>
  );
}
