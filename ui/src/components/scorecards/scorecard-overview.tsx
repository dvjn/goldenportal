import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Loader2, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { BlueprintLabel } from "@/components/catalog/blueprint-label";
import { EmptyValue } from "@/components/catalog/property-value";
import { Code } from "@/components/code";
import { Detail } from "@/components/detail-list";
import { AboutCard, CriteriaCard } from "@/components/scorecards/scorecard-cards";
import { Button } from "@/components/ui/button";
import { blueprints } from "@/features/blueprints";
import {
  evaluateScorecard,
  type ScorecardSpec,
  scorecards,
  summaryQueryOptions,
} from "@/features/scorecards";

// ScorecardOverview shows a scorecard's definition to those who build it:
// what it grades and through which property, and its criteria. How
// entities fare is the catalog's to show.
export function ScorecardOverview({
  spec,
  published,
}: Readonly<{ spec: ScorecardSpec; published: boolean }>) {
  const summary = useQuery({ ...summaryQueryOptions(spec.identifier), enabled: published });
  const { data: blueprint } = useQuery(blueprints.queries.detail(spec.blueprint));
  const property = blueprint?.published?.spec.properties.find(
    (p) => p.scorecard === spec.identifier,
  );

  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
      <CriteriaCard spec={spec} />
      <AboutCard
        spec={spec}
        summary={summary.data}
        action={published && <EvaluateButton identifier={spec.identifier} />}
      >
        <Detail label="Scores">
          <Link
            to="/blueprints/$identifier"
            params={{ identifier: spec.blueprint }}
            className="min-w-0 hover:underline"
          >
            <BlueprintLabel blueprint={spec.blueprint} />
          </Link>
        </Detail>
        {blueprint && (
          <Detail label="Property">
            {property ? (
              <Code>{property.identifier}</Code>
            ) : (
              <EmptyValue label="None, so the entities table has no tier column" />
            )}
          </Detail>
        )}
      </AboutCard>
    </div>
  );
}

function EvaluateButton({ identifier }: Readonly<{ identifier: string }>) {
  const queryClient = useQueryClient();
  const evaluate = useMutation({
    mutationFn: () => evaluateScorecard(identifier),
    onSuccess: async ({ entities }) => {
      // Results and the summary both change, so refresh everything about the scorecard.
      await queryClient.invalidateQueries({ queryKey: scorecards.keys.detail(identifier) });
      toast.success(`Evaluated ${entities} ${entities === 1 ? "entity" : "entities"}`);
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <Button
      variant="outline"
      size="sm"
      onClick={() => evaluate.mutate()}
      disabled={evaluate.isPending}
    >
      {evaluate.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
      Evaluate Now
    </Button>
  );
}
