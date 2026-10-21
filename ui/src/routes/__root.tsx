import type { QueryClient } from "@tanstack/react-query";
import {
  createRootRouteWithContext,
  type ErrorComponentProps,
  Link,
  Outlet,
  useRouter,
} from "@tanstack/react-router";
import { useEffect } from "react";
import { Button } from "@/components/ui/button";
import { configQueryOptions, useConfig } from "@/features/config";

type RouterContext = {
  queryClient: QueryClient;
};

export const Route = createRootRouteWithContext<RouterContext>()({
  loader: ({ context }) => context.queryClient.ensureQueryData(configQueryOptions),
  component: RootLayout,
  errorComponent: RootError,
  notFoundComponent: NotFound,
});

function RootLayout() {
  const portalName = useConfig().name;

  useEffect(() => {
    document.title = portalName;
  }, [portalName]);

  return <Outlet />;
}

function RootError({ error }: ErrorComponentProps) {
  const router = useRouter();
  const message = error instanceof Error ? error.message : String(error);

  return (
    <section className="flex min-h-screen flex-col items-center justify-center gap-4 text-center">
      <h1 className="text-2xl font-semibold">Something went wrong</h1>
      <p className="text-muted-foreground">{message}</p>
      <Button variant="outline" onClick={() => router.invalidate()}>
        Try again
      </Button>
    </section>
  );
}

function NotFound() {
  return (
    <section className="flex flex-1 flex-col items-center justify-center gap-4 py-16 text-center">
      <h1 className="text-2xl font-semibold">Page not found</h1>
      <p className="text-muted-foreground">The page you are looking for does not exist.</p>
      <Button asChild variant="outline">
        <Link to="/">Go to the home page</Link>
      </Button>
    </section>
  );
}
