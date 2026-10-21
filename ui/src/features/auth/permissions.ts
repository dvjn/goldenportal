import type { QueryClient } from "@tanstack/react-query";
import type { Permission } from "./api";
import { meQueryOptions, useMe } from "./queries";

export class ForbiddenError extends Error {
  readonly permission: Permission;

  constructor(permission: Permission) {
    super(`This page needs the ${permission} permission.`);
    this.name = "ForbiddenError";
    this.permission = permission;
  }
}

export function useCan() {
  const { permissions } = useMe();
  return (permission: Permission) => permissions.includes(permission);
}

export async function requirePermission(queryClient: QueryClient, permission: Permission) {
  const me = await queryClient.ensureQueryData(meQueryOptions);
  if (!me.permissions.includes(permission)) throw new ForbiddenError(permission);
}
