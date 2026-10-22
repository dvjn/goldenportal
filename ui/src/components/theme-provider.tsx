import { createContext, useContext, useEffect, useState, useSyncExternalStore } from "react";

type Theme = "dark" | "light" | "system";

type ResolvedTheme = Exclude<Theme, "system">;

type ThemeProviderProps = {
  children: React.ReactNode;
  defaultTheme?: Theme;
  storageKey?: string;
};

type ThemeProviderState = {
  theme: Theme;
  resolvedTheme: ResolvedTheme;
  setTheme: (theme: Theme) => void;
};

const initialState: ThemeProviderState = {
  theme: "system",
  resolvedTheme: "light",
  setTheme: () => null,
};

const ThemeProviderContext = createContext<ThemeProviderState>(initialState);

const darkScheme = "(prefers-color-scheme: dark)";

function subscribeToSystemTheme(onChange: () => void) {
  const query = window.matchMedia(darkScheme);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}

function systemTheme(): ResolvedTheme {
  return window.matchMedia(darkScheme).matches ? "dark" : "light";
}

export function ThemeProvider({
  children,
  defaultTheme = "system",
  storageKey = "goldport-ui-theme",
  ...props
}: ThemeProviderProps) {
  const [theme, setTheme] = useState<Theme>(
    () => (localStorage.getItem(storageKey) as Theme) || defaultTheme,
  );
  const system = useSyncExternalStore(subscribeToSystemTheme, systemTheme);
  const resolvedTheme = theme === "system" ? system : theme;

  useEffect(() => {
    const root = window.document.documentElement;
    root.classList.remove("light", "dark");
    root.classList.add(resolvedTheme);
  }, [resolvedTheme]);

  const value = {
    theme,
    resolvedTheme,
    setTheme: (theme: Theme) => {
      localStorage.setItem(storageKey, theme);
      setTheme(theme);
    },
  };

  return (
    <ThemeProviderContext.Provider {...props} value={value}>
      {children}
    </ThemeProviderContext.Provider>
  );
}

export const useTheme = () => {
  const context = useContext(ThemeProviderContext);

  if (context === undefined) throw new Error("useTheme must be used within a ThemeProvider");

  return context;
};
