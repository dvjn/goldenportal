import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/definitions/history/$revision")({
  validateSearch: (search: Record<string, unknown>): { compare?: number } => ({
    compare: search.compare === undefined ? undefined : Number(search.compare),
  }),
  beforeLoad: ({ params, search }) => {
    throw redirect({
      to: "/settings/catalog-setup/history/$revision",
      params: { revision: Number(params.revision) },
      search: { compare: search.compare },
      replace: true,
    });
  },
});
