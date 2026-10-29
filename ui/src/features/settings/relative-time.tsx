import type { ReactNode } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatDateTime, formatRelative, notSet } from "@/lib/time";

export function RelativeTime({
  at,
  className,
  empty = notSet,
}: {
  at: string | null | undefined;
  className?: string;
  empty?: ReactNode;
}) {
  if (!at) return <span className="text-muted-foreground">{empty}</span>;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <time dateTime={at} className={className}>
          {formatRelative(at)}
        </time>
      </TooltipTrigger>
      <TooltipContent>{formatDateTime(at)}</TooltipContent>
    </Tooltip>
  );
}
