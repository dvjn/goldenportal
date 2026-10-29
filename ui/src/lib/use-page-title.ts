import { useEffect } from "react";
import { useConfig } from "@/features/config";

export function usePageTitle(title: string) {
  const portalName = useConfig().name;

  useEffect(() => {
    document.title = `${title} | ${portalName}`;
    return () => {
      document.title = portalName;
    };
  }, [title, portalName]);
}
