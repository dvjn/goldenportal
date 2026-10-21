import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { GrantRole, Subject } from "./api";
import { useGrants, useSetGrant, useSubjects } from "./queries";
import { SubjectPicker } from "./subject-picker";

const roleOptions: { role: GrantRole; title: string; description: string }[] = [
  {
    role: "maintainer",
    title: "Maintainer",
    description: "Edits the catalog and its model, syncs integrations and manages their tokens.",
  },
  {
    role: "admin",
    title: "Admin",
    description: "Everything a Maintainer can do, plus system settings and access.",
  },
];

type GrantDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  subject?: Subject;
};

export function GrantDialog({ open, onOpenChange, subject }: GrantDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        {open && <GrantForm initialSubject={subject} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function GrantForm({ initialSubject, onDone }: { initialSubject?: Subject; onDone: () => void }) {
  const grants = useGrants();
  const subjects = useSubjects();
  const setGrant = useSetGrant();
  const [subject, setSubject] = useState(initialSubject);
  const existing = grants.find(
    (grant) => grant.subject.type === subject?.type && grant.subject.id === subject?.id,
  );
  const [role, setRole] = useState<GrantRole>(existing?.role ?? "maintainer");

  function pick(picked: Subject) {
    setSubject(picked);
    const held = grants.find(
      (grant) => grant.subject.type === picked.type && grant.subject.id === picked.id,
    );
    setRole(held?.role ?? "maintainer");
  }

  function submit(event: React.FormEvent) {
    event.preventDefault();
    if (subject) setGrant.mutate({ subject, role }, { onSuccess: onDone });
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-6">
      <DialogHeader>
        <DialogTitle>{existing ? "Change role" : "Grant access"}</DialogTitle>
        <DialogDescription>Give a user or group a system role.</DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <Field>
          <Label htmlFor="grant-subject">User or group</Label>
          <SubjectPicker
            id="grant-subject"
            subjects={subjects}
            value={subject}
            onChange={pick}
            placeholder="Pick a user or group"
          />
        </Field>
        <FieldSet>
          <FieldLegend variant="label">Role</FieldLegend>
          <RadioGroup
            aria-label="Role"
            value={role}
            onValueChange={(value) => setRole(value as GrantRole)}
          >
            {roleOptions.map((option) => (
              <RoleCard
                key={option.role}
                {...option}
                lastAdmin={Boolean(existing?.last_admin) && option.role !== "admin"}
              />
            ))}
          </RadioGroup>
          <FieldDescription>
            Members need no grant. Every user that SCIM provisions can read the portal.
          </FieldDescription>
        </FieldSet>
        {setGrant.isError && <FieldError>{setGrant.error.message}</FieldError>}
      </FieldGroup>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={!subject || role === existing?.role || setGrant.isPending}>
          {existing ? "Change role" : "Grant access"}
        </Button>
      </DialogFooter>
    </form>
  );
}

type RoleCardProps = (typeof roleOptions)[number] & { lastAdmin: boolean };

function RoleCard({ role, title, description, lastAdmin }: RoleCardProps) {
  const card = (
    <FieldLabel htmlFor={`grant-role-${role}`}>
      <Field orientation="horizontal" data-disabled={lastAdmin}>
        <RadioGroupItem value={role} id={`grant-role-${role}`} disabled={lastAdmin} />
        <FieldContent>
          <FieldTitle>{title}</FieldTitle>
          <FieldDescription>{description}</FieldDescription>
        </FieldContent>
      </Field>
    </FieldLabel>
  );

  if (!lastAdmin) return card;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div>{card}</div>
      </TooltipTrigger>
      <TooltipContent>This is the last Admin grant</TooltipContent>
    </Tooltip>
  );
}
