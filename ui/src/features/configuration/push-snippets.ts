import type { IntegrationMapping } from "@/features/integrations";

export const tokenVariable = "GOLDPORT_INGEST_TOKEN";

const topLevelField = /(?<![\w\])])\.([A-Za-z_]\w*)/g;

export function ingestPath(integration: string, kind: string): string {
  return `/api/v1/ingest/${encodeURIComponent(integration)}/${encodeURIComponent(kind)}`;
}

function expressions(mapping: IntegrationMapping): string[] {
  return [
    mapping.identifier ?? "",
    mapping.title ?? "",
    ...mapping.properties.map((property) => property.expression),
    ...mapping.relations.map((relation) => relation.expression),
  ];
}

export function exampleRecord(mapping: IntegrationMapping): Record<string, string> {
  if (mapping.canonical) return { identifier: "example", title: "Example" };
  const fields = new Set(
    expressions(mapping).flatMap((expression) =>
      [...expression.matchAll(topLevelField)].map((match) => match[1]),
    ),
  );
  return Object.fromEntries([...fields].map((field) => [field, "example"]));
}

function curlLines(url: string, mapping: IntegrationMapping): string[] {
  const body = JSON.stringify({ mode: "upsert", entities: [exampleRecord(mapping)] });
  return [
    `curl -sS --fail-with-body -X POST "${url}" \\`,
    `  -H "Authorization: Bearer $${tokenVariable}" \\`,
    `  -H "Content-Type: application/json" \\`,
    `  -d '${body}'`,
  ];
}

export function curlSnippet(origin: string, integration: string, mapping: IntegrationMapping) {
  return curlLines(`${origin}${ingestPath(integration, mapping.kind)}`, mapping).join("\n");
}

export function gitlabSnippet(origin: string, integration: string, mapping: IntegrationMapping) {
  const curl = curlLines(`$GOLDPORT_URL${ingestPath(integration, mapping.kind)}`, mapping);
  return [
    `register-${mapping.kind}:`,
    "  stage: deploy",
    "  image: curlimages/curl:8.10.1",
    "  variables:",
    `    GOLDPORT_URL: ${origin}`,
    "  script:",
    "    - |",
    ...curl.map((line) => `      ${line}`),
  ].join("\n");
}
