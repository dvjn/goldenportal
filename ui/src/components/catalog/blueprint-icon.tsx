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
