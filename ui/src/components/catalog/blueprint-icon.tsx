import { cn } from "cn";
import { Box } from "lucide-react";
import { DynamicIcon, type IconName, iconNames } from "lucide-react/dynamic";
import { swatch } from "@/lib/colors";

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

const tileSizes = {
  // sm heads a group of rows.
  sm: { tile: "size-6 rounded-md", icon: "size-3.5" },
  default: { tile: "size-9", icon: "size-4.5" },
  lg: { tile: "size-10", icon: "size-5" },
  // xl stands as tall as a stat's title over its number.
  xl: { tile: "size-12", icon: "size-6" },
};

// IconTile sets an icon on a tile beside a title, tinted when given one of
// the config colors.
export function IconTile({
  icon,
  color,
  size = "default",
}: Readonly<{ icon?: string; color?: string; size?: keyof typeof tileSizes }>) {
  return (
    <span
      className={cn(
        "flex shrink-0 items-center justify-center rounded-lg",
        swatch(color)?.badge ?? "bg-muted",
        tileSizes[size].tile,
      )}
    >
      <BlueprintIcon icon={icon} className={tileSizes[size].icon} />
    </span>
  );
}
