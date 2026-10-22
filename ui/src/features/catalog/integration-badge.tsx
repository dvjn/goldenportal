import { Link } from "@tanstack/react-router";
import { Badge } from "@/components/ui/badge";
import type { Integration } from "./api";

export function IntegrationBadge({ integration }: { integration: Integration }) {
  return (
    <Badge variant={integration.manual ? "outline" : "secondary"} asChild>
      <Link to="/integrations/$integration" params={{ integration: integration.identifier }}>
        {integration.title}
      </Link>
    </Badge>
  );
}
