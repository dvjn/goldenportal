export {
  type Connector,
  type CreatedIngestToken,
  type FieldMapping,
  type IngestToken,
  type IntegrationDetail,
  type IntegrationIcon,
  type IntegrationKind,
  type IntegrationMapping,
  type IntegrationSecret,
  type IntegrationSummary,
  integrationKeys,
  type Run,
  type RunStatus,
  type RunTrigger,
} from "./api";
export {
  ingestTokensQueryOptions,
  integrationQueryOptions,
  integrationsQueryOptions,
  runsQueryOptions,
  useCreateIngestToken,
  useIngestTokens,
  useIntegration,
  useIntegrations,
  useRevokeIngestToken,
  useRuns,
  useSyncIntegration,
} from "./queries";

export const connectorLabels: Record<string, string> = {
  manual: "Built in",
  push: "Push",
  gitlab: "GitLab",
  sonarqube: "SonarQube",
  static: "Static",
};
