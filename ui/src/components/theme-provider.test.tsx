import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { ThemeProvider, useTheme } from "./theme-provider";

function stubSystemTheme(dark: boolean) {
  const listeners = new Set<() => void>();
  const original = window.matchMedia;
  window.matchMedia = vi.fn((query: string) => ({
    get matches() {
      return dark;
    },
    media: query,
    onchange: null,
    addEventListener: (_: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_: string, listener: () => void) => listeners.delete(listener),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })) as unknown as typeof window.matchMedia;

  return {
    change(next: boolean) {
      dark = next;
      for (const listener of listeners) listener();
    },
    restore() {
      window.matchMedia = original;
    },
  };
}

function ResolvedTheme() {
  return <p>{useTheme().resolvedTheme}</p>;
}

let restore = () => {};

afterEach(() => restore());

describe("ThemeProvider", () => {
  it("follows an operating system theme change with the system theme", () => {
    const system = stubSystemTheme(false);
    restore = system.restore;
    render(
      <ThemeProvider>
        <ResolvedTheme />
      </ThemeProvider>,
    );
    expect(screen.getByText("light")).toBeInTheDocument();
    expect(document.documentElement).toHaveClass("light");

    act(() => system.change(true));

    expect(screen.getByText("dark")).toBeInTheDocument();
    expect(document.documentElement).toHaveClass("dark");
    expect(document.documentElement).not.toHaveClass("light");
  });

  it("keeps a chosen theme when the operating system theme changes", () => {
    const system = stubSystemTheme(false);
    restore = system.restore;
    localStorage.setItem("goldport-ui-theme", "light");
    render(
      <ThemeProvider>
        <ResolvedTheme />
      </ThemeProvider>,
    );

    act(() => system.change(true));

    expect(screen.getByText("light")).toBeInTheDocument();
    expect(document.documentElement).toHaveClass("light");
  });
});
