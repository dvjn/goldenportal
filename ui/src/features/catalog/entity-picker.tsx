import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Check, ChevronsUpDown, X } from "lucide-react";
import { useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { EntityName, RelationView } from "./api";
import { entitiesQueryOptions } from "./queries";

type EntityPickerProps = {
  id: string;
  relation: RelationView;
  value: EntityName[];
  onChange: (value: EntityName[]) => void;
  invalid: boolean;
};

// EntityPicker searches the target blueprint's entities on the server, so it
// works for blueprints with more entities than one page holds.
export function EntityPicker({ id, relation, value, onChange, invalid }: EntityPickerProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const options = useQuery({
    ...entitiesQueryOptions(relation.target, { q: query || undefined, page: 1 }),
    enabled: open,
    placeholderData: keepPreviousData,
  });
  const chosen = new Set(value.map((entity) => entity.identifier));

  function toggle(entity: EntityName) {
    if (!relation.many) {
      onChange(chosen.has(entity.identifier) ? [] : [entity]);
      setOpen(false);
      return;
    }
    onChange(
      chosen.has(entity.identifier)
        ? value.filter((other) => other.identifier !== entity.identifier)
        : [...value, entity],
    );
  }

  const label = relation.many
    ? `${value.length} selected`
    : (value[0]?.title ?? `Choose a ${relation.target_title.toLowerCase()}`);

  return (
    <div className="flex flex-col gap-2">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            id={id}
            variant="outline"
            role="combobox"
            aria-expanded={open}
            aria-invalid={invalid}
            className="w-full justify-between"
          >
            <span className="truncate">{label}</span>
            <ChevronsUpDown />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-(--radix-popover-trigger-width) p-0" align="start">
          <Command shouldFilter={false}>
            <CommandInput
              placeholder={`Search ${relation.target_title.toLowerCase()} entities`}
              value={query}
              onValueChange={setQuery}
            />
            <CommandList>
              <CommandEmpty>
                {options.isPending ? "Searching" : "Nothing matches your search."}
              </CommandEmpty>
              <CommandGroup>
                {options.data?.entities.map((entity) => (
                  <CommandItem
                    key={entity.identifier}
                    value={entity.identifier}
                    onSelect={() =>
                      toggle({
                        blueprint: relation.target,
                        identifier: entity.identifier,
                        title: entity.title,
                      })
                    }
                  >
                    <Check className={chosen.has(entity.identifier) ? "" : "invisible"} />
                    <span className="truncate">{entity.title}</span>
                    <span className="text-muted-foreground ml-auto truncate font-mono text-xs">
                      {entity.identifier}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {relation.many && value.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {value.map((entity) => (
            <Badge key={entity.identifier} variant="secondary">
              {entity.title}
              <button
                type="button"
                aria-label={`Remove ${entity.title}`}
                onClick={() => toggle(entity)}
              >
                <X className="size-3" />
              </button>
            </Badge>
          ))}
        </div>
      )}
    </div>
  );
}
