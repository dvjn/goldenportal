import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/settings/api-tokens")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/service-tokens", replace: true });
  },
});
