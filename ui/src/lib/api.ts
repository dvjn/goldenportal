import ky, { type BeforeErrorHook, HTTPError } from "ky";

export type ApiDataResponse<T> = { data: T };

type ApiErrorBody = { error?: { message?: string } };

const adoptEnvelopeMessage: BeforeErrorHook = ({ error }) => {
  if (error instanceof HTTPError) {
    const body = error.data as ApiErrorBody | string | undefined;
    const message = typeof body === "object" ? body.error?.message : undefined;
    if (message) error.message = message;
  }
  return error;
};

export function createApi(prefix: string) {
  return ky.create({
    prefix,
    credentials: "include",
    timeout: 30000,
    headers: {
      Accept: "application/json",
    },
    hooks: {
      beforeError: [adoptEnvelopeMessage],
    },
  });
}

export const api = createApi("/api/v1");
