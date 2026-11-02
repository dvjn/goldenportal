import { afterEach, describe, expect, it, vi } from "vite-plus/test";

const toast = vi.fn();
vi.mock("sonner", () => ({ toast }));

// load imports the module afresh as a tab that loaded the given build.
async function load(build?: string) {
  vi.resetModules();
  document.head.innerHTML = build ? `<meta name="gp-build" content="${build}">` : "";
  return import("./new-build");
}

function respond(build?: string) {
  const response = new Response(null, { headers: build ? { "X-GP-Build": build } : {} });
  return { response } as Parameters<Awaited<ReturnType<typeof load>>["noticeNewBuild"]>[0];
}

afterEach(() => {
  toast.mockClear();
  sessionStorage.clear();
});

describe("new builds", () => {
  it("offers to reload when the server serves another build", async () => {
    const { noticeNewBuild } = await load("a1");
    await noticeNewBuild(respond("a1"));
    expect(toast).not.toHaveBeenCalled();
    await noticeNewBuild(respond("b2"));
    expect(toast).toHaveBeenCalledWith(
      "New version available",
      expect.objectContaining({
        id: "new-build",
        action: expect.objectContaining({ label: "Reload" }),
      }),
    );
  });

  it("doesn't check without a build of its own, as in dev", async () => {
    const { noticeNewBuild } = await load();
    await noticeNewBuild(respond("b2"));
    expect(toast).not.toHaveBeenCalled();
  });

  it("reloads for a stale chunk once in a while at most", async () => {
    const reload = vi.fn();
    vi.stubGlobal("location", { reload });
    const { reloadOnStaleChunks } = await load("a1");
    reloadOnStaleChunks();
    const fail = () => {
      const event = new Event("vite:preloadError", { cancelable: true });
      window.dispatchEvent(event);
      return event.defaultPrevented;
    };
    expect(fail()).toBe(true);
    expect(fail()).toBe(false);
    expect(reload).toHaveBeenCalledTimes(1);
    vi.unstubAllGlobals();
  });
});
