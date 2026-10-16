import { useState } from 'react'
import { createPortal } from 'react-dom'
import {
  bucketAllowsMoves,
  movePhoto,
  imageUrl,
  toastToneForBucket,
  type Bucket,
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

export default function CompareMode({ photos, bucket, onExit, onToast, onMoved }: Props) {
  const [active, setActive] = useState(0)
  const [zoomed, setZoomed] = useState<Photo | null>(null)

  useKeyboard((e) => {
    if (e.key === 'Escape') {
      e.preventDefault()
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

  const canMove = bucketAllowsMoves(bucket)

  const moveActive = async (to: Bucket) => {
    const p = photos[active]
    if (!p || !canMove || !bucketAllowsMoves(to)) return
    try {
      await movePhoto(p.name, bucket, to)
      onToast(`Moved ${p.name} to ${to === 'root' ? 'Unclassified' : to}`, toastToneForBucket(to))
      onMoved()
      onExit()
    } catch (e) {
      onToast(e instanceof Error ? e.message : 'Move failed', 'error')
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
        {canMove ? (
          <>
            <button type="button" className="btn btn--like" onClick={() => void moveActive('liked')}>
              Like focused
            </button>
            <button type="button" className="btn btn--shortlisted" onClick={() => void moveActive('shortlisted')}>
              Shortlist focused
            </button>
            <button type="button" className="btn btn--posting" onClick={() => void moveActive('posting')}>
              Posting focused
            </button>
            <button type="button" className="btn btn--sharing" onClick={() => void moveActive('sharing')}>
              Sharing focused
            </button>
            <button type="button" className="btn btn--dislike" onClick={() => void moveActive('disliked')}>
              Reject focused
            </button>
            <button type="button" className="btn btn--neutral" onClick={() => void moveActive('root')}>
              Unclassified focused
            </button>
          </>
        ) : (
          <span className="muted">Posted is Finder-managed — files cannot be moved</span>
        )}
        <button type="button" className="btn btn--neutral" onClick={() => setZoomed(photos[active] ?? null)}>
          Open larger
        </button>
      </div>
      {zoomed &&
        createPortal(
          <div className="library-viewer" onClick={() => setZoomed(null)} role="dialog" aria-modal="true">
            <div className="library-viewer-inner" onClick={(e) => e.stopPropagation()}>
              <div className="library-viewer-stage">
                <ZoomableImage
                  src={imageUrl(zoomed.url, 'view')}
                  alt={zoomed.name}
                  resetKey={zoomed.name}
                />
              </div>
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
