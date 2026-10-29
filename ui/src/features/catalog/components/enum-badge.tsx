import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { EnumColor, PropertyView } from "../api";

const enumColorClasses: Record<EnumColor, string> = {
  gray: "bg-zinc-500/15 text-zinc-700 dark:text-zinc-300",
  blue: "bg-blue-500/15 text-blue-700 dark:text-blue-300",
  green: "bg-green-500/15 text-green-700 dark:text-green-300",
  yellow: "bg-yellow-500/15 text-yellow-700 dark:text-yellow-300",
  orange: "bg-orange-500/15 text-orange-700 dark:text-orange-300",
  red: "bg-red-500/15 text-red-700 dark:text-red-300",
  purple: "bg-purple-500/15 text-purple-700 dark:text-purple-300",
  teal: "bg-teal-500/15 text-teal-700 dark:text-teal-300",
};

export function enumColor(
  property: Pick<PropertyView, "enum_colors"> | undefined,
  value: string,
): EnumColor {
  return property?.enum_colors?.[value] ?? "gray";
}

export function enumLabel(
  property: Pick<PropertyView, "enum_labels"> | undefined,
  value: string,
): string {
  return property?.enum_labels?.[value] ?? value;
}

export function EnumBadge({ label, color }: Readonly<{ label: string; color: EnumColor }>) {
  return (
    <Badge variant="outline" data-color={color} className={cn("border-0", enumColorClasses[color])}>
      {label}
    </Badge>
  );
}
