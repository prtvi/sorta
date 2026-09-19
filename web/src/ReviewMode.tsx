import { useCallback, useEffect, useRef, useState } from 'react'
import { movePhoto, undoLast, imageUrl, type Photo, type Stats } from './api'
import { useKeyboard } from './hooks/useKeyboard'

type Props = {
  photos: Photo[]
  onExit: () => void
  onStats: (stats: Stats) => void
  onToast: (msg: string) => void
  fullscreen: boolean
  onToggleFullscreen: () => void
}

export default function ReviewMode({
  photos: initial,
  onExit,
  onStats,
  onToast,
  fullscreen,
  onToggleFullscreen,
}: Props) {
  const [photos, setPhotos] = useState(initial)
  const [index, setIndex] = useState(0)
  const [busy, setBusy] = useState(false)
  const shownAt = useRef(Date.now())
  const current = photos[index]

  useEffect(() => {
    shownAt.current = Date.now()
  }, [current?.name])

  const act = useCallback(
    async (to: 'liked' | 'disliked' | 'root') => {
      if (!current || busy) return
      setBusy(true)
      try {
        const res = await movePhoto(current.name, 'review', to, Date.now() - shownAt.current)
        onStats(res.stats)
        setPhotos((prev) => {
          const next = prev.filter((p) => p.name !== current.name)
          setIndex((i) => Math.min(i, Math.max(0, next.length - 1)))
          if (next.length === 0) {
            onToast("You're all caught up.")
            onExit()
          }
          return next
        })
      } catch (e) {
        onToast(e instanceof Error ? e.message : 'Could not move photo')
      } finally {
        setBusy(false)
      }
    },
    [current, busy, onStats, onToast, onExit],
  )

  const runUndo = useCallback(async () => {
    try {
      const res = await undoLast()
      if (!res.success) {
        onToast('Nothing to undo')
        return
      }
      onToast('Undone — refresh Library if needed')
    } catch (e) {
      onToast(e instanceof Error ? e.message : 'Undo failed')
    }
  }, [onToast])

  useKeyboard(
    (e) => {
      if (e.key === 'Escape') {
        onExit()
        return
      }
      if (e.key === 'f' || e.key === 'F') {
        e.preventDefault()
        onToggleFullscreen()
        return
      }
      if (e.key === 'Backspace' || ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'z')) {
        e.preventDefault()
        void runUndo()
        return
      }
      if (e.key === 'l' || e.key === 'L' || e.key === 'ArrowRight') {
        e.preventDefault()
        void act('liked')
      } else if (e.key === 'd' || e.key === 'D' || e.key === 'ArrowLeft' || e.key === 'x' || e.key === 'X') {
        e.preventDefault()
        void act('disliked')
      } else if (e.key === 'u' || e.key === 'U') {
        e.preventDefault()
        void act('root')
      }
    },
    [act, runUndo, onExit, onToggleFullscreen],
  )

  if (!current) {
    return (
      <div className="state">
        <p>No photos need review.</p>
        <p className="muted">You&apos;re all caught up.</p>
        <button type="button" className="btn btn--neutral" onClick={onExit}>
          Back to Library
        </button>
      </div>
    )
  }

  return (
    <div className={`review-mode${fullscreen ? ' review-mode--fs' : ''}`}>
      <header className="review-header">
        <span>
          Review — {index + 1} / {photos.length}
        </span>
        <button type="button" className="btn btn--neutral btn--small" onClick={onExit}>
          Exit Review
        </button>
      </header>
      <div className="viewer">
        <img key={current.name} src={imageUrl(current.url, 'view')} alt={current.name} />
      </div>
      <p className="filename">{current.name}</p>
      <div className="actions">
        <button type="button" className="btn btn--dislike" disabled={busy} onClick={() => void act('disliked')}>
          Reject <kbd>D</kbd>
        </button>
        <button type="button" className="btn btn--like" disabled={busy} onClick={() => void act('liked')}>
          Keep <kbd>L</kbd>
        </button>
        <button type="button" className="btn btn--neutral" disabled={busy} onClick={() => void act('root')}>
          Unclassify <kbd>U</kbd>
        </button>
        <button type="button" className="btn btn--neutral" disabled={busy} onClick={() => void runUndo()}>
          Undo
        </button>
      </div>
    </div>
  )
}
