import { keepPreviousData, useQuery } from "@tanstack/react-query";
import type { CatalogFilters } from "@/components/catalog/catalog-filters";
import { EntityLink } from "@/components/catalog/entity-link";
import { ListValue } from "@/components/catalog/list-value";
import { EmptyValue } from "@/components/catalog/property-value";
import {
  DataTable,
  DataTableColumnHeader,
  dataTableColumns,
  type TableSearch,
  usePageSize,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { TierBadge } from "@/components/scorecards/tier-badge";
import type { Filter } from "@/features/catalog";
import { type EntityResult, resultsQueryOptions, type ScorecardSpec } from "@/features/scorecards";
import { formatPercent } from "@/lib/format";

const column = dataTableColumns<EntityResult>();

const count = (title: string, id: "passed" | "failed" | "skipped") =>
  column.accessor(id, {
    header: ({ column }) => <DataTableColumnHeader column={column} title={title} />,
    cell: ({ getValue }) => <span className="tabular-nums">{getValue()}</span>,
  });

// ScorecardEntities lists the result of every entity where matches on a
// scorecard, worst tier first unless sorted otherwise. catalogFilters show
// where as the catalog page's chips.
export function ScorecardEntities({
  spec,
  search,
  where,
  catalogFilters,
}: Readonly<{
  spec: ScorecardSpec;
  search: TableSearch;
  where?: Filter;
  catalogFilters: CatalogFilters;
}>) {
  const page = useQuery({
    ...resultsQueryOptions(spec.identifier, {
      filter: where,
      q: search.q,
      sort: search.sort,
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
      header: ({ column }) => <DataTableColumnHeader column={column} title="Tier" />,
      cell: ({ getValue }) => <TierBadge tiers={spec.tiers} name={getValue()} />,
    }),
    column.accessor("score", {
      header: ({ column }) => <DataTableColumnHeader column={column} title="Score" />,
      cell: ({ getValue }) => {
        const score = getValue();
        return (
          <span className="tabular-nums">
            {score === null ? <EmptyValue title="Not evaluated" /> : formatPercent(score, 100)}
          </span>
        );
      },
    }),
    count("Passed", "passed"),
    count("Failed", "failed"),
    count("Skipped", "skipped"),
    column.display({
      id: "failing",
      header: () => "Failing",
      cell: ({ row }) => {
        // Failing checks keep the scorecard's order.
        const failing = [...titles.keys()].filter(
          (id) => row.original.criteria[id]?.status === "failed",
        );
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

  return (
    <DataTable
      noun="entities"
      title="Entities"
      data={page.data?.items ?? []}
      columns={columns}
      getRowId={(r) => `${r.blueprint}/${r.identifier}`}
      search={search}
      searchLabel="Search entities"
      rowCount={page.data?.total ?? 0}
      loading={page.isPlaceholderData}
      pending={page.isPending}
      error={page.error}
      onRetry={() => void page.refetch()}
      filters={catalogFilters.filters}
      onClearFilters={catalogFilters.onClearFilters}
      empty={
        <EmptyState
          title="No scores yet"
          description="Entities show up here once the scorecard evaluates them."
        />
      }
    />
  );
}
