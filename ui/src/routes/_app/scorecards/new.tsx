import { createFileRoute, redirect } from "@tanstack/react-router";

// A scorecard is written from the blueprint it grades.
export const Route = createFileRoute("/_app/scorecards/new")({
  beforeLoad: () => {
    throw redirect({ to: "/blueprints", replace: true });
  },
});
