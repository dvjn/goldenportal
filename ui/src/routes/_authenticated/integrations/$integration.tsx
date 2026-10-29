import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/integrations/$integration")({
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/settings/catalog-setup",
      search: { tab: "integrations", integration: params.integration },
      replace: true,
    });
  },
});
