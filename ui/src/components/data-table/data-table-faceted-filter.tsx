import { Check } from "lucide-react";
import { EmptyState } from "@/components/empty-state";
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
        <CommandEmpty className="py-0">
          {loading ? (
            <span className="block py-6">Loading…</span>
          ) : (
            <EmptyState compact title="No options match" />
          )}
        </CommandEmpty>
        <CommandGroup>
          {options.map((option) => (
            <CommandItem
              key={option.value}
              value={option.label}
              onSelect={() => toggle(option.value)}
              aria-selected={chosen.has(option.value)}
            >
              <OptionMark multiple={multiple} checked={chosen.has(option.value)} />
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

// OptionMark marks an option of a list as picked or not: a checkbox where
// several can be, and a radio where one can.
export function OptionMark({
  multiple,
  checked,
}: Readonly<{ multiple: boolean; checked: boolean }>) {
  if (multiple) {
    return (
      <span
        className={`flex size-4 items-center justify-center rounded-sm border ${
          checked
            ? "border-primary bg-primary text-primary-foreground"
            : "border-input [&_svg]:invisible"
        }`}
      >
        <Check className="size-3" />
      </span>
    );
  }
  // A radio for a single value, round despite the squircle default.
  return (
    <span
      className={`flex size-4 items-center justify-center rounded-full border [corner-shape:round] ${
        checked ? "border-primary" : "border-input"
      }`}
    >
      {checked && <span className="size-2 rounded-full bg-primary [corner-shape:round]" />}
    </span>
  );
}
