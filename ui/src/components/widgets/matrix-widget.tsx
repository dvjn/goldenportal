import { cn } from "cn";
import { EmptyValue, exactTitle } from "@/components/catalog/property-value";
import { tableCellClass } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { MatrixCell, MatrixData } from "@/features/widgets";
import { formatPercent } from "@/lib/format";
import { formatReading, isShare } from "./reading";
import { WidgetCard, WidgetError, WidgetLoading, type WidgetProps } from "./widget-grid";
import { WidgetLink } from "./widget-link";

// MatrixWidget shows each measure for each group, so a viewer reads down a
// column for the group that lags and across a row for what it lags in.
// All, when asked for, comes last, as a total does. A share's cell opens
// the entities that don't match, the ones to work on; any other measure's
// the entities it measured.
export function MatrixWidget({ widget, context, data, error }: WidgetProps<MatrixData>) {
  if (error) return <WidgetError widget={widget} error={error} />;
  if (!data) return <WidgetLoading widget={widget} height="h-40" />;

  const live = !context.draft;
  // A number's bar runs to the largest in its column, the overall row's
  // left out, as it would dwarf every group's.
  const most = data.measures.map((_, i) =>
    Math.max(0, ...data.rows.map((row) => row.cells[i]?.value ?? 0)),
  );
  const cell = (c: MatrixCell, i: number) => (
    <TableCell key={data.measures[i].title} className={tableCellClass}>
      {isShare(c) ? (
        <Share cell={c} live={live} />
      ) : (
        <Amount cell={c} unit={data.measures[i].unit} most={most[i]} live={live} />
      )}
    </TableCell>
  );
  return (
    <WidgetCard widget={widget} flush>
      {data.rows.length === 0 ? (
        <EmptyState compact title="No groups yet" />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className={tableCellClass}>
                <span className="sr-only">Group</span>
              </TableHead>
              {data.measures.map(({ title }) => (
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
                {row.cells.map(cell)}
              </TableRow>
            ))}
            {data.overall && (
              <TableRow className="[&>td]:border-t-2">
                <TableCell className={cn(tableCellClass, "font-medium")}>All</TableCell>
                {data.overall.map(cell)}
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

function DoesntApply() {
  return (
    <div className="text-right">
      <EmptyValue title="Doesn't apply" />
    </div>
  );
}

// Amount is a cell's number in its measure's unit, and a bar to scan a
// column by. A group the measure has nothing to work on has none.
function Amount({
  cell,
  unit,
  most,
  live,
}: Readonly<{ cell: MatrixCell; unit?: string; most: number; live: boolean }>) {
  if (cell.value === null) return <DoesntApply />;
  return (
    <div className="grid min-w-24 gap-1">
      <span className="text-right">
        <WidgetLink
          link={live ? cell.link : undefined}
          className="font-medium tabular-nums hover:underline"
        >
          <span title={exactTitle(cell.value, unit)}>{formatReading(cell, unit)}</span>
        </WidgetLink>
      </span>
      <span className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden>
        <span
          className="block h-full rounded-full bg-primary"
          style={{ width: `${most ? Math.min((cell.value / most) * 100, 100) : 0}%` }}
        />
      </span>
    </div>
  );
}

// Share is a cell's percent, how many of how many beside it, and a bar to
// scan a column by. A group the measure doesn't apply to has none.
function Share({
  cell,
  live,
}: Readonly<{ cell: MatrixCell & { count: number; total: number }; live: boolean }>) {
  if (cell.value === null) return <DoesntApply />;
  return (
    <div className="grid min-w-24 gap-1">
      <span
        className={cn("flex items-baseline justify-end gap-2", cell.total < few && "opacity-50")}
      >
        <WidgetLink
          link={live ? cell.link : undefined}
          className="font-medium tabular-nums hover:underline"
        >
          {formatPercent(cell.count, cell.total)}
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
