import { createFileRoute, Outlet } from "@tanstack/react-router";
import { PageError } from "@/components/page-error";
import { entityQueryOptions } from "@/features/catalog";

export const Route = createFileRoute("/_authenticated/catalog/$blueprint/$identifier")({
  loader: ({ context, params }) => {
    void context.queryClient.prefetchQuery(entityQueryOptions(params.blueprint, params.identifier));
  },
  component: Outlet,
  errorComponent: PageError,
});
