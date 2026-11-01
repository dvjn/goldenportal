import { cn } from "cn";
import { EntityLink } from "@/components/catalog/entity-link";
import { EmptyValue, formatNumber, PropertyValue } from "@/components/catalog/property-value";
import { tableCellClass } from "@/components/data-table";
import { ScoreLink } from "@/components/scorecards/scorecard-value";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { Link } from "@/features/catalog";
import type { TableColumn, TableData } from "@/features/widgets";
import { formatAbsolute, formatRelative } from "@/lib/format";
import { ViewAll, WidgetCard, WidgetError, WidgetLoading, type WidgetProps } from "./widget-grid";
import { linkable, WidgetLink } from "./widget-link";

// TableWidget lists the first entities that match, with a link to the rest.
// The server reads each column and says how to show it.
export function TableWidget({ widget, context, data, error }: WidgetProps<TableData>) {
  if (error) return <WidgetError widget={widget} error={error} />;
  if (!data) return <WidgetLoading widget={widget} height="h-40" />;

  // Every row leads with its title, so a title column would say it twice.
  const columns = data.columns.flatMap((column, index) =>
    column.path === "title" ? [] : [{ column, index }],
  );
  const live = !context.draft;

  return (
    <WidgetCard widget={widget} action={live && <ViewAll link={data.link} />} flush>
      {data.rows.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">Nothing matches yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className={tableCellClass}>Title</TableHead>
              {columns.map(({ column, index }) => (
                <TableHead key={index} className={tableCellClass}>
                  {column.title}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.rows.map((row) => {
              const link = live ? row.link : undefined;
              return (
                <TableRow key={row.identifier}>
                  <TableCell className={cn(tableCellClass, "font-medium")}>
                    {linkable(link) ? (
                      <WidgetLink link={link} className="hover:underline">
                        {row.title}
                      </WidgetLink>
                    ) : (
                      <EntityLink blueprint={data.blueprint} identifier={row.identifier}>
                        {row.title}
                      </EntityLink>
                    )}
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
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
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

// Computed shows what a jq column worked out: numbers in its unit, and
// lists as their items.
function Computed({ value, unit }: Readonly<{ value: unknown; unit?: string }>) {
  if (value === null || value === undefined || value === "") {
    return <EmptyValue />;
  }
  if (typeof value === "number") {
    return <span className="tabular-nums">{formatNumber(value, unit)}</span>;
  }
  if (typeof value === "boolean") return value ? "Yes" : "No";
  if (Array.isArray(value)) {
    return value.length === 0 ? (
      <EmptyValue />
    ) : (
      value.map((item) => (typeof item === "string" ? item : JSON.stringify(item))).join(", ")
    );
  }
  return typeof value === "string" ? value : JSON.stringify(value);
}
