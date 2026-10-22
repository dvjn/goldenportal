export {
  type Connector,
  type CreatedIngestToken,
  type IngestToken,
  type IntegrationDetail,
  type IntegrationSummary,
  integrationKeys,
  type Run,
  type RunStatus,
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
export { RunHistory, RunStatusBadge, runCounts, runSource } from "./run-history";

export const connectorLabels: Record<string, string> = {
  manual: "Built in",
  push: "Push",
  gitlab: "GitLab",
  sonarqube: "SonarQube",
};
