import { createFileRoute, Outlet } from "@tanstack/react-router";
import { PageError } from "@/components/page-error";
import { blueprintQueryOptions } from "@/features/catalog";

export const Route = createFileRoute("/_authenticated/catalog/$blueprint")({
  loader: async ({ context, params }) => {
    const blueprint = await context.queryClient.ensureQueryData(
      blueprintQueryOptions(params.blueprint),
    );
    return { breadcrumb: blueprint.title };
  },
  component: Outlet,
  errorComponent: PageError,
});
