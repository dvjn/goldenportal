import { AppWindow } from "lucide-react";
import type * as React from "react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";

// AppLogo is an authorized app's logo, which only apps that publish their
// metadata have; the others show the app icon.
export function AppLogo({
  logo,
  ...props
}: { logo?: string } & React.ComponentProps<typeof Avatar>) {
  return (
    <Avatar {...props}>
      {logo && <AvatarImage src={logo} alt="" />}
      <AvatarFallback>
        <AppWindow className="size-1/2" aria-hidden />
      </AvatarFallback>
    </Avatar>
  );
}
