import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronsUpDown, Plus, User, Users } from "lucide-react";
import { useState } from "react";
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
  groupsQueryOptions,
  type SubjectType,
  usersQueryOptions,
} from "@/features/access";
import { type Role, useMe } from "@/features/auth";

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
          Grant access
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
            <DialogTitle>Grant access</DialogTitle>
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
              Grant access
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function SubjectPicker({
  value,
  onChange,
}: Readonly<{ value?: Subject; onChange: (subject: Subject) => void }>) {
  const [open, setOpen] = useState(false);
  const me = useMe();
  const users = useQuery(usersQueryOptions).data ?? [];
  const groups = useQuery(groupsQueryOptions).data ?? [];

  function choose(subject: Subject) {
    onChange(subject);
    setOpen(false);
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
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
        <Command>
          <CommandInput placeholder="Search people and groups" />
          <CommandList>
            <CommandEmpty>Nobody matches. People appear after they first sign in.</CommandEmpty>
            <CommandGroup heading="Groups">
              {groups.map((group) => (
                <CommandItem
                  key={group.id}
                  value={`group ${group.name}`}
                  onSelect={() => choose({ type: "group", id: group.id, name: group.name })}
                >
                  <Users />
                  <span className="flex-1">{group.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {group.memberCount} {group.memberCount === 1 ? "member" : "members"}
                  </span>
                  {value?.id === group.id && <Check />}
                </CommandItem>
              ))}
            </CommandGroup>
            <CommandGroup heading="People">
              {users
                .filter((user) => user.id !== me.id)
                .map((user) => (
                  <CommandItem
                    key={user.id}
                    value={`user ${user.displayName} ${user.email}`}
                    onSelect={() => choose({ type: "user", id: user.id, name: user.displayName })}
                  >
                    <User />
                    <span className="flex-1">{user.displayName}</span>
                    <span className="text-xs text-muted-foreground">{user.email}</span>
                    {value?.id === user.id && <Check />}
                  </CommandItem>
                ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
