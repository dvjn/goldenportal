import { createFileRoute, Link } from "@tanstack/react-router";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { PageHeader } from "@/components/common/page-header";
import { StateCard } from "@/components/common/state-card";
import { TableFrame } from "@/components/common/table-frame";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  appliedText,
  type RevisionPage,
  revisionsQueryOptions,
  useRevisions,
} from "@/features/definitions";
import { formatDateTime, formatRelative } from "@/lib/time";
import { usePageTitle } from "@/lib/use-page-title";

type HistorySearch = { page?: number };

export const Route = createFileRoute("/_authenticated/settings/catalog-setup/history/")({
  validateSearch: (search: Record<string, unknown>): HistorySearch => {
    const page = Math.floor(Number(search.page)) || 1;
    return { page: page > 1 ? page : undefined };
  },
  loaderDeps: ({ search }) => ({ page: search.page ?? 1 }),
  loader: ({ context, deps }) => {
    void context.queryClient.prefetchQuery(revisionsQueryOptions(deps.page));
  },
  component: HistoryPage,
});

function HistoryPage() {
  const page = Route.useSearch().page ?? 1;
  const history = useRevisions(page);
  usePageTitle("Definitions history");

  return (
    <>
      <PageHeader
        title="Definitions history"
        description="Every applied change to the definitions, newest first. Open a revision to see its changes or to roll back to it."
      />
      {history.isError ? (
        <StateCard title="History unavailable" message="We could not load the revisions." />
      ) : history.data ? (
        <RevisionTable history={history.data} page={page} />
      ) : (
        <ListSkeleton />
      )}
    </>
  );
}

function RevisionTable({ history, page }: { history: RevisionPage; page: number }) {
  const first = history.total === 0 ? 0 : history.offset + 1;
  const last = history.offset + history.revisions.length;

  return (
    <TableFrame
      footer={
        <div className="flex w-full items-center justify-between gap-2">
          <p className="text-muted-foreground text-sm">
            {first} to {last} of {history.total}
          </p>
          <div className="flex gap-2">
            <PageLink page={page - 1} disabled={page <= 1} label="Previous" />
            <PageLink page={page + 1} disabled={last >= history.total} label="Next" />
          </div>
        </div>
      }
    >
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
                to="/settings/catalog-setup/history/$revision"
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
            <TableCell className="text-muted-foreground">{appliedText(revision.summary)}</TableCell>
          </TableRow>
        ))}
        {history.revisions.length === 0 && (
          <TableRow>
            <TableCell colSpan={5} className="text-muted-foreground h-24 text-center">
              Nothing applied yet.
            </TableCell>
          </TableRow>
        )}
      </TableBody>
    </TableFrame>
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
