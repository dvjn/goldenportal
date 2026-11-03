import ky, { type BeforeErrorHook, HTTPError } from "ky";
import { noticeNewBuild } from "./new-build";

export type ApiDataResponse<T> = { data: T };

// Problems are what's wrong with each field of a request the server turned
// down, by field; "" holds those that aren't about one field.
export type Problems = Record<string, string[]>;

type ApiErrorBody = { error?: { message?: string; problems?: Problems } };

const adoptEnvelopeMessage: BeforeErrorHook = ({ error }) => {
  if (error instanceof HTTPError) {
    const body = error.data as ApiErrorBody | string | undefined;
    const message = typeof body === "object" ? body.error?.message : undefined;
    if (message) error.message = message;
  }
  return error;
};

export function apiProblems(error: unknown): Problems | undefined {
  if (!(error instanceof HTTPError)) return undefined;
  const body = error.data as ApiErrorBody | string | undefined;
  return typeof body === "object" ? body.error?.problems : undefined;
}

export function isUnauthorized(error: unknown) {
  return error instanceof HTTPError && error.response.status === 401;
}

export function createApi(prefix: string) {
  return ky.create({
    prefix,
    credentials: "include",
    timeout: 30000,
    headers: {
      Accept: "application/json",
      // The server rejects state-changing requests without this header, which
      // cross-site pages can't send.
      "X-GP-CSRF": "1",
    },
    hooks: {
      afterResponse: [noticeNewBuild],
      beforeError: [adoptEnvelopeMessage],
    },
  });
}

// webApi is where the server serves the web UI's API.
export const webApi = "/api/web";

export const api = createApi(webApi);
