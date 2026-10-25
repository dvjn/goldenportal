import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Loader2, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { EntityLink } from "@/components/catalog/entity-link";
import {
  DataTable,
  DataTableColumnHeader,
  DataTableFacetedFilter,
  dataTableColumns,
  type TableSearch,
} from "@/components/data-table";
import { TierBadge } from "@/components/scorecards/tier-badge";
import { TierDistribution } from "@/components/scorecards/tier-distribution";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import {
  type Criterion,
  type EntityResult,
  evaluateScorecard,
  resultsQueryOptions,
  type ScorecardSpec,
  type Summary,
  scorecards,
  summaryQueryOptions,
} from "@/features/scorecards";
import { formatAbsolute, formatRelative } from "@/lib/format";

// ScorecardSearch is the entities table's state in the URL; tier is a tier
// name, or "none" for entities that reached no tier.
export type ScorecardSearch = TableSearch & { tier?: string };

const column = dataTableColumns<EntityResult>();

// ScorecardOverview shows how a scorecard's entities are doing: how many
// reached each tier, how each criterion fares, and every entity's result.
export function ScorecardOverview({
  spec,
  search,
  canEvaluate,
}: Readonly<{ spec: ScorecardSpec; search: ScorecardSearch; canEvaluate: boolean }>) {
  const summary = useQuery(summaryQueryOptions(spec.identifier));

  return (
    <div className="grid gap-4">
      <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <TiersCard spec={spec} summary={summary.data} canEvaluate={canEvaluate} />
        <AboutCard spec={spec} />
      </div>
      <CriteriaCard spec={spec} summary={summary.data} />
      <EntitiesTable spec={spec} search={search} />
    </div>
  );
}

function TiersCard({
  spec,
  summary,
  canEvaluate,
}: Readonly<{ spec: ScorecardSpec; summary?: Summary; canEvaluate: boolean }>) {
  const queryClient = useQueryClient();
  const evaluate = useMutation({
    mutationFn: () => evaluateScorecard(spec.identifier),
    onSuccess: async ({ entities }) => {
      // Results and the summary both change, so refresh everything about the scorecard.
      await queryClient.invalidateQueries({ queryKey: scorecards.keys.detail(spec.identifier) });
      toast.success(`Evaluated ${entities} ${entities === 1 ? "entity" : "entities"}`);
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle>Tiers</CardTitle>
        <CardAction className="flex items-center gap-2 text-xs text-muted-foreground">
          {summary?.evaluatedAt && (
            <span title={formatAbsolute(summary.evaluatedAt)}>
              Evaluated {formatRelative(summary.evaluatedAt)}
            </span>
          )}
          {canEvaluate && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => evaluate.mutate()}
              disabled={evaluate.isPending}
            >
              {evaluate.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              Evaluate Now
            </Button>
          )}
        </CardAction>
      </CardHeader>
      <CardContent className="grid gap-3">
        {summary ? (
          <>
            <p className="text-sm text-muted-foreground">
              {summary.entities} {summary.entities === 1 ? "entity" : "entities"} scored
            </p>
            <TierDistribution tiers={spec.tiers} counts={summary.tiers} />
          </>
        ) : (
          <Loader2 className="size-4 animate-spin text-muted-foreground" aria-label="Loading" />
        )}
      </CardContent>
    </Card>
  );
}

function AboutCard({ spec }: Readonly<{ spec: ScorecardSpec }>) {
  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle>About</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-3 text-sm">
        {spec.description && <p className="text-muted-foreground">{spec.description}</p>}
        <dl className="grid grid-cols-[5rem_minmax(0,1fr)] gap-x-3 gap-y-2">
          <dt className="text-muted-foreground">Scores</dt>
          <dd>
            <Link
              to="/catalog/$blueprint"
              params={{ blueprint: spec.blueprint }}
              className="font-mono hover:underline"
            >
              {spec.blueprint}
            </Link>
          </dd>
          {spec.filter && (
            <>
              <dt className="text-muted-foreground">Only</dt>
              <dd>
                <code className="font-mono text-xs break-all">{describeFilter(spec.filter)}</code>
              </dd>
            </>
          )}
        </dl>
      </CardContent>
    </Card>
  );
}

// describeFilter writes a filter compactly, e.g. tier ne experimental.
function describeFilter(filter: unknown): string {
  const f = filter as Record<string, unknown>;
  if (Array.isArray(f.and)) return f.and.map(describeFilter).join(" and ");
  if (Array.isArray(f.or)) return `(${f.or.map(describeFilter).join(" or ")})`;
  if (f.not) return `not ${describeFilter(f.not)}`;
  const value = f.values ?? f.value;
  return [f.property, f.op, value === undefined ? "" : JSON.stringify(value)].join(" ").trim();
}

