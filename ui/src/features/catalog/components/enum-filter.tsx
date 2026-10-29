import type { ReactNode } from "react";
import {
  Command,
  CommandGroup,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { EntityFilter } from "../api";
import { listFilter, toggleValue } from "../search";
import { CheckMark, FilterTrigger, selectionSummary } from "./filter-trigger";

export type EnumOption = { value: string; label: string; content: ReactNode };

export function EnumFilter({
  label,
  options,
  filter,
  onChange,
}: Readonly<{
  label: string;
  options: EnumOption[];
  filter: EntityFilter | undefined;
  onChange: (filter: EntityFilter | undefined) => void;
}>) {
  const selected = filter?.in ?? [];
  const labelOf = (value: string) =>
    options.find((option) => option.value === value)?.label ?? value;
  const select = (values: string[]) => onChange(listFilter(values));

  return (
    <Popover>
      <PopoverTrigger asChild>
        <FilterTrigger label={label} summary={selectionSummary(selected.map(labelOf))} />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-56 p-0">
        <Command>
          <CommandList>
            <CommandGroup>
              {options.map((option) => (
                <CommandItem
                  key={option.value}
                  value={option.value}
                  data-checked={selected.includes(option.value)}
                  onSelect={() => select(toggleValue(selected, option.value))}
                >
                  <CheckMark checked={selected.includes(option.value)} />
                  {option.content}
                </CommandItem>
              ))}
            </CommandGroup>
            {selected.length > 0 ? (
              <>
                <CommandSeparator />
                <CommandGroup>
                  <CommandItem onSelect={() => select([])}>Clear selection</CommandItem>
                </CommandGroup>
              </>
            ) : null}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
