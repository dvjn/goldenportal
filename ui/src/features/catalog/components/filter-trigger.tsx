import { ChevronDownIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";

export function CheckMark({ checked }: Readonly<{ checked: boolean }>) {
  return (
    <Checkbox checked={checked} tabIndex={-1} aria-hidden="true" className="pointer-events-none" />
  );
}

export function selectionSummary(values: string[]): string {
  if (values.length === 0) return "Any";
  if (values.length === 1) return values[0] ?? "Any";
  return `${values.length} selected`;
}

export function FilterTrigger({
  label,
  summary,
  ...props
}: Readonly<{ label: string; summary: string } & ComponentProps<typeof Button>>) {
  return (
    <Button
      type="button"
      variant="outline"
      aria-label={`${label}: ${summary}`}
      className="gap-1.5 pr-2 pl-2.5 font-normal"
      {...props}
    >
      <span className="text-muted-foreground">{label}</span>
      <span className="max-w-40 truncate">{summary}</span>
      <ChevronDownIcon aria-hidden="true" className="text-muted-foreground" />
    </Button>
  );
}
