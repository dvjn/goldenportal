import type { IntegrationSummary } from "@/features/integrations/api";

export type SourceIcons = {
  ofIntegration: (identifier: string) => string;
  ofBlueprint: (blueprint: string) => string;
};

export function sourceIcons(integrations: IntegrationSummary[]): SourceIcons {
  const byIntegration = new Map(integrations.map(({ identifier, icon }) => [identifier, icon]));
  const byBlueprint = new Map<string, string>();
  for (const integration of integrations) {
    for (const { blueprint } of integration.kinds) {
      if (!byBlueprint.has(blueprint)) byBlueprint.set(blueprint, integration.icon);
    }
  }
  return {
    ofIntegration: (identifier) => byIntegration.get(identifier) ?? "",
    ofBlueprint: (blueprint) => byBlueprint.get(blueprint) ?? "",
  };
}
