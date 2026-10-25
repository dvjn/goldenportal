import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { formatNumber } from "@/components/catalog/property-value";
import { useMe } from "@/features/auth";
import { blueprints } from "@/features/blueprints";
import { type StatData, widgetQueryOptions } from "@/features/widgets";
import { toCatalogFilters } from "./catalog-filter";
import { WidgetCard, WidgetError, WidgetLoading, type WidgetProps } from "./widget-grid";

const verbs = { sum: "Total", avg: "Average", min: "Lowest", max: "Highest" } as const;

// StatWidget shows one number: how many entities match, or a sum of a
// property across them.
export function StatWidget({ source, widget, context }: WidgetProps) {
  const me = useMe();
  const data = useQuery(widgetQueryOptions<StatData>(source, widget.identifier, context));
  const { data: bp } = useQuery(blueprints.queries.detail(widget.blueprint ?? ""));
  if (data.error) return <WidgetError widget={widget} error={data.error} />;
  if (!data.data) return <WidgetLoading widget={widget} height="h-9" />;

  const property = bp?.published?.spec.properties.find((p) => p.identifier === widget.property);
  const value = data.data.value;
  const filters = toCatalogFilters(widget.filter, { entity: context.entity, me: me.email });
  const number = (
    <span className="text-3xl font-semibold tracking-tight tabular-nums">
      {value === null ? "—" : formatNumber(value, widget.unit ?? property?.display?.unit)}
    </span>
  );

  return (
    <WidgetCard widget={widget}>
      <div className="grid gap-1">
        {widget.aggregate === "count" && filters && widget.blueprint && !context.draft ? (
          <Link
            to="/catalog/$blueprint"
            params={{ blueprint: widget.blueprint }}
            search={{ view: "entities", f: filters }}
            className="w-fit hover:underline"
          >
            {number}
          </Link>
        ) : (
          number
        )}
        {widget.aggregate && widget.aggregate !== "count" && (
          <span className="text-xs text-muted-foreground">
            {verbs[widget.aggregate]} {property?.title ?? widget.property}
          </span>
        )}
      </div>
    </WidgetCard>
  );
}
