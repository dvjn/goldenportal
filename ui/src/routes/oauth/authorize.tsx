import { useMutation, useQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import {
  Check,
  CircleAlert,
  Loader2,
  MonitorSmartphone,
  ShieldQuestion,
  TriangleAlert,
} from "lucide-react";
import { AppLogo } from "@/components/app-logo";
import { AuthPage } from "@/components/auth-page";
import { Code } from "@/components/code";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useConfig } from "@/features/app-config";
import {
  type AuthorizeRequest,
  decideConsent,
  describeConsent,
  scopeLabels,
} from "@/features/apps";
import { meQueryOptions, useMe } from "@/features/auth";
import { isUnauthorized } from "@/lib/api";
import { usePageTitle } from "@/lib/page-title";

// The authorization request comes as the client sent it; the server checks
// every parameter, so the page only passes them on.
function validateAuthorizeSearch(search: Record<string, unknown>): AuthorizeRequest {
  return Object.fromEntries(
    Object.entries(search).flatMap(([key, value]) =>
      typeof value === "string" || typeof value === "number" ? [[key, String(value)]] : [],
    ),
  );
}

export const Route = createFileRoute("/oauth/authorize")({
  validateSearch: validateAuthorizeSearch,
  // Consenting needs someone signed in, who comes back here afterwards.
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQueryOptions);
    } catch (error) {
      if (isUnauthorized(error)) {
        throw redirect({ to: "/sign-in", search: { redirect: location.href } });
      }
      throw error;
    }
  },
  component: AuthorizePage,
});

// AuthorizePage is where an app such as an MCP client asks to act as the
// signed-in user. It names the app and where it sends the answer, says
// what it would be allowed, and sends the browser back with the decision.
function AuthorizePage() {
  const request = Route.useSearch();
  const { name } = useConfig();
  const me = useMe();
  usePageTitle("Authorize app");
  const consent = useQuery({
    queryKey: ["oauth", "authorize", request],
    queryFn: () => describeConsent(request),
    retry: false,
  });
  const decide = useMutation({
    mutationFn: (approve: boolean) => decideConsent(request, approve),
    onSuccess: (to) => window.location.assign(to),
  });

  if (consent.error) {
    return (
      <AuthPage>
        <Alert variant="destructive">
          <CircleAlert />
          <AlertTitle>This app can't be authorized</AlertTitle>
          <AlertDescription>{consent.error.message}</AlertDescription>
        </Alert>
      </AuthPage>
    );
  }
  if (!consent.data) {
    return (
      <AuthPage>
        <Loader2
          className="size-5 animate-spin self-center text-muted-foreground"
          aria-label="Loading"
        />
      </AuthPage>
    );
  }

  const {
    client,
    clientHost,
    logo,
    redirectHost,
    local,
    scopes,
    withheld,
    problem,
    redirect: back,
  } = consent.data;
  return (
    <AuthPage>
      <Card>
        <CardHeader className="justify-items-center text-center">
          <AppLogo logo={logo} size="lg" />
          <CardTitle className="text-xl">Authorize {client}</CardTitle>
          <CardDescription>
            {client} wants to use {name} as {me.email}.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 text-sm">
          {problem ? (
            <Alert variant="destructive">
              <CircleAlert />
              <AlertTitle>The request can't be granted</AlertTitle>
              <AlertDescription>{problem}</AlertDescription>
            </Alert>
          ) : (
            <>
              <ul className="grid gap-2" aria-label="Allowed">
                {scopes.map((scope) => (
                  <li key={scope} className="flex items-start gap-2">
                    <Check className="mt-0.5 size-4 shrink-0 text-success" aria-hidden />
                    {scopeLabels[scope] ?? scope}
                  </li>
                ))}
              </ul>
              {withheld && withheld.length > 0 && (
                <p className="text-muted-foreground">
                  Your role can't allow:{" "}
                  {withheld.map((scope) => scopeLabels[scope] ?? scope).join("; ")}
                </p>
              )}
            </>
          )}
          <div className="grid gap-1.5">
            <span className="text-muted-foreground">Published by</span>
            <span className="flex flex-wrap items-center gap-2">
              {clientHost ? (
                <Code className="text-sm">{clientHost}</Code>
              ) : (
                <Badge variant="outline">
                  <ShieldQuestion />
                  Unverified
                </Badge>
              )}
            </span>
          </div>
          <div className="grid gap-1.5">
            <span className="text-muted-foreground">Sends you back to</span>
            <span className="flex flex-wrap items-center gap-2">
              <Code className="text-sm">{redirectHost}</Code>
              {local && (
                <Badge variant="outline">
                  <MonitorSmartphone />
                  This device
                </Badge>
              )}
            </span>
            {local && (
              <span className="flex items-start gap-1.5 text-xs text-muted-foreground">
                <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-warning" aria-hidden />
                Any app on this device can use this address. Only allow it if you just started
                connecting {client}.
              </span>
            )}
          </div>
          {decide.error && (
            <Alert variant="destructive">
              <CircleAlert />
              <AlertDescription>{decide.error.message}</AlertDescription>
            </Alert>
          )}
        </CardContent>
        <CardFooter className="grid grid-cols-2 gap-2">
          {problem ? (
            <Button className="col-span-2" asChild>
              <a href={back}>Return to App</a>
            </Button>
          ) : (
            <>
              <Button
                variant="outline"
                disabled={decide.isPending}
                onClick={() => decide.mutate(false)}
              >
                Deny
              </Button>
              <Button disabled={decide.isPending} onClick={() => decide.mutate(true)}>
                Allow
              </Button>
            </>
          )}
        </CardFooter>
      </Card>
    </AuthPage>
  );
}
