import { cn } from "cn";
import { Box } from "lucide-react";
import { DynamicIcon, type IconName, iconNames } from "lucide-react/dynamic";

// BlueprintIcon draws the lucide icon a blueprint names, or a box when it
// names none or one that doesn't exist.
export function BlueprintIcon({
  icon,
  className,
}: Readonly<{ icon?: string; className?: string }>) {
  if (icon && (iconNames as readonly string[]).includes(icon)) {
    return <DynamicIcon name={icon as IconName} className={className} aria-hidden />;
  }
  return <Box className={className} aria-hidden />;
}

// IconTile sets an icon on a tile beside a title.
export function IconTile({
  icon,
  size = "default",
}: Readonly<{ icon?: string; size?: "default" | "lg" }>) {
  return (
    <span
      className={cn(
        "flex shrink-0 items-center justify-center rounded-lg bg-muted",
        size === "lg" ? "size-10" : "size-9",
      )}
    >
      <BlueprintIcon icon={icon} className={size === "lg" ? "size-5" : "size-4.5"} />
    </span>
  );
}
