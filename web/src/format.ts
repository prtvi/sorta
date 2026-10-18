/** Format directory size as MB or GB (decimal). */
export function formatDirSize(bytes: number): string {
  const gb = 1_000_000_000
  const mb = 1_000_000
  if (bytes >= gb) {
    const v = bytes / gb
    return `${v >= 10 ? v.toFixed(0) : v.toFixed(1)} GB`
  }
  const v = bytes / mb
  if (bytes > 0 && v < 0.1) return '< 0.1 MB'
  return `${v >= 10 ? v.toFixed(0) : v.toFixed(1)} MB`
}
