import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  burstKeep,
  detectBursts,
  exportBucket,
  type ExportMode,
  fetchExif,
  fetchFavorites,
  fetchLibrary,
  fetchStats,
  imageUrl,
  movePhoto,
  movePhotosBulk,
  resetToUnclassified,
  revealPhoto,
  setPhotoStarred,
  undoLast,
  type Bucket,
  type BurstGroup,
  type ExifMetadata,
  type LibraryResponse,
  type Photo,
  type SessionStats,
  type Stats,
  type ToastTone,
  toastToneForBucket,
} from './api'
import { useKeyboard } from './hooks/useKeyboard'
import ZoomableImage from './ZoomableImage'

type Props = {
  initialBucket?: Bucket
  initialPhoto?: string
  onOpenCompare: (photos: Photo[], bucket: Bucket) => void
  onStats: (fs: Stats, session: SessionStats) => void
  onToast: (msg: string, tone?: ToastTone) => void
  onSession: (bucket: Bucket, photo: string) => void
  fullscreen: boolean
  onToggleFullscreen: () => void
}

const LIBRARY_BUCKETS: Bucket[] = ['root', 'liked', 'shortlisted', 'posting', 'sharing', 'disliked']
const EXPORTABLE_BUCKETS: Bucket[] = ['liked', 'shortlisted', 'posting', 'sharing']
const EXPORT_BATCH_SIZE = 30
const EXPORT_MAX_BYTES = 15 * 1024 * 1024

function canExportPhotos(b: Bucket): boolean {
  return EXPORTABLE_BUCKETS.includes(b)
}

function bucketLabel(b: Bucket) {
  if (b === 'root') return 'Unclassified'
  return b.charAt(0).toUpperCase() + b.slice(1)
}

