import { cn } from "cn";
import { EmptyValue } from "@/components/catalog/property-value";
import { tableCellClass } from "@/components/data-table";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { MatrixCell, MatrixData } from "@/features/widgets";
import { WidgetCard, WidgetError, WidgetLoading, type WidgetProps } from "./widget-grid";
import { WidgetLink } from "./widget-link";

// MatrixWidget shows each measure for each group, so a viewer reads down a
// column for the group that lags and across a row for what it lags in.
// All, when asked for, comes last, as a total does. Each cell opens the
// entities that don't match, the ones to work on.
export function MatrixWidget({ widget, context, data, error }: WidgetProps<MatrixData>) {
  if (error) return <WidgetError widget={widget} error={error} />;
  if (!data) return <WidgetLoading widget={widget} height="h-40" />;

  const live = !context.draft;
  return (
    <WidgetCard widget={widget} flush>
      {data.rows.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">Nothing to count yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className={tableCellClass}>
                <span className="sr-only">Group</span>
              </TableHead>
              {data.measures.map((title) => (
                <TableHead key={title} className={cn(tableCellClass, "text-right")}>
                  {title}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.rows.map((row) => (
              <TableRow key={JSON.stringify(row.value)}>
                <TableCell className={cn(tableCellClass, "font-medium")}>
                  <WidgetLink link={live ? row.link : undefined} className="hover:underline">
                    {row.title ?? String(row.value)}
                  </WidgetLink>
                </TableCell>
                {row.cells.map((cell, i) => (
                  <TableCell key={data.measures[i]} className={tableCellClass}>
                    <Share cell={cell} live={live} />
                  </TableCell>
                ))}
              </TableRow>
            ))}
            {data.overall && (
              <TableRow className="[&>td]:border-t-2">
                <TableCell className={cn(tableCellClass, "font-medium")}>All</TableCell>
                {data.overall.map((cell, i) => (
                  <TableCell key={data.measures[i]} className={tableCellClass}>
                    <Share cell={cell} live={live} />
                  </TableCell>
                ))}
              </TableRow>
            )}
          </TableBody>
        </Table>
      )}
    </WidgetCard>
  );
}

// few is how many entities a share needs before it says much; fewer, and
// one entity swings it, so its numbers are set back. Its bar isn't, or a
// full one would look as empty as its track.
const few = 3;

// Share is a cell's percent, how many of how many beside it, and a bar to
// scan a column by. A group the measure doesn't apply to has none.
function Share({ cell, live }: Readonly<{ cell: MatrixCell; live: boolean }>) {
  if (cell.value === null) {
    return (
      <div className="text-right">
        <EmptyValue title="Doesn't apply" />
      </div>
    );
  }
  return (
    <div className="grid min-w-24 gap-1">
      <span
        className={cn("flex items-baseline justify-end gap-2", cell.total < few && "opacity-50")}
      >
        <WidgetLink
          link={live ? cell.link : undefined}
          className="font-medium tabular-nums hover:underline"
        >
          {Math.round(cell.value)}%
        </WidgetLink>
        <span className="text-xs text-muted-foreground tabular-nums">
          {cell.count} of {cell.total}
        </span>
      </span>
      <span className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden>
        <span
          className="block h-full rounded-full bg-primary"
          style={{ width: `${cell.value}%` }}
        />
      </span>
    </div>
  );
}
