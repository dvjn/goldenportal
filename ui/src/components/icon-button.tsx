import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

type IconButtonProps = ComponentProps<typeof Button> & {
  // label names the action for screen readers and in the tooltip.
  label: string;
};

// IconButton is a compact button showing only an icon, labelled by a tooltip.
export function IconButton({
  label,
  variant = "ghost",
  size = "icon-sm",
  ...props
}: IconButtonProps) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant={variant} size={size} aria-label={label} {...props} />
        </TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