function bucketShortLabel(b: Bucket) {
  switch (b) {
    case 'root':
      return 'Unc'
    case 'liked':
      return 'Liked'
    case 'shortlisted':
      return 'Short'
    case 'posting':
      return 'Post'
    case 'sharing':
      return 'Share'
    case 'disliked':
      return 'Dis'
  }
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

const GRID_COLS = 8

type SearchHit = { photo: Photo; bucket: Bucket }

function matchPhotoName(name: string, query: string): boolean {
  return name.toLowerCase().includes(query.toLowerCase())
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
  const [counts, setCounts] = useState({
    root: 0,
    liked: 0,
    shortlisted: 0,
    posting: 0,
    sharing: 0,
    disliked: 0,
  })
  const [focused, setFocused] = useState(0)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [viewer, setViewer] = useState<Photo | null>(null)
  const [showExif, setShowExif] = useState(false)
  const [exif, setExif] = useState<ExifMetadata | null>(null)
  const [starred, setStarred] = useState<Set<string>>(() => new Set())
  const [starBusy, setStarBusy] = useState(false)
  const [starredOnly, setStarredOnly] = useState(false)
  const [showNames, setShowNames] = useState(false)
  const [busy, setBusy] = useState(false)
  const [detecting, setDetecting] = useState(false)
  const [confirm, setConfirm] = useState<{ to: Bucket; names: string[] } | null>(null)
  const [dragOver, setDragOver] = useState<Bucket | null>(null)
  const [exportOpen, setExportOpen] = useState(false)
  const [exportPath, setExportPath] = useState('')
  const [exportNames, setExportNames] = useState<string[] | null>(null)
  const [exportMode, setExportMode] = useState<ExportMode>('compressed')
  const [exporting, setExporting] = useState(false)
  const [resetOpen, setResetOpen] = useState(false)
  const [toolsOpen, setToolsOpen] = useState(false)
  const [exportSubOpen, setExportSubOpen] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchExpanded, setSearchExpanded] = useState(false)
  const [searchAllHits, setSearchAllHits] = useState<SearchHit[] | null>(null)
  const [searchExpanding, setSearchExpanding] = useState(false)
  const lastClickRef = useRef<number>(-1)
  const shownAt = useRef(Date.now())
  const cacheRef = useRef<Map<Bucket, LibraryResponse>>(new Map())
  const loadGen = useRef(0)
  const toolsMenuRef = useRef<HTMLDivElement>(null)
  const focusedTileRef = useRef<HTMLDivElement | null>(null)
  const searchInputRef = useRef<HTMLInputElement>(null)

  const applyCountsFromStats = useCallback(
    (fs: Stats) => {
      setCounts({
        root: fs.root,
        liked: fs.liked,
        shortlisted: fs.shortlisted,
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
      const list = starredOnly ? data.photos.filter((p) => starred.has(p.name)) : data.photos
      let idx = 0
      if (focusName) {
        const i = list.findIndex((p) => p.name === focusName)
        if (i >= 0) idx = i
      }
      setFocused(idx)
      if (list[idx]) onSession(b, list[idx].name)
      if (data.stats) applyCountsFromStats(data.stats)
    },
    [onSession, applyCountsFromStats, starredOnly, starred],
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
        onToast(e instanceof Error ? e.message : 'Could not load library', 'error')
      }
    },
    [applyLibraryData, onToast, onStats, prefetchOthers, refreshCounts],
  )

  useEffect(() => {
    void loadBucket(bucket, initialPhoto).catch((e) =>
      onToast(e instanceof Error ? e.message : 'Could not load library', 'error'),
    )
    void fetchFavorites()
      .then((fav) => setStarred(new Set(fav.names ?? [])))
      .catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

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
        onToast(e instanceof Error ? e.message : 'Could not update favourite', 'error')
      } finally {
        setStarBusy(false)
      }
    },
    [starred, starBusy, onToast],
  )

  const visiblePhotos = useMemo(
    () => (starredOnly ? photos.filter((p) => starred.has(p.name)) : photos),
    [starredOnly, photos, starred],
  )

  const visibleBursts = useMemo(() => {
    if (!starredOnly) return bursts
    return bursts
      .map((g) => {
        const gp = g.photos.filter((p) => starred.has(p.name))
        return { ...g, photos: gp, size: gp.length }
      })
      .filter((g) => g.photos.length > 0)
  }, [starredOnly, bursts, starred])

  const visibleSingles = useMemo(
    () => (starredOnly ? singles.filter((p) => starred.has(p.name)) : singles),
    [starredOnly, singles, starred],
  )

  const starredInBucket = useMemo(
    () => photos.reduce((n, p) => n + (starred.has(p.name) ? 1 : 0), 0),
    [photos, starred],
  )

  const searchTrimmed = searchQuery.trim()
  const searchActive = searchOpen && searchTrimmed.length > 0

  const localSearchHits = useMemo((): SearchHit[] => {
    if (!searchTrimmed) return []
    return photos
      .filter((p) => matchPhotoName(p.name, searchTrimmed))
      .map((photo) => ({ photo, bucket }))
  }, [photos, searchTrimmed, bucket])

  const displaySearchHits = searchExpanded ? (searchAllHits ?? []) : localSearchHits
  const showExpandSearch =
    searchActive && !searchExpanded && localSearchHits.length === 0 && !searchExpanding
  const searchNotFound =
    searchActive &&
    searchExpanded &&
    !searchExpanding &&
    searchAllHits !== null &&
    searchAllHits.length === 0

  const closeSearch = useCallback(() => {
    setSearchOpen(false)
    setSearchQuery('')
    setSearchExpanded(false)
    setSearchAllHits(null)
    setSearchExpanding(false)
  }, [])

  const openSearch = useCallback(() => {
    setSearchOpen(true)
    setSearchExpanded(false)
    setSearchAllHits(null)
    window.setTimeout(() => searchInputRef.current?.focus(), 0)
  }, [])

  const expandSearch = useCallback(async () => {
    const q = searchQuery.trim()
    if (!q) return
    setSearchExpanding(true)
    setSearchExpanded(true)
    setSearchAllHits(null)
    const hits: SearchHit[] = []
    try {
      await Promise.all(
        LIBRARY_BUCKETS.map(async (b) => {
          let data = cacheRef.current.get(b)
          if (!data) {
            data = await fetchLibrary(b)
            cacheRef.current.set(b, data)
          }
          for (const photo of data.photos) {
            if (matchPhotoName(photo.name, q)) hits.push({ photo, bucket: b })
          }
        }),
      )
      hits.sort((a, b) => {
        if (a.bucket !== b.bucket) {
          return LIBRARY_BUCKETS.indexOf(a.bucket) - LIBRARY_BUCKETS.indexOf(b.bucket)
        }
        return a.photo.name.localeCompare(b.photo.name)
      })
      setSearchAllHits(hits)
    } catch (e) {
      setSearchAllHits([])
      onToast(e instanceof Error ? e.message : 'Search failed', 'error')
    } finally {
      setSearchExpanding(false)
    }
  }, [searchQuery, onToast])

  const openSearchHit = useCallback(
    async (hit: SearchHit) => {
      closeSearch()
      if (hit.bucket !== bucket) {
        await loadBucket(hit.bucket, hit.photo.name)
      } else {
        const i = photos.findIndex((p) => p.name === hit.photo.name)
        if (i >= 0) {
          const vis = starredOnly ? photos.filter((p) => starred.has(p.name)) : photos
          const vi = vis.findIndex((p) => p.name === hit.photo.name)
          setFocused(vi >= 0 ? vi : 0)
        }
      }
      setViewer(hit.photo)
      shownAt.current = Date.now()
    },
    [closeSearch, bucket, loadBucket, photos, starredOnly, starred],
  )

  useEffect(() => {
    setSearchExpanded(false)
    setSearchAllHits(null)
    setSearchExpanding(false)
  }, [bucket])

  useEffect(() => {
    setSearchExpanded(false)
    setSearchAllHits(null)
  }, [searchQuery])

  useEffect(() => {
    if (!searchOpen) return
    searchInputRef.current?.focus()
  }, [searchOpen])

  const pendingFocusName = useRef<string | null>(null)

  useEffect(() => {
    if (pendingFocusName.current) {
      const name = pendingFocusName.current
      pendingFocusName.current = null
      const i = visiblePhotos.findIndex((p) => p.name === name)
      setFocused(i >= 0 ? i : 0)
      return
    }
    setFocused((f) => {
      if (visiblePhotos.length === 0) return 0
      return Math.min(f, visiblePhotos.length - 1)
    })
  }, [visiblePhotos])

  useEffect(() => {
    if (!viewer || !starredOnly) return
    if (starred.has(viewer.name)) return
    const next = visiblePhotos[0] ?? null
    setViewer(next)
    if (next) shownAt.current = Date.now()
  }, [viewer, starredOnly, starred, visiblePhotos])

  useEffect(() => {
    const p = visiblePhotos[focused]
    if (p) onSession(bucket, p.name)
  }, [visiblePhotos, focused, bucket, onSession])

  useEffect(() => {
    if (viewer) return
    focusedTileRef.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }, [focused, visiblePhotos, viewer])

  const selectedList = useMemo(
    () => visiblePhotos.filter((p) => selected.has(p.name)),
    [visiblePhotos, selected],
  )

  const targets = useCallback(() => {
    if (selected.size > 0) return [...selected]
    const p = visiblePhotos[focused]
    return p ? [p.name] : []
  }, [selected, visiblePhotos, focused])

  const advanceViewerAfterRemove = useCallback(
    (prev: Photo[], removed: Set<string>) => {
      const nextAll = prev.filter((p) => !removed.has(p.name))
      const next = starredOnly ? nextAll.filter((p) => starred.has(p.name)) : nextAll
      if (!viewer || !removed.has(viewer.name)) {
        setFocused((f) => Math.min(f, Math.max(0, next.length - 1)))
        return
      }
      const idx = next.findIndex((p) => p.name === viewer.name)
      if (next.length === 0) {
        setViewer(null)
        setFocused(0)
        return
      }
      const nextIdx = idx >= 0 ? idx : Math.min(Math.max(prev.findIndex((p) => p.name === viewer.name), 0), next.length - 1)
      setFocused(nextIdx)
      setViewer(next[nextIdx])
      shownAt.current = Date.now()
    },
    [viewer, starredOnly, starred],
  )

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
            onToast(`Moved ${res.movedCount}. ${res.failedCount} could not be moved.`, 'error')
          } else {
            onToast(`Moved ${res.movedCount} photos to ${bucketLabel(to)}`, toastToneForBucket(to))
          }
        }
        await refreshCounts()
        shownAt.current = Date.now()
      } catch (e) {
        onToast(e instanceof Error ? e.message : 'Move failed', 'error')
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
        onToast('Nothing to undo', 'neutral')
        return
      }
      invalidateCache()
      await loadBucket(bucket, undefined, { force: true })
      onToast(res.count && res.count > 1 ? `Undid ${res.count} photos` : 'Undone', 'neutral')
    } catch (e) {
      onToast(e instanceof Error ? e.message : 'Undo failed', 'error')
    }
  }, [bucket, loadBucket, onToast, invalidateCache])

  const exportPhotoCount = exportNames?.length ?? photos.length

  const openPhotoExport = useCallback(
    (names: string[] | null) => {
      setExportNames(names)
      setExportMode('compressed')
      const suffix = names ? 'selected' : bucket
      setExportPath(`~/Desktop/sorta-${suffix}-export`)
      setExportOpen(true)
    },
    [bucket],
  )

  const runExportPhotos = useCallback(async () => {
    const dest = exportPath.trim()
    if (!dest) {
      onToast('Enter a destination folder path', 'error')
      return
    }
    if (!canExportPhotos(bucket)) {
      onToast('Export is not available for this category', 'error')
      return
    }
    if (exportNames && exportNames.length === 0) {
      onToast('No photos selected to export', 'error')
      return
    }
    setExporting(true)
    try {
      const res = await exportBucket(bucket, dest, {
        batchSize: EXPORT_BATCH_SIZE,
        maxBytes: exportMode === 'compressed' ? EXPORT_MAX_BYTES : undefined,
        mode: exportMode,
        names: exportNames ?? undefined,
      })
      setExportOpen(false)
      setExportNames(null)
      if (res.failed > 0) {
        onToast(`Exported ${res.exported} · ${res.failed} failed · ${res.batches} batches`, 'error')
      } else {
        const quality =
          exportMode === 'original' ? 'as originals' : 'compressed to ≤15MB'
        onToast(
          `Exported ${res.exported} photos ${quality} into ${res.batches} batches at ${res.destination}`,
          toastToneForBucket(bucket),
        )
      }
    } catch (e) {
      onToast(e instanceof Error ? e.message : 'Export failed', 'error')
    } finally {
      setExporting(false)
    }
  }, [bucket, exportMode, exportNames, exportPath, onToast])

  const downloadFileList = useCallback(
    (files: string[], b: Bucket, selected: boolean) => {
      const category = b === 'root' ? 'unclassified' : b
      const payload = {
        category,
        count: files.length,
        files,
        ...(selected ? { selection: true } : {}),
      }
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = selected ? `sorta-${category}-selected-files.json` : `sorta-${category}-files.json`
      a.click()
      URL.revokeObjectURL(url)
      onToast(
        `Exported ${files.length} file name${files.length === 1 ? '' : 's'}${selected ? ' (selected)' : ''} from ${bucketLabel(b)}`,
        toastToneForBucket(b),
      )
    },
    [onToast],
  )

  const exportBucketFileList = useCallback(
    async (b: Bucket) => {
      try {
        let data = cacheRef.current.get(b)
        if (!data) {
          data = await fetchLibrary(b)
          cacheRef.current.set(b, data)
        }
        downloadFileList(
          data.photos.map((p) => p.name),
          b,
          false,
        )
      } catch (e) {
        onToast(e instanceof Error ? e.message : 'Could not export file list', 'error')
      }
    },
    [downloadFileList, onToast],
  )

  const exportSelectedFileList = useCallback(() => {
    const files = [...selected]
    if (files.length === 0) {
      onToast('Select photos first', 'error')
      return
    }
    downloadFileList(files, bucket, true)
  }, [bucket, downloadFileList, onToast, selected])

  const runResetUnclassified = useCallback(async () => {
    setBusy(true)
    try {
      const res = await resetToUnclassified()
      setResetOpen(false)
      invalidateCache()
      await loadBucket('root', undefined, { force: true })
      if (res.failedCount > 0) {
        onToast(`Moved ${res.movedCount} · ${res.failedCount} failed`, 'error')
      } else if (res.movedCount === 0) {
        onToast('Nothing to move — already all unclassified', 'neutral')
      } else {
        onToast(`Moved ${res.movedCount} photos to Unclassified`, 'root')
      }
      onStats(res.stats, (await fetchStats()).session)
    } catch (e) {
      onToast(e instanceof Error ? e.message : 'Reset failed', 'error')
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
        'neutral',
      )
    } catch (e) {
      onToast(e instanceof Error ? e.message : 'Burst detection failed', 'error')
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
        onToast(`Kept ${keepName} · discarded ${Math.max(0, res.moved.length - 1)}`, 'liked')
        await refreshCounts()
        const s = await fetchStats()
        onStats(s.filesystem, s.session)
      } catch (e) {
        onToast(e instanceof Error ? e.message : 'Burst keep failed', 'error')
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

  const toggleSelectNames = useCallback((names: string[]) => {
    if (names.length === 0) return
    setSelected((prev) => {
      const n = new Set(prev)
      const allSelected = names.every((name) => n.has(name))
      if (allSelected) {
        for (const name of names) n.delete(name)
      } else {
        for (const name of names) n.add(name)
      }
      return n
    })
  }, [])

  const gridItems = visibleBursts.length > 0 ? visibleSingles : visiblePhotos

  const selectRowContaining = useCallback(
    (name: string) => {
      const idx = gridItems.findIndex((p) => p.name === name)
      if (idx < 0) return
      const start = Math.floor(idx / GRID_COLS) * GRID_COLS
      toggleSelectNames(gridItems.slice(start, start + GRID_COLS).map((p) => p.name))
    },
    [gridItems, toggleSelectNames],
  )

  const onTileClick = (index: number, e: React.MouseEvent) => {
    const photo = visiblePhotos[index]
    if (!photo) return
    if (e.altKey) {
      e.preventDefault()
      selectRowContaining(photo.name)
      setFocused(index)
      lastClickRef.current = index
      return
    }
    if (e.shiftKey && lastClickRef.current >= 0) {
      const a = Math.min(lastClickRef.current, index)
      const b = Math.max(lastClickRef.current, index)
      setSelected((prev) => {
        const n = new Set(prev)
        for (let i = a; i <= b; i++) {
          const p = visiblePhotos[i]
          if (p) n.add(p.name)
        }
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
    if (!toolsOpen) {
      setExportSubOpen(false)
      return
    }
    const onDocDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (toolsMenuRef.current?.contains(t)) return
      setToolsOpen(false)
    }
    document.addEventListener('mousedown', onDocDown)
    return () => document.removeEventListener('mousedown', onDocDown)
  }, [toolsOpen])

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

  const revealInFinder = useCallback(() => {
    if (!viewer) return
    void revealPhoto(viewer.name, bucket)
      .then(() => onToast('Shown in Finder', 'neutral'))
      .catch((err: unknown) =>
        onToast(err instanceof Error ? err.message : 'Could not show in Finder', 'error'),
      )
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
      if (toolsOpen) {
        if (e.key === 'Escape') {
          e.preventDefault()
          setToolsOpen(false)
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
      if (searchOpen) {
        if (e.key === 'Escape') {
          e.preventDefault()
          closeSearch()
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
        if (e.key === '*') {
          e.preventDefault()
          void toggleStar(viewer.name)
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
          const burst = burstContaining(visibleBursts, viewer.name)
          if (burst) {
            const bi = burst.photos.findIndex((p) => p.name === viewer.name)
            const nextBi = bi + delta
            if (nextBi < 0 || nextBi >= burst.photos.length) return
            const nextPhoto = burst.photos[nextBi]
            const gi = visiblePhotos.findIndex((p) => p.name === nextPhoto.name)
            if (gi < 0) return
            setFocused(gi)
            setViewer(visiblePhotos[gi])
            shownAt.current = Date.now()
            return
          }
          const next = focused + delta
          if (next >= 0 && next < visiblePhotos.length) {
            setFocused(next)
            setViewer(visiblePhotos[next])
            shownAt.current = Date.now()
          }
          return
        }
        if (e.key === 'l' || e.key === 'L') {
          e.preventDefault()
          requestMove('liked')
        } else if (e.key === 't' || e.key === 'T') {
          e.preventDefault()
          requestMove('shortlisted')
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
      if (visiblePhotos.length === 0) return

      if (e.key === 'ArrowRight') {
        e.preventDefault()
        setFocused((f) => Math.min(visiblePhotos.length - 1, f + 1))
      } else if (e.key === 'ArrowLeft') {
        e.preventDefault()
        setFocused((f) => Math.max(0, f - 1))
      } else if (e.key === 'ArrowDown') {
        e.preventDefault()
        setFocused((f) => Math.min(visiblePhotos.length - 1, f + GRID_COLS))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setFocused((f) => Math.max(0, f - GRID_COLS))
      } else if (e.key === ' ') {
        e.preventDefault()
        const p = visiblePhotos[focused]
        if (p) toggleSelect(p.name)
      } else if (e.key === 'r' || e.key === 'R') {
        e.preventDefault()
        const p = visiblePhotos[focused]
        if (p) selectRowContaining(p.name)
      } else if (e.key === 'Enter') {
        e.preventDefault()
        const p = visiblePhotos[focused]
        if (p) {
          setViewer(p)
          shownAt.current = Date.now()
        }
      } else if (e.key === 'l' || e.key === 'L') {
        e.preventDefault()
        requestMove('liked')
      } else if (e.key === 't' || e.key === 'T') {
        e.preventDefault()
        requestMove('shortlisted')
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
        setSelected(new Set(visiblePhotos.map((p) => p.name)))
      }
    },
    [visiblePhotos, visibleBursts, focused, viewer, confirm, toolsOpen, exportOpen, exporting, resetOpen, busy, detecting, bucket, searchOpen, closeSearch, selectRowContaining, requestMove, runUndo, onToggleFullscreen, revealInFinder, toggleStar],
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
  const viewerBurst = viewer ? burstContaining(visibleBursts, viewer.name) : undefined
  const viewerBurstIndex = viewerBurst
    ? viewerBurst.photos.findIndex((p) => p.name === viewer!.name)
    : -1
  return (
    <div className={`library${fullscreen ? ' library--fs' : ''}`}>
      <section className="library-main">
        <div className="category-cards">
          {LIBRARY_BUCKETS.map((b) => (
            <button
              key={b}
              type="button"
              className={`category-card category-card--${b}${bucket === b ? ' category-card--active' : ''}${dragOver === b ? ' category-card--drag' : ''}`}
              disabled={detecting}
              title={bucketLabel(b)}
              onClick={() => void loadBucket(b)}
              onDragOver={(e) => {
                e.preventDefault()
                setDragOver(b)
              }}
              onDragLeave={() => setDragOver(null)}
              onDrop={(e) => onDropBucket(e, b)}
            >
              <span className="category-card-label">{bucketShortLabel(b)}</span>
              <span className="category-card-count">{counts[b]}</span>
            </button>
          ))}
        </div>
        <div className="library-toolbar">
          <div className="library-toolbar-meta">
            <span className="muted">
              {bucketLabel(bucket)} ·{' '}
              {starredOnly ? `${visiblePhotos.length} of ${photos.length}` : photos.length}
              {visibleBursts.length > 0
                ? ` · ${visibleBursts.length} burst${visibleBursts.length === 1 ? '' : 's'}`
                : ''}
              {selected.size > 0 ? ` · ${selected.size} selected` : ''}
            </span>
            <button
              type="button"
              className={`filter-star${starredOnly ? ' filter-star--on' : ''}`}
              aria-pressed={starredOnly}
              disabled={detecting || (photos.length > 0 && starredInBucket === 0 && !starredOnly)}
              title={
                starredOnly
                  ? 'Show all photos'
                  : starredInBucket === 0
                    ? 'No starred photos in this bucket'
                    : 'Show starred only'
              }
              onClick={() => {
                pendingFocusName.current = visiblePhotos[focused]?.name ?? null
                setStarredOnly((v) => !v)
              }}
            >
              {starredOnly ? '★ Starred' : '☆ Starred'}
              {starredInBucket > 0 ? ` · ${starredInBucket}` : ''}
            </button>
            <button
              type="button"
              className={`filter-star${showNames ? ' filter-star--active' : ''}`}
              aria-pressed={showNames}
              disabled={detecting}
              title={showNames ? 'Hide file names' : 'Show file names'}
              onClick={() => setShowNames((v) => !v)}
            >
              Names
            </button>
            <div className={`library-search${searchOpen ? ' library-search--open' : ''}`}>
              {!searchOpen ? (
                <button
                  type="button"
                  className="library-search-toggle"
                  disabled={detecting}
                  title="Search by file name"
                  aria-label="Search by file name"
                  onClick={openSearch}
                >
                  <svg width="15" height="15" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                    <circle cx="6.5" cy="6.5" r="4.5" stroke="currentColor" strokeWidth="1.5" />
                    <path d="M10 10l3.5 3.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                  </svg>
                </button>
              ) : (
                <div className="library-search-bar">
                  <svg
                    className="library-search-bar-icon"
                    width="14"
                    height="14"
                    viewBox="0 0 16 16"
                    fill="none"
                    aria-hidden="true"
                  >
                    <circle cx="6.5" cy="6.5" r="4.5" stroke="currentColor" strokeWidth="1.5" />
                    <path d="M10 10l3.5 3.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                  </svg>
                  <input
                    ref={searchInputRef}
                    type="search"
                    className="library-search-input"
                    placeholder={`Search in ${bucketLabel(bucket)}…`}
                    value={searchQuery}
                    autoComplete="off"
                    spellCheck={false}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Escape') {
                        e.preventDefault()
                        e.stopPropagation()
                        closeSearch()
                      }
                    }}
                  />
                  <button
                    type="button"
                    className="library-search-clear"
                    title="Close search"
                    aria-label="Close search"
                    onClick={closeSearch}
                  >
                    ×
                  </button>
                </div>
              )}
            </div>
          </div>
          <div className="library-toolbar-actions">
            {visiblePhotos.length > 0 && (
              <span className="muted library-toolbar-hint">⌥-click / R select row</span>
            )}
            <button
              type="button"
              className="btn btn--neutral btn--small"
              disabled={visiblePhotos.length === 0 || detecting}
              onClick={() => setSelected(new Set(visiblePhotos.map((p) => p.name)))}
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
                className={`btn btn--neutral btn--small${toolsOpen ? ' btn--menu-open' : ''}`}
                disabled={detecting}
                aria-haspopup="menu"
                aria-expanded={toolsOpen}
                onClick={() => setToolsOpen((o) => !o)}
              >
                Tools ▾
              </button>
              {toolsOpen && (
                <div className="toolbar-menu-panel" role="menu">
                  <button
                    type="button"
                    role="menuitem"
                    className="toolbar-menu-item"
                    disabled={busy || detecting || photos.length === 0}
                    onClick={() => {
                      setToolsOpen(false)
                      void runDetectBursts()
                    }}
                  >
                    {detecting ? 'Detecting bursts…' : 'Detect bursts'}
                  </button>
                  <button
                    type="button"
                    role="menuitem"
                    className={`toolbar-menu-item toolbar-menu-item--submenu${exportSubOpen ? ' toolbar-menu-item--open' : ''}`}
                    aria-expanded={exportSubOpen}
                    onClick={() => setExportSubOpen((o) => !o)}
                  >
                    Export {exportSubOpen ? '▾' : '▸'}
                  </button>
                  {exportSubOpen && (
                    <div className="toolbar-submenu" role="group" aria-label="Export">
                      <button
                        type="button"
                        role="menuitem"
                        className="toolbar-menu-item"
                        disabled={photos.length === 0}
                        onClick={() => {
                          setToolsOpen(false)
                          void exportBucketFileList(bucket)
                        }}
                      >
                        File names (JSON) · {photos.length}
                      </button>
                      <button
                        type="button"
                        role="menuitem"
                        className="toolbar-menu-item"
                        disabled={selected.size === 0}
                        onClick={() => {
                          setToolsOpen(false)
                          exportSelectedFileList()
                        }}
                      >
                        Selected file names (JSON) · {selected.size}
                      </button>
                      {canExportPhotos(bucket) && (
                        <>
                          <button
                            type="button"
                            role="menuitem"
                            className="toolbar-menu-item"
                            disabled={busy || exporting || photos.length === 0}
                            onClick={() => {
                              setToolsOpen(false)
                              openPhotoExport(null)
                            }}
                          >
                            All photos · {photos.length}
                          </button>
                          <button
                            type="button"
                            role="menuitem"
                            className="toolbar-menu-item"
                            disabled={busy || exporting || selected.size === 0}
                            onClick={() => {
                              setToolsOpen(false)
                              openPhotoExport([...selected])
                            }}
                          >
                            Selected photos… · {selected.size}
                          </button>
                        </>
                      )}
                    </div>
                  )}
                  {bucket === 'root' && (
                    <button
                      type="button"
                      role="menuitem"
                      className="toolbar-menu-item toolbar-menu-item--danger"
                      disabled={
                        busy ||
                        detecting ||
                        (counts.liked === 0 &&
                          counts.shortlisted === 0 &&
                          counts.posting === 0 &&
                          counts.sharing === 0 &&
                          counts.disliked === 0)
                      }
                      onClick={() => {
                        setToolsOpen(false)
                        setResetOpen(true)
                      }}
                    >
                      Reset all to Unclassified
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
            {bucket !== 'shortlisted' && (
              <button
                type="button"
                className="btn btn--shortlisted btn--small"
                disabled={busy}
                onClick={() => requestMove('shortlisted')}
              >
                Shortlist
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

        {searchActive ? (
          <div className="library-scroll library-search-results">
            {searchExpanding ? (
              <div className="state">
                <p>Searching all categories…</p>
              </div>
            ) : displaySearchHits.length > 0 ? (
              <>
                <p className="library-search-summary muted">
                  {searchExpanded
                    ? `${displaySearchHits.length} match${displaySearchHits.length === 1 ? '' : 'es'} across all categories`
                    : `${displaySearchHits.length} match${displaySearchHits.length === 1 ? '' : 'es'} in ${bucketLabel(bucket)}`}
                </p>
                <div className="library-search-grid">
                  {displaySearchHits.map((hit) => (
                    <button
                      key={`${hit.bucket}:${hit.photo.name}`}
                      type="button"
                      className="library-search-hit"
                      onClick={() => void openSearchHit(hit)}
                    >
                      <div className="library-search-hit-media">
                        <img
                          src={imageUrl(hit.photo.url, 'thumb')}
                          alt={hit.photo.name}
                          loading="lazy"
                          draggable={false}
                        />
                      </div>
                      <span className="library-search-hit-name" title={hit.photo.name}>
                        {hit.photo.name}
                      </span>
                      {searchExpanded && (
                        <span className="library-search-hit-bucket muted">{bucketLabel(hit.bucket)}</span>
                      )}
                    </button>
                  ))}
                </div>
              </>
            ) : showExpandSearch ? (
              <div className="state library-search-empty">
                <p>Not found in {bucketLabel(bucket)}</p>
                <p className="muted">Search other categories and Unclassified?</p>
                <button
                  type="button"
                  className="btn btn--neutral btn--small"
                  disabled={searchExpanding}
                  onClick={() => void expandSearch()}
                >
                  Expand search
                </button>
              </div>
            ) : searchNotFound ? (
              <div className="state">
                <p>Not found</p>
                <p className="muted">No photo matches “{searchTrimmed}” in any category.</p>
              </div>
            ) : null}
          </div>
        ) : photos.length === 0 ? (
          <div className="state">
            <p>No photos here.</p>
            <p className="muted">Photos you move to this bucket will appear here.</p>
          </div>
        ) : visiblePhotos.length === 0 ? (
          <div className="state">
            <p>No starred photos here.</p>
            <p className="muted">Star photos in the open viewer, or turn off the starred filter.</p>
          </div>
        ) : (
          <div className="library-scroll">
            {visibleBursts.length > 0 && (
              <div className="burst-groups">
                <h3 className="section-title">Bursts</h3>
                {visibleBursts.map((g) => {
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
                        const i = visiblePhotos.findIndex((x) => x.name === p.name)
                        return (
                          <div
                            key={p.name}
                            ref={focused === i ? focusedTileRef : undefined}
                            className={`photo-tile photo-tile--burst${showNames ? ' photo-tile--named' : ''}${focused === i ? ' photo-tile--focused' : ''}${selected.has(p.name) ? ' photo-tile--selected' : ''}`}
                            title={p.name}
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
                              {starred.has(p.name) && (
                                <span className="photo-star-badge" title="Favourited" aria-label="Favourited">
                                  ★
                                </span>
                              )}
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
                            {showNames && <span className="photo-tile-name">{p.name}</span>}
                          </div>
                        )
                      })}
                    </div>
                  </div>
                  )
                })}
              </div>
            )}

            {(visibleSingles.length > 0 || visibleBursts.length === 0) && (
              <div className="singles-section">
                {visibleBursts.length > 0 && <h3 className="section-title">Singles</h3>}
                <div
                  className="photo-grid photo-grid--fixed"
                  style={{ ['--grid-cols' as string]: String(GRID_COLS) }}
                >
                  {gridItems.map((p) => {
                    const i = visiblePhotos.findIndex((x) => x.name === p.name)
                    return (
                      <div
                        key={p.name}
                        ref={focused === i ? focusedTileRef : undefined}
                        className={`photo-tile${showNames ? ' photo-tile--named' : ''}${focused === i ? ' photo-tile--focused' : ''}${selected.has(p.name) ? ' photo-tile--selected' : ''}`}
                        title={p.name}
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
                          {starred.has(p.name) && (
                            <span className="photo-star-badge" title="Favourited" aria-label="Favourited">
                              ★
                            </span>
                          )}
                        </div>
                        {showNames && <span className="photo-tile-name">{p.name}</span>}
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
          <div
            className="library-viewer"
            role="dialog"
            aria-modal="true"
            onClick={() => setViewer(null)}
          >
            <div className="library-viewer-inner" onClick={(e) => e.stopPropagation()}>
              <div className="library-viewer-stage">
                <ZoomableImage
                  src={imageUrl(viewer.url, 'view')}
                  alt={viewer.name}
                  resetKey={viewer.name}
                />
                {starred.has(viewer.name) && (
                  <span className="viewer-star-badge" title="Favourited" aria-label="Favourited">
                    ★
                  </span>
                )}
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
                  : `${bucketLabel(bucket)} — ${focused + 1} / ${visiblePhotos.length} · ${viewer.name}`}
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
                {bucket !== 'shortlisted' && (
                  <button type="button" className="btn btn--shortlisted" onClick={() => requestMove('shortlisted')}>
                    Shortlist <kbd>T</kbd>
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
                  className={`btn btn--neutral${starred.has(viewer.name) ? ' btn--star-on' : ''}`}
                  disabled={starBusy}
                  onClick={() => void toggleStar(viewer.name)}
                  title={starred.has(viewer.name) ? 'Unfavourite (*)' : 'Favourite (*)'}
                >
                  {starred.has(viewer.name) ? '★ Starred' : '☆ Star'} <kbd>*</kbd>
                </button>
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
          <div
            className="confirm-modal"
            role="alertdialog"
            aria-modal="true"
            onClick={() => setConfirm(null)}
          >
            <div className="confirm-card" onClick={(e) => e.stopPropagation()}>
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
          <div
            className="confirm-modal"
            role="dialog"
            aria-modal="true"
            onClick={() => {
              if (!exporting) {
                setExportOpen(false)
                setExportNames(null)
              }
            }}
          >
            <div className="confirm-card export-card" onClick={(e) => e.stopPropagation()}>
              <h3 className="export-title">
                Export {exportNames ? 'Selected' : bucketLabel(bucket)} Photos
              </h3>
              <p className="muted">
                Creates folders of {EXPORT_BATCH_SIZE} photos. Originals stay in{' '}
                {bucketLabel(bucket)}.
              </p>
              <fieldset className="export-mode" disabled={exporting}>
                <legend className="export-label">Quality</legend>
                <label className="export-mode-option">
                  <input
                    type="radio"
                    name="export-mode"
                    value="compressed"
                    checked={exportMode === 'compressed'}
                    onChange={() => setExportMode('compressed')}
                  />
                  <span>
                    Compress to ≤15MB
                    <span className="export-mode-hint">JPEG copies for sharing / upload limits</span>
                  </span>
                </label>
                <label className="export-mode-option">
                  <input
                    type="radio"
                    name="export-mode"
                    value="original"
                    checked={exportMode === 'original'}
                    onChange={() => setExportMode('original')}
                  />
                  <span>
                    Original quality
                    <span className="export-mode-hint">
                      Exact copies — same size, format, and quality
                    </span>
                  </span>
                </label>
              </fieldset>
              <label className="export-label" htmlFor="export-path">
                Destination folder
              </label>
              <input
                id="export-path"
                className="export-input"
                type="text"
                value={exportPath}
                disabled={exporting}
                placeholder={`~/Desktop/sorta-${exportNames ? 'selected' : bucket}-export`}
                onChange={(e) => setExportPath(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !exporting) void runExportPhotos()
                }}
                autoFocus
              />
              <p className="muted export-meta">
                {exportPhotoCount} photos → {Math.ceil(exportPhotoCount / EXPORT_BATCH_SIZE) || 0}{' '}
                batches
                {exportMode === 'original' ? ' · originals' : ' · ≤15MB JPEGs'}
              </p>
              <div className="actions">
                <button
                  type="button"
                  className="btn btn--neutral"
                  disabled={exporting}
                  onClick={() => {
                    setExportOpen(false)
                    setExportNames(null)
                  }}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn btn--like"
                  disabled={exporting || !exportPath.trim() || exportPhotoCount === 0}
                  onClick={() => void runExportPhotos()}
                >
                  {exporting
                    ? `Exporting ${exportPhotoCount}…`
                    : `Export ${exportPhotoCount}`}
                </button>
              </div>
            </div>
          </div>,
          document.body,
        )}

      {resetOpen &&
        createPortal(
          <div
            className="confirm-modal"
            role="alertdialog"
            aria-modal="true"
            onClick={() => {
              if (!busy) setResetOpen(false)
            }}
          >
            <div className="confirm-card export-card" onClick={(e) => e.stopPropagation()}>
              <h3 className="export-title">Reset All to Unclassified</h3>
              <p className="muted">
                Move every photo from Liked, Shortlisted, Posting, Sharing, and Disliked back to Unclassified.
                This can be undone with Undo.
              </p>
              <p className="muted export-meta">
                {(
                  counts.liked +
                  counts.shortlisted +
                  counts.posting +
                  counts.sharing +
                  counts.disliked
                ).toLocaleString()}{' '}
                classified photos
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
