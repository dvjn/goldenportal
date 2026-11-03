import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { type ApiDataResponse, api } from "@/lib/api";

// scopeLabels say what each OAuth scope lets an app do, in the user's words.
export const scopeLabels: Record<string, string> = {
  "portal:read": "Read the catalog, scorecards, dashboards and configuration",
  "portal:run": "Run actions and workflows as you",
  "portal:author": "Change, publish and delete configuration, and run syncs",
};

// Consent is an app's request to act as the signed-in user, as the server
// checked it: who asks, where the answer goes, and what it can be allowed.
// problem, when set, is why it can't be, and redirect tells the app.
export type Consent = {
  client: string;
  // clientHost is the domain that published the app's metadata; an app
  // that registered itself has none, so its name is unverified.
  clientHost?: string;
  // logo is the app's logo from its metadata, as a PNG data URL.
  logo?: string;
  redirectUri: string;
  redirectHost: string;
  // local is a redirect to this machine, which any app on it could claim.
  local: boolean;
  scopes: string[];
  // withheld are scopes the app asked for that the user's role can't allow.
  withheld?: string[];
  problem?: string;
  redirect?: string;
};

// AuthorizeRequest is the authorization request's own parameters, passed
// through to the server unchanged.
export type AuthorizeRequest = Record<string, string>;

export async function describeConsent(request: AuthorizeRequest): Promise<Consent> {
  return (
    await api.get("oauth/authorize", { searchParams: request }).json<ApiDataResponse<Consent>>()
  ).data;
}

// decideConsent answers the app, returning where to send the browser with
// the answer.
export async function decideConsent(request: AuthorizeRequest, approve: boolean): Promise<string> {
  return (
    await api
      .post("oauth/authorize", { json: { ...request, approve } })
      .json<ApiDataResponse<{ redirect: string }>>()
  ).data.redirect;
}

// AppGrant is an app the user authorized, until they revoke it.
export type AppGrant = {
  id: string;
  client: string;
  logo?: string;
  scopes: string[];
  createdAt: string;
  lastUsedAt: string | null;
};

export const appKeys = { all: ["apps"] as const };

export async function listApps(): Promise<AppGrant[]> {
  return (await api.get("account/apps").json<ApiDataResponse<AppGrant[]>>()).data;
}

export async function revokeApp(id: string): Promise<void> {
  await api.delete(`account/apps/${encodeURIComponent(id)}`);
}

export const appsQueryOptions = queryOptions({ queryKey: appKeys.all, queryFn: listApps });

export function useApps() {
  return useSuspenseQuery(appsQueryOptions).data;
}
