import { ChevronsUpDown } from "lucide-react";
import { useState } from "react";
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
import type { Subject, SubjectOption, Subjects, SubjectType } from "./api";
import { roleLabels } from "./role-badge";

function detail(option: SubjectOption) {
  if (!option.active) return "Inactive";
  return option.role ? roleLabels[option.role] : option.email;
}

type SubjectPickerProps = {
  id?: string;
  "aria-label"?: string;
  subjects: Subjects;
  value: Subject | undefined;
  onChange: (subject: Subject) => void;
  usersOnly?: boolean;
  placeholder: string;
};

// The grant picker offers groups and active users; the user check offers
// every user, inactive ones too, to explain why they cannot sign in.
export function SubjectPicker({
  id,
  "aria-label": label,
  subjects,
  value,
  onChange,
  usersOnly,
  placeholder,
}: SubjectPickerProps) {
  const [open, setOpen] = useState(false);
  const groups: [SubjectType, string, SubjectOption[]][] = [
    ["group", "Groups", usersOnly ? [] : subjects.groups],
    ["user", "Users", subjects.users],
  ];
  const selected = groups
    .find(([type]) => type === value?.type)?.[2]
    .find((option) => option.id === value?.id);

  function pick(subject: Subject) {
    onChange(subject);
    setOpen(false);
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          id={id}
          aria-label={label}
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="w-full justify-between"
        >
          <span className="truncate">{selected ? selected.name : placeholder}</span>
          <ChevronsUpDown />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-(--radix-popover-trigger-width) p-0" align="start">
        <Command>
          <CommandInput placeholder={usersOnly ? "Search users" : "Search users and groups"} />
          <CommandList>
            <CommandEmpty>Nothing matches your search.</CommandEmpty>
            {groups
              .filter(([, , options]) => options.length > 0)
              .map(([type, heading, options]) => (
                <CommandGroup key={type} heading={heading}>
                  {options.map((option) => (
                    <CommandItem
                      key={option.id}
                      value={`${option.name} ${option.email ?? ""} ${option.id}`}
                      disabled={!usersOnly && !option.active}
                      onSelect={() => pick({ type, id: option.id })}
                    >
                      <span className="truncate">{option.name}</span>
                      <span className="text-muted-foreground ml-auto truncate text-xs">
                        {detail(option)}
                      </span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
