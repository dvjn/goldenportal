import type { QueryClient } from "@tanstack/react-query";
import { createRouter, type RouterHistory } from "@tanstack/react-router";
import { PageError } from "@/components/page-error";
import { PagePending } from "@/components/page-pending";
import { routeTree } from "@/routeTree.gen";

// createAppRouter builds the router the app and its tests share. Pages that
// fail or are slow to load show in place, below the header.
export function createAppRouter(queryClient: QueryClient, history?: RouterHistory) {
  return createRouter({
    routeTree,
    history,
    context: { queryClient },
    defaultErrorComponent: PageError,
    defaultPendingComponent: PagePending,
  });
}
