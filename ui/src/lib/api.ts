import ky, { type BeforeErrorHook, HTTPError, NetworkError, TimeoutError } from "ky";
import { noticeNewBuild } from "./new-build";

export type ApiDataResponse<T> = { data: T };

// Page is one page of a list, and how many items match across every page.
export type Page<T> = { items: T[]; total: number };

// Problems are what's wrong with each field of a request the server turned
// down, by field; "" holds those that aren't about one field.
export type Problems = Record<string, string[]>;

type ApiErrorBody = { error?: { message?: string; problems?: Problems } };

function envelopeMessage(error: HTTPError) {
  const body = error.data as ApiErrorBody | string | undefined;
  return typeof body === "object" ? body.error?.message : undefined;
}

// ErrorDescription is an error as people read it: what happened, and what
// to do about it or the server's own reason.
export type ErrorDescription = { title: string; detail: string };

// describeError words any error the same way everywhere. Failures that
// never reached the server, or that it didn't explain, read plainly rather
// than as a request URL; fallback titles everything else.
export function describeError(error: unknown, fallback = "Something went wrong"): ErrorDescription {
  if (error instanceof NetworkError) {
    return { title: "Can't connect", detail: "Try again." };
  }
  if (error instanceof TimeoutError) {
    return { title: "No response", detail: "Try again." };
  }
  if (error instanceof HTTPError) {
    const message = envelopeMessage(error);
    if (error.response.status >= 500) {
      return { title: "Something went wrong", detail: message ?? "Try again." };
    }
    return { title: fallback, detail: message ?? `Error ${error.response.status}` };
  }
  return { title: fallback, detail: error instanceof Error ? error.message : String(error) };
}

// plainMessage gives every failed request a message people can read, so
// toasts and forms that show error.message say the same as error cards.
const plainMessage: BeforeErrorHook = ({ error }) => {
  if (error instanceof HTTPError && error.response.status < 500 && envelopeMessage(error)) {
    error.message = envelopeMessage(error) ?? error.message;
  } else if (
    error instanceof HTTPError ||
    error instanceof NetworkError ||
    error instanceof TimeoutError
  ) {
    const { title, detail } = describeError(error);
    error.message = `${title}. ${detail}`;
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
      beforeError: [plainMessage],
    },
  });
}

// webApi is where the server serves the web UI's API.
export const webApi = "/api/web";

export const api = createApi(webApi);
