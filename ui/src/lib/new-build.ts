import type { AfterResponseHook } from "ky";
import { toast } from "sonner";

// build is the UI build this tab loaded, from index.html. The dev server
// and tests have none, so they never check.
const build = document.querySelector<HTMLMetaElement>('meta[name="gp-build"]')?.content;

// noticeNewBuild offers to reload when the server says it serves another
// build than this tab runs, after a deploy, since the web API changes with
// the UI.
export const noticeNewBuild: AfterResponseHook = ({ response }) => {
  const served = response.headers.get("X-GP-Build");
  if (!build || !served || served === build) return;
  toast("New version available", {
    id: "new-build",
    duration: Infinity,
    action: { label: "Reload", onClick: () => location.reload() },
  });
};

const reloadedKey = "gp-chunk-reload";
const reloadCooldown = 30_000;

// reloadOnStaleChunks reloads a tab opened before a deploy when a page it
// opens next asks for a chunk the new build replaced. It reloads once in a
// while at most, so a chunk that's really missing shows its error rather
// than a reload loop, and not at all when it can't keep count.
export function reloadOnStaleChunks() {
  window.addEventListener("vite:preloadError", (event) => {
    try {
      if (Date.now() - Number(sessionStorage.getItem(reloadedKey)) < reloadCooldown) return;
      sessionStorage.setItem(reloadedKey, String(Date.now()));
    } catch {
      return;
    }
    event.preventDefault();
    location.reload();
  });
}
