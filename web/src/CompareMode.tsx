import { useState } from 'react'
import { createPortal } from 'react-dom'
import { movePhoto, imageUrl, type Bucket, type Photo } from './api'
import { useKeyboard } from './hooks/useKeyboard'

type Props = {
  photos: Photo[]
  bucket: Bucket
  onExit: () => void
  onToast: (msg: string) => void
  onMoved: () => void
}

export default function CompareMode({ photos, bucket, onExit, onToast, onMoved }: Props) {
  const [active, setActive] = useState(0)
  const [zoomed, setZoomed] = useState<Photo | null>(null)

  useKeyboard((e) => {
    if (e.key === 'Escape') {
      if (zoomed) setZoomed(null)
      else onExit()
      return
    }
    if (e.key === 'ArrowRight') {
      e.preventDefault()
      setActive((i) => Math.min(photos.length - 1, i + 1))
    } else if (e.key === 'ArrowLeft') {
      e.preventDefault()
      setActive((i) => Math.max(0, i - 1))
    }
  }, [zoomed, photos.length, onExit])

  const moveActive = async (to: Bucket) => {
    const p = photos[active]
    if (!p) return
    try {
      await movePhoto(p.name, bucket, to)
      onToast(`Moved ${p.name} to ${to}`)
      onMoved()
      onExit()
    } catch (e) {
      onToast(e instanceof Error ? e.message : 'Move failed')
    }
  }

  return (
    <div className="compare-mode">
      <header className="compare-header">
        <h2>Compare</h2>
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
            onDoubleClick={() => setZoomed(p)}
          >
            <img src={imageUrl(p.url, 'view')} alt={p.name} />
            <span className="filename">{p.name}</span>
          </button>
        ))}
      </div>
      <div className="actions">
        <button type="button" className="btn btn--like" onClick={() => void moveActive('liked')}>
          Like focused
        </button>
        <button type="button" className="btn btn--review" onClick={() => void moveActive('review')}>
          Review focused
        </button>
        <button type="button" className="btn btn--dislike" onClick={() => void moveActive('disliked')}>
          Reject focused
        </button>
        <button type="button" className="btn btn--neutral" onClick={() => void moveActive('root')}>
          Unclassified focused
        </button>
        <button type="button" className="btn btn--neutral" onClick={() => setZoomed(photos[active] ?? null)}>
          Open larger
        </button>
      </div>
      {zoomed &&
        createPortal(
          <div className="library-viewer" onClick={() => setZoomed(null)} role="dialog" aria-modal="true">
            <div className="library-viewer-inner" onClick={(e) => e.stopPropagation()}>
              <img src={imageUrl(zoomed.url, 'view')} alt={zoomed.name} />
              <p className="filename">{zoomed.name}</p>
              <button type="button" className="btn btn--neutral" onClick={() => setZoomed(null)}>
                Close
              </button>
            </div>
          </div>,
          document.body,
        )}
    </div>
  )
}
