import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { invalidateResults } from "@/features/refresh";
import { summaryQueryOptions } from "./api";

// useSummary reads a scorecard's summary, which polls while the results
// catch up with a publish, and refreshes the results shown beside it once
// they have.
export function useSummary(
  identifier: string,
  options?: Partial<Pick<ReturnType<typeof summaryQueryOptions>, "enabled">>,
) {
  const queryClient = useQueryClient();
  const summary = useQuery({ ...summaryQueryOptions(identifier), ...options });
  const evaluating = summary.data?.evaluating ?? false;
  const wasEvaluating = useRef(evaluating);
  useEffect(() => {
    if (wasEvaluating.current && !evaluating) {
      void invalidateResults(queryClient, identifier);
    }
    wasEvaluating.current = evaluating;
  }, [evaluating, identifier, queryClient]);
  return summary;
}
