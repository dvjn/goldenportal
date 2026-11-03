import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ChevronsUpDown } from "lucide-react";
import { useEffect, useState } from "react";
import { OptionMark } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { entitiesQueryOptions } from "@/features/catalog";

// pickerSize is how many entities the picker offers at once; typing narrows them.
const pickerSize = 20;

const searchDelay = 200;

type EntityPickerProps = {
  id: string;
  blueprint: string;
  // many picks several entities, and one otherwise.
  many: boolean;
  value: string[];
  // title is the entity just picked or dropped, for naming it elsewhere.
  onChange: (value: string[], title: string) => void;
  invalid?: boolean;
  required?: boolean;
};

// EntityPicker picks entities of a blueprint by identifier, searching the
// catalog as it's typed into.
export function EntityPicker({
  id,
  blueprint,
  many,
  value,
  onChange,
  invalid,
  required,
}: EntityPickerProps) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [query, setQuery] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => setQuery(text.trim()), searchDelay);
    return () => clearTimeout(timer);
  }, [text]);
  const found = useQuery({
    ...entitiesQueryOptions({ blueprint, q: query || undefined, size: pickerSize }),
    enabled: open,
    placeholderData: keepPreviousData,
  });
  // Values given before, such as a run's, are named once their entities load.
  const picked = useQuery({
    ...entitiesQueryOptions({
      blueprint,
      filter: { property: "identifier", op: "in", values: value },
      size: 100,
    }),
    enabled: value.length > 0,
    placeholderData: keepPreviousData,
  });
  const [titles, setTitles] = useState<Record<string, string>>({});
  const titleOf = (identifier: string) =>
    titles[identifier] ??
    picked.data?.entities.find((entity) => entity.identifier === identifier)?.title ??
    identifier;

  function toggle(identifier: string, title: string) {
    setTitles((was) => ({ ...was, [identifier]: title }));
    if (!many) {
      onChange(value.includes(identifier) ? [] : [identifier], title);
      setOpen(false);
      return;
    }
    onChange(
      value.includes(identifier) ? value.filter((v) => v !== identifier) : [...value, identifier],
      title,
    );
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          id={id}
          type="button"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          aria-invalid={invalid || undefined}
          aria-required={required || undefined}
          className="w-full justify-between font-normal"
        >
          {value.length > 0 ? (
            <span className="truncate">{value.map(titleOf).join(", ")}</span>
          ) : (
            <span className="text-muted-foreground">Choose…</span>
          )}
          <ChevronsUpDown className="text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-(--radix-popover-trigger-width) p-0" align="start">
        {/* The catalog searches and ranks entities, so the list shows them as given. */}
        <Command shouldFilter={false}>
          <CommandInput value={text} onValueChange={setText} placeholder="Search" />
          <CommandList>
            <CommandEmpty>
              {found.isFetching || query !== text.trim() ? "Searching…" : "Nothing matches."}
            </CommandEmpty>
            <CommandGroup>
              {found.data?.entities.map((entity) => (
                <CommandItem
                  key={entity.identifier}
                  value={entity.identifier}
                  onSelect={() => toggle(entity.identifier, entity.title)}
                >
                  <OptionMark multiple={many} checked={value.includes(entity.identifier)} />
                  <span className="truncate">{entity.title}</span>
                  <CommandShortcut className="min-w-0 truncate font-mono tracking-normal">
                    {entity.identifier}
                  </CommandShortcut>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
