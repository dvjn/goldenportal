import { Check, PlusCircle } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Separator } from "@/components/ui/separator";

type Option = { value: string; label: string };

type DataTableFacetedFilterProps = {
  title: string;
  options: Option[];
  selected: string[];
  onChange: (selected: string[]) => void;
};

// DataTableFacetedFilter picks any number of values for one field, after the
// shadcn data table's faceted filter.
export function DataTableFacetedFilter({
  title,
  options,
  selected,
  onChange,
}: DataTableFacetedFilterProps) {
  const chosen = new Set(selected);

  function toggle(value: string) {
    const next = new Set(chosen);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    onChange(options.map((option) => option.value).filter((v) => next.has(v)));
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" className="border-dashed">
          <PlusCircle />
          {title}
          {selected.length > 0 && (
            <>
              <Separator orientation="vertical" className="mx-1 data-vertical:h-4" />
              {selected.length > 2 ? (
                <Badge variant="secondary">{selected.length} selected</Badge>
              ) : (
                options
                  .filter((option) => chosen.has(option.value))
                  .map((option) => (
                    <Badge key={option.value} variant="secondary">
                      {option.label}
                    </Badge>
                  ))
              )}
            </>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-60 p-0" align="start">
        <Command>
          <CommandInput placeholder={title} />
          <CommandList>
            <CommandEmpty>Nothing matches.</CommandEmpty>
            <CommandGroup>
              {options.map((option) => (
                <CommandItem
                  key={option.value}
                  value={option.label}
                  onSelect={() => toggle(option.value)}
                  aria-selected={chosen.has(option.value)}
                >
                  <span
                    className={`flex size-4 items-center justify-center rounded-sm border ${
                      chosen.has(option.value)
                        ? "border-primary bg-primary text-primary-foreground"
                        : "border-input [&_svg]:invisible"
                    }`}
                  >
                    <Check className="size-3" />
                  </span>
                  {option.label}
                </CommandItem>
              ))}
            </CommandGroup>
            {selected.length > 0 && (
              <>
                <CommandSeparator />
                <CommandGroup>
                  <CommandItem onSelect={() => onChange([])} className="justify-center">
                    Clear
                  </CommandItem>
                </CommandGroup>
              </>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
