import { createFileRoute } from "@tanstack/react-router";
import { blueprintsQueryOptions, CatalogHome, changesQueryOptions } from "@/features/catalog";
import { integrationsQueryOptions } from "@/features/integrations";

export const Route = createFileRoute("/_authenticated/")({
  loader: ({ context: { queryClient } }) => {
    void queryClient.prefetchQuery(blueprintsQueryOptions);
    void queryClient.prefetchQuery(integrationsQueryOptions);
    void queryClient.prefetchQuery(changesQueryOptions(8));
  },
  component: CatalogHome,
});
