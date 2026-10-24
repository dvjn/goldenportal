import type * as React from "react";
import { useConfig } from "@/features/config";

// AuthPage is the login-03 block's page frame: the portal brand above a centred card.
// It sits on the page background, not bg-muted, so the card reads as raised in dark mode.
export function AuthPage({ children }: Readonly<{ children: React.ReactNode }>) {
  const { name, logoUrl } = useConfig();

  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-6 bg-background p-6 md:p-10">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div className="flex items-center gap-2 self-center font-medium">
          {logoUrl && <img src={logoUrl} alt="" className="size-6 shrink-0 object-contain" />}
          {name}
        </div>
        {children}
      </div>
    </div>
  );
}
