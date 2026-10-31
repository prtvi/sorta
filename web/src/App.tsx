import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  classifyPhoto,
  chooseConfigFolder,
  fetchConfig,
  fetchExif,
  fetchPhotos,
  fetchPreviewStatus,
  fetchSession,
  fetchStats,
  imageUrl,
  saveSession,
  startPreviewWarmup,
  undoLast,
  type Bucket,
  type ExifMetadata,
  type Photo,
  type PhotoAction,
  type PreviewWarmupStatus,
  type SessionStats,
  type Stats,
  type ToastTone,
} from './api'
import CompareMode from './CompareMode'
import { formatDirSize } from './format'
import LibraryView from './LibraryView'
import ThumbWarmupScreen from './ThumbWarmupScreen'
import ZoomableImage from './ZoomableImage'
import { useFullscreen } from './hooks/useFullscreen'
import { useKeyboard } from './hooks/useKeyboard'
import './App.css'

type AnimDirection = 'left' | 'right' | 'down' | 'up' | null
type AppStatus = 'loading' | 'ready' | 'error' | 'empty' | 'done'
type TopMode = 'cull' | 'library'

/** Survives Cull remounts / mode switches once view previews are warm. */
let cullViewsWarmed = false

const emptyStats: Stats = {
  total: 0,
  remaining: 0,
  root: 0,
  liked: 0,
  shortlisted: 0,
  posting: 0,
  disliked: 0,
  posted: 0,
  sharing: 0,
  root_dir_size_bytes: 0,
}

