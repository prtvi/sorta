import { useCallback, useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  bucketAllowsMoves,
  bucketLabel,
  fetchExif,
  fetchFavorites,
  imageUrl,
  movePhoto,
  POSTED_READONLY_MSG,
  revealPhoto,
  setPhotoStarred,
  toastToneForBucket,
  type Bucket,
  type ExifMetadata,
  type Photo,
  type ToastTone,
} from './api'
import { useKeyboard } from './hooks/useKeyboard'
import ZoomableImage from './ZoomableImage'

type Props = {
  photos: Photo[]
  bucket: Bucket
  onExit: () => void
  onToast: (msg: string, tone?: ToastTone) => void
  onMoved: () => void
}

function exifLines(meta: ExifMetadata | null): string[] {
  if (!meta) return []
  return [meta.camera, meta.lens, meta.focal_length, meta.aperture, meta.shutter, meta.iso, meta.taken_at].filter(
    (v): v is string => Boolean(v && v.trim()),
  )
}

export default function CompareMode({ photos, bucket, onExit, onToast, onMoved }: Props) {
  const [active, setActive] = useState(0)
  const [zoomed, setZoomed] = useState(false)
  const [showExif, setShowExif] = useState(false)
  const [exif, setExif] = useState<ExifMetadata | null>(null)
  const [starred, setStarred] = useState<Set<string>>(() => new Set())
  const [starBusy, setStarBusy] = useState(false)

  const canMove = bucketAllowsMoves(bucket)
  const zoomedPhoto = zoomed ? photos[active] ?? null : null
  const lines = exifLines(exif)

  useEffect(() => {
    void fetchFavorites()
      .then((fav) => setStarred(new Set(fav.names ?? [])))
      .catch(() => {})
  }, [])

  useEffect(() => {
    setShowExif(false)
    setExif(null)
  }, [zoomedPhoto?.name])

  useEffect(() => {
    if (!zoomedPhoto || !showExif) {
      if (!showExif) setExif(null)
      return
    }
    let cancelled = false
    void fetchExif(zoomedPhoto.name, bucket)
      .then((meta) => {
        if (!cancelled) setExif(meta)
      })
      .catch(() => {
        if (!cancelled) setExif(null)
      })
    return () => {
      cancelled = true
    }
  }, [zoomedPhoto, showExif, bucket])

  const moveActive = useCallback(
    async (to: Bucket) => {
      const p = photos[active]
      if (!p || !canMove || !bucketAllowsMoves(to) || to === bucket) return
      try {
        await movePhoto(p.name, bucket, to)
        onToast(`Moved ${p.name} to ${bucketLabel(to)}`, toastToneForBucket(to))
        onMoved()
        onExit()
      } catch (e) {
        onToast(e instanceof Error ? e.message : 'Move failed', 'error')
      }
    },
    [photos, active, canMove, bucket, onToast, onMoved, onExit],
  )

  const toggleStar = useCallback(
    async (name: string) => {
      if (!name || starBusy) return
      const next = !starred.has(name)
      setStarBusy(true)
      setStarred((prev) => {
        const n = new Set(prev)
        if (next) n.add(name)
        else n.delete(name)
        return n
      })
      try {
        await setPhotoStarred(name, next)
      } catch (e) {
        setStarred((prev) => {
          const n = new Set(prev)
          if (next) n.delete(name)
          else n.add(name)
          return n
        })
        onToast(e instanceof Error ? e.message : 'Could not update favourite', 'error')
      } finally {
        setStarBusy(false)
      }
    },
    [starred, starBusy, onToast],
  )

  const revealInFinder = useCallback(() => {
    const p = photos[active]
    if (!p) return
    void revealPhoto(p.name, bucket)
      .then(() => onToast('Shown in Finder', 'neutral'))
      .catch((err: unknown) =>
        onToast(err instanceof Error ? err.message : 'Could not show in Finder', 'error'),
      )
  }, [photos, active, bucket, onToast])

  const navigateZoom = useCallback(
    (delta: -1 | 1) => {
      setActive((i) => {
        const next = i + delta
        if (next < 0 || next >= photos.length) return i
        return next
      })
    },
    [photos.length],
  )

  useKeyboard(
    (e) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        if (zoomed) setZoomed(false)
        else onExit()
        return
      }
      if (zoomed) {
        if (e.key === 'i' || e.key === 'I') {
          e.preventDefault()
          setShowExif((p) => !p)
          return
        }
        if (e.key === 'o' || e.key === 'O') {
          e.preventDefault()
          revealInFinder()
          return
        }
        if (e.key === '*') {
          e.preventDefault()
          const p = photos[active]
          if (p) void toggleStar(p.name)
          return
        }
        if (e.key === 'ArrowRight') {
          e.preventDefault()
          navigateZoom(1)
          return
        }
        if (e.key === 'ArrowLeft') {
          e.preventDefault()
          navigateZoom(-1)
          return
        }
        if (!canMove) return
        if (e.key === 'l' || e.key === 'L') {
          e.preventDefault()
          void moveActive('liked')
        } else if (e.key === 't' || e.key === 'T') {
          e.preventDefault()
          void moveActive('shortlisted')
        } else if (e.key === 'p' || e.key === 'P') {
          e.preventDefault()
          void moveActive('posting')
        } else if (e.key === 'd' || e.key === 'D') {
          e.preventDefault()
          void moveActive('disliked')
        } else if (e.key === 's' || e.key === 'S') {
          e.preventDefault()
          void moveActive('sharing')
        } else if (e.key === 'u' || e.key === 'U') {
          e.preventDefault()
          void moveActive('root')
        }
        return
      }
      if (e.key === 'ArrowRight') {
        e.preventDefault()
        setActive((i) => Math.min(photos.length - 1, i + 1))
        return
      }
      if (e.key === 'ArrowLeft') {
        e.preventDefault()
        setActive((i) => Math.max(0, i - 1))
        return
      }
      if (!canMove) return
      if (e.key === 'l' || e.key === 'L') {
        e.preventDefault()
        void moveActive('liked')
      } else if (e.key === 't' || e.key === 'T') {
        e.preventDefault()
        void moveActive('shortlisted')
      } else if (e.key === 'p' || e.key === 'P') {
        e.preventDefault()
        void moveActive('posting')
      } else if (e.key === 'd' || e.key === 'D') {
        e.preventDefault()
        void moveActive('disliked')
      } else if (e.key === 's' || e.key === 'S') {
        e.preventDefault()
        void moveActive('sharing')
      } else if (e.key === 'u' || e.key === 'U') {
        e.preventDefault()
        void moveActive('root')
      }
    },
    [
      zoomed,
      photos,
      active,
      onExit,
      canMove,
      moveActive,
      revealInFinder,
      toggleStar,
      navigateZoom,
    ],
  )

  const moveHint = canMove ? ` · D S L T P${bucket !== 'root' ? ' U' : ''} decide` : ''

  return (
    <div className="compare-mode">
      <header className="compare-header">
        <h2>Compare</h2>
        <span className="muted cull-nav-hint">← → focus{moveHint.replace('decide', 'move')}</span>
        <button type="button" className="btn btn--neutral btn--small" onClick={onExit}>
          Exit Compare
        </button>
      </header>
      <div className={`compare-grid compare-grid--${photos.length}`}>
        {photos.map((p, i) => (
          <button
            key={p.name}
            type="button"
            className={`compare-pane${active === i ? ' compare-pane--active' : ''}`}
            onClick={() => setActive(i)}
            onDoubleClick={() => setZoomed(true)}
          >
            <img src={imageUrl(p.url, 'view')} alt={p.name} />
            <span className="filename">{p.name}</span>
          </button>
        ))}
      </div>
      <div className="actions">
        {canMove ? (
          <>
            {bucket !== 'disliked' && (
              <button type="button" className="btn btn--dislike" onClick={() => void moveActive('disliked')}>
                Disliked <kbd>D</kbd>
              </button>
            )}
            {bucket !== 'sharing' && (
              <button type="button" className="btn btn--sharing" onClick={() => void moveActive('sharing')}>
                Sharing <kbd>S</kbd>
              </button>
            )}
            {bucket !== 'liked' && (
              <button type="button" className="btn btn--like" onClick={() => void moveActive('liked')}>
                Liked <kbd>L</kbd>
              </button>
            )}
            {bucket !== 'shortlisted' && (
              <button type="button" className="btn btn--shortlisted" onClick={() => void moveActive('shortlisted')}>
                Shortlisted <kbd>T</kbd>
              </button>
            )}
            {bucket !== 'posting' && (
              <button type="button" className="btn btn--posting" onClick={() => void moveActive('posting')}>
                Posting <kbd>P</kbd>
              </button>
            )}
            {bucket !== 'root' && (
              <button type="button" className="btn btn--neutral" onClick={() => void moveActive('root')}>
                Unclassified <kbd>U</kbd>
              </button>
            )}
          </>
        ) : (
          <span className="muted">{POSTED_READONLY_MSG}</span>
        )}
        <button type="button" className="btn btn--neutral" onClick={() => setZoomed(true)} disabled={!photos[active]}>
          Open larger
        </button>
      </div>
      {zoomedPhoto &&
        createPortal(
          <div
            className="library-viewer"
            role="dialog"
            aria-modal="true"
            onClick={() => setZoomed(false)}
          >
            <div className="library-viewer-inner" onClick={(e) => e.stopPropagation()}>
              <div className="library-viewer-stage">
                <button
                  type="button"
                  className="cull-nav cull-nav--prev"
                  disabled={active <= 0}
                  onClick={() => navigateZoom(-1)}
                  title="Previous (←)"
                  aria-label="Previous photo"
                >
                  ‹
                </button>
                <ZoomableImage
                  src={imageUrl(zoomedPhoto.url, 'view')}
                  alt={zoomedPhoto.name}
                  resetKey={zoomedPhoto.name}
                />
                <button
                  type="button"
                  className="cull-nav cull-nav--next"
                  disabled={active >= photos.length - 1}
                  onClick={() => navigateZoom(1)}
                  title="Next (→)"
                  aria-label="Next photo"
                >
                  ›
                </button>
                <button
                  type="button"
                  className="icon-btn viewer-close-toggle"
                  onClick={() => setZoomed(false)}
                  title="Close (Esc)"
                  aria-label="Close"
                >
                  ✕
                </button>
                {starred.has(zoomedPhoto.name) && (
                  <span className="viewer-star-badge" title="Starred" aria-label="Starred">
                    ★
                  </span>
                )}
                {showExif && (
                  <div className="exif-overlay">
                    {lines.length > 0 ? (
                      <ul>
                        {lines.map((line) => (
                          <li key={line}>{line}</li>
                        ))}
                      </ul>
                    ) : (
                      <p className="exif-empty">No EXIF metadata</p>
                    )}
                  </div>
                )}
              </div>
              <p className="filename">
                {bucketLabel(bucket)} — {active + 1} / {photos.length} · {zoomedPhoto.name}
                <span className="cull-nav-hint"> · ← → browse{moveHint}</span>
              </p>
              <div className="actions">
                {canMove && (
                  <>
                    {bucket !== 'disliked' && (
                      <button type="button" className="btn btn--dislike" onClick={() => void moveActive('disliked')}>
                        Disliked <kbd>D</kbd>
                      </button>
                    )}
                    {bucket !== 'sharing' && (
                      <button type="button" className="btn btn--sharing" onClick={() => void moveActive('sharing')}>
                        Sharing <kbd>S</kbd>
                      </button>
                    )}
                    {bucket !== 'liked' && (
                      <button type="button" className="btn btn--like" onClick={() => void moveActive('liked')}>
                        Liked <kbd>L</kbd>
                      </button>
                    )}
                    {bucket !== 'shortlisted' && (
                      <button type="button" className="btn btn--shortlisted" onClick={() => void moveActive('shortlisted')}>
                        Shortlisted <kbd>T</kbd>
                      </button>
                    )}
                    {bucket !== 'posting' && (
                      <button type="button" className="btn btn--posting" onClick={() => void moveActive('posting')}>
                        Posting <kbd>P</kbd>
                      </button>
                    )}
                    {bucket !== 'root' && (
                      <button type="button" className="btn btn--neutral" onClick={() => void moveActive('root')}>
                        Unclassified <kbd>U</kbd>
                      </button>
                    )}
                  </>
                )}
                <button
                  type="button"
                  className={`btn btn--neutral${starred.has(zoomedPhoto.name) ? ' btn--star-on' : ''}`}
                  disabled={starBusy}
                  onClick={() => void toggleStar(zoomedPhoto.name)}
                  title={starred.has(zoomedPhoto.name) ? 'Unstar (*)' : 'Star (*)'}
                >
                  {starred.has(zoomedPhoto.name) ? '★ Starred' : '☆ Starred'} <kbd>*</kbd>
                </button>
                <button
                  type="button"
                  className={`btn btn--neutral${showExif ? ' btn--exif-on' : ''}`}
                  onClick={() => setShowExif((p) => !p)}
                  title="EXIF (I)"
                >
                  EXIF <kbd>I</kbd>
                </button>
                <button
                  type="button"
                  className="btn btn--neutral"
                  onClick={revealInFinder}
                  title="Show in Finder (O)"
                >
                  Finder <kbd>O</kbd>
                </button>
              </div>
            </div>
          </div>,
          document.body,
        )}
    </div>
  )
}
