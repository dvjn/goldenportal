import { Check } from "lucide-react";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
} from "@/components/ui/command";

// count, when known, is how many rows have the value.
export type DataTableOption = { value: string; label: string; count?: number };

type DataTableFacetedOptionsProps = {
  title: string;
  options: DataTableOption[];
  selected: string[];
  onChange: (selected: string[]) => void;
  // multiple is false when picking a value replaces the last one.
  multiple?: boolean;
};

// DataTableFacetedOptions is the searchable list of values a filter picks from.
export function DataTableFacetedOptions({
  title,
  options,
  selected,
  onChange,
  multiple = true,
  loading = false,
}: DataTableFacetedOptionsProps & {
  // loading says the options are still on their way.
  loading?: boolean;
}) {
  const chosen = new Set(selected);

  function toggle(value: string) {
    if (!multiple) {
      onChange(chosen.has(value) ? [] : [value]);
      return;
    }
    const next = new Set(chosen);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    onChange(options.map((option) => option.value).filter((v) => next.has(v)));
  }

  return (
    <Command>
      <CommandInput placeholder={title} />
      <CommandList>
        <CommandEmpty>{loading ? "Loading…" : "Nothing matches."}</CommandEmpty>
        <CommandGroup>
          {options.map((option) => (
            <CommandItem
              key={option.value}
              value={option.label}
              onSelect={() => toggle(option.value)}
              aria-selected={chosen.has(option.value)}
            >
              {multiple ? (
                <span
                  className={`flex size-4 items-center justify-center rounded-sm border ${
                    chosen.has(option.value)
                      ? "border-primary bg-primary text-primary-foreground"
                      : "border-input [&_svg]:invisible"
                  }`}
                >
                  <Check className="size-3" />
                </span>
              ) : (
                // A radio for a single value, round despite the squircle default.
                <span
                  className={`flex size-4 items-center justify-center rounded-full border [corner-shape:round] ${
                    chosen.has(option.value) ? "border-primary" : "border-input"
                  }`}
                >
                  {chosen.has(option.value) && (
                    <span className="size-2 rounded-full bg-primary [corner-shape:round]" />
                  )}
                </span>
              )}
              {option.label}
              {option.count !== undefined && (
                <CommandShortcut className="font-mono tracking-normal tabular-nums">
                  {option.count}
                </CommandShortcut>
              )}
            </CommandItem>
          ))}
        </CommandGroup>
        {selected.length > 0 && (
          <>
            <CommandSeparator className="mx-1" />
            <CommandGroup>
              <CommandItem onSelect={() => onChange([])} className="justify-center">
                Clear
              </CommandItem>
            </CommandGroup>
          </>
        )}
      </CommandList>
    </Command>
  );
}
