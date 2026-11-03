import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  cancelPreviewWarmup,
  classifyPhoto,
  chooseConfigFolder,
  fetchConfig,
  fetchDirectoryPreviewStatus,
  fetchExif,
  fetchFavorites,
  fetchPhotos,
  fetchSession,
  fetchStats,
  imageUrl,
  revealPhoto,
  saveSession,
  setPhotoStarred,
  startDirectoryPreviewWarmup,
  undoLast,
  type Bucket,
  type ExifMetadata,
  type Photo,
  type PhotoAction,
  type PreviewWarmupStatus,
  type Stats,
  type ToastTone,
} from './api'
import CompareMode from './CompareMode'
import { formatDirSize } from './format'
import LibraryView from './LibraryView'
import {
  areDirectoryPreviewsReady,
  markDirectoryPreviewsReady,
  resetDirectoryPreviewsReady,
} from './previewWarm'
import ThumbWarmupScreen from './ThumbWarmupScreen'
import ZoomableImage from './ZoomableImage'
import { useFullscreen } from './hooks/useFullscreen'
import { useKeyboard } from './hooks/useKeyboard'
import './App.css'

type AnimDirection = 'left' | 'right' | 'down' | 'up' | null
type AppStatus = 'loading' | 'ready' | 'error' | 'empty' | 'done'
type TopMode = 'cull' | 'library'

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
  const [status, setStatus] = useState<AppStatus>('loading')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [anim, setAnim] = useState<AnimDirection>(null)
  const [toast, setToast] = useState<{ message: string; tone: ToastTone } | null>(null)
  const { fullscreen, escHolding, toggleFullscreen } = useFullscreen()
  const [showExif, setShowExif] = useState(false)
  const [exif, setExif] = useState<ExifMetadata | null>(null)
  const [starred, setStarred] = useState<Set<string>>(() => new Set())
  const [starBusy, setStarBusy] = useState(false)
  const [comparePhotos, setComparePhotos] = useState<Photo[]>([])
  const [compareBucket, setCompareBucket] = useState<Bucket>('liked')
  const [libBucket, setLibBucket] = useState<Bucket>('liked')
  const [libPhoto, setLibPhoto] = useState<string | undefined>()
  /** Bumps when Cull explicitly opens a library bucket so a kept-alive Library jumps. */
  const [libraryJumpToken, setLibraryJumpToken] = useState(0)
  const [libraryMounted, setLibraryMounted] = useState(false)
  const [libraryEpoch, setLibraryEpoch] = useState(0)
  const [needsPath, setNeedsPath] = useState(false)
  const [pathBusy, setPathBusy] = useState(false)
  const [pathError, setPathError] = useState<string | null>(null)
  const [dirWarmStatus, setDirWarmStatus] = useState<PreviewWarmupStatus | null>(null)
  const [dirWarmScanning, setDirWarmScanning] = useState(false)
  const [dirWarmGate, setDirWarmGate] = useState(false)

  const busyRef = useRef(false)
  const photosRef = useRef(photos)
  const indexRef = useRef(currentIndex)
  const toastTimer = useRef<number | null>(null)
  const dirWarmGen = useRef(0)
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

  const refreshStats = useCallback(async () => {
    try {
      const s = await fetchStats()
      setStats(s.filesystem)
    } catch {
      /* ignore */
    }
  }, [])

  const skipDirectoryWarmup = useCallback(() => {
    // Cancel server work and let boot continue — do NOT mark the directory
    // durably ready; next reload will resume remaining converts.
    dirWarmGen.current += 1
    setDirWarmGate(false)
    setDirWarmScanning(false)
    void cancelPreviewWarmup().catch(() => {})
  }, [])

  /** First-load only: generate thumb + view for every photo in the directory. */
  const ensureDirectoryPreviews = useCallback(async () => {
    if (areDirectoryPreviewsReady()) {
      setDirWarmGate(false)
      setDirWarmScanning(false)
      return true
    }
    const gen = ++dirWarmGen.current
    setDirWarmScanning(true)
    setDirWarmGate(false)
    setDirWarmStatus(null)
    try {
      let status = await startDirectoryPreviewWarmup()
      if (gen !== dirWarmGen.current) return true // skipped — proceed without durable ready
      setDirWarmStatus(status)
      setDirWarmScanning(false)
      if (!status.running && status.pending <= 0 && status.failed <= 0) {
        markDirectoryPreviewsReady()
        setDirWarmGate(false)
        return true
      }
      if (!status.running && status.pending <= 0 && status.failed > 0) {
        // Partial failure: browse now; next boot retries failed files.
        setDirWarmGate(false)
        return true
      }
      setDirWarmGate(true)
      while (gen === dirWarmGen.current) {
        await new Promise((r) => window.setTimeout(r, 250))
        status = await fetchDirectoryPreviewStatus()
        if (gen !== dirWarmGen.current) return true // skipped
        setDirWarmStatus(status)
        if (!status.running && status.pending <= 0 && status.failed <= 0) {
          markDirectoryPreviewsReady()
          setDirWarmGate(false)
          return true
        }
        if (!status.running && status.pending <= 0) {
          // Finished with failures — allow browse; do not mark durable ready.
          setDirWarmGate(false)
          return true
        }
      }
      return true
    } catch {
      if (gen !== dirWarmGen.current) return true
      setDirWarmScanning(false)
      setDirWarmGate(false)
      // Allow browsing; do not mark ready so reload retries.
      return true
    }
  }, [])

  const loadCull = useCallback(async (resumePhoto?: string) => {
    setError(null)
    if (photosRef.current.length === 0) {
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
        setStatus(data.stats.total > 0 ? 'done' : 'empty')
      } else {
        setStatus('ready')
        if (data.photos[idx]) void persistSession('cull', data.photos[idx].name, 'root')
      }
      await refreshStats()
      void fetchFavorites()
        .then((fav) => setStarred(new Set(fav.names ?? [])))
        .catch(() => {})
    } catch (e) {
      setStatus('error')
      setError(e instanceof Error ? e.message : 'Could not load photos.')
    }
  }, [persistSession, refreshStats])

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

    setStatus('loading')
    await ensureDirectoryPreviews()

    setTopMode(resumeMode)
    setLibBucket(resumeMode === 'library' ? resumeBucket : 'root')
    setLibPhoto(resumePhoto)
    if (resumeMode === 'library') {
      setLibraryMounted(true)
      setStatus('ready')
    } else {
      await loadCull(resumePhoto)
    }
  }, [ensureDirectoryPreviews, loadCull])

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
      resetDirectoryPreviewsReady()
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
            : action === 'shortlisted' || action === 'posting'
              ? 'up'
              : 'down'
      setAnim(direction)

      try {
        const res = await classifyPhoto(photo.name, action)
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
      } catch (e) {
        setAnim(null)
        const msg = e instanceof Error ? e.message : 'Could not move photo.'
        showToast(msg, 'error')
      } finally {
        busyRef.current = false
        setBusy(false)
      }
    },
    [topMode, showToast],
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
        showToast(e instanceof Error ? e.message : 'Could not update favourite', 'error')
      } finally {
        setStarBusy(false)
      }
    },
    [starred, starBusy, showToast],
  )

  const revealInFinder = useCallback(() => {
    const photo = photosRef.current[indexRef.current]
    if (!photo) return
    void revealPhoto(photo.name, 'root')
      .then(() => showToast('Shown in Finder', 'neutral'))
      .catch((err: unknown) =>
        showToast(err instanceof Error ? err.message : 'Could not show in Finder', 'error'),
      )
  }, [showToast])

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
          restored.forEach((p) => preloadImage(p.url))
        } else {
          await refreshStats()
        }
      } else {
        showToast(res.count && res.count > 1 ? `Undid ${res.count}` : 'Undone', 'neutral')
      }
      await refreshStats()
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Undo failed', 'error')
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }, [topMode, showToast, refreshStats])

  useKeyboard(
    (e) => {
      if (topMode !== 'cull' || subMode !== 'browse') return

      if (e.key === 'i' || e.key === 'I') {
        if (status !== 'ready') return
        e.preventDefault()
        setShowExif((p) => !p)
        return
      }
      if (e.key === 'o' || e.key === 'O') {
        if (status !== 'ready') return
        e.preventDefault()
        revealInFinder()
        return
      }
      if (e.key === '*') {
        if (status !== 'ready') return
        e.preventDefault()
        const photo = photosRef.current[indexRef.current]
        if (photo) void toggleStar(photo.name)
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
      } else if (e.key === 'p' || e.key === 'P') {
        e.preventDefault()
        void runAction('posting')
      } else if (e.key === 'd' || e.key === 'D') {
        e.preventDefault()
        void runAction('disliked')
      } else if (e.key === 's' || e.key === 'S') {
        e.preventDefault()
        void runAction('sharing')
      }
    },
    [topMode, subMode, status, runAction, runUndo, toggleFullscreen, navigateCull, revealInFinder, toggleStar],
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
      setLibPhoto(undefined)
      setLibraryJumpToken((n) => n + 1)
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
        <div className="header-right">
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
        </div>
      </header>
      {fullscreen && (
        <div className={`fs-esc-hint${escHolding ? ' fs-esc-hint--holding' : ''}`} aria-live="polite">
          {escHolding ? 'Keep holding Esc…' : 'Hold Esc to exit fullscreen'}
        </div>
      )}

      {libraryMounted && !dirWarmGate && (
        <div
          className="view-pane"
          key="library-browse"
          hidden={!(topMode === 'library' && subMode === 'browse')}
        >
          <LibraryView
            active={topMode === 'library' && subMode === 'browse'}
            libraryEpoch={libraryEpoch}
            jumpToken={libraryJumpToken}
            initialBucket={libBucket}
            initialPhoto={libPhoto}
            fullscreen={fullscreen}
            onToggleFullscreen={toggleFullscreen}
            onToast={showToast}
            onStats={(fs) => {
              setStats(fs)
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
            onMoved={() => void refreshStats()}
          />
        </div>
      )}

      {dirWarmGate && (
        <div className="view-pane view-pane--warmup">
          <main className="stage stage--warming">
            <ThumbWarmupScreen
              mode="cull"
              status={dirWarmStatus}
              scanning={dirWarmScanning || !dirWarmStatus}
              onSkip={dirWarmStatus && dirWarmStatus.pending > 0 ? skipDirectoryWarmup : undefined}
            />
          </main>
        </div>
      )}

      {topMode === 'cull' && !dirWarmGate && (
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
                  {starred.has(current.name) && (
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
                  Unclassified — {currentIndex + 1} / {photos.length} · {current.name}
                  <span className="cull-nav-hint"> · ← → browse · D S L T P decide</span>
                </p>
              </>
            )}
          </main>

          {(status === 'ready' || status === 'done') && (
            <footer className="footer">
              <div className="footer-bar">
                {status === 'ready' && (
                  <div className="actions">
                    <button
                      type="button"
                      className="btn btn--dislike btn--with-count"
                      disabled={busy}
                      onClick={() => void runAction('disliked')}
                    >
                      <span className="btn-main">
                        Disliked <kbd>D</kbd>
                      </span>
                      <span className={`btn-count${stats.disliked === 0 ? ' btn-count--zero' : ''}`}>
                        {stats.disliked}
                      </span>
                    </button>
                    <button
                      type="button"
                      className="btn btn--sharing btn--with-count"
                      disabled={busy}
                      onClick={() => void runAction('sharing')}
                    >
                      <span className="btn-main">
                        Sharing <kbd>S</kbd>
                      </span>
                      <span className={`btn-count${stats.sharing === 0 ? ' btn-count--zero' : ''}`}>
                        {stats.sharing}
                      </span>
                    </button>
                    <button
                      type="button"
                      className="btn btn--like btn--with-count"
                      disabled={busy}
                      onClick={() => void runAction('liked')}
                    >
                      <span className="btn-main">
                        Liked <kbd>L</kbd>
                      </span>
                      <span className={`btn-count${stats.liked === 0 ? ' btn-count--zero' : ''}`}>
                        {stats.liked}
                      </span>
                    </button>
                    <button
                      type="button"
                      className="btn btn--shortlisted btn--with-count"
                      disabled={busy}
                      onClick={() => void runAction('shortlisted')}
                    >
                      <span className="btn-main">
                        Shortlisted <kbd>T</kbd>
                      </span>
                      <span className={`btn-count${stats.shortlisted === 0 ? ' btn-count--zero' : ''}`}>
                        {stats.shortlisted}
                      </span>
                    </button>
                    <button
                      type="button"
                      className="btn btn--posting btn--with-count"
                      disabled={busy}
                      onClick={() => void runAction('posting')}
                    >
                      <span className="btn-main">
                        Posting <kbd>P</kbd>
                      </span>
                      <span className={`btn-count${stats.posting === 0 ? ' btn-count--zero' : ''}`}>
                        {stats.posting}
                      </span>
                    </button>
                    <button type="button" className="btn btn--neutral" disabled={busy} onClick={() => void runUndo()}>
                      Undo <kbd>⌫</kbd>
                    </button>
                    <button
                      type="button"
                      className={`btn btn--neutral${current && starred.has(current.name) ? ' btn--star-on' : ''}`}
                      disabled={starBusy || !current}
                      onClick={() => current && void toggleStar(current.name)}
                      title={current && starred.has(current.name) ? 'Unstar (*)' : 'Star (*)'}
                    >
                      {current && starred.has(current.name) ? '★ Starred' : '☆ Starred'} <kbd>*</kbd>
                    </button>
                    <button
                      type="button"
                      className={`btn btn--neutral${showExif ? ' btn--exif-on' : ''}`}
                      disabled={!current}
                      onClick={() => setShowExif((p) => !p)}
                      title="EXIF (I)"
                    >
                      EXIF <kbd>I</kbd>
                    </button>
                    <button
                      type="button"
                      className="btn btn--neutral"
                      disabled={!current}
                      onClick={revealInFinder}
                      title="Show in Finder (O)"
                    >
                      Finder <kbd>O</kbd>
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
