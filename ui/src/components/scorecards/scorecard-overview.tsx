import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Loader2, RefreshCw } from "lucide-react";
import { useRef } from "react";
import { toast } from "sonner";
import { CardSection, CardTitleBar } from "@/components/card-sections";
import { BlueprintLabel } from "@/components/catalog/blueprint-label";
import { EntityLink } from "@/components/catalog/entity-link";
import { ListValue } from "@/components/catalog/list-value";
import { EmptyValue } from "@/components/catalog/property-value";
import { Code } from "@/components/code";
import {
  DataTable,
  DataTableColumnHeader,
  dataTableColumns,
  type TableSearch,
  usePageSize,
} from "@/components/data-table";
import { Detail, DetailList } from "@/components/detail-list";
import { EmptyState } from "@/components/empty-state";
import { ChecksPassing, checkTotals } from "@/components/scorecards/checks-passing";
import { CriterionDetails } from "@/components/scorecards/criterion-details";
import { TierBadge } from "@/components/scorecards/tier-badge";
import { TierDistribution } from "@/components/scorecards/tier-distribution";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { blueprints } from "@/features/blueprints";
import {
  type EntityResult,
  evaluateScorecard,
  resultsQueryOptions,
  type ScorecardSpec,
  type Summary,
  scorecards,
  summaryQueryOptions,
} from "@/features/scorecards";
import { formatAbsolute, formatRelative } from "@/lib/format";

// ScorecardSearch is the entities table's state in the URL; tier is a
// comma-separated list of tier names, with "none" for entities that
// reached no tier.
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
  const navigate = useNavigate();
  // A tier picked above brings the entities table, filtered to it, into view.
  const entities = useRef<HTMLDivElement>(null);

  return (
    <div className="grid gap-4">
      <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <TiersCard
          spec={spec}
          summary={summary.data}
          canEvaluate={canEvaluate}
          onPick={(tier) => {
            entities.current?.scrollIntoView({ behavior: "smooth", block: "start" });
            void navigate({
              to: ".",
              search: (prev: ScorecardSearch) => ({
                ...prev,
                tier: tier || "none",
                page: undefined,
              }),
              replace: true,
              resetScroll: false,
            } as never);
          }}
        />
        <AboutCard spec={spec} summary={summary.data} />
      </div>
      <CriteriaCard spec={spec} summary={summary.data} />
      <div ref={entities} className="scroll-mt-16">
        <EntitiesTable spec={spec} search={search} />
      </div>
    </div>
  );
}

// TiersCard's legend picks a tier to filter the entities table to.
function TiersCard({
  spec,
  summary,
  canEvaluate,
  onPick,
}: Readonly<{
  spec: ScorecardSpec;
  summary?: Summary;
  canEvaluate: boolean;
  onPick: (tier: string) => void;
}>) {
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
      <CardTitleBar
        title="Tiers"
        action={
          canEvaluate && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => evaluate.mutate()}
              disabled={evaluate.isPending}
            >
              {evaluate.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              Evaluate Now
            </Button>
          )
        }
      />
      <CardContent className="grid gap-3">
        {summary ? (
          <>
            <ChecksPassing {...checkTotals(summary.criteria)} />
            <TierDistribution tiers={spec.tiers} counts={summary.tiers} onPick={onPick} />
          </>
        ) : (
          <Loader2 className="size-4 animate-spin text-muted-foreground" aria-label="Loading" />
        )}
      </CardContent>
    </Card>
  );
}

