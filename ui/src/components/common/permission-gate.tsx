import type { ReactNode } from "react";
import { type Permission, useCan } from "@/features/auth";
import { StateCard } from "./state-card";

const noAccess = (
  <StateCard title="No access" message="Your account does not have permission to see this page." />
);

export function PermissionGate({
  permission,
  fallback = noAccess,
  children,
}: {
  permission: Permission;
  fallback?: ReactNode;
  children: ReactNode;
}) {
  const can = useCan();
  return can(permission) ? children : fallback;
}
