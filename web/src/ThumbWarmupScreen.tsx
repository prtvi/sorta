import { useEffect, useState } from 'react'
import type { PreviewWarmupStatus } from './api'

const LIBRARY_QUIPS = [
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

const CULL_QUIPS = [
  'Lining up the keepers and the almosts…',
  'Sharpening your yes/no reflexes…',
  'Warming the enlarger for full-frame peeks…',
  'Coaxing big previews out of shy HEICs…',
  'Stacking the reject pile (metaphorically)…',
  'Giving every frame a fair first look…',
  'Loading the cull conveyor…',
  'One more sip—wait, that’s still the image tool…',
  'Teaching pixels to take rejection well…',
  'Preparing the stage for decisive thumbs…',
]

type Props = {
  status: PreviewWarmupStatus | null
  scanning?: boolean
  mode?: 'library' | 'cull'
  onSkip?: () => void
}

export default function ThumbWarmupScreen({ status, scanning, mode = 'library', onSkip }: Props) {
  const quips = mode === 'cull' ? CULL_QUIPS : LIBRARY_QUIPS
  const [quipIndex, setQuipIndex] = useState(0)

  useEffect(() => {
    setQuipIndex(0)
    const id = window.setInterval(() => {
      setQuipIndex((i) => (i + 1) % quips.length)
    }, 2800)
    return () => window.clearInterval(id)
  }, [quips.length, mode])

  const total = status?.total ?? 0
  const ready = status?.ready ?? 0
  const pending = status?.pending ?? 0
  const failed = status?.failed ?? 0
  const pct = total > 0 ? Math.min(100, Math.round((ready / total) * 100)) : scanning ? 0 : 100
  const title =
    mode === 'cull' ? 'Preparing thumbnails & previews' : 'Brewing your library'

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

        <h2 className="thumb-warmup-title">{title}</h2>
        <p className="thumb-warmup-quip" key={`${mode}-${quipIndex}`}>
          {scanning && (!status || total === 0) ? 'Counting frames in the folder…' : quips[quipIndex]}
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
