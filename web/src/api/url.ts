function withTrailingSlash(value: string): string {
  return value.endsWith('/') ? value : `${value}/`
}

export function absoluteApiUrl(
  path: string,
  configuredBase: string,
  browserOrigin: string,
): string {
  const trimmedBase = configuredBase.trim()
  const trimmedOrigin = browserOrigin.trim()

  const base = trimmedBase
    ? new URL(
        withTrailingSlash(trimmedBase),
        trimmedOrigin ? withTrailingSlash(trimmedOrigin) : undefined,
      )
    : new URL(withTrailingSlash(trimmedOrigin))

  return new URL(path.replace(/^\/+/, ''), base).toString()
}
