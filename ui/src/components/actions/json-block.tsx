import { cn } from "cn";
import { ChevronDown } from "lucide-react";
import { useState } from "react";
import { CodeBlock } from "@/components/code";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";

// longJson is how many lines of JSON show before a block folds away.
const longJson = 15;

export const blockHeading = "text-xs font-medium tracking-wide text-muted-foreground uppercase";

// JsonBlock lays a value out as JSON to read, under its title. A long one
// starts folded, with its line count, so it doesn't bury what follows.
export function JsonBlock({ title, value }: Readonly<{ title?: string; value: unknown }>) {
  const json = JSON.stringify(value, null, 2) ?? "";
  const lines = json.split("\n").length;
  const [open, setOpen] = useState(lines <= longJson);

  if (lines <= longJson) {
    return (
      <section className="grid gap-1.5">
        {title && <h4 className={blockHeading}>{title}</h4>}
        <CodeBlock>{json}</CodeBlock>
      </section>
    );
  }
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="grid gap-1.5">
      <CollapsibleTrigger
        className={cn(
          "flex w-fit items-center gap-1 outline-none hover:text-foreground focus-visible:text-foreground",
          blockHeading,
        )}
      >
        <ChevronDown
          className={cn("size-3.5 transition-transform", !open && "-rotate-90")}
          aria-hidden
        />
        {title ?? "JSON"}
        <span className="font-normal normal-case tabular-nums">{lines} lines</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <CodeBlock>{json}</CodeBlock>
      </CollapsibleContent>
    </Collapsible>
  );
}
