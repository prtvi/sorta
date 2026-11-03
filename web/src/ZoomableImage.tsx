import { useCallback, useEffect, useRef, useState } from 'react'

const MIN_SCALE = 1
const MAX_SCALE = 8
const DOUBLE_ZOOM = 2.5

type Props = {
  src: string
  alt: string
  className?: string
  imgClassName?: string
  /** Change this to reset zoom (defaults to src). */
  resetKey?: string
}

type Point = { x: number; y: number }

function clamp(n: number, min: number, max: number) {
  return Math.min(max, Math.max(min, n))
}

/**
 * Fit-contain image with wheel / trackpad-pinch zoom and drag-to-pan when zoomed.
 * macOS pinch arrives as wheel events with ctrlKey; Safari also sends gesture* events.
 */
export default function ZoomableImage({ src, alt, className, imgClassName, resetKey }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const scaleRef = useRef(1)
  const panRef = useRef<Point>({ x: 0, y: 0 })
  const [scale, setScale] = useState(1)
  const [pan, setPan] = useState<Point>({ x: 0, y: 0 })
  const [dragging, setDragging] = useState(false)
  const [displaySrc, setDisplaySrc] = useState(src)
  const [loadTries, setLoadTries] = useState(0)
  const dragRef = useRef<{ pointerId: number; x: number; y: number; pan: Point } | null>(null)
  const gestureStartScale = useRef(1)

  useEffect(() => {
    setDisplaySrc(src)
    setLoadTries(0)
  }, [src])

  const sync = useCallback((nextScale: number, nextPan: Point) => {
    const s = clamp(nextScale, MIN_SCALE, MAX_SCALE)
    const p = s <= MIN_SCALE + 0.001 ? { x: 0, y: 0 } : nextPan
    scaleRef.current = s
    panRef.current = p
    setScale(s)
    setPan(p)
  }, [])

  const reset = useCallback(() => {
    sync(1, { x: 0, y: 0 })
  }, [sync])

  useEffect(() => {
    reset()
  }, [src, resetKey, reset])

  const zoomAt = useCallback(
    (clientX: number, clientY: number, nextScale: number) => {
      const el = containerRef.current
      if (!el) return
      const rect = el.getBoundingClientRect()
      const ox = clientX - rect.left - rect.width / 2
      const oy = clientY - rect.top - rect.height / 2
      const prev = scaleRef.current
      const s = clamp(nextScale, MIN_SCALE, MAX_SCALE)
      if (s === prev) return
      const { x: tx, y: ty } = panRef.current
      const ratio = s / prev
      sync(s, {
        x: ox - (ox - tx) * ratio,
        y: oy - (oy - ty) * ratio,
      })
    },
    [sync],
  )

  useEffect(() => {
    const el = containerRef.current
    if (!el) return

    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      // Pinch (ctrlKey) is usually finer; plain wheel scrolls a bit coarser.
      const sensitivity = e.ctrlKey ? 0.012 : 0.0022
      const factor = Math.exp(-e.deltaY * sensitivity)
      zoomAt(e.clientX, e.clientY, scaleRef.current * factor)
    }

    const onGestureStart = (e: Event) => {
      e.preventDefault()
      gestureStartScale.current = scaleRef.current
    }

    const onGestureChange = (e: Event) => {
      e.preventDefault()
      const ge = e as Event & { scale?: number; clientX?: number; clientY?: number }
      if (typeof ge.scale !== 'number') return
      const rect = el.getBoundingClientRect()
      const cx = typeof ge.clientX === 'number' ? ge.clientX : rect.left + rect.width / 2
      const cy = typeof ge.clientY === 'number' ? ge.clientY : rect.top + rect.height / 2
      zoomAt(cx, cy, gestureStartScale.current * ge.scale)
    }

    const onGestureEnd = (e: Event) => {
      e.preventDefault()
    }

    el.addEventListener('wheel', onWheel, { passive: false })
    el.addEventListener('gesturestart', onGestureStart as EventListener, { passive: false } as AddEventListenerOptions)
    el.addEventListener('gesturechange', onGestureChange as EventListener, { passive: false } as AddEventListenerOptions)
    el.addEventListener('gestureend', onGestureEnd as EventListener, { passive: false } as AddEventListenerOptions)

    return () => {
      el.removeEventListener('wheel', onWheel)
      el.removeEventListener('gesturestart', onGestureStart as EventListener)
      el.removeEventListener('gesturechange', onGestureChange as EventListener)
      el.removeEventListener('gestureend', onGestureEnd as EventListener)
    }
  }, [zoomAt])

  const onPointerDown = (e: React.PointerEvent) => {
    if (scaleRef.current <= MIN_SCALE + 0.001) return
    if (e.button !== 0) return
    e.preventDefault()
    dragRef.current = {
      pointerId: e.pointerId,
      x: e.clientX,
      y: e.clientY,
      pan: { ...panRef.current },
    }
    setDragging(true)
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  const onPointerMove = (e: React.PointerEvent) => {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== e.pointerId) return
    sync(scaleRef.current, {
      x: drag.pan.x + (e.clientX - drag.x),
      y: drag.pan.y + (e.clientY - drag.y),
    })
  }

  const endDrag = (e: React.PointerEvent) => {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== e.pointerId) return
    dragRef.current = null
    setDragging(false)
    try {
      e.currentTarget.releasePointerCapture(e.pointerId)
    } catch {
      /* ignore */
    }
  }

  const onDoubleClick = (e: React.MouseEvent) => {
    e.preventDefault()
    if (scaleRef.current > MIN_SCALE + 0.05) {
      reset()
    } else {
      zoomAt(e.clientX, e.clientY, DOUBLE_ZOOM)
    }
  }

  const zoomed = scale > MIN_SCALE + 0.001

  return (
    <div
      ref={containerRef}
      className={`zoomable${zoomed ? ' zoomable--zoomed' : ''}${dragging ? ' zoomable--dragging' : ''}${className ? ` ${className}` : ''}`}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onDoubleClick={onDoubleClick}
    >
      <img
        src={displaySrc}
        alt={alt}
        className={imgClassName}
        draggable={false}
        style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${scale})` }}
        onError={() => {
          if (loadTries >= 2) return
          const next = loadTries + 1
          setLoadTries(next)
          const sep = src.includes('?') ? '&' : '?'
          setDisplaySrc(`${src}${sep}retry=${Date.now()}-${next}`)
        }}
      />
      {zoomed && <span className="zoomable-badge">{Math.round(scale * 100)}%</span>}
    </div>
  )
}
