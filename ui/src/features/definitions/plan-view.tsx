import { Badge } from "@/components/ui/badge";
import type { Change, EntityName, FieldChange, Impact, Summary } from "./api";

const actionLabels: Record<Change["action"], string> = {
  add: "Add",
  change: "Change",
  remove: "Remove",
};

export function summaryText(summary: Summary) {
  return `${summary.added} to add, ${summary.changed} to change, ${summary.removed} to remove`;
}

function display(value: unknown) {
  return typeof value === "string" ? value : JSON.stringify(value);
}

export function ChangeList({ changes }: { changes: Change[] }) {
  if (changes.length === 0) {
    return <p className="text-muted-foreground text-sm">No documents change.</p>;
  }

  return (
    <ul className="flex flex-col gap-3">
      {changes.map((change) => (
        <li key={`${change.kind}/${change.identifier}`} className="flex flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <Badge variant={change.action === "remove" ? "destructive" : "secondary"}>
              {actionLabels[change.action]}
            </Badge>
            <span className="font-medium">{change.title}</span>
            <span className="text-muted-foreground">
              {change.kind} <span className="font-mono text-xs">{change.identifier}</span>
            </span>
          </div>
          {change.fields && change.fields.length > 0 && <FieldChanges fields={change.fields} />}
        </li>
      ))}
    </ul>
  );
}

function FieldChanges({ fields }: { fields: FieldChange[] }) {
  return (
    <dl className="grid gap-x-3 gap-y-1 border-l pl-3 text-xs sm:grid-cols-[minmax(8rem,auto)_1fr]">
      {fields.map((field) => (
        <div key={field.path} className="contents">
          <dt className="font-mono">{field.path}</dt>
          <dd className="flex flex-wrap items-center gap-1 font-mono break-all">
            {field.before === undefined ? (
              <span className="text-muted-foreground">added</span>
            ) : (
              <del className="text-muted-foreground">{display(field.before)}</del>
            )}
            <span aria-hidden="true">→</span>
            {field.after === undefined ? (
              <span className="text-muted-foreground">removed</span>
            ) : (
              <ins>{display(field.after)}</ins>
            )}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function names(examples: EntityName[], count: number) {
  const listed = examples.map((example) => example.title).join(", ");
  return count > examples.length ? `${listed} and ${count - examples.length} more` : listed;
}

function entities(count: number) {
  return count === 1 ? "1 entity" : `${count} entities`;
}

export function ImpactList({ impact }: { impact: Impact }) {
  const lines = [
    ...impact.deleted_entities.map((group) => ({
      key: `deleted ${group.blueprint ?? group.integration}`,
      text: group.blueprint
        ? `Deletes ${entities(group.count)} of the ${group.blueprint} blueprint`
        : `Deletes ${entities(group.count)} that the ${group.integration} integration owns`,
      examples: names(group.examples, group.count),
    })),
    ...impact.cleared_values.map((field) => ({
      key: `cleared ${field.blueprint}.${field.name}`,
      text: `Clears ${field.blueprint}.${field.name} on ${entities(field.count)}`,
      examples: names(field.examples, field.count),
    })),
    ...impact.dropped_links.map((field) => ({
      key: `dropped ${field.blueprint}.${field.name}`,
      text: `Drops the ${field.blueprint}.${field.name} links of ${entities(field.count)}`,
      examples: names(field.examples, field.count),
    })),
  ];

  if (lines.length === 0) {
    return <p className="text-muted-foreground text-sm">No entity changes.</p>;
  }

  return (
    <ul className="flex flex-col gap-2 text-sm">
      {lines.map((line) => (
        <li key={line.key} className="flex flex-col">
          <span className="font-medium">{line.text}</span>
          <span className="text-muted-foreground">{line.examples}</span>
        </li>
      ))}
    </ul>
  );
}
