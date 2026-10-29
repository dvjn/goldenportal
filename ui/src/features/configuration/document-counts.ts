import type { Blueprint } from "@/features/catalog";
import type { IntegrationSummary } from "@/features/integrations";
import { documentKey } from "./format";

export type DocumentFacts = { count: number; icon?: string };

export function documentFacts(
  blueprints: Blueprint[] | undefined,
  integrations: IntegrationSummary[] | undefined,
): Map<string, DocumentFacts> {
  return new Map([
    ...(blueprints ?? []).map(
      (blueprint) =>
        [
          documentKey("Blueprint", blueprint.identifier),
          { count: blueprint.entity_count, icon: blueprint.icon },
        ] as const,
    ),
    ...(integrations ?? []).map(
      (integration) =>
        [
          documentKey("Integration", integration.identifier),
          { count: integration.entity_count, icon: integration.icon },
        ] as const,
    ),
  ]);
}
