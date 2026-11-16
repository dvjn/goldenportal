import { cn } from "cn";
import { RowActions, useRowActions } from "@/components/actions/entity-actions";
import { EntityLink } from "@/components/catalog/entity-link";
import {
  BooleanValue,
  EmptyValue,
  exactTitle,
  formatNumber,
  PropertyValue,
} from "@/components/catalog/property-value";
import { tableCellClass } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { ScoreLink } from "@/components/scorecards/scorecard-value";
import { Truncated } from "@/components/truncated";
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { Link } from "@/features/catalog";
import type { TableColumn, TableData } from "@/features/widgets";
import { formatAbsolute, formatRelative } from "@/lib/format";
import { ViewAll, WidgetCard, WidgetError, WidgetLoading, type WidgetProps } from "./widget-grid";
import { linkable, WidgetLink } from "./widget-link";
import { labelCell, WidgetTable } from "./widget-table";

// TableWidget lists the first entities that match, with a link to the rest.
// The server reads each column and says how to show it.
export function TableWidget({ widget, context, data, error }: WidgetProps<TableData>) {
  const live = !context.draft;
  // Rows that open something else aren't entities to act on.
  const entities = !widget.rowLink;
  const rowActions = useRowActions(
    data?.blueprint ?? "",
    entities ? (data?.rows ?? []).map((row) => row.identifier) : [],
    live,
  );
  if (error) return <WidgetError widget={widget} error={error} />;
  if (!data) return <WidgetLoading widget={widget} height="h-40" />;

  // Every row leads with its title, so a title column would say it twice.
  const columns = data.columns.flatMap((column, index) =>
    column.path === "title" ? [] : [{ column, index }],
  );

  return (
    <WidgetCard widget={widget} action={live && <ViewAll link={data.link} />} flush>
      {data.rows.length === 0 ? (
        <EmptyState compact title="No entities yet" />
      ) : (
        <WidgetTable>
          <TableHeader>
            <TableRow>
              <TableHead className={tableCellClass}>Title</TableHead>
              {columns.map(({ column, index }) => (
                <TableHead key={index} className={tableCellClass}>
                  {column.title}
                </TableHead>
              ))}
              {entities && live && (
                <TableHead className={tableCellClass}>
                  <span className="sr-only">Actions</span>
                </TableHead>
              )}
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.rows.map((row) => {
              const link = live ? row.link : undefined;
              return (
                <TableRow key={row.identifier}>
                  <TableCell className={cn(tableCellClass, labelCell, "font-medium")}>
                    <Truncated className="max-w-none" title={row.title}>
                      {linkable(link) ? (
                        <WidgetLink link={link} className="hover:underline">
                          {row.title}
                        </WidgetLink>
                      ) : (
                        <EntityLink blueprint={data.blueprint} identifier={row.identifier}>
                          {row.title}
                        </EntityLink>
                      )}
                    </Truncated>
                  </TableCell>
                  {columns.map(({ column, index }) => (
                    <TableCell key={index} className={tableCellClass}>
                      <Cell
                        column={column}
                        value={row.values[index]}
                        links={row.links?.[index] ?? undefined}
                      />
                    </TableCell>
                  ))}
                  {entities && live && (
                    <TableCell className={cn(tableCellClass, "text-right")}>
                      <RowActions
                        entity={{
                          blueprint: data.blueprint,
                          identifier: row.identifier,
                          title: row.title,
                        }}
                        actions={rowActions(row.identifier)}
                      />
                    </TableCell>
                  )}
                </TableRow>
              );
            })}
          </TableBody>
        </WidgetTable>
      )}
    </WidgetCard>
  );
}

function Cell({
  column,
  value,
  links,
}: Readonly<{
  column: TableColumn;
  value: unknown;
  // links name a relation's entities, or the entity a tier was scored for.
  links?: Link[];
}>) {
  if (column.condition) {
    return typeof value === "boolean" ? <BooleanValue value={value} compact /> : <EmptyValue />;
  }
  if (column.format) return <Computed value={value} unit={column.unit} />;
  if (column.scorecard) {
    const scored = links?.[0];
    return (
      <ScoreLink
        scorecard={column.scorecard.identifier}
        tiers={column.scorecard.tiers}
        tier={typeof value === "string" ? value : null}
        entity={scored && { blueprint: column.scorecard.blueprint, identifier: scored.identifier }}
      />
    );
  }
  if (column.property) {
    const property = column.unit
      ? { ...column.property, display: { ...column.property.display, unit: column.unit } }
      : column.property;
    return <PropertyValue property={property} value={value} links={links} compact />;
  }
  if (column.builtIn) return <BuiltIn name={column.builtIn} value={value} />;
  return <Computed value={value} unit={column.unit} />;
}

// BuiltIn shows one of the fields every entity has.
function BuiltIn({ name, value }: Readonly<{ name: string; value: unknown }>) {
  if (typeof value !== "string" || value === "") {
    return <EmptyValue />;
  }
  switch (name) {
    case "identifier":
      return <code className="font-mono text-xs">{value}</code>;
    case "createdAt":
    case "updatedAt":
      return (
        <span className="whitespace-nowrap text-muted-foreground" title={formatAbsolute(value)}>
          {formatRelative(value)}
        </span>
      );
  }
  return value;
}

// Computed shows what a column's format worked out: numbers in its unit,
// and lists as their items.
function Computed({ value, unit }: Readonly<{ value: unknown; unit?: string }>) {
  if (value === null || value === undefined || value === "") {
    return <EmptyValue />;
  }
  if (typeof value === "number") {
    return (
      <span className="tabular-nums" title={exactTitle(value, unit)}>
        {formatNumber(value, unit)}
      </span>
    );
  }
  // A yes or no reads the same as a boolean property's.
  if (typeof value === "boolean") return <BooleanValue value={value} compact />;
  if (Array.isArray(value)) {
    return value.length === 0 ? (
      <EmptyValue />
    ) : (
      value.map((item) => (typeof item === "string" ? item : JSON.stringify(item))).join(", ")
    );
  }
  return typeof value === "string" ? value : JSON.stringify(value);
}
