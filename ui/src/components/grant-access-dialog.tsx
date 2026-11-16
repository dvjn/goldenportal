import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronsUpDown, Plus, User, Users } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { RoleSelect, roleLabels } from "@/components/role-select";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  accessKeys,
  grantRole,
  type SubjectMatch,
  type SubjectType,
  subjectsQueryOptions,
} from "@/features/access";
import type { Role } from "@/features/auth";
import { useDebounced } from "@/hooks/use-debounced";
import { formatCount } from "@/lib/format";

type Subject = { type: SubjectType; id: string; name: string };

export function GrantAccessDialog() {
  const [open, setOpen] = useState(false);
  const [subject, setSubject] = useState<Subject>();
  const [role, setRole] = useState<Role>("builder");
  const queryClient = useQueryClient();
  const grant = useMutation({
    mutationFn: () => {
      if (!subject) throw new Error("Choose a person or group");
      return grantRole(subject.type, subject.id, role);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: accessKeys.grants });
      if (subject) toast.success(`Granted ${roleLabels[role].toLowerCase()} to ${subject.name}`);
      handleOpenChange(false);
    },
  });

  function handleOpenChange(next: boolean) {
    setOpen(next);
    if (!next) {
      setSubject(undefined);
      setRole("builder");
      grant.reset();
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button>
          <Plus />
          Grant Access
        </Button>
      </DialogTrigger>
      <DialogContent>
        <form
          className="grid gap-6"
          onSubmit={(event) => {
            event.preventDefault();
            grant.mutate();
          }}
        >
          <DialogHeader>
            <DialogTitle>Grant Access</DialogTitle>
            <DialogDescription>
              Give a person or a whole group a role. Granting a group covers everyone in it.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="grant-subject">Person or group</FieldLabel>
              <SubjectPicker value={subject} onChange={setSubject} />
            </Field>
            <Field>
              <FieldLabel htmlFor="grant-role">Role</FieldLabel>
              <RoleSelect
                id="grant-role"
                label="Role"
                value={role}
                onChange={setRole}
                className="w-full"
              />
            </Field>
            <FieldError>{grant.error?.message}</FieldError>
          </FieldGroup>
          <DialogFooter>
            <Button type="submit" disabled={!subject || grant.isPending}>
              Grant Access
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// searchDelay waits for a pause in typing before asking the server.
const searchDelay = 200;

function SubjectPicker({
  value,
  onChange,
}: Readonly<{ value?: Subject; onChange: (subject: Subject) => void }>) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const query = useDebounced(text.trim(), searchDelay);
  const found = useQuery({ ...subjectsQueryOptions(query), enabled: query !== "" });
  // Matches for earlier text stay hidden until the search catches up with the input.
  const current = query === text.trim();
  const matches = current ? (found.data ?? []) : [];
  // New matches highlight the closest, so Enter picks it.
  const [selected, setSelected] = useState("");
  const best = matches[0] ? itemValue(matches[0]) : "";
  useEffect(() => setSelected(best), [best]);

  function handleOpenChange(next: boolean) {
    setOpen(next);
    if (!next) setText("");
  }

  function choose(subject: Subject) {
    onChange(subject);
    handleOpenChange(false);
  }

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          id="grant-subject"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="w-full justify-between font-normal"
        >
          {value ? value.name : <span className="text-muted-foreground">Choose…</span>}
          <ChevronsUpDown className="text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-(--radix-popover-trigger-width) p-0" align="start">
        {/* The server matches and ranks people and groups together, so the list shows them as given. */}
        <Command
          label="Search people and groups"
          shouldFilter={false}
          value={selected}
          onValueChange={setSelected}
        >
          <CommandInput
            value={text}
            onValueChange={setText}
            placeholder="Search people and groups"
          />
          <CommandList>
            {text.trim() !== "" && (
              <CommandEmpty>
                {!current || found.isFetching
                  ? "Searching…"
                  : (found.error?.message ??
                    "Nobody matches. People appear after they first sign in.")}
              </CommandEmpty>
            )}
            {matches.length > 0 && (
              // One unheaded group gives the list the stock padding.
              <CommandGroup>
                {matches.map((match) => (
                  <CommandItem
                    key={itemValue(match)}
                    value={itemValue(match)}
                    onSelect={() => choose({ type: match.type, id: match.id, name: match.name })}
                  >
                    {match.type === "group" ? <Users /> : <User />}
                    <span className="flex-1">{match.name}</span>
                    <span className="text-xs text-muted-foreground">{detail(match)}</span>
                    {value?.id === match.id && <Check />}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function itemValue(match: SubjectMatch) {
  return `${match.type}:${match.id}`;
}

function detail(match: SubjectMatch) {
  if (match.type === "user") return match.email;
  const members = match.memberCount ?? 0;
  return `${formatCount(members)} ${members === 1 ? "member" : "members"}`;
}
