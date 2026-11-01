import { useQuery } from "@tanstack/react-query";
import { PropertyValue } from "@/components/catalog/property-value";
import { Detail, DetailList } from "@/components/detail-list";
import { blueprints } from "@/features/blueprints";
import { entityQueryOptions } from "@/features/catalog";
import { WidgetCard, WidgetLoading, type WidgetProps } from "./widget-grid";

// PropertiesWidget shows some of the page's entity's properties, in the
// order the widget lists them, or else every one that isn't hidden.
export function PropertiesWidget({ source, widget, context }: WidgetProps) {
  const blueprint = "blueprint" in source ? source.blueprint : "";
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
      <DetailList>
        {shown.map((property) => (
          <Detail
            key={property.identifier}
            label={<span title={property.description}>{property.title}</span>}
          >
            <PropertyValue
              property={property}
              value={entity.properties[property.identifier]}
              links={entity.related[property.identifier]}
              entity={entity}
            />
          </Detail>
        ))}
      </DetailList>
    </WidgetCard>
  );
}