function CriteriaCard({ spec, summary }: Readonly<{ spec: ScorecardSpec; summary?: Summary }>) {
  const counts = new Map(summary?.criteria.map((c) => [c.identifier, c]));

  return (
    <Card className="gap-0 pb-0">
      <CardHeader className="border-b">
        <CardTitle>Criteria</CardTitle>
      </CardHeader>
      <CardContent className="grid px-0">
        {spec.tiers.map((tier) => (
          <section key={tier.name} className="grid border-b last:border-b-0">
            <h3 className="px-4 pt-3 pb-1">
              <TierBadge tiers={spec.tiers} name={tier.name} />
            </h3>
            <ul className="grid">
              {tier.criteria.map((criterion) => {
                const c = counts.get(criterion.identifier);
                const applicable = c ? c.passed + c.failed + c.errored : 0;
                const rate = applicable > 0 && c ? Math.round((c.passed / applicable) * 100) : null;
                return (
                  <li
                    key={criterion.identifier}
                    className="grid grid-cols-[minmax(0,1fr)_8rem_6rem] items-center gap-4 px-4 py-2 text-sm"
                  >
                    <div className="grid min-w-0">
                      <CriterionDetails criterion={criterion} />
                      {criterion.fix && (
                        <span className="truncate text-xs text-muted-foreground">
                          {criterion.fix}
                        </span>
                      )}
                    </div>
                    <div className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden>
                      <div
                        className="h-full rounded-full bg-emerald-500"
                        style={{ width: `${rate ?? 0}%` }}
                      />
                    </div>
                    <span className="text-right text-xs text-muted-foreground tabular-nums">
                      {rate === null ? "—" : `${rate}% pass`}
                      {c && c.errored > 0 && (
                        <span className="block text-destructive">{c.errored} errored</span>
                      )}
                    </span>
                  </li>
                );
              })}
            </ul>
          </section>
        ))}
      </CardContent>
    </Card>
  );
}

// CriterionDetails shows a criterion's jq on hover, laid out to read.
function CriterionDetails({ criterion }: Readonly<{ criterion: Criterion }>) {
  return (
    <HoverCard openDelay={200}>
      <HoverCardTrigger asChild>
        <span className="-mb-1 w-fit max-w-full cursor-default truncate pb-1 font-medium underline decoration-muted-foreground/40 decoration-dotted underline-offset-4">
          {criterion.title}
        </span>
      </HoverCardTrigger>
      <HoverCardContent align="start" className="grid w-md gap-3">
        {criterion.description && <p className="text-sm">{criterion.description}</p>}
        <Expression label="Check" value={criterion.check} />
        {criterion.when && <Expression label="Only when" value={criterion.when} />}
      </HoverCardContent>
    </HoverCard>
  );
}

function Expression({ label, value }: Readonly<{ label: string; value: string }>) {
  return (
    <div className="grid gap-1">
      <span className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
        {label}
      </span>
      <pre className="rounded-md bg-muted px-2.5 py-2 font-mono text-xs leading-5 break-words whitespace-pre-wrap">
        {value}
      </pre>
    </div>
  );
}

function EntitiesTable({
  spec,
  search,
}: Readonly<{ spec: ScorecardSpec; search: ScorecardSearch }>) {
  const navigate = useNavigate();
  const page = useQuery({
    ...resultsQueryOptions(spec.identifier, {
      tier: search.tier,
      q: search.q,
      page: search.page,
      size: search.size,
    }),
    placeholderData: keepPreviousData,
  });
  const titles = new Map(
    spec.tiers.flatMap((tier) => tier.criteria.map((c) => [c.identifier, c.title])),
  );

  const columns = [
    column.accessor("title", {
      id: "title",
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="Entity" />,
      cell: ({ row }) => (
        <EntityLink blueprint={row.original.blueprint} identifier={row.original.identifier}>
          <span className="font-medium">{row.original.title}</span>
        </EntityLink>
      ),
    }),
    column.accessor("tier", {
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="Tier" />,
      cell: ({ getValue }) => <TierBadge tiers={spec.tiers} name={getValue()} />,
    }),
    column.accessor("score", {
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="Score" />,
      cell: ({ getValue }) => (
        <span className="tabular-nums">{getValue() === null ? "—" : `${getValue()}%`}</span>
      ),
    }),
    column.display({
      id: "failing",
      header: () => "Failing",
      cell: ({ row }) => {
        const failing = Object.entries(row.original.criteria)
          .filter(([, c]) => c.status === "failed" || c.status === "errored")
          .map(([id]) => titles.get(id) ?? id);
        return failing.length === 0 ? (
          <span className="text-muted-foreground">—</span>
        ) : (
          <span className="text-sm text-muted-foreground">{failing.join(", ")}</span>
        );
      },
    }),
  ];

  const tierOptions = [
    { value: "none", label: "No tier" },
    ...spec.tiers.map((tier) => ({ value: tier.name, label: tier.title })),
  ];

  return (
    <DataTable
      data={page.data?.results ?? []}
      columns={columns}
      getRowId={(r) => `${r.blueprint}/${r.identifier}`}
      search={search}
      searchLabel="Search entities"
      rowCount={page.data?.total ?? 0}
      loading={page.isPlaceholderData}
      filters={
        <DataTableFacetedFilter
          title="Tier"
          options={tierOptions}
          selected={search.tier ? [search.tier] : []}
          // The server filters by one tier, so the newest pick wins.
          onChange={(values) =>
            navigate({
              to: ".",
              search: (prev: ScorecardSearch) => ({
                ...prev,
                tier: values.at(-1),
                page: undefined,
              }),
              replace: true,
              resetScroll: false,
            } as never)
          }
        />
      }
      filtered={Boolean(search.tier)}
      onClearFilters={() =>
        navigate({
          to: ".",
          search: (prev: ScorecardSearch) => ({ ...prev, tier: undefined, page: undefined }),
          replace: true,
        } as never)
      }
      empty={
        <Empty>
          <EmptyHeader>
            <EmptyTitle>Nothing scored yet</EmptyTitle>
            <EmptyDescription>
              Entities of {spec.blueprint} show up here once they're evaluated.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      }
    />
  );
}
