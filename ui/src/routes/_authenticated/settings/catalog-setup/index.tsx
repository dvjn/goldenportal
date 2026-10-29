import { createFileRoute } from "@tanstack/react-router";
import { blueprintsQueryOptions } from "@/features/catalog";
import {
  type ConfigurationSearch,
  ConfigurationView,
  configurationTabs,
} from "@/features/configuration";
import { integrationsQueryOptions } from "@/features/integrations";
import { usePageTitle } from "@/lib/use-page-title";

function optionalString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

export const Route = createFileRoute("/_authenticated/settings/catalog-setup/")({
  validateSearch: (raw: Record<string, unknown>): ConfigurationSearch => ({
    tab: configurationTabs.find((tab) => tab === raw.tab),
    blueprint: optionalString(raw.blueprint),
    integration: optionalString(raw.integration),
    document: optionalString(raw.document),
  }),
  loader: ({ context: { queryClient } }) => {
    void queryClient.prefetchQuery(blueprintsQueryOptions);
    void queryClient.prefetchQuery(integrationsQueryOptions);
  },
  component: ConfigurationPage,
});

function ConfigurationPage() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  usePageTitle("Catalog setup");

  return (
    <ConfigurationView
      search={search}
      onSearchChange={(next) => void navigate({ search: next, replace: true })}
    />
  );
}
