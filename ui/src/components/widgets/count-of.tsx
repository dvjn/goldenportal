import { cn } from "cn";
import { formatCount } from "@/lib/format";

// CountOf says how many of how many a share is of, beside the share.
export function CountOf({
  count,
  total,
  className,
}: Readonly<{ count: number; total: number; className?: string }>) {
  return (
    <span className={cn("text-xs text-muted-foreground tabular-nums", className)}>
      {formatCount(count)} of {formatCount(total)}
    </span>
  );
}