const emptySession: SessionStats = {
  reviewed: 0,
  liked: 0,
  shortlisted: 0,
  disliked: 0,
  sharing: 0,
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
  const [subMode, setSubMode] = useState<'browse' | 'compare'>('browse')
  const [photos, setPhotos] = useState<Photo[]>([])
  const [currentIndex, setCurrentIndex] = useState(0)
  const [stats, setStats] = useState<Stats>(emptyStats)
  const [sessionStats, setSessionStats] = useState<SessionStats>(emptySession)
  const [status, setStatus] = useState<AppStatus>('loading')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [anim, setAnim] = useState<AnimDirection>(null)
  const [toast, setToast] = useState<{ message: string; tone: ToastTone } | null>(null)
  const { fullscreen, escHolding, toggleFullscreen } = useFullscreen()
  const [showExif, setShowExif] = useState(false)
  const [exif, setExif] = useState<ExifMetadata | null>(null)
  const [comparePhotos, setComparePhotos] = useState<Photo[]>([])
  const [compareBucket, setCompareBucket] = useState<Bucket>('liked')
  const [libBucket, setLibBucket] = useState<Bucket>('liked')
  const [libPhoto, setLibPhoto] = useState<string | undefined>()
  const [libraryMounted, setLibraryMounted] = useState(false)
  const [libraryEpoch, setLibraryEpoch] = useState(0)
  const [needsPath, setNeedsPath] = useState(false)
  const [pathBusy, setPathBusy] = useState(false)
  const [pathError, setPathError] = useState<string | null>(null)
  const [cullWarmStatus, setCullWarmStatus] = useState<PreviewWarmupStatus | null>(null)
  const [cullWarmScanning, setCullWarmScanning] = useState(false)
  const [cullWarmGate, setCullWarmGate] = useState(false)

  const busyRef = useRef(false)
  const photosRef = useRef(photos)
  const indexRef = useRef(currentIndex)
  const shownAt = useRef(Date.now())
  const toastTimer = useRef<number | null>(null)
  const cullWarmGen = useRef(0)
  photosRef.current = photos
  indexRef.current = currentIndex

  const showToast = useCallback((message: string, tone: ToastTone = 'neutral') => {
    if (toastTimer.current != null) window.clearTimeout(toastTimer.current)
    setToast({ message, tone })
    toastTimer.current = window.setTimeout(() => {
      setToast(null)
      toastTimer.current = null
    }, 2800)
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

  const skipCullWarmup = useCallback(() => {
    cullWarmGen.current += 1
    cullViewsWarmed = true
    setCullWarmGate(false)
    setCullWarmScanning(false)
  }, [])

  const warmCullViews = useCallback(async () => {
    if (cullViewsWarmed) {
      setCullWarmGate(false)
      setCullWarmScanning(false)
      return true
    }
    const gen = ++cullWarmGen.current
    setCullWarmScanning(true)
    setCullWarmGate(true)
    setCullWarmStatus(null)
    try {
      let status = await startPreviewWarmup('root', 'view')
      if (gen !== cullWarmGen.current) return false
      setCullWarmStatus(status)
      setCullWarmScanning(false)
      if (!status.running && status.pending <= 0) {
        cullViewsWarmed = true
        setCullWarmGate(false)
        return true
      }
      while (gen === cullWarmGen.current) {
        await new Promise((r) => window.setTimeout(r, 280))
        status = await fetchPreviewStatus('root', 'view')
        if (gen !== cullWarmGen.current) return false
        setCullWarmStatus(status)
        if (!status.running && status.pending <= 0) {
          cullViewsWarmed = true
          setCullWarmGate(false)
          return true
        }
        if (!status.running && status.ready + status.failed >= status.total) {
          cullViewsWarmed = true
          setCullWarmGate(false)
          return true
        }
      }
      return false
    } catch {
      if (gen !== cullWarmGen.current) return false
      setCullWarmScanning(false)
      setCullWarmGate(false)
      return true
    }
  }, [])

  const loadCull = useCallback(async (resumePhoto?: string) => {
    const showWarm = !cullViewsWarmed
    setError(null)
    if (showWarm) {
      setStatus('loading')
      setCullWarmGate(true)
      setCullWarmScanning(true)
      setCullWarmStatus(null)
    } else if (photosRef.current.length === 0) {
      setStatus('loading')
    }
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
        setCullWarmGate(false)
        setCullWarmScanning(false)
        setStatus(data.stats.total > 0 ? 'done' : 'empty')
      } else {
        if (showWarm) {
          // Warm full-size cull previews; skip still proceeds to ready.
          await warmCullViews()
        }
        setCullWarmGate(false)
        setCullWarmScanning(false)
        setStatus('ready')
        shownAt.current = Date.now()
        if (data.photos[idx]) void persistSession('cull', data.photos[idx].name, 'root')
      }
      await refreshSessionStats()
    } catch (e) {
      setCullWarmGate(false)
      setCullWarmScanning(false)
      setStatus('error')
      setError(e instanceof Error ? e.message : 'Could not load photos.')
    }
  }, [persistSession, refreshSessionStats, warmCullViews])

  const bootApp = useCallback(async () => {
    let resumePhoto: string | undefined
    let resumeMode: TopMode = 'cull'
    let resumeBucket: Bucket = 'liked'
    try {
      const sess = await fetchSession()
      if (sess.mode === 'library') {
        resumeMode = 'library'
        if (
          sess.lastBucket === 'liked' ||
          sess.lastBucket === 'shortlisted' ||
          sess.lastBucket === 'posting' ||
          sess.lastBucket === 'sharing' ||
          sess.lastBucket === 'disliked' ||
          sess.lastBucket === 'posted' ||
          sess.lastBucket === 'root'
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
    if (resumeMode === 'library') setLibraryMounted(true)
    if (resumeMode === 'cull') await loadCull(resumePhoto)
    else setStatus('ready')
  }, [loadCull])

  useEffect(() => {
    void (async () => {
      try {
        const cfg = await fetchConfig()
        if (!cfg.configured) {
          setNeedsPath(true)
          return
        }
      } catch {
        /* treat as already configured — fall through to normal boot */
      }
      await bootApp()
    })()
  }, [bootApp])

  const chooseFolder = useCallback(async () => {
    if (pathBusy) return
    setPathBusy(true)
    setPathError(null)
    try {
      const cfg = await chooseConfigFolder()
      if (cfg.cancelled || !cfg.configured) {
        return
      }
      setNeedsPath(false)
      await bootApp()
    } catch (e) {
      setPathError(e instanceof Error ? e.message : 'Could not open that folder')
    } finally {
      setPathBusy(false)
    }
  }, [pathBusy, bootApp])

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
        action === 'liked'
          ? 'right'
          : action === 'disliked'
            ? 'left'
            : action === 'shortlisted'
              ? 'up'
              : 'down'
      setAnim(direction)
      const decisionMs = Date.now() - shownAt.current

      try {
        const res = await classifyPhoto(photo.name, action, decisionMs)
        await new Promise((r) => setTimeout(r, 220))
        setAnim(null)
        setStats(res.stats)
        setLibraryEpoch((n) => n + 1)
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
        showToast(msg, 'error')
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
        showToast('Nothing to undo', 'neutral')
        return
      }

      if (res.stats) setStats(res.stats)
      setLibraryEpoch((n) => n + 1)

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
        showToast(res.count && res.count > 1 ? `Undid ${res.count}` : 'Undone', 'neutral')
      }
      await refreshSessionStats()
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Undo failed', 'error')
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }, [topMode, showToast, refreshSessionStats])

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
      } else if (e.key === 't' || e.key === 'T') {
        e.preventDefault()
        void runAction('shortlisted')
      } else if (e.key === 'd' || e.key === 'D' || e.key === 'x' || e.key === 'X') {
        e.preventDefault()
        void runAction('disliked')
      } else if (e.key === 's' || e.key === 'S') {
        e.preventDefault()
        void runAction('sharing')
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

  const switchToLibrary = (bucket?: Bucket) => {
    if (bucket) {
      setLibBucket(bucket)
      setLibPhoto('')
    }
    setLibraryMounted(true)
    setTopMode('library')
    setSubMode('browse')
    setStatus('ready')
  }

  const cullSections: { bucket: Bucket; label: string; count: number }[] = [
    { bucket: 'liked', label: 'Liked', count: stats.liked },
    { bucket: 'shortlisted', label: 'Shortlisted', count: stats.shortlisted },
    { bucket: 'posting', label: 'Posting', count: stats.posting },
    { bucket: 'sharing', label: 'Sharing', count: stats.sharing },
    { bucket: 'disliked', label: 'Disliked', count: stats.disliked },
  ]

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
            onClick={() => switchToLibrary()}
          >
            Library
          </button>
        </nav>
        {topMode !== 'library' && (
          <div className="session-stats" title="Session statistics">
            <span>S:{sessionStats.reviewed}</span>
            <span>L:{sessionStats.liked}</span>
            <span>T:{sessionStats.shortlisted}</span>
            <span>D:{sessionStats.disliked}</span>
            <span>Sh:{sessionStats.sharing}</span>
            {sessionStats.avg_decision_seconds > 0 && (
              <span>~{sessionStats.avg_decision_seconds.toFixed(1)}s</span>
            )}
          </div>
        )}
        {topMode === 'cull' && (status === 'ready' || status === 'done') && (
          <div className="progress" title="Unclassified (root) folder size · progress">
            {formatDirSize(stats.root_dir_size_bytes ?? 0)}
            <span className="progress-sep">·</span>
            {processed} / {stats.total}
          </div>
        )}
        <button
          type="button"
          className="icon-btn"
          onClick={toggleFullscreen}
          title={fullscreen ? 'Hold Esc to exit fullscreen' : 'Fullscreen (F)'}
        >
          {fullscreen ? '⤢' : '⛶'}
        </button>
      </header>
      {fullscreen && (
        <div className={`fs-esc-hint${escHolding ? ' fs-esc-hint--holding' : ''}`} aria-live="polite">
          {escHolding ? 'Keep holding Esc…' : 'Hold Esc to exit fullscreen'}
        </div>
      )}

      {libraryMounted && (
        <div
          className="view-pane"
          key="library-browse"
          hidden={!(topMode === 'library' && subMode === 'browse')}
        >
          <LibraryView
            active={topMode === 'library' && subMode === 'browse'}
            libraryEpoch={libraryEpoch}
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
            onOpenCompare={(ps, b) => {
              setComparePhotos(ps)
              setCompareBucket(b)
              setSubMode('compare')
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
          <main className={`stage${cullWarmGate ? ' stage--warming' : ''}`}>
            {cullWarmGate && (
              <ThumbWarmupScreen
                mode="cull"
                status={cullWarmStatus}
                scanning={cullWarmScanning || !cullWarmStatus}
                onSkip={
                  cullWarmStatus && cullWarmStatus.pending > 0 ? skipCullWarmup : undefined
                }
              />
            )}
            {status === 'loading' && !cullWarmGate && <p className="state">Loading photos…</p>}
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
                <p className="muted done-subtitle">
                  {cullSections.reduce((n, s) => n + s.count, 0).toLocaleString()} photos across all sections
                </p>
                <ul className="done-stats">
                  {cullSections.map((s) => (
                    <li key={s.bucket}>
                      <button
                        type="button"
                        className={`done-stat done-stat--${s.bucket}`}
                        onClick={() => switchToLibrary(s.bucket)}
                        title={`Open ${s.label} in Library`}
                      >
                        <span className="done-stat-label">{s.label}</span>
                        <span className="done-stat-count">{s.count}</span>
                      </button>
                    </li>
                  ))}
                </ul>
                <button type="button" className="btn btn--neutral" onClick={() => switchToLibrary()}>
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
                  <ZoomableImage
                    key={current.name}
                    src={imageUrl(current.url, 'view')}
                    alt={current.name}
                    imgClassName="viewer-img"
                    resetKey={current.name}
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
                  <span className="cull-nav-hint"> · ← → browse · L T D S decide</span>
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
                    <button type="button" className="btn btn--sharing" disabled={busy} onClick={() => void runAction('sharing')}>
                      Sharing <kbd>S</kbd>
                    </button>
                    <button
                      type="button"
                      className="btn btn--shortlisted"
                      disabled={busy}
                      onClick={() => void runAction('shortlisted')}
                    >
                      Shortlist <kbd>T</kbd>
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
                  <span title="Liked">L:{stats.liked}</span>
                  <span title="Shortlisted">Sl:{stats.shortlisted}</span>
                  <span title="Posting">P:{stats.posting}</span>
                  <span title="Sharing">Sh:{stats.sharing}</span>
                  <span title="Disliked">D:{stats.disliked}</span>
                  <span title="Unclassified">Left:{stats.remaining}</span>
                </div>
              </div>
            </footer>
          )}
        </div>
      )}

      {toast && (
        <div className={`toast toast--${toast.tone}`} role="status">
          {toast.message}
        </div>
      )}

      {needsPath &&
        createPortal(
          <div className="confirm-modal" role="dialog" aria-modal="true" aria-labelledby="path-dialog-title">
            <div className="confirm-card export-card" onClick={(e) => e.stopPropagation()}>
              <h3 id="path-dialog-title" className="export-title">
                Choose photo folder
              </h3>
              <p className="muted">
                Pick the folder you want to cull. Sorta will create liked/, shortlisted/, posting/, sharing/,
                disliked/, and posted/ inside it.
              </p>
              {pathError ? <p className="path-dialog-error">{pathError}</p> : null}
              <div className="actions">
                <button
                  type="button"
                  className="btn btn--like"
                  disabled={pathBusy}
                  autoFocus
                  onClick={() => void chooseFolder()}
                >
                  {pathBusy ? 'Waiting for folder…' : 'Choose folder…'}
                </button>
              </div>
            </div>
          </div>,
          document.body,
        )}
    </div>
  )
}