function AboutCard({ spec, summary }: Readonly<{ spec: ScorecardSpec; summary?: Summary }>) {
  const { data: blueprint } = useQuery(blueprints.queries.detail(spec.blueprint));
  const property = blueprint?.published?.spec.properties.find(
    (p) => p.scorecard === spec.identifier,
  );
  return (
    <Card>
      <CardTitleBar title="About" />
      <CardContent className="grid gap-3 text-sm">
        {spec.description && <p className="text-muted-foreground">{spec.description}</p>}
        <DetailList>
          <Detail label="Scores">
            <Link
              to="/catalog/$blueprint"
              params={{ blueprint: spec.blueprint }}
              className="min-w-0 hover:underline"
            >
              <BlueprintLabel blueprint={spec.blueprint} />
            </Link>
          </Detail>
          {blueprint && (
            <Detail label="Property">
              {property ? (
                <Code>{property.identifier}</Code>
              ) : (
                <EmptyValue label="None, so results show only here" />
              )}
            </Detail>
          )}
          {summary && (
            <Detail label="Entities">
              <span className="font-medium tabular-nums">{summary.entities}</span>
            </Detail>
          )}
          {summary?.evaluatedAt && (
            <Detail label="Evaluated">
              <span title={formatAbsolute(summary.evaluatedAt)}>
                {formatRelative(summary.evaluatedAt)}
              </span>
            </Detail>
          )}
          {spec.filter && (
            <Detail label="Only">
              <Code>{describeFilter(spec.filter)}</Code>
            </Detail>
          )}
        </DetailList>
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
      <CardTitleBar title="Criteria" />
      <CardContent className="grid px-0">
        {spec.tiers.map((tier) => (
          <CardSection key={tier.name} heading={<TierBadge tiers={spec.tiers} name={tier.name} />}>
            <ul className="grid">
              {tier.criteria.map((criterion) => {
                const c = counts.get(criterion.identifier);
                const applicable = c ? c.passed + c.failed + c.errored : 0;
                const rate = applicable > 0 && c ? Math.round((c.passed / applicable) * 100) : null;
                return (
                  <li
                    key={criterion.identifier}
                    className="grid grid-cols-[minmax(0,1fr)_8rem_6rem] items-center gap-4 py-1.5 text-sm"
                  >
                    <div className="grid min-w-0">
                      <CriterionDetails criterion={criterion} className="-mb-1 pb-1 font-medium" />
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
                      {rate === null ? "-" : `${rate}%`}
                      {c && c.errored > 0 && (
                        <span className="block text-destructive">{c.errored} errored</span>
                      )}
                    </span>
                  </li>
                );
              })}
            </ul>
          </CardSection>
        ))}
      </CardContent>
    </Card>
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
      size: usePageSize(search),
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
        <EntityLink
          blueprint={row.original.blueprint}
          identifier={row.original.identifier}
          tab={spec.identifier}
          className="font-medium hover:underline"
        >
          {row.original.title}
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
        <span className="tabular-nums">{getValue() === null ? "-" : `${getValue()}%`}</span>
      ),
    }),
    column.display({
      id: "failing",
      header: () => "Failing",
      cell: ({ row }) => {
        // Failing checks keep the scorecard's order.
        const failing = [...titles.keys()].filter((id) => {
          const status = row.original.criteria[id]?.status;
          return status === "failed" || status === "errored";
        });
        if (failing.length === 0) return <EmptyValue />;
        return (
          <ListValue
            compact
            items={failing.map((id) => ({ key: id, node: titles.get(id) ?? id }))}
            viewAll={
              <EntityLink
                blueprint={row.original.blueprint}
                identifier={row.original.identifier}
                tab={spec.identifier}
              >
                View all
              </EntityLink>
            }
          />
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
      title="Entities"
      data={page.data?.results ?? []}
      columns={columns}
      getRowId={(r) => `${r.blueprint}/${r.identifier}`}
      search={search}
      searchLabel="Search entities"
      rowCount={page.data?.total ?? 0}
      loading={page.isPlaceholderData}
      pending={page.isPending}
      error={page.error}
      onRetry={() => void page.refetch()}
      filters={{
        fields: [
          {
            kind: "options",
            id: "tier",
            title: "Tier",
            useOptions: () => tierOptions,
          },
        ],
        value: search.tier ? { tier: search.tier.split(",") } : {},
        onChange: (_, value) =>
          navigate({
            to: ".",
            search: (prev: ScorecardSearch) => ({
              ...prev,
              tier: Array.isArray(value) && value.length > 0 ? value.join(",") : undefined,
              page: undefined,
            }),
            replace: true,
            resetScroll: false,
          } as never),
      }}
      onClearFilters={() =>
        navigate({
          to: ".",
          search: (prev: ScorecardSearch) => ({ ...prev, tier: undefined, page: undefined }),
          replace: true,
        } as never)
      }
      empty={
        <EmptyState
          title="Nothing scored yet"
          description={`Entities of ${spec.blueprint} show up here once they're evaluated.`}
        />
      }
    />
  );
}
