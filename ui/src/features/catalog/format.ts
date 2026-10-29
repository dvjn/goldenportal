export function isSafeUrl(value: string): boolean {
  try {
    const { protocol } = new URL(value);
    return protocol === "http:" || protocol === "https:";
  } catch {
    return false;
  }
}

export function displayUrl(value: string): string {
  try {
    const url = new URL(value);
    return `${url.host}${url.pathname}`.replace(/\/$/, "");
  } catch {
    return value;
  }
}

export function hostLabel(value: string): string {
  return new URL(value).hostname.replace(/^www\./, "");
}

export function splitTitle(title: string): { namespace: string; name: string } {
  const cut = title.lastIndexOf("/") + 1;
  return { namespace: title.slice(0, cut), name: title.slice(cut) };
}

export function leafTitle(title: string): string {
  return splitTitle(title).name || title;
}

export function entityCountLabel(count: number): string {
  return count === 1 ? "entity" : "entities";
}
