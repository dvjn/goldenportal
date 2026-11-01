import { useEffect, useRef } from "react";

// isMac decides which modifier shortcuts use: ⌘ on Apple devices, Ctrl
// everywhere else, as each platform's own apps do.
const isMac =
  typeof navigator !== "undefined" &&
  /mac|iphone|ipad/i.test(
    (navigator as Navigator & { userAgentData?: { platform: string } }).userAgentData?.platform ??
      navigator.platform,
  );

// modKey is how the shortcut modifier is written on this platform.
export const modKey = isMac ? "⌘" : "Ctrl";

// useShortcut runs onPress when the platform's modifier and key are pressed
// together, anywhere on the page, instead of the browser's own use of them.
// Other modifiers held, a key held down repeating, typing through an input
// method, and presses something else already handled are left alone.
export function useShortcut(key: string, onPress: () => void) {
  const handler = useRef(onPress);
  handler.current = onPress;

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      const mod = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
      if (!mod || event.altKey || event.shiftKey || event.repeat || event.isComposing) return;
      if (event.defaultPrevented || event.key.toLowerCase() !== key) return;
      event.preventDefault();
      handler.current();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [key]);
}
