import { PropertyType } from "@/components/blueprints/property-type";
import type { BlueprintSpec } from "@/features/blueprints";

// BlueprintPreview is a compact list of a blueprint's properties.
export function BlueprintPreview({ spec }: Readonly<{ spec: BlueprintSpec }>) {
  if (spec.properties.length === 0) {
    return <p className="text-sm text-muted-foreground">No properties yet.</p>;
  }

  return (
    <ul className="grid gap-2">
      {spec.properties.map((property) => (
        <li key={property.identifier} className="flex items-center justify-between gap-3 text-sm">
          <div className="grid min-w-0">
            <span className="truncate font-medium">{property.title}</span>
            <code className="truncate font-mono text-xs text-muted-foreground">
              {property.identifier}
            </code>
          </div>
          <PropertyType property={property} />
        </li>
      ))}
    </ul>
  );
}
