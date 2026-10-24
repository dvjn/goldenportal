import { Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { Property } from "@/features/blueprints";

// PropertyType shows a property's type, linking relations to their blueprint.
export function PropertyType({ property }: Readonly<{ property: Property }>) {
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
