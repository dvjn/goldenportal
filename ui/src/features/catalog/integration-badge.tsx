import { Badge } from "@/components/ui/badge";
import type { Integration } from "./api";

export function IntegrationBadge({ integration }: { integration: Integration }) {
  return <Badge variant={integration.manual ? "outline" : "secondary"}>{integration.title}</Badge>;
}
