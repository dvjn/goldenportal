import { type ApiDataResponse, api } from "@/lib/api";

// ServerConfiguration summarises how the portal is set up. It is read-only:
// the settings come from the environment, and secrets only say whether they are set.
export type ServerConfiguration = {
  portal: { name: string; logoUrl: string; baseUrl: string };
  oidc: {
    configured: boolean;
    issuer: string;
    clientId: string;
    clientSecretSet: boolean;
    scopes: string[];
    groupsClaim: string;
    redirectUrl: string;
    bootstrapAdminEmail: string;
  };
  // scim.url is the SCIM base address to give the identity provider.
  scim: { enabled: boolean; url: string };
  sessions: { secureCookies: boolean; idleTimeoutHours: number; maxAgeHours: number };
  tokens: { prefix: string; maxDays: number };
  database: {
    dialect: string;
    host: string;
    port: string;
    name: string;
    user: string;
    passwordSet: boolean;
    sslMode: string;
  };
  server: {
    environment: string;
    version: string;
    httpPort: string;
    metricsPort: string;
    logLevel: string;
  };
};

export type ActionResult = { ok: boolean; message: string };

export const configurationKeys = {
  all: ["configuration"] as const,
};

export async function getConfiguration(): Promise<ServerConfiguration> {
  return (await api.get("settings/configuration").json<ApiDataResponse<ServerConfiguration>>())
    .data;
}

// runConfigAction runs a check, e.g. "oidc.check" tests the identity provider.
export async function runConfigAction(id: string): Promise<ActionResult> {
  return (
    await api.post(`settings/configuration/actions/${id}`).json<ApiDataResponse<ActionResult>>()
  ).data;
}
