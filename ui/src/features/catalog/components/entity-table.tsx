import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { TableFrame } from "@/components/common/table-frame";
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import type { Blueprint, EntitySummary, SortDirection } from "../api";
import { type Column, propertyByKey } from "../columns";
import { ColumnValue } from "./column-value";
import { DescribedLabel } from "./described-label";
import { Timestamp } from "./property-value";
import { SortHeader } from "./sort-header";
import { LeafTitle } from "./split-title";

export type EntitySort = { by: string; direction: SortDirection };

export const titleSortKey = "title";
export const updatedSortKey = "updatedAt";

export function toggleSort(
  current: EntitySort,
  key: string,
  initial: SortDirection = "asc",
): EntitySort {
  if (current.by !== key) return { by: key, direction: initial };
  return { by: key, direction: current.direction === "asc" ? "desc" : "asc" };
}

function initialDirection(column: Column): SortDirection {
  return column.property?.format === "date-time" ? "desc" : "asc";
}

function subtitleOf(blueprint: Blueprint, entity: EntitySummary) {
  const property = propertyByKey(blueprint, blueprint.display.subtitle);
  const value = property ? entity.properties[property.key] : undefined;
  if (property && typeof value === "string" && value !== "" && value !== entity.title) {
    return { text: value, mono: property.display === "mono" };
  }
  return { text: entity.identifier, mono: true };
}

function TitleCell({
  blueprint,
  entity,
}: Readonly<{ blueprint: Blueprint; entity: EntitySummary }>) {
  const title = entity.title || entity.identifier;
  const subtitle = subtitleOf(blueprint, entity);
  return (
    <TableCell className="max-w-72">
      <Link
        to="/catalog/$blueprint/$identifier"
        params={{ blueprint: blueprint.identifier, identifier: entity.identifier }}
        title={title}
        className="block truncate font-medium hover:underline"
      >
        <LeafTitle title={title} />
      </Link>
      {subtitle.text !== title ? (
        <div
          className={cn("truncate text-xs text-muted-foreground", subtitle.mono && "font-mono")}
          title={subtitle.text}
        >
          {subtitle.text}
        </div>
      ) : null}
    </TableCell>
  );
}

export function EntityTable({
  blueprint,
  entities,
  columns,
  sort,
  onSortChange,
  footer,
}: Readonly<{
  blueprint: Blueprint;
  entities: EntitySummary[];
  columns: Column[];
  sort: EntitySort;
  onSortChange: (sort: EntitySort) => void;
  footer?: ReactNode;
}>) {
  const directionOf = (key: string) => (sort.by === key ? sort.direction : undefined);
  const sortBy = (key: string, initial?: SortDirection) =>
    onSortChange(toggleSort(sort, key, initial));

  return (
    <TableFrame footer={footer}>
      <TableHeader>
        <TableRow>
          <SortHeader
            label="Title"
            direction={directionOf(titleSortKey)}
            onSort={() => sortBy(titleSortKey)}
          />
          {columns.map((column) => {
            const label = <DescribedLabel label={column.label} description={column.description} />;
            if (!column.sortable) {
              return (
                <TableHead
                  key={column.key}
                  className={cn("whitespace-nowrap", column.align === "right" && "text-right")}
                >
                  {label}
                </TableHead>
              );
            }
            return (
              <SortHeader
                key={column.key}
                label={label}
                align={column.align}
                direction={directionOf(column.key)}
                onSort={() => sortBy(column.key, initialDirection(column))}
              />
            );
          })}
          <SortHeader
            label="Updated"
            direction={directionOf(updatedSortKey)}
            onSort={() => sortBy(updatedSortKey, "desc")}
          />
        </TableRow>
      </TableHeader>
      <TableBody>
        {entities.map((entity) => (
          <TableRow key={entity.identifier} className="hover:bg-muted/40">
            <TitleCell blueprint={blueprint} entity={entity} />
            {columns.map((column) => (
              <TableCell key={column.key} className={column.className}>
                <ColumnValue column={column} entity={entity} />
              </TableCell>
            ))}
            <TableCell className="whitespace-nowrap text-muted-foreground">
              <Timestamp value={entity.updated_at} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </TableFrame>
  );
}
