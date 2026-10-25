import { useQuery } from "@tanstack/react-query";
import { PropertyValue } from "@/components/catalog/property-value";
import { blueprints } from "@/features/blueprints";
import { entityQueryOptions } from "@/features/catalog";
import { WidgetCard, WidgetLoading, type WidgetProps } from "./widget-grid";

// PropertiesWidget shows some of the page's entity's properties, in the
// order the widget lists them, or else every one that isn't hidden.
export function PropertiesWidget({ widget, context }: WidgetProps) {
  const blueprint = widget.blueprint ?? "";
  const { data: bp } = useQuery(blueprints.queries.detail(blueprint));
  const { data: entity } = useQuery({
    ...entityQueryOptions(blueprint, context.entity ?? ""),
    enabled: Boolean(context.entity),
  });
  const spec = bp?.published?.spec;
  if (!spec || !entity) return <WidgetLoading widget={widget} height="h-24" />;

  const shown = widget.properties?.length
    ? widget.properties.flatMap((id) => spec.properties.filter((p) => p.identifier === id))
    : spec.properties.filter((property) => !property.display?.hidden);

  return (
    <WidgetCard widget={widget}>
      <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[minmax(7rem,auto)_minmax(0,1fr)]">
        {shown.map((property) => (
          <div key={property.identifier} className="contents">
            <dt className="text-muted-foreground" title={property.description}>
              {property.title}
            </dt>
            <dd className="min-w-0">
              <PropertyValue
                property={property}
                value={entity.properties[property.identifier]}
                links={entity.related[property.identifier]}
              />
            </dd>
          </div>
        ))}
      </dl>
    </WidgetCard>
  );
}
