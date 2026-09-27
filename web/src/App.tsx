import { useCallback, useEffect, useRef, useState } from 'react'
import {
  classifyPhoto,
  fetchExif,
  fetchPhotos,
  fetchSession,
  fetchStats,
  imageUrl,
  saveSession,
  undoLast,
  type Bucket,
  type ExifMetadata,
  type Photo,
  type PhotoAction,
  type SessionStats,
  type Stats,
} from './api'
import CompareMode from './CompareMode'
import LibraryView from './LibraryView'
import ReviewMode from './ReviewMode'
import { useKeyboard } from './hooks/useKeyboard'
import './App.css'

type AnimDirection = 'left' | 'right' | 'down' | null
type AppStatus = 'loading' | 'ready' | 'error' | 'empty' | 'done'
type TopMode = 'cull' | 'library'

const emptyStats: Stats = {
  total: 0,
  remaining: 0,
  root: 0,
  liked: 0,
  disliked: 0,
  review: 0,
  deleted: 0,
}

const emptySession: SessionStats = {
  reviewed: 0,
  liked: 0,
  disliked: 0,
  review: 0,
  avg_decision_seconds: 0,
}

function preloadImage(url: string) {
  const img = new Image()
  img.src = imageUrl(url, 'view')
}

function exifLines(meta: ExifMetadata | null): string[] {
  if (!meta) return []
  return [meta.camera, meta.lens, meta.focal_length, meta.aperture, meta.shutter, meta.iso, meta.taken_at].filter(
    (v): v is string => Boolean(v && v.trim()),
  )
}

function clampIndex(i: number, len: number) {
  if (len <= 0) return 0
  return Math.max(0, Math.min(i, len - 1))
}

