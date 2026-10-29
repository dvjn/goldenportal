import { useState } from "react";
import { Button } from "@/components/ui/button";
import { formatConfigValue, isStructured } from "../format";

const collapsedLines = 6;

export function ConfigValue({ value }: Readonly<{ value: unknown }>) {
  const [expanded, setExpanded] = useState(false);
  const text = formatConfigValue(value);
  if (!isStructured(value)) return <code className="font-mono text-xs">{text}</code>;

  const lines = text.split("\n");
  const long = lines.length > collapsedLines;
  const shown = long && !expanded ? lines.slice(0, collapsedLines).join("\n") : text;

  return (
    <div className="flex flex-col items-start gap-1">
      <pre className="w-full overflow-x-auto rounded-lg border bg-muted/30 p-3 font-mono text-xs leading-5">
        {shown}
      </pre>
      {long ? (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-expanded={expanded}
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? "Show less" : `Show more (${lines.length - collapsedLines} lines)`}
        </Button>
      ) : null}
    </div>
  );
}
