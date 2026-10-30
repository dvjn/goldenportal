import { createContext, type ReactNode, useContext, useEffect, useState } from "react";

const PageTitleContext = createContext<(title: string | undefined) => void>(() => {});

// PageTitleProvider names the browser tab after the current page, then the portal.
export function PageTitleProvider({ portal, children }: { portal: string; children: ReactNode }) {
  const [title, setTitle] = useState<string>();

  useEffect(() => {
    document.title = title ? `${title} | ${portal}` : portal;
  }, [title, portal]);

  return <PageTitleContext.Provider value={setTitle}>{children}</PageTitleContext.Provider>;
}

// usePageTitle names the browser tab after the page while it's shown.
export function usePageTitle(title: string) {
  const setTitle = useContext(PageTitleContext);

  useEffect(() => {
    setTitle(title);
    return () => setTitle(undefined);
  }, [setTitle, title]);
}
