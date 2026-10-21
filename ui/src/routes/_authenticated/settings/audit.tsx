import { createFileRoute, Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  type AuditEntry,
  auditPageQueryOptions,
  describeDetails,
  useAuditPage,
} from "@/features/audit";
import { requirePermission } from "@/features/auth";
import { formatDateTime, formatRelative } from "@/lib/time";

type AuditSearch = { page: number };

export const Route = createFileRoute("/_authenticated/settings/audit")({
  validateSearch: (search: Record<string, unknown>): AuditSearch => ({
    page: Math.max(1, Math.floor(Number(search.page)) || 1),
  }),
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "audit.read"),
  loaderDeps: ({ search }) => ({ page: search.page }),
  loader: ({ context, deps }) =>
    context.queryClient.ensureQueryData(auditPageQueryOptions(deps.page)),
  staticData: { breadcrumb: "Audit log" },
  component: AuditPage,
});

const actionLabels: Record<string, string> = {
  "auth.signed_in": "Signed in",
  "auth.signed_out": "Signed out",
  "auth.sign_in_refused": "Sign-in refused",
  "grant.created": "Granted a role",
  "grant.changed": "Changed a role",
  "grant.removed": "Removed a grant",
  "sessions.revoked": "Revoked sessions",
  "scim_token.rotated": "Rotated the SCIM token",
  "api_token.created": "Created an API token",
  "api_token.revoked": "Revoked an API token",
  "definitions.applied": "Applied definitions",
  "definitions.rolled_back": "Rolled back definitions",
  "entity.created": "Created an entity",
  "entity.updated": "Edited an entity",
  "entity.deleted": "Deleted an entity",
};

const targetLabels: Record<string, string> = {
  user: "User",
  group: "Group",
  api_token: "API token",
  scim_token: "SCIM token",
  definitions_revision: "Definitions",
  entity: "Entity",
};

function AuditPage() {
  const { page } = Route.useSearch();
  const audit = useAuditPage(page);
  const first = audit.total === 0 ? 0 : audit.offset + 1;
  const last = audit.offset + audit.entries.length;

  return (
    <>
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold">Audit log</h1>
        <p className="text-muted-foreground text-sm">
          Sign-ins and every change to access, definitions and manual entities, newest first.
        </p>
      </header>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>When</TableHead>
            <TableHead>Actor</TableHead>
            <TableHead>Action</TableHead>
            <TableHead>Target</TableHead>
            <TableHead>Details</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {audit.entries.map((entry) => (
            <TableRow key={entry.id}>
              <TableCell title={formatDateTime(entry.at)}>{formatRelative(entry.at)}</TableCell>
              <TableCell>{entry.actor.label}</TableCell>
              <TableCell>{actionLabels[entry.action] ?? entry.action}</TableCell>
              <TableCell>
                <Target entry={entry} />
              </TableCell>
              <TableCell className="text-muted-foreground whitespace-normal">
                {describeDetails(entry)}
              </TableCell>
            </TableRow>
          ))}
          {audit.entries.length === 0 && (
            <TableRow>
              <TableCell colSpan={5} className="text-muted-foreground text-center">
                Nothing recorded yet.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <div className="flex items-center justify-between gap-2">
        <p className="text-muted-foreground text-sm">
          {first} to {last} of {audit.total}
        </p>
        <div className="flex gap-2">
          <PageLink page={page - 1} disabled={page <= 1} label="Previous" />
          <PageLink page={page + 1} disabled={last >= audit.total} label="Next" />
        </div>
      </div>
    </>
  );
}

function Target({ entry }: { entry: AuditEntry }) {
  if (!entry.target.kind) return null;
  const name = entry.target.name || entry.target.id;
  const kind = targetLabels[entry.target.kind] ?? entry.target.kind;

  return (
    <div className="flex flex-col">
      <span>{name}</span>
      {kind !== name && <span className="text-muted-foreground text-xs">{kind}</span>}
    </div>
  );
}

function PageLink({ page, disabled, label }: { page: number; disabled: boolean; label: string }) {
  if (disabled) {
    return (
      <Button variant="outline" size="sm" disabled>
        {label}
      </Button>
    );
  }

  return (
    <Button variant="outline" size="sm" asChild>
      <Link from={Route.fullPath} search={{ page }}>
        {label}
      </Link>
    </Button>
  );
}
