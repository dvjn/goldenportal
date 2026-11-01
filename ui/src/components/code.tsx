import { cn } from "cn";
import type { ComponentProps } from "react";

// Code is an expression or pattern set off on its own tint, in a line of
// text or a table cell.
export function Code({ className, ...props }: ComponentProps<"code">) {
  return (
    <code
      className={cn("rounded bg-muted px-1 font-mono text-xs break-all", className)}
      {...props}
    />
  );
}

// CodeBlock is a longer expression laid out to read, such as a check's jq.
export function CodeBlock({ className, ...props }: ComponentProps<"pre">) {
  return (
    <pre
      className={cn(
        "rounded-md bg-muted px-2.5 py-2 font-mono text-xs leading-5 break-words whitespace-pre-wrap",
        className,
      )}
      {...props}
    />
  );
}
