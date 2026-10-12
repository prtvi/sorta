import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  burstKeep,
  detectBursts,
  exportLiked,
  fetchExif,
  fetchLibrary,
  fetchStats,
  imageUrl,
  movePhoto,
  movePhotosBulk,
  resetToUnclassified,
  revealPhoto,
  undoLast,
  type Bucket,
  type BurstGroup,
  type ExifMetadata,
  type LibraryResponse,
  type Photo,
  type SessionStats,
  type Stats,
} from './api'
import { useKeyboard } from './hooks/useKeyboard'
import ZoomableImage from './ZoomableImage'

type Props = {
  initialBucket?: Bucket
  initialPhoto?: string
  onOpenCompare: (photos: Photo[], bucket: Bucket) => void
  onStats: (fs: Stats, session: SessionStats) => void
  onToast: (msg: string) => void
  onSession: (bucket: Bucket, photo: string) => void
  fullscreen: boolean
  onToggleFullscreen: () => void
}

const LIBRARY_BUCKETS: Bucket[] = ['root', 'liked', 'posting', 'sharing', 'disliked']
const EXPORT_BATCH_SIZE = 30
const EXPORT_MAX_BYTES = 15 * 1024 * 1024

function bucketLabel(b: Bucket) {
  if (b === 'root') return 'Unclassified'
  return b.charAt(0).toUpperCase() + b.slice(1)
}

function burstContaining(groups: BurstGroup[], name: string): BurstGroup | undefined {
  return groups.find((g) => g.photos.some((p) => p.name === name))
}

function pruneBursts(groups: BurstGroup[], removed: Set<string>): BurstGroup[] {
  return groups
    .map((g) => {
      const photos = g.photos.filter((p) => !removed.has(p.name))
      return { ...g, photos, size: photos.length }
    })
    .filter((g) => g.photos.length >= 3)
}

function exifLines(meta: ExifMetadata | null): string[] {
  if (!meta) return []
  return [meta.camera, meta.lens, meta.focal_length, meta.aperture, meta.shutter, meta.iso, meta.taken_at].filter(
    (v): v is string => Boolean(v && v.trim()),
  )
}

