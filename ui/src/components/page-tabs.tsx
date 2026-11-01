import { cn } from "cn";
import type { ComponentProps } from "react";
import { TabsTrigger } from "@/components/ui/tabs";

// PageTab is one of a page's tabs, in the line under its header. A soft
// highlight suits underlined tabs better than the default ring, so focus
// takes the hover tint instead.
export function PageTab({ className, ...props }: ComponentProps<typeof TabsTrigger>) {
  return (
    <TabsTrigger
      className={cn(
        "focus-visible:border-transparent focus-visible:bg-accent focus-visible:ring-0 focus-visible:outline-none",
        className,
      )}
      {...props}
    />
  );
}
