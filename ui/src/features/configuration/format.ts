import type { IntegrationSummary } from "@/features/integrations";

export function scheduleLabel(integration: Pick<IntegrationSummary, "pull" | "interval">): string {
  if (!integration.pull) return "Fed by pushes";
  return integration.interval ? `Every ${integration.interval}` : "Syncs on request";
}

export function entityCountLabel(count: number): string {
  return count === 1 ? "1 entity" : `${count.toLocaleString("en")} entities`;
}

export function isStructured(value: unknown): value is object {
  return typeof value === "object" && value !== null;
}

export function formatConfigValue(value: unknown): string {
  if (typeof value === "string") return value;
  return JSON.stringify(value, null, isStructured(value) ? 2 : undefined);
}

export function documentKey(kind: "Blueprint" | "Integration", identifier: string): string {
  return `${kind}/${identifier}`;
}
