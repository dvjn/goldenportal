import { createFileRoute, redirect } from "@tanstack/react-router";

// Scorecards show in the catalog, each a tab of the blueprint it grades.
export const Route = createFileRoute("/_app/scorecards/")({
  beforeLoad: () => {
    throw redirect({ to: "/catalog", replace: true });
  },
});
