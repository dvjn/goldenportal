import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";
import { EntityLink } from "@/components/catalog/entity-link";
import { PropertyValue } from "@/components/catalog/property-value";
import { ScorecardCell } from "@/components/scorecards/scorecard-cell";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useMe } from "@/features/auth";
import { blueprints, type Property } from "@/features/blueprints";
import type { Entity, EntityPage } from "@/features/catalog";
import { scorecardColumnsQueryOptions } from "@/features/scorecards";
import { widgetQueryOptions } from "@/features/widgets";
import { formatAbsolute, formatRelative } from "@/lib/format";
import { toCatalogFilters } from "./catalog-filter";
import { WidgetCard, WidgetError, WidgetLoading, type WidgetProps } from "./widget-grid";

const builtIns: Record<string, string> = {
  $identifier: "Identifier",
  $integration: "Integration",
  $createdAt: "Created",
  $updatedAt: "Updated",
};

// TableWidget lists the first entities that match, with a link to the rest.
export function TableWidget({ source, widget, context }: WidgetProps) {
  const me = useMe();
  const data = useQuery(widgetQueryOptions<EntityPage>(source, widget.identifier, context));
  const { data: bp } = useQuery(blueprints.queries.detail(widget.blueprint ?? ""));
  const { data: scorecards = [] } = useQuery(scorecardColumnsQueryOptions(widget.blueprint ?? ""));
  if (data.error) return <WidgetError widget={widget} error={data.error} />;
  const spec = bp?.published?.spec;
  if (!data.data || !spec) return <WidgetLoading widget={widget} height="h-40" />;

  const columns = (widget.columns ?? []).filter((column) => column !== "$title");
  const filters = toCatalogFilters(widget.filter, {
    entity: context.entity,
    entityData: context.entityData,
    me,
    filters: context.filters,
  });
  const { entities, total } = data.data;
  const more =
    filters && !context.draft ? (
      <Link
        to="/catalog/$blueprint"
        params={{ blueprint: spec.identifier }}
        search={{ view: "entities", f: filters, sort: widget.sort }}
        className="flex items-center gap-1 text-muted-foreground hover:text-foreground"
      >
        {total > entities.length ? `All ${total}` : "Open in catalog"}
        <ArrowRight className="size-3.5" aria-hidden />
      </Link>
    ) : undefined;

  return (
    <WidgetCard widget={widget} action={more} flush>
      {entities.length === 0 ? (
        <p className="px-4 pb-4 text-sm text-muted-foreground">Nothing matches yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">Title</TableHead>
              {columns.map((column) => (
                <TableHead key={column}>
                  {scorecards.find((s) => column === `$scorecard.${s.identifier}`)?.title ??
                    builtIns[column] ??
                    spec.properties.find((p) => p.identifier === column)?.title ??
                    column}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {entities.map((entity) => (
              <TableRow key={entity.identifier}>
                <TableCell className="pl-4 font-medium">
                  <EntityLink blueprint={spec.identifier} identifier={entity.identifier}>
                    {entity.title}
                  </EntityLink>
                </TableCell>
                {columns.map((column) => (
                  <TableCell key={column}>
                    {column.startsWith("$scorecard.") ? (
                      <ScorecardCell
                        entity={entity}
                        column={scorecards.find((s) => column === `$scorecard.${s.identifier}`)}
                      />
                    ) : (
                      <Cell
                        entity={entity}
                        column={column}
                        property={spec.properties.find((p) => p.identifier === column)}
                      />
                    )}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </WidgetCard>
  );
}

function Cell({
  entity,
  column,
  property,
}: Readonly<{ entity: Entity; column: string; property?: Property }>) {
  if (property) {
    return (
      <PropertyValue
        property={property}
        value={entity.properties[column]}
        links={entity.related[column]}
      />
    );
  }
  switch (column) {
    case "$identifier":
      return <code className="font-mono text-xs">{entity.identifier}</code>;
    case "$integration":
      return entity.integration;
    case "$createdAt":
    case "$updatedAt": {
      const at = column === "$createdAt" ? entity.createdAt : entity.updatedAt;
      return (
        <span className="whitespace-nowrap text-muted-foreground" title={formatAbsolute(at)}>
          {formatRelative(at)}
        </span>
      );
    }
  }
  return null;
}
