export function signingLink(token: string): string {
  return `${window.location.origin}/sign/${encodeURIComponent(token)}`;
}
