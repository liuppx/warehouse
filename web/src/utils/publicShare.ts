export function buildPublicShareURL(
  origin: string,
  share: { token: string; isDir: boolean; name: string }
): string {
  const token = encodeURIComponent(share.token)
  const path = share.isDir
    ? `/share/${token}`
    : `/api/v1/public/share/${token}/${encodeURIComponent(share.name)}`
  return new URL(path, origin).toString()
}
