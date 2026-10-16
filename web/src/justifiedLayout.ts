export type JustifiedItem = {
  name: string
  aspectRatio: number
}

export type JustifiedTile = {
  name: string
  width: number
  height: number
}

/** Pack items into Flickr-style justified rows of shared height. */
export function packJustifiedRows(
  items: JustifiedItem[],
  containerWidth: number,
  targetRowHeight: number,
  gap: number,
): JustifiedTile[][] {
  if (containerWidth <= 0 || items.length === 0 || targetRowHeight <= 0) return []

  const rows: JustifiedTile[][] = []
  let row: JustifiedItem[] = []
  let aspectSum = 0

  const finalize = (fill: boolean) => {
    if (row.length === 0) return
    const gaps = gap * Math.max(0, row.length - 1)
    const available = Math.max(1, containerWidth - gaps)
    const h = fill ? available / aspectSum : targetRowHeight
    rows.push(
      row.map((item) => ({
        name: item.name,
        width: item.aspectRatio * h,
        height: h,
      })),
    )
    row = []
    aspectSum = 0
  }

  for (const raw of items) {
    const aspectRatio = raw.aspectRatio > 0.05 && Number.isFinite(raw.aspectRatio) ? raw.aspectRatio : 1
    const nextSum = aspectSum + aspectRatio
    const widthAtTarget = nextSum * targetRowHeight + gap * row.length
    if (row.length > 0 && widthAtTarget > containerWidth) {
      finalize(true)
    }
    row.push({ name: raw.name, aspectRatio })
    aspectSum += aspectRatio
  }

  // Last row: keep target height; don't stretch a short remainder across the full width.
  finalize(false)
  return rows
}
