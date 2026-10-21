import { createFileRoute, Link } from "@tanstack/react-router";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { appliedText, revisionsQueryOptions, useRevisions } from "@/features/definitions";
import { formatDateTime, formatRelative } from "@/lib/time";

type HistorySearch = { page?: number };

export const Route = createFileRoute("/_authenticated/definitions/history/")({
  validateSearch: (search: Record<string, unknown>): HistorySearch => {
    const page = Math.floor(Number(search.page)) || 1;
    return { page: page > 1 ? page : undefined };
  },
  loaderDeps: ({ search }) => ({ page: search.page ?? 1 }),
  loader: ({ context, deps }) =>
    context.queryClient.ensureQueryData(revisionsQueryOptions(deps.page)),
  component: HistoryPage,
});

function HistoryPage() {
  const page = Route.useSearch().page ?? 1;
  const history = useRevisions(page);
  const first = history.total === 0 ? 0 : history.offset + 1;
  const last = history.offset + history.revisions.length;

  return (
    <>
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold">Definitions history</h1>
        <p className="text-muted-foreground text-sm">
          Every applied change to the definitions, newest first. Open a revision to see its changes
          or to roll back to it.
        </p>
      </header>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Revision</TableHead>
            <TableHead>Message</TableHead>
            <TableHead>Author</TableHead>
            <TableHead>When</TableHead>
            <TableHead>Changes</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {history.revisions.map((revision) => (
            <TableRow key={revision.number}>
              <TableCell>
                <Link
                  to="/definitions/history/$revision"
                  params={{ revision: revision.number }}
                  className="font-medium underline-offset-4 hover:underline"
                >
                  Revision {revision.number}
                </Link>
              </TableCell>
              <TableCell className="whitespace-normal">
                <span className="flex flex-wrap items-center gap-2">
                  {revision.rollback_of !== undefined && (
                    <Badge variant="outline">Rolled back to {revision.rollback_of}</Badge>
                  )}
                  {revision.message || <span className="text-muted-foreground">No message</span>}
                </span>
              </TableCell>
              <TableCell>{revision.author}</TableCell>
              <TableCell title={formatDateTime(revision.created_at)}>
                {formatRelative(revision.created_at)}
              </TableCell>
              <TableCell className="text-muted-foreground">
                {appliedText(revision.summary)}
              </TableCell>
            </TableRow>
          ))}
          {history.revisions.length === 0 && (
            <TableRow>
              <TableCell colSpan={5} className="text-muted-foreground text-center">
                Nothing applied yet.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <div className="flex items-center justify-between gap-2">
        <p className="text-muted-foreground text-sm">
          {first} to {last} of {history.total}
        </p>
        <div className="flex gap-2">
          <PageLink page={page - 1} disabled={page <= 1} label="Previous" />
          <PageLink page={page + 1} disabled={last >= history.total} label="Next" />
        </div>
      </div>
    </>
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
      <Link from={Route.fullPath} search={{ page: page > 1 ? page : undefined }}>
        {label}
      </Link>
    </Button>
  );
}
