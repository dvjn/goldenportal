import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/definitions/")({
  validateSearch: (search: Record<string, unknown>): { document?: string } => ({
    document: typeof search.document === "string" ? search.document : undefined,
  }),
  beforeLoad: ({ search }) => {
    throw redirect({
      to: "/settings/catalog-setup",
      search: { tab: "definitions", document: search.document },
      replace: true,
    });
  },
});
