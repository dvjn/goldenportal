import { createFileRoute, Outlet } from "@tanstack/react-router";
import { PageError } from "@/components/page-error";
import { blueprintQueryOptions } from "@/features/catalog";

export const Route = createFileRoute("/_authenticated/catalog/$blueprint")({
  loader: ({ context, params }) => {
    void context.queryClient.prefetchQuery(blueprintQueryOptions(params.blueprint));
  },
  component: Outlet,
  errorComponent: PageError,
});