export default function LibraryView({
  initialBucket = 'root',
  initialPhoto,
  onOpenCompare,
  onStats,
  onToast,
  onSession,
  fullscreen,
  onToggleFullscreen,
}: Props) {
  const [bucket, setBucket] = useState<Bucket>(
    LIBRARY_BUCKETS.includes(initialBucket) ? initialBucket : 'root',
  )
  const [photos, setPhotos] = useState<Photo[]>([])
  const [bursts, setBursts] = useState<BurstGroup[]>([])
  const [singles, setSingles] = useState<Photo[]>([])
  const [counts, setCounts] = useState({ root: 0, liked: 0, posting: 0, sharing: 0, disliked: 0 })
  const [focused, setFocused] = useState(0)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [viewer, setViewer] = useState<Photo | null>(null)
  const [showExif, setShowExif] = useState(false)
  const [exif, setExif] = useState<ExifMetadata | null>(null)
  const [busy, setBusy] = useState(false)
  const [detecting, setDetecting] = useState(false)
  const [confirm, setConfirm] = useState<{ to: Bucket; names: string[] } | null>(null)
  const [dragOver, setDragOver] = useState<Bucket | null>(null)
  const [exportOpen, setExportOpen] = useState(false)
  const [exportPath, setExportPath] = useState('')
  const [exporting, setExporting] = useState(false)
  const [resetOpen, setResetOpen] = useState(false)
  const [openMenu, setOpenMenu] = useState<'tools' | 'export' | null>(null)
  const lastClickRef = useRef<number>(-1)
  const shownAt = useRef(Date.now())
  const cacheRef = useRef<Map<Bucket, LibraryResponse>>(new Map())
  const loadGen = useRef(0)
  const toolsMenuRef = useRef<HTMLDivElement>(null)
  const exportMenuRef = useRef<HTMLDivElement>(null)

  const applyCountsFromStats = useCallback(
    (fs: Stats) => {
      setCounts({
        root: fs.root,
        liked: fs.liked,
        posting: fs.posting,
        sharing: fs.sharing,
        disliked: fs.disliked,
      })
    },
    [],
  )

  const refreshCounts = useCallback(async () => {
    const s = await fetchStats()
    applyCountsFromStats(s.filesystem)
    onStats(s.filesystem, s.session)
  }, [onStats, applyCountsFromStats])

  const applyLibraryData = useCallback(
    (b: Bucket, data: LibraryResponse, focusName?: string) => {
      setPhotos(data.photos)
      setBursts(data.bursts ?? [])
      setSingles(data.singles ?? data.photos)
      setSelected(new Set())
      let idx = 0
      if (focusName) {
        const i = data.photos.findIndex((p) => p.name === focusName)
        if (i >= 0) idx = i
      }
      setFocused(idx)
      if (data.photos[idx]) onSession(b, data.photos[idx].name)
      if (data.stats) applyCountsFromStats(data.stats)
    },
    [onSession, applyCountsFromStats],
  )

  const invalidateCache = useCallback((...buckets: Bucket[]) => {
    if (buckets.length === 0) {
      cacheRef.current.clear()
      return
    }
    for (const b of buckets) cacheRef.current.delete(b)
  }, [])

  const prefetchOthers = useCallback((current: Bucket) => {
    window.setTimeout(() => {
      for (const b of LIBRARY_BUCKETS) {
        if (b === current || cacheRef.current.has(b)) continue
        void fetchLibrary(b)
          .then((data) => {
            cacheRef.current.set(b, data)
          })
          .catch(() => {})
      }
    }, 300)
  }, [])

  const loadBucket = useCallback(
    async (b: Bucket, focusName?: string, opts?: { force?: boolean }) => {
      const gen = ++loadGen.current
      setBucket(b)

      if (!opts?.force) {
        const cached = cacheRef.current.get(b)
        if (cached) {
          applyLibraryData(b, cached, focusName)
        }
      } else {
        cacheRef.current.delete(b)
      }

      try {
        const data = await fetchLibrary(b)
        if (gen !== loadGen.current) return
        cacheRef.current.set(b, data)
        applyLibraryData(b, data, focusName)
        if (data.stats) {
          void fetchStats()
            .then((s) => onStats(s.filesystem, s.session))
            .catch(() => {})
        } else {
          void refreshCounts()
        }
        prefetchOthers(b)
      } catch (e) {
        if (gen !== loadGen.current) return
        onToast(e instanceof Error ? e.message : 'Could not load library')
      }
    },
    [applyLibraryData, onToast, onStats, prefetchOthers, refreshCounts],
  )

  useEffect(() => {
    void loadBucket(bucket, initialPhoto).catch((e) =>
      onToast(e instanceof Error ? e.message : 'Could not load library'),
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const p = photos[focused]
    if (p) onSession(bucket, p.name)
  }, [photos, focused, bucket, onSession])

  const selectedList = useMemo(
    () => photos.filter((p) => selected.has(p.name)),
    [photos, selected],
  )

  const targets = useCallback(() => {
    if (selected.size > 0) return [...selected]
    const p = photos[focused]
    return p ? [p.name] : []
  }, [selected, photos, focused])

  const advanceViewerAfterRemove = useCallback((prev: Photo[], removed: Set<string>) => {
    if (!viewer || !removed.has(viewer.name)) {
      setFocused((f) => Math.min(f, Math.max(0, prev.length - removed.size - 1)))
      return
    }
    const idx = prev.findIndex((p) => p.name === viewer.name)
    const next = prev.filter((p) => !removed.has(p.name))
    if (next.length === 0) {
      setViewer(null)
      setFocused(0)
      return
    }
    const nextIdx = Math.min(Math.max(idx, 0), next.length - 1)
    setFocused(nextIdx)
    setViewer(next[nextIdx])
    shownAt.current = Date.now()
  }, [viewer])

  const doMove = useCallback(
    async (names: string[], to: Bucket) => {
      if (names.length === 0 || to === bucket || busy) return
      setBusy(true)
      try {
        if (names.length === 1) {
          const res = await movePhoto(names[0], bucket, to, Date.now() - shownAt.current)
          const removed = new Set(names)
          setPhotos((prev) => {
            advanceViewerAfterRemove(prev, removed)
            return prev.filter((p) => p.name !== names[0])
          })
          setBursts((prev) => pruneBursts(prev, removed))
          setSingles((prev) => prev.filter((p) => !removed.has(p.name)))
          setSelected((prev) => {
            const n = new Set(prev)
            n.delete(names[0])
            return n
          })
          invalidateCache(bucket, to)
          onStats(res.stats, (await fetchStats()).session)
        } else {
          const res = await movePhotosBulk(
            names.map((filename) => ({ filename, from: bucket })),
            to,
          )
          const ok = new Set(res.results.filter((r) => r.success).map((r) => r.filename))
          setPhotos((prev) => {
            advanceViewerAfterRemove(prev, ok)
            return prev.filter((p) => !ok.has(p.name))
          })
          setBursts((prev) => pruneBursts(prev, ok))
          setSingles((prev) => prev.filter((p) => !ok.has(p.name)))
          setSelected(new Set())
          invalidateCache(bucket, to)
          onStats(res.stats, (await fetchStats()).session)
          if (res.failedCount > 0) {
            onToast(`Moved ${res.movedCount}. ${res.failedCount} could not be moved.`)
          } else {
            onToast(`Moved ${res.movedCount} photos`)
          }
        }
        await refreshCounts()
        shownAt.current = Date.now()
      } catch (e) {
        onToast(e instanceof Error ? e.message : 'Move failed')
      } finally {
        setBusy(false)
        setConfirm(null)
      }
    },
    [bucket, busy, advanceViewerAfterRemove, onStats, onToast, refreshCounts, invalidateCache],
  )

  const requestMove = useCallback(
    (to: Bucket) => {
      const names = targets()
      if (names.length === 0) return
      if (names.length > 1) {
        setConfirm({ to, names })
        return
      }
      void doMove(names, to)
    },
    [targets, doMove],
  )

  const runUndo = useCallback(async () => {
    try {
      const res = await undoLast()
      if (!res.success) {
        onToast('Nothing to undo')
        return
      }
      invalidateCache()
      await loadBucket(bucket, undefined, { force: true })
      onToast(res.count && res.count > 1 ? `Undid ${res.count} photos` : 'Undone')
    } catch (e) {
      onToast(e instanceof Error ? e.message : 'Undo failed')
    }
  }, [bucket, loadBucket, onToast, invalidateCache])

  const runExportLiked = useCallback(async () => {
    const dest = exportPath.trim()
    if (!dest) {
      onToast('Enter a destination folder path')
      return
    }
    setExporting(true)
    try {
      const res = await exportLiked(dest, EXPORT_BATCH_SIZE, EXPORT_MAX_BYTES)
      setExportOpen(false)
      if (res.failed > 0) {
        onToast(`Exported ${res.exported} · ${res.failed} failed · ${res.batches} batches`)
      } else {
        onToast(`Exported ${res.exported} photos into ${res.batches} batches at ${res.destination}`)
      }
    } catch (e) {
      onToast(e instanceof Error ? e.message : 'Export failed')
    } finally {
      setExporting(false)
    }
  }, [exportPath, onToast])

  const exportBucketFileList = useCallback(
    async (b: Bucket) => {
      try {
        let data = cacheRef.current.get(b)
        if (!data) {
          data = await fetchLibrary(b)
          cacheRef.current.set(b, data)
        }
        const files = data.photos.map((p) => p.name)
        const category = b === 'root' ? 'unclassified' : b
        const payload = { category, count: files.length, files }
        const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = `sorta-${category}-files.json`
        a.click()
        URL.revokeObjectURL(url)
        onToast(`Exported ${files.length} file name${files.length === 1 ? '' : 's'} from ${bucketLabel(b)}`)
      } catch (e) {
        onToast(e instanceof Error ? e.message : 'Could not export file list')
      }
    },
    [onToast],
  )

  const runResetUnclassified = useCallback(async () => {
    setBusy(true)
    try {
      const res = await resetToUnclassified()
      setResetOpen(false)
      invalidateCache()
      await loadBucket('root', undefined, { force: true })
      if (res.failedCount > 0) {
        onToast(`Moved ${res.movedCount} · ${res.failedCount} failed`)
      } else if (res.movedCount === 0) {
        onToast('Nothing to move — already all unclassified')
      } else {
        onToast(`Moved ${res.movedCount} photos to Unclassified`)
      }
      onStats(res.stats, (await fetchStats()).session)
    } catch (e) {
      onToast(e instanceof Error ? e.message : 'Reset failed')
    } finally {
      setBusy(false)
    }
  }, [invalidateCache, loadBucket, onToast, onStats])

  const runDetectBursts = useCallback(async () => {
    if (detecting || photos.length === 0) return
    setDetecting(true)
    try {
      const data = await detectBursts(bucket)
      cacheRef.current.set(bucket, data)
      applyLibraryData(bucket, data)
      const n = data.bursts?.length ?? 0
      onToast(
        n === 0
          ? 'No bursts found (need ≥3 frames within 1s)'
          : `Found ${n} burst${n === 1 ? '' : 's'}`,
      )
    } catch (e) {
      onToast(e instanceof Error ? e.message : 'Burst detection failed')
    } finally {
      setDetecting(false)
    }
  }, [detecting, photos.length, bucket, applyLibraryData, onToast])

  const runBurstKeep = useCallback(
    async (keepName: string) => {
      if (busy || detecting) return
      setBusy(true)
      try {
        const res = await burstKeep(keepName, bucket)
        const moved = new Set(res.moved)
        setPhotos((prev) => {
          advanceViewerAfterRemove(prev, moved)
          return prev.filter((p) => !moved.has(p.name))
        })
        setBursts((prev) => prev.filter((g) => !g.photos.some((p) => moved.has(p.name))))
        setSingles((prev) => prev.filter((p) => !moved.has(p.name)))
        setSelected(new Set())
        invalidateCache()
        onToast(`Kept ${keepName} · discarded ${Math.max(0, res.moved.length - 1)}`)
        await refreshCounts()
        const s = await fetchStats()
        onStats(s.filesystem, s.session)
      } catch (e) {
        onToast(e instanceof Error ? e.message : 'Burst keep failed')
      } finally {
        setBusy(false)
      }
    },
    [bucket, busy, detecting, advanceViewerAfterRemove, onToast, onStats, refreshCounts, invalidateCache],
  )

  const toggleSelect = (name: string) => {
    setSelected((prev) => {
      const n = new Set(prev)
      if (n.has(name)) n.delete(name)
      else n.add(name)
      return n
    })
  }

  const onTileClick = (index: number, e: React.MouseEvent) => {
    const photo = photos[index]
    if (!photo) return
    if (e.shiftKey && lastClickRef.current >= 0) {
      const a = Math.min(lastClickRef.current, index)
      const b = Math.max(lastClickRef.current, index)
      setSelected((prev) => {
        const n = new Set(prev)
        for (let i = a; i <= b; i++) n.add(photos[i].name)
        return n
      })
      setFocused(index)
      return
    }
    if (e.metaKey || e.ctrlKey) {
      toggleSelect(photo.name)
      setFocused(index)
      lastClickRef.current = index
      return
    }
    setFocused(index)
    lastClickRef.current = index
    setViewer(photo)
    shownAt.current = Date.now()
  }

  useEffect(() => {
    setShowExif(false)
    setExif(null)
  }, [viewer?.name])

  useEffect(() => {
    if (!openMenu) return
    const onDocDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (toolsMenuRef.current?.contains(t) || exportMenuRef.current?.contains(t)) return
      setOpenMenu(null)
    }
    document.addEventListener('mousedown', onDocDown)
    return () => document.removeEventListener('mousedown', onDocDown)
  }, [openMenu])

  useEffect(() => {
    if (!viewer || !showExif) {
      if (!showExif) setExif(null)
      return
    }
    let cancelled = false
    void fetchExif(viewer.name, bucket)
      .then((meta) => {
        if (!cancelled) setExif(meta)
      })
      .catch(() => {
        if (!cancelled) setExif(null)
      })
    return () => {
      cancelled = true
    }
  }, [viewer, showExif, bucket])

  const gridCols = 6

  const revealInFinder = useCallback(() => {
    if (!viewer) return
    void revealPhoto(viewer.name, bucket)
      .then(() => onToast('Shown in Finder'))
      .catch((err: unknown) => onToast(err instanceof Error ? err.message : 'Could not show in Finder'))
  }, [viewer, bucket, onToast])

  useKeyboard(
    (e) => {
      if (detecting) return
      if (confirm) {
        if (e.key === 'Escape') {
          e.preventDefault()
          setConfirm(null)
        }
        return
      }
      if (openMenu) {
        if (e.key === 'Escape') {
          e.preventDefault()
          setOpenMenu(null)
        }
        return
      }
      if (exportOpen) {
        if (e.key === 'Escape' && !exporting) {
          e.preventDefault()
          setExportOpen(false)
        }
        return
      }
      if (resetOpen) {
        if (e.key === 'Escape' && !busy) {
          e.preventDefault()
          setResetOpen(false)
        }
        return
      }
      if (viewer) {
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
        if (e.key === 'Escape') {
          e.preventDefault()
          setViewer(null)
          return
        }
        if (e.key === 'f' || e.key === 'F') {
          e.preventDefault()
          onToggleFullscreen()
          return
        }
        if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
          e.preventDefault()
          const delta = e.key === 'ArrowRight' ? 1 : -1
          const burst = burstContaining(bursts, viewer.name)
          if (burst) {
            const bi = burst.photos.findIndex((p) => p.name === viewer.name)
            const nextBi = bi + delta
            if (nextBi < 0 || nextBi >= burst.photos.length) return
            const nextPhoto = burst.photos[nextBi]
            const gi = photos.findIndex((p) => p.name === nextPhoto.name)
            if (gi < 0) return
            setFocused(gi)
            setViewer(photos[gi])
            shownAt.current = Date.now()
            return
          }
          const next = focused + delta
          if (next >= 0 && next < photos.length) {
            setFocused(next)
            setViewer(photos[next])
            shownAt.current = Date.now()
          }
          return
        }
        if (e.key === 'l' || e.key === 'L') {
          e.preventDefault()
          requestMove('liked')
        } else if (e.key === 'p' || e.key === 'P') {
          e.preventDefault()
          requestMove('posting')
        } else if (e.key === 'd' || e.key === 'D' || e.key === 'x' || e.key === 'X') {
          e.preventDefault()
          requestMove('disliked')
        } else if (e.key === 's' || e.key === 'S') {
          e.preventDefault()
          requestMove('sharing')
        } else if (e.key === 'u' || e.key === 'U') {
          e.preventDefault()
          requestMove('root')
        } else if (e.key === 'Delete' && bucket !== 'disliked') {
          e.preventDefault()
          requestMove('disliked')
        } else if (e.key === 'Backspace' || ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'z')) {
          e.preventDefault()
          void runUndo()
        }
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
      if (photos.length === 0) return

      if (e.key === 'ArrowRight') {
        e.preventDefault()
        setFocused((f) => Math.min(photos.length - 1, f + 1))
      } else if (e.key === 'ArrowLeft') {
        e.preventDefault()
        setFocused((f) => Math.max(0, f - 1))
      } else if (e.key === 'ArrowDown') {
        e.preventDefault()
        setFocused((f) => Math.min(photos.length - 1, f + gridCols))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setFocused((f) => Math.max(0, f - gridCols))
      } else if (e.key === ' ') {
        e.preventDefault()
        const p = photos[focused]
        if (p) toggleSelect(p.name)
      } else if (e.key === 'Enter') {
        e.preventDefault()
        const p = photos[focused]
        if (p) {
          setViewer(p)
          shownAt.current = Date.now()
        }
      } else if (e.key === 'l' || e.key === 'L') {
        e.preventDefault()
        requestMove('liked')
      } else if (e.key === 'p' || e.key === 'P') {
        e.preventDefault()
        requestMove('posting')
      } else if (e.key === 'd' || e.key === 'D') {
        e.preventDefault()
        requestMove('disliked')
      } else if (e.key === 'x' || e.key === 'X') {
        e.preventDefault()
        requestMove('disliked')
      } else if (e.key === 's' || e.key === 'S') {
        e.preventDefault()
        requestMove('sharing')
      } else if (e.key === 'u' || e.key === 'U') {
        e.preventDefault()
        requestMove('root')
      } else if (e.key === 'Delete' && bucket !== 'disliked') {
        e.preventDefault()
        requestMove('disliked')
      } else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') {
        e.preventDefault()
        setSelected(new Set(photos.map((p) => p.name)))
      }
    },
    [photos, bursts, focused, viewer, confirm, openMenu, exportOpen, exporting, resetOpen, busy, detecting, bucket, requestMove, runUndo, onToggleFullscreen, revealInFinder],
  )

  const onDragStart = (e: React.DragEvent, name: string) => {
    const names = selected.has(name) && selected.size > 0 ? [...selected] : [name]
    e.dataTransfer.setData('application/x-sorta-photos', JSON.stringify(names))
    e.dataTransfer.effectAllowed = 'move'
  }

  const onDropBucket = (e: React.DragEvent, to: Bucket) => {
    e.preventDefault()
    setDragOver(null)
    try {
      const names = JSON.parse(e.dataTransfer.getData('application/x-sorta-photos')) as string[]
      if (!Array.isArray(names) || names.length === 0) return
      if (names.length > 1) setConfirm({ to, names })
      else void doMove(names, to)
    } catch {
      /* ignore */
    }
  }

  const compareReady = selectedList.length >= 2 && selectedList.length <= 4
  const viewerBurst = viewer ? burstContaining(bursts, viewer.name) : undefined
  const viewerBurstIndex = viewerBurst
    ? viewerBurst.photos.findIndex((p) => p.name === viewer!.name)
    : -1

  return (
    <div className={`library${fullscreen ? ' library--fs' : ''}`}>
      <aside className="library-sidebar">
        {LIBRARY_BUCKETS.map((b) => (
          <button
            key={b}
            type="button"
            className={`bucket-item${bucket === b ? ' bucket-item--active' : ''}${dragOver === b ? ' bucket-item--drag' : ''}`}
            disabled={detecting}
            onClick={() => void loadBucket(b)}
            onDragOver={(e) => {
              e.preventDefault()
              setDragOver(b)
            }}
            onDragLeave={() => setDragOver(null)}
            onDrop={(e) => onDropBucket(e, b)}
          >
            <span>{bucketLabel(b)}</span>
            <span className="bucket-count">{counts[b]}</span>
          </button>
        ))}
      </aside>

      <section className="library-main">
        <div className="library-toolbar">
          <span className="muted">
            {bucketLabel(bucket)} · {photos.length}
            {bursts.length > 0 ? ` · ${bursts.length} burst${bursts.length === 1 ? '' : 's'}` : ''}
            {selected.size > 0 ? ` · ${selected.size} selected` : ''}
          </span>
          <div className="library-toolbar-actions">
            <button
              type="button"
              className="btn btn--neutral btn--small"
              disabled={photos.length === 0 || detecting}
              onClick={() => setSelected(new Set(photos.map((p) => p.name)))}
            >
              Select All
            </button>
            {selected.size > 0 && (
              <button type="button" className="btn btn--neutral btn--small" onClick={() => setSelected(new Set())}>
                Clear
              </button>
            )}
            {compareReady && (
              <button
                type="button"
                className="btn btn--neutral btn--small"
                onClick={() => onOpenCompare(selectedList, bucket)}
              >
                Compare
              </button>
            )}

            <div className="toolbar-menu" ref={toolsMenuRef}>
              <button
                type="button"
                className={`btn btn--neutral btn--small${openMenu === 'tools' ? ' btn--menu-open' : ''}`}
                disabled={detecting}
                aria-haspopup="menu"
                aria-expanded={openMenu === 'tools'}
                onClick={() => setOpenMenu((m) => (m === 'tools' ? null : 'tools'))}
              >
                Tools ▾
              </button>
              {openMenu === 'tools' && (
                <div className="toolbar-menu-panel" role="menu">
                  <button
                    type="button"
                    role="menuitem"
                    className="toolbar-menu-item"
                    disabled={busy || detecting || photos.length === 0}
                    onClick={() => {
                      setOpenMenu(null)
                      void runDetectBursts()
                    }}
                  >
                    {detecting ? 'Detecting bursts…' : 'Detect bursts'}
                  </button>
                  {bucket === 'root' && (
                    <button
                      type="button"
                      role="menuitem"
                      className="toolbar-menu-item toolbar-menu-item--danger"
                      disabled={
                        busy ||
                        detecting ||
                        (counts.liked === 0 && counts.posting === 0 && counts.sharing === 0 && counts.disliked === 0)
                      }
                      onClick={() => {
                        setOpenMenu(null)
                        setResetOpen(true)
                      }}
                    >
                      Reset all to Unclassified
                    </button>
                  )}
                </div>
              )}
            </div>

            <div className="toolbar-menu" ref={exportMenuRef}>
              <button
                type="button"
                className={`btn btn--neutral btn--small${openMenu === 'export' ? ' btn--menu-open' : ''}`}
                disabled={detecting || exporting}
                aria-haspopup="menu"
                aria-expanded={openMenu === 'export'}
                onClick={() => setOpenMenu((m) => (m === 'export' ? null : 'export'))}
              >
                Export ▾
              </button>
              {openMenu === 'export' && (
                <div className="toolbar-menu-panel" role="menu">
                  <button
                    type="button"
                    role="menuitem"
                    className="toolbar-menu-item"
                    disabled={photos.length === 0}
                    onClick={() => {
                      setOpenMenu(null)
                      void exportBucketFileList(bucket)
                    }}
                  >
                    File names (JSON)
                  </button>
                  {bucket === 'liked' && (
                    <button
                      type="button"
                      role="menuitem"
                      className="toolbar-menu-item"
                      disabled={busy || exporting || photos.length === 0}
                      onClick={() => {
                        setOpenMenu(null)
                        setExportPath((p) => p || '~/Desktop/sorta-liked-export')
                        setExportOpen(true)
                      }}
                    >
                      Liked photos…
                    </button>
                  )}
                </div>
              )}
            </div>
          </div>
        </div>

        {selected.size > 0 && (
          <div className="selection-bar">
            <span>{selected.size} selected</span>
            {bucket !== 'root' && (
              <button type="button" className="btn btn--neutral btn--small" disabled={busy} onClick={() => requestMove('root')}>
                Unclassified
              </button>
            )}
            {bucket !== 'sharing' && (
              <button type="button" className="btn btn--sharing btn--small" disabled={busy} onClick={() => requestMove('sharing')}>
                Sharing
              </button>
            )}
            {bucket !== 'liked' && (
              <button type="button" className="btn btn--like btn--small" disabled={busy} onClick={() => requestMove('liked')}>
                Like
              </button>
            )}
            {bucket !== 'posting' && (
              <button type="button" className="btn btn--posting btn--small" disabled={busy} onClick={() => requestMove('posting')}>
                Posting
              </button>
            )}
            {bucket !== 'disliked' && (
              <button type="button" className="btn btn--dislike btn--small" disabled={busy} onClick={() => requestMove('disliked')}>
                Reject
              </button>
            )}
          </div>
        )}

        {photos.length === 0 ? (
          <div className="state">
            <p>No photos here.</p>
            <p className="muted">Photos you move to this bucket will appear here.</p>
          </div>
        ) : (
          <div className="library-scroll">
            {bursts.length > 0 && (
              <div className="burst-groups">
                <h3 className="section-title">Bursts</h3>
                {bursts.map((g) => {
                  const allSelected = g.photos.length > 0 && g.photos.every((p) => selected.has(p.name))
                  return (
                  <div key={g.id} className="burst-group">
                    <div className="burst-group-header">
                      <span>
                        Burst · {g.size} photos
                        {g.photos[0]?.taken_at ? ` · ${g.photos[0].taken_at}` : ''}
                      </span>
                      <button
                        type="button"
                        className="btn btn--neutral btn--small"
                        disabled={busy || detecting || g.photos.length === 0}
                        onClick={() => {
                          setSelected((prev) => {
                            const n = new Set(prev)
                            if (allSelected) {
                              for (const p of g.photos) n.delete(p.name)
                            } else {
                              for (const p of g.photos) n.add(p.name)
                            }
                            return n
                          })
                        }}
                      >
                        {allSelected ? 'Deselect' : 'Select all'}
                      </button>
                    </div>
                    <div className="burst-group-strip">
                      {g.photos.map((p) => {
                        const i = photos.findIndex((x) => x.name === p.name)
                        return (
                          <div
                            key={p.name}
                            className={`photo-tile photo-tile--burst${focused === i ? ' photo-tile--focused' : ''}${selected.has(p.name) ? ' photo-tile--selected' : ''}`}
                            draggable
                            onDragStart={(e) => onDragStart(e, p.name)}
                            onClick={(e) => onTileClick(i, e)}
                          >
                            <div className="photo-tile-media">
                              <label className="photo-check" onClick={(e) => e.stopPropagation()}>
                                <input
                                  type="checkbox"
                                  checked={selected.has(p.name)}
                                  onChange={() => toggleSelect(p.name)}
                                />
                              </label>
                              <img src={imageUrl(p.url, 'thumb')} alt={p.name} loading="lazy" draggable={false} />
                              <button
                                type="button"
                                className="burst-keep-one"
                                disabled={busy || detecting}
                                onClick={(e) => {
                                  e.stopPropagation()
                                  void runBurstKeep(p.name)
                                }}
                                title="Keep this, dislike rest of burst"
                              >
                                Keep
                              </button>
                            </div>
                            <span className="photo-name" title={p.name}>
                              {p.name}
                            </span>
                          </div>
                        )
                      })}
                    </div>
                  </div>
                  )
                })}
              </div>
            )}

            {(singles.length > 0 || bursts.length === 0) && (
              <div className="singles-section">
                {bursts.length > 0 && <h3 className="section-title">Singles</h3>}
                <div className="photo-grid">
                  {(bursts.length > 0 ? singles : photos).map((p) => {
                    const i = photos.findIndex((x) => x.name === p.name)
                    return (
                      <div
                        key={p.name}
                        className={`photo-tile${focused === i ? ' photo-tile--focused' : ''}${selected.has(p.name) ? ' photo-tile--selected' : ''}`}
                        draggable
                        onDragStart={(e) => onDragStart(e, p.name)}
                        onClick={(e) => onTileClick(i, e)}
                      >
                        <div className="photo-tile-media">
                          <label className="photo-check" onClick={(e) => e.stopPropagation()}>
                            <input
                              type="checkbox"
                              checked={selected.has(p.name)}
                              onChange={() => toggleSelect(p.name)}
                            />
                          </label>
                          <img src={imageUrl(p.url, 'thumb')} alt={p.name} loading="lazy" draggable={false} />
                        </div>
                        <span className="photo-name" title={p.name}>
                          {p.name}
                        </span>
                      </div>
                    )
                  })}
                </div>
              </div>
            )}
          </div>
        )}
      </section>

      {detecting &&
        createPortal(
          <div className="detect-overlay" role="status" aria-live="polite">
            <div className="detect-card">
              <div className="detect-spinner" aria-hidden="true" />
              <h3 className="export-title">Detecting bursts</h3>
              <p className="muted">
                Reading capture times in {bucketLabel(bucket)}… this can take a while on large folders.
              </p>
            </div>
          </div>,
          document.body,
        )}

      {viewer &&
        createPortal(
          <div className="library-viewer" role="dialog" aria-modal="true">
            <div className="library-viewer-inner">
              <div className="library-viewer-stage">
                <ZoomableImage
                  src={imageUrl(viewer.url, 'view')}
                  alt={viewer.name}
                  resetKey={viewer.name}
                />
              </div>
              {showExif && (
                <div className="library-exif">
                  {exifLines(exif).length > 0 ? (
                    <ul>
                      {exifLines(exif).map((line) => (
                        <li key={line}>{line}</li>
                      ))}
                    </ul>
                  ) : (
                    <p className="exif-empty">No EXIF metadata</p>
                  )}
                </div>
              )}
              <p className="filename">
                {viewerBurst && viewerBurstIndex >= 0
                  ? `${bucketLabel(bucket)} — burst ${viewerBurstIndex + 1} / ${viewerBurst.photos.length} · ${viewer.name}`
                  : `${bucketLabel(bucket)} — ${focused + 1} / ${photos.length} · ${viewer.name}`}
              </p>
              <div className="actions">
                {bucket !== 'disliked' && (
                  <button type="button" className="btn btn--dislike" onClick={() => requestMove('disliked')}>
                    Reject <kbd>X</kbd>
                  </button>
                )}
                {bucket !== 'sharing' && (
                  <button type="button" className="btn btn--sharing" onClick={() => requestMove('sharing')}>
                    Sharing <kbd>S</kbd>
                  </button>
                )}
                {bucket !== 'liked' && (
                  <button type="button" className="btn btn--like" onClick={() => requestMove('liked')}>
                    Like <kbd>L</kbd>
                  </button>
                )}
                {bucket !== 'posting' && (
                  <button type="button" className="btn btn--posting" onClick={() => requestMove('posting')}>
                    Posting <kbd>P</kbd>
                  </button>
                )}
                {bucket !== 'root' && (
                  <button type="button" className="btn btn--neutral" onClick={() => requestMove('root')}>
                    Unclassified <kbd>U</kbd>
                  </button>
                )}
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
                <button type="button" className="btn btn--neutral" onClick={() => setViewer(null)}>
                  Close
                </button>
              </div>
            </div>
          </div>,
          document.body,
        )}

      {confirm &&
        createPortal(
          <div className="confirm-modal" role="alertdialog" aria-modal="true">
            <div className="confirm-card">
              <p>{`Move ${confirm.names.length} photos to ${bucketLabel(confirm.to)}?`}</p>
              <p className="muted">This will physically move the files.</p>
              <div className="actions">
                <button type="button" className="btn btn--neutral" onClick={() => setConfirm(null)}>
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn btn--like"
                  onClick={() => void doMove(confirm.names, confirm.to)}
                >
                  {`Move ${confirm.names.length} Photos`}
                </button>
              </div>
            </div>
          </div>,
          document.body,
        )}

      {exportOpen &&
        createPortal(
          <div className="confirm-modal" role="dialog" aria-modal="true">
            <div className="confirm-card export-card">
              <h3 className="export-title">Export Liked</h3>
              <p className="muted">
                Creates folders of {EXPORT_BATCH_SIZE} photos and compresses each copy to ≤15MB.
                Originals stay in Liked.
              </p>
              <label className="export-label" htmlFor="export-path">
                Destination folder
              </label>
              <input
                id="export-path"
                className="export-input"
                type="text"
                value={exportPath}
                disabled={exporting}
                placeholder="~/Desktop/sorta-liked-export"
                onChange={(e) => setExportPath(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !exporting) void runExportLiked()
                }}
                autoFocus
              />
              <p className="muted export-meta">
                {photos.length} photos → {Math.ceil(photos.length / EXPORT_BATCH_SIZE)} batches
              </p>
              <div className="actions">
                <button
                  type="button"
                  className="btn btn--neutral"
                  disabled={exporting}
                  onClick={() => setExportOpen(false)}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn btn--like"
                  disabled={exporting || !exportPath.trim()}
                  onClick={() => void runExportLiked()}
                >
                  {exporting ? 'Exporting…' : 'Export'}
                </button>
              </div>
            </div>
          </div>,
          document.body,
        )}

      {resetOpen &&
        createPortal(
          <div className="confirm-modal" role="alertdialog" aria-modal="true">
            <div className="confirm-card export-card">
              <h3 className="export-title">Reset All to Unclassified</h3>
              <p className="muted">
                Move every photo from Liked, Posting, Sharing, and Disliked back to Unclassified.
                This can be undone with Undo.
              </p>
              <p className="muted export-meta">
                {(counts.liked + counts.posting + counts.sharing + counts.disliked).toLocaleString()} classified photos
              </p>
              <div className="actions">
                <button
                  type="button"
                  className="btn btn--neutral"
                  disabled={busy}
                  onClick={() => setResetOpen(false)}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn btn--dislike"
                  disabled={busy}
                  onClick={() => void runResetUnclassified()}
                >
                  {busy ? 'Moving…' : 'Reset All'}
                </button>
              </div>
            </div>
          </div>,
          document.body,
        )}
    </div>
  )
}
