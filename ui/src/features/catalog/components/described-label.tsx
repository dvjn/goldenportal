import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

export function DescribedLabel({
  label,
  description,
}: Readonly<{ label: string; description?: string }>) {
  if (!description) return label;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="cursor-help underline decoration-dotted underline-offset-4">{label}</span>
      </TooltipTrigger>
      <TooltipContent>{description}</TooltipContent>
    </Tooltip>
  );
}
