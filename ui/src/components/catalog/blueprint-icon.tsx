import { cn } from "cn";
import { Box, type LucideIcon } from "lucide-react";
import { DynamicIcon, type IconName, iconNames } from "lucide-react/dynamic";
import { createContext, useContext } from "react";
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

// InCrumb is set inside a breadcrumb, where icons stand at the size of its
// text, so a document's tile shrinks to fit the trail.
export const InCrumb = createContext(false);

const tileSizes = {
  // xs leads a breadcrumb, its glyph the size of the tab icons beside it.
  xs: { tile: "size-6 rounded-md", icon: "size-4" },
  // sm heads a group of rows.
  sm: { tile: "size-6 rounded-md", icon: "size-3.5" },
  default: { tile: "size-9", icon: "size-4.5" },
  lg: { tile: "size-10", icon: "size-5" },
  // xl stands as tall as a stat's title over its number.
  xl: { tile: "size-12", icon: "size-6" },
};

// IconTile sets an icon on a tile beside a title, tinted when given one of
// the config colors. icon names a lucide icon, as config does; the app's own
// pages pass theirs as Icon instead.
export function IconTile({
  icon,
  Icon,
  color,
  size = "default",
}: Readonly<{
  icon?: string;
  Icon?: LucideIcon;
  color?: string;
  size?: keyof typeof tileSizes;
}>) {
  const shown = useContext(InCrumb) ? "xs" : size;
  return (
    <span
      className={cn(
        "flex shrink-0 items-center justify-center rounded-lg",
        swatch(color)?.badge ?? "bg-muted",
        tileSizes[shown].tile,
      )}
    >
      {Icon ? (
        <Icon className={tileSizes[shown].icon} aria-hidden />
      ) : (
        <BlueprintIcon icon={icon} className={tileSizes[shown].icon} />
      )}
    </span>
  );
}
