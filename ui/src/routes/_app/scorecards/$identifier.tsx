import { createFileRoute, Link, redirect } from "@tanstack/react-router";
import { PageError } from "@/components/page-error";
import { builtUnder, scorecards, scorecardView } from "@/features/scorecards";

const text = (value: unknown) => (typeof value === "string" && value !== "" ? value : undefined);

// Scorecards had a page of their own, so links to it open where its parts
// went: its results in its blueprint's catalog, its definition and
// versions on its build page, and anything unpublished on the latter.
export const Route = createFileRoute("/_app/scorecards/$identifier")({
  validateSearch: (search) => ({ tab: text(search.tab) }),
  loaderDeps: ({ search }) => search,
  loader: async ({ context, params: { identifier }, deps: { tab } }) => {
    const doc = await context.queryClient.ensureQueryData(scorecards.queries.detail(identifier));
    const built = tab === "definition" || tab === "versions";
    if (doc.published && !built) {
      throw redirect({
        to: "/catalog/$blueprint",
        params: { blueprint: doc.published.spec.blueprint },
        search: { view: scorecardView(identifier) },
        replace: true,
      });
    }
    throw redirect({
      to: "/blueprints/$blueprint/scorecards/$identifier",
      params: { blueprint: builtUnder(doc), identifier },
      search: built ? { tab } : {},
      replace: true,
    });
  },
  errorComponent: (props) => (
    <PageError {...props} back={<Link to="/catalog">Back to Catalog</Link>} />
  ),
});
