import { useEffect, useState } from 'react'
import type { PreviewWarmupStatus } from './api'

const QUIPS = [
  'Developing the contact sheet…',
  'Teaching thumbnails their stage marks…',
  'Negotiating with stubborn HEICs…',
  'Filing pixels into neat little boxes…',
  'Warming up the darkroom enlarger…',
  'Convincing JPEGs to arrive fashionably on time…',
  'Shuffling the deck so the pretty ones land first…',
  'One more sip—wait, that’s the image tool…',
  'Polishing corners so the grid looks smug…',
  'Asking each photo to smile for the library…',
]

type Props = {
  status: PreviewWarmupStatus | null
  scanning?: boolean
  onSkip?: () => void
}

export default function ThumbWarmupScreen({ status, scanning, onSkip }: Props) {
  const [quipIndex, setQuipIndex] = useState(0)

  useEffect(() => {
    const id = window.setInterval(() => {
      setQuipIndex((i) => (i + 1) % QUIPS.length)
    }, 2800)
    return () => window.clearInterval(id)
  }, [])

  const total = status?.total ?? 0
  const ready = status?.ready ?? 0
  const pending = status?.pending ?? 0
  const failed = status?.failed ?? 0
  const pct = total > 0 ? Math.min(100, Math.round((ready / total) * 100)) : scanning ? 0 : 100

  return (
    <div className="thumb-warmup" role="status" aria-live="polite" aria-busy="true">
      <div className="thumb-warmup-glow" aria-hidden />
      <div className="thumb-warmup-stage">
        <div className="thumb-warmup-film" aria-hidden>
          <div className="thumb-warmup-film-track">
            {Array.from({ length: 8 }).map((_, i) => (
              <div key={i} className="thumb-warmup-frame">
                <span className="thumb-warmup-sprocket" />
                <span className="thumb-warmup-sprocket thumb-warmup-sprocket--right" />
                <div className={`thumb-warmup-neg thumb-warmup-neg--${(i % 4) + 1}`} />
              </div>
            ))}
          </div>
        </div>

        <div className="thumb-warmup-iris" aria-hidden>
          <div className="thumb-warmup-iris-ring" />
          <div className="thumb-warmup-iris-core" />
        </div>

        <h2 className="thumb-warmup-title">Brewing your library</h2>
        <p className="thumb-warmup-quip" key={quipIndex}>
          {scanning && (!status || total === 0) ? 'Counting frames in the folder…' : QUIPS[quipIndex]}
        </p>

        <div className="thumb-warmup-meter">
          <div className="thumb-warmup-bar" style={{ width: `${pct}%` }} />
        </div>

        <div className="thumb-warmup-stats">
          {scanning && total === 0 ? (
            <span>Scanning…</span>
          ) : (
            <>
              <span className="thumb-warmup-pct">{pct}%</span>
              <span className="thumb-warmup-sep">·</span>
              <span>
                {ready} ready
                {pending > 0 ? ` · ${pending} pending` : ''}
                {failed > 0 ? ` · ${failed} skipped` : ''}
              </span>
              <span className="thumb-warmup-sep">·</span>
              <span>{total} photos</span>
            </>
          )}
        </div>

        {onSkip && pending > 0 && (
          <button type="button" className="thumb-warmup-skip" onClick={onSkip}>
            Browse anyway
          </button>
        )}
      </div>
    </div>
  )
}
