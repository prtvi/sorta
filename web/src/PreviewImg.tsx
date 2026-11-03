import { useEffect, useState } from 'react'

type Props = {
  src: string
  alt: string
  className?: string
  loading?: 'lazy' | 'eager'
  draggable?: boolean
  onLoad?: (e: React.SyntheticEvent<HTMLImageElement>) => void
  /** Extra retries after the first failure (default 2). */
  maxRetries?: number
}

/**
 * Image that re-requests the preview URL on error so failed warmup converts
 * are retried when the tile/viewer actually loads in Library.
 */
export default function PreviewImg({
  src,
  alt,
  className,
  loading = 'lazy',
  draggable = false,
  onLoad,
  maxRetries = 2,
}: Props) {
  const [displaySrc, setDisplaySrc] = useState(src)
  const [tries, setTries] = useState(0)

  useEffect(() => {
    setDisplaySrc(src)
    setTries(0)
  }, [src])

  return (
    <img
      src={displaySrc}
      alt={alt}
      className={className}
      loading={loading}
      draggable={draggable}
      onLoad={onLoad}
      onError={() => {
        if (tries >= maxRetries) return
        const next = tries + 1
        setTries(next)
        const sep = src.includes('?') ? '&' : '?'
        setDisplaySrc(`${src}${sep}retry=${Date.now()}-${next}`)
      }}
    />
  )
}