export default function App() {
  const [topMode, setTopMode] = useState<TopMode>('cull')
  const [subMode, setSubMode] = useState<'browse' | 'review' | 'compare'>('browse')
  const [photos, setPhotos] = useState<Photo[]>([])
  const [currentIndex, setCurrentIndex] = useState(0)
  const [stats, setStats] = useState<Stats>(emptyStats)
  const [sessionStats, setSessionStats] = useState<SessionStats>(emptySession)
  const [status, setStatus] = useState<AppStatus>('loading')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [anim, setAnim] = useState<AnimDirection>(null)
  const [toast, setToast] = useState<string | null>(null)
  const [fullscreen, setFullscreen] = useState(false)
  const [showExif, setShowExif] = useState(false)
  const [exif, setExif] = useState<ExifMetadata | null>(null)
  const [reviewQueue, setReviewQueue] = useState<Photo[]>([])
  const [comparePhotos, setComparePhotos] = useState<Photo[]>([])
  const [compareBucket, setCompareBucket] = useState<Bucket>('liked')
  const [libBucket, setLibBucket] = useState<Bucket>('liked')
  const [libPhoto, setLibPhoto] = useState<string | undefined>()

  const busyRef = useRef(false)
  const photosRef = useRef(photos)
  const indexRef = useRef(currentIndex)
  const shownAt = useRef(Date.now())
  photosRef.current = photos
  indexRef.current = currentIndex

  const showToast = useCallback((message: string) => {
    setToast(message)
    window.setTimeout(() => setToast(null), 2800)
  }, [])

  const persistSession = useCallback(async (mode: string, photo: string, bucket: string) => {
    try {
      await saveSession({ mode, lastPhoto: photo, lastBucket: bucket })
    } catch {
      /* non-fatal */
    }
  }, [])

  const refreshSessionStats = useCallback(async () => {
    try {
      const s = await fetchStats()
      setStats(s.filesystem)
      setSessionStats(s.session)
    } catch {
      /* ignore */
    }
  }, [])

  const loadCull = useCallback(async (resumePhoto?: string) => {
    setStatus('loading')
    setError(null)
    try {
      const data = await fetchPhotos()
      setPhotos(data.photos)
      setStats(data.stats)
      let idx = 0
      if (resumePhoto) {
        const i = data.photos.findIndex((p) => p.name === resumePhoto)
        if (i >= 0) idx = i
      }
      setCurrentIndex(idx)
      if (data.photos.length === 0) {
        setStatus(data.stats.total > 0 ? 'done' : 'empty')
      } else {
        setStatus('ready')
        shownAt.current = Date.now()
        if (data.photos[idx]) void persistSession('cull', data.photos[idx].name, 'root')
      }
      await refreshSessionStats()
    } catch (e) {
      setStatus('error')
      setError(e instanceof Error ? e.message : 'Could not load photos.')
    }
  }, [persistSession, refreshSessionStats])

  useEffect(() => {
    void (async () => {
      let resumePhoto: string | undefined
      let resumeMode: TopMode = 'cull'
      let resumeBucket: Bucket = 'liked'
      try {
        const sess = await fetchSession()
        if (sess.mode === 'library') {
          resumeMode = 'library'
          if (
            sess.lastBucket === 'liked' ||
            sess.lastBucket === 'review' ||
            sess.lastBucket === 'disliked' ||
            sess.lastBucket === 'deleted'
          ) {
            resumeBucket = sess.lastBucket
          }
          resumePhoto = sess.lastPhoto || undefined
        } else if (sess.lastPhoto) {
          resumePhoto = sess.lastPhoto
        }
      } catch {
        /* ignore */
      }
      setTopMode(resumeMode)
      setLibBucket(resumeMode === 'library' ? resumeBucket : 'root')
      setLibPhoto(resumePhoto)
      if (resumeMode === 'cull') await loadCull(resumePhoto)
      else setStatus('ready')
    })()
  }, [loadCull])

  const current = photos[currentIndex]

  useEffect(() => {
    if (topMode !== 'cull' || !current) return
    void persistSession('cull', current.name, 'root')
  }, [topMode, current, persistSession])

  // Reset EXIF when the cull photo changes — load only on demand.
  useEffect(() => {
    setShowExif(false)
    setExif(null)
  }, [current?.name])

  useEffect(() => {
    if (topMode !== 'cull') return
    const next = photos[currentIndex + 1]
    const prev = photos[currentIndex - 1]
    if (next) preloadImage(next.url)
    if (prev) preloadImage(prev.url)
  }, [topMode, photos, currentIndex])

  useEffect(() => {
    if (topMode !== 'cull' || !showExif || !current || status !== 'ready') {
      if (!showExif) setExif(null)
      return
    }
    let cancelled = false
    void fetchExif(current.name)
      .then((meta) => {
        if (!cancelled) setExif(meta)
      })
      .catch(() => {
        if (!cancelled) setExif(null)
      })
    return () => {
      cancelled = true
    }
  }, [topMode, current, showExif, status])

  const navigateCull = useCallback(
    (delta: number) => {
      if (topMode !== 'cull' || status !== 'ready' || busyRef.current) return
      const list = photosRef.current
      if (list.length === 0) return
      const next = clampIndex(indexRef.current + delta, list.length)
      if (next === indexRef.current) return
      setCurrentIndex(next)
      shownAt.current = Date.now()
    },
    [topMode, status],
  )

  const runAction = useCallback(
    async (action: PhotoAction) => {
      if (busyRef.current || topMode !== 'cull') return
      const list = photosRef.current
      const idx = indexRef.current
      const photo = list[idx]
      if (!photo) return

      busyRef.current = true
      setBusy(true)
      const direction: AnimDirection =
        action === 'liked' ? 'right' : action === 'disliked' ? 'left' : 'down'
      setAnim(direction)
      const decisionMs = Date.now() - shownAt.current

      try {
        const res = await classifyPhoto(photo.name, action, decisionMs)
        await new Promise((r) => setTimeout(r, 220))
        setAnim(null)
        setStats(res.stats)
        setPhotos((prev) => {
          const next = prev.filter((p) => p.name !== photo.name)
          setCurrentIndex(clampIndex(idx, next.length))
          if (next.length === 0) setStatus(res.stats.total > 0 ? 'done' : 'empty')
          return next
        })
        shownAt.current = Date.now()
        await refreshSessionStats()
      } catch (e) {
        setAnim(null)
        const msg = e instanceof Error ? e.message : 'Could not move photo.'
        showToast(msg)
      } finally {
        busyRef.current = false
        setBusy(false)
      }
    },
    [topMode, showToast, refreshSessionStats],
  )

  const runUndo = useCallback(async () => {
    if (busyRef.current) return
    busyRef.current = true
    setBusy(true)
    try {
      const res = await undoLast()
      if (!res.success) {
        showToast('Nothing to undo')
        return
      }

      if (res.stats) setStats(res.stats)

      if (topMode === 'cull') {
        // Restore into the queue immediately — avoid full rescan/EXIF (slow).
        const restored = (res.photos ?? [])
          .filter((p) => p.bucket === 'root')
          .map((p) => ({
            name: p.name,
            // Cache-bust so the browser refetches after the file returns to root.
            url: `${imageUrl(p.url, 'view')}?u=${Date.now()}`,
          }))

        if (restored.length > 0) {
          const idx = indexRef.current
          setPhotos((prev) => {
            const names = new Set(restored.map((p) => p.name))
            const without = prev.filter((p) => !names.has(p.name))
            const at = Math.min(idx, without.length)
            return [...without.slice(0, at), ...restored, ...without.slice(at)]
          })
          setCurrentIndex(idx)
          setStatus('ready')
          shownAt.current = Date.now()
          restored.forEach((p) => preloadImage(p.url))
        } else {
          await refreshSessionStats()
        }
      } else {
        showToast(res.count && res.count > 1 ? `Undid ${res.count}` : 'Undone')
      }
      await refreshSessionStats()
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Undo failed')
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }, [topMode, showToast, refreshSessionStats])

  const toggleFullscreen = useCallback(() => {
    if (!document.fullscreenElement) {
      void document.documentElement.requestFullscreen?.().then(() => setFullscreen(true))
    } else {
      void document.exitFullscreen?.().then(() => setFullscreen(false))
    }
  }, [])

  useEffect(() => {
    const onFs = () => setFullscreen(Boolean(document.fullscreenElement))
    document.addEventListener('fullscreenchange', onFs)
    return () => document.removeEventListener('fullscreenchange', onFs)
  }, [])

  useKeyboard(
    (e) => {
      if (topMode !== 'cull' || subMode !== 'browse') return

      if (e.key === 'i' || e.key === 'I') {
        if (status !== 'ready') return
        e.preventDefault()
        setShowExif((p) => !p)
        return
      }
      if (e.key === 'f' || e.key === 'F') {
        e.preventDefault()
        toggleFullscreen()
        return
      }
      if (e.key === 'Backspace' || ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'z')) {
        e.preventDefault()
        void runUndo()
        return
      }
      if (status !== 'ready') return
      if (e.key === 'ArrowRight') {
        e.preventDefault()
        navigateCull(1)
        return
      }
      if (e.key === 'ArrowLeft') {
        e.preventDefault()
        navigateCull(-1)
        return
      }
      if (e.key === 'l' || e.key === 'L') {
        e.preventDefault()
        void runAction('liked')
      } else if (e.key === 'd' || e.key === 'D' || e.key === 'x' || e.key === 'X') {
        e.preventDefault()
        void runAction('disliked')
      } else if (e.key === 'r' || e.key === 'R') {
        e.preventDefault()
        void runAction('review')
      }
    },
    [topMode, subMode, status, runAction, runUndo, toggleFullscreen, navigateCull],
  )

  const processed = stats.total - stats.remaining
  const lines = exifLines(exif)

  const switchToCull = () => {
    setTopMode('cull')
    setSubMode('browse')
    void loadCull()
  }

  const switchToLibrary = () => {
    setTopMode('library')
    setSubMode('browse')
    setStatus('ready')
  }

  return (
    <div className={`app${fullscreen ? ' app--fullscreen' : ''}`}>
      <header className="header">
        <h1 className="brand">Sorta</h1>
        <nav className="top-nav">
          <button
            type="button"
            className={`nav-btn${topMode === 'cull' ? ' nav-btn--active' : ''}`}
            onClick={switchToCull}
          >
            Cull
          </button>
          <button
            type="button"
            className={`nav-btn${topMode === 'library' ? ' nav-btn--active' : ''}`}
            onClick={switchToLibrary}
          >
            Library
          </button>
        </nav>
        <div className="session-stats" title="Session statistics">
          <span>S:{sessionStats.reviewed}</span>
          <span>L:{sessionStats.liked}</span>
          <span>D:{sessionStats.disliked}</span>
          <span>R:{sessionStats.review}</span>
          {sessionStats.avg_decision_seconds > 0 && (
            <span>~{sessionStats.avg_decision_seconds.toFixed(1)}s</span>
          )}
        </div>
        {topMode === 'cull' && (status === 'ready' || status === 'done') && (
          <div className="progress">
            {processed} / {stats.total}
          </div>
        )}
        <button type="button" className="icon-btn" onClick={toggleFullscreen} title="Fullscreen (F)">
          {fullscreen ? '⤢' : '⛶'}
        </button>
      </header>

      {topMode === 'library' && subMode === 'browse' && (
        <div className="view-pane" key="library-browse">
          <LibraryView
            initialBucket={libBucket}
            initialPhoto={libPhoto}
            fullscreen={fullscreen}
            onToggleFullscreen={toggleFullscreen}
            onToast={showToast}
            onStats={(fs, sess) => {
              setStats(fs)
              setSessionStats(sess)
            }}
            onSession={(b, photo) => {
              setLibBucket(b)
              setLibPhoto(photo)
              void persistSession('library', photo, b)
            }}
            onOpenReview={(q) => {
              setReviewQueue(q)
              setSubMode('review')
            }}
            onOpenCompare={(ps, b) => {
              setComparePhotos(ps)
              setCompareBucket(b)
              setSubMode('compare')
            }}
          />
        </div>
      )}

      {topMode === 'library' && subMode === 'review' && (
        <div className="view-pane" key="library-review">
          <ReviewMode
            photos={reviewQueue}
            fullscreen={fullscreen}
            onToggleFullscreen={toggleFullscreen}
            onExit={() => setSubMode('browse')}
            onToast={showToast}
            onStats={(s) => {
              setStats(s)
              void refreshSessionStats()
            }}
          />
        </div>
      )}

      {topMode === 'library' && subMode === 'compare' && (
        <div className="view-pane" key="library-compare">
          <CompareMode
            photos={comparePhotos}
            bucket={compareBucket}
            onExit={() => setSubMode('browse')}
            onToast={showToast}
            onMoved={() => void refreshSessionStats()}
          />
        </div>
      )}

      {topMode === 'cull' && (
        <div className="view-pane" key="cull">
          <main className="stage">
            {status === 'loading' && <p className="state">Loading photos…</p>}
            {status === 'error' && (
              <div className="state">
                <p>Could not load photos.</p>
                {error ? <p className="muted">{error}</p> : null}
                <button type="button" className="btn btn--neutral" onClick={() => void loadCull()}>
                  Retry
                </button>
              </div>
            )}
            {status === 'empty' && (
              <div className="state">
                <p>No photos found in this directory.</p>
                <p className="muted">Supported: .jpg, .jpeg, .png, .webp, .heic, .heif</p>
              </div>
            )}
            {status === 'done' && (
              <div className="state state--done">
                <p className="done-title">All photos have been classified.</p>
                <ul className="done-stats">
                  <li>Liked: {stats.liked}</li>
                  <li>Disliked: {stats.disliked}</li>
                  <li>Review: {stats.review}</li>
                </ul>
                <button type="button" className="btn btn--neutral" onClick={switchToLibrary}>
                  Open Library
                </button>
              </div>
            )}
            {status === 'ready' && current && (
              <>
                <div className={`viewer${anim ? ` viewer--fly-${anim}` : ''}`}>
                  <button
                    type="button"
                    className="cull-nav cull-nav--prev"
                    disabled={busy || currentIndex <= 0}
                    onClick={() => navigateCull(-1)}
                    title="Previous (←)"
                    aria-label="Previous photo"
                  >
                    ‹
                  </button>
                  <img
                    key={current.name}
                    className="viewer-img"
                    src={imageUrl(current.url, 'view')}
                    alt={current.name}
                    draggable={false}
                  />
                  <button
                    type="button"
                    className="cull-nav cull-nav--next"
                    disabled={busy || currentIndex >= photos.length - 1}
                    onClick={() => navigateCull(1)}
                    title="Next (→)"
                    aria-label="Next photo"
                  >
                    ›
                  </button>
                  <button
                    type="button"
                    className={`icon-btn exif-toggle${showExif ? ' icon-btn--active' : ''}`}
                    onClick={() => setShowExif((p) => !p)}
                    title="EXIF (I)"
                  >
                    EXIF
                  </button>
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
                  Unclassified — {currentIndex + 1} / {photos.length} · {current.name}
                  <span className="cull-nav-hint"> · ← → browse · L D R decide</span>
                </p>
              </>
            )}
          </main>

          {(status === 'ready' || status === 'done') && (
            <footer className="footer">
              <div className="footer-bar">
                {status === 'ready' && (
                  <div className="actions">
                    <button type="button" className="btn btn--dislike" disabled={busy} onClick={() => void runAction('disliked')}>
                      Dislike <kbd>D</kbd>/<kbd>X</kbd>
                    </button>
                    <button type="button" className="btn btn--review" disabled={busy} onClick={() => void runAction('review')}>
                      Review <kbd>R</kbd>
                    </button>
                    <button type="button" className="btn btn--like" disabled={busy} onClick={() => void runAction('liked')}>
                      Like <kbd>L</kbd>
                    </button>
                    <button type="button" className="btn btn--neutral" disabled={busy} onClick={() => void runUndo()}>
                      Undo <kbd>⌫</kbd>
                    </button>
                  </div>
                )}
                {status === 'done' && (
                  <div className="actions">
                    <button type="button" className="btn btn--neutral" disabled={busy} onClick={() => void runUndo()}>
                      Undo <kbd>⌫</kbd>
                    </button>
                  </div>
                )}
                <div className="counts">
                  <span>D:{stats.disliked}</span>
                  <span>R:{stats.review}</span>
                  <span>L:{stats.liked}</span>
                  <span>Left:{stats.remaining}</span>
                </div>
              </div>
            </footer>
          )}
        </div>
      )}

      {toast && (
        <div className="toast" role="status">
          {toast}
        </div>
      )}
    </div>
  )
}
