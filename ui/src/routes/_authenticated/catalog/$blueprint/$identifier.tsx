import { createFileRoute, Outlet } from "@tanstack/react-router";
import { PageError } from "@/components/page-error";
import { entityQueryOptions } from "@/features/catalog";

export const Route = createFileRoute("/_authenticated/catalog/$blueprint/$identifier")({
  loader: async ({ context, params }) => {
    const entity = await context.queryClient.ensureQueryData(
      entityQueryOptions(params.blueprint, params.identifier),
    );
    return { breadcrumb: entity.title };
  },
  component: Outlet,
  errorComponent: PageError,
});
