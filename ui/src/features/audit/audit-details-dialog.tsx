import { Link } from "@tanstack/react-router";
import { cn } from "cn";
import type { ReactNode } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatDateTime, notSet } from "@/lib/time";
import type { AuditChange, AuditEntry } from "./api";
import { describeDetails, formatValue } from "./describe";
import { actionLabel, actorKindLabel, resourceKindLabel } from "./labels";
import { ActorLabel, parentLink, ResourceLabel } from "./subjects";

export function AuditDetailsDialog({
  entry,
  onClose,
}: {
  entry: AuditEntry | null;
  onClose: () => void;
}) {
  return (
    <Dialog open={entry !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-4xl">
        {entry && <AuditDetails entry={entry} />}
      </DialogContent>
    </Dialog>
  );
}

function AuditDetails({ entry }: { entry: AuditEntry }) {
  const summary = describeDetails(entry);
  const { changes, ...rest } = entry.details ?? {};
  const changed = Object.entries(changes ?? {});
  const hasDetails = Object.keys(rest).length > 0;
  const { actor, resource, client } = entry;

  return (
    <>
      <DialogHeader>
        <DialogTitle>{actionLabel(entry.action)}</DialogTitle>
        <DialogDescription>
          {formatDateTime(entry.at)}
          {summary && `. ${summary}`}
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-5 text-sm">
        <Section title="Actor">
          <Detail label="Actor" value={<ActorLabel actor={actor} />} />
          <Detail label="Kind" value={actorKindLabel(actor.kind)} />
          <Detail label="Email" value={actor.email} />
          <Detail label="ID" value={actor.id} mono />
        </Section>
        <Section title="Resource">
          <Detail
            label="Resource"
            value={resource.kind ? <ResourceLabel entry={entry} /> : undefined}
          />
          <Detail label="Kind" value={resource.kind && resourceKindLabel(resource.kind)} />
          <Detail label="ID" value={resource.id} mono />
          <Detail label="Belongs to" value={resource.parent && <ParentValue entry={entry} />} />
        </Section>
        <Section title="Request">
          <Detail label="Action" value={entry.action} mono />
          <Detail label="Entry" value={String(entry.id)} mono />
          <Detail label="IP address" value={client.ip} mono />
          <Detail label="User agent" value={client.user_agent} className="sm:col-span-2" />
        </Section>
        {changed.length > 0 && <Changes changes={changed} />}
        <div className="flex flex-col gap-1.5">
          <span className="text-xs font-medium text-muted-foreground">Details</span>
          <pre className="max-h-72 w-full overflow-auto rounded-md border bg-muted/40 p-3 text-xs leading-relaxed break-words whitespace-pre-wrap">
            {hasDetails ? JSON.stringify(rest, null, 2) : "No other details recorded."}
          </pre>
        </div>
      </div>
    </>
  );
}

function ParentValue({ entry }: { entry: AuditEntry }) {
  const parent = entry.resource.parent;
  if (!parent) return null;
  const link = parentLink(parent);
  const label = `${resourceKindLabel(parent.kind)} ${parent.name || parent.id}`;

  return link ? (
    <Link {...link} className="font-medium hover:underline">
      {label}
    </Link>
  ) : (
    label
  );
}

function Changes({ changes }: { changes: [string, AuditChange][] }) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs font-medium text-muted-foreground">Changes</span>
      <div className="overflow-hidden rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Field</TableHead>
              <TableHead>Before</TableHead>
              <TableHead>After</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {changes.map(([field, change]) => (
              <TableRow key={field}>
                <TableCell className="align-top font-mono text-xs">{field}</TableCell>
                <ChangeValue value={change.before} />
                <ChangeValue value={change.after} />
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

function ChangeValue({ value }: { value: unknown }) {
  const text = formatValue(value);

  return (
    <TableCell className="max-w-72 align-top break-words whitespace-normal">
      {text || <span className="text-muted-foreground">{notSet}</span>}
    </TableCell>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>
      <dl className="grid grid-cols-1 gap-x-6 gap-y-3 rounded-md border p-3 sm:grid-cols-2">
        {children}
      </dl>
    </section>
  );
}

function Detail({
  label,
  value,
  mono,
  className,
}: {
  label: string;
  value?: ReactNode;
  mono?: boolean;
  className?: string;
}) {
  return (
    <div className={cn("min-w-0", className)}>
      <dt className="text-xs font-medium text-muted-foreground">{label}</dt>
      {value ? (
        <dd className={mono ? "mt-0.5 font-mono text-xs break-all" : "mt-0.5 break-words"}>
          {value}
        </dd>
      ) : (
        <dd className="mt-0.5 text-muted-foreground">{notSet}</dd>
      )}
    </div>
  );
}
