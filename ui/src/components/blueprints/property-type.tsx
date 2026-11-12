import { Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { Property } from "@/features/blueprints";

// PropertyType shows a property's type, linking relations to their
// blueprint and, given the blueprint the property is on, scorecard
// properties to their scorecard, which grades that blueprint.
export function PropertyType({
  property,
  blueprint,
}: Readonly<{ property: Property; blueprint?: string }>) {
  if (property.relation) {
    return (
      <Badge asChild variant="outline" className="font-mono">
        <Link to="/blueprints/$identifier" params={{ identifier: property.relation }}>
          <ArrowRight aria-hidden />
          {property.relation}
          {property.many && "[]"}
        </Link>
      </Badge>
    );
  }
  if (property.scorecard && blueprint) {
    return (
      <Badge asChild variant="outline" className="font-mono">
        <Link
          to="/blueprints/$blueprint/scorecards/$identifier"
          params={{ blueprint, identifier: property.scorecard }}
        >
          <ArrowRight aria-hidden />
          {property.scorecard}
        </Link>
      </Badge>
    );
  }
  if (!property.type) {
    return <span className="text-xs text-muted-foreground">Looked up</span>;
  }
  const label =
    property.type === "array" && property.items
      ? `${property.items.format ?? property.items.type}[]`
      : (property.format ?? property.type);
  return (
    <Badge variant="secondary" className="font-mono">
      {label}
    </Badge>
  );
}
