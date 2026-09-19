export type PhotoAction = 'liked' | 'disliked' | 'review'
export type Bucket = 'root' | 'liked' | 'review' | 'disliked'
export type AppMode = 'cull' | 'library' | 'review' | 'compare'
export type ImageSize = 'view' | 'thumb'

export interface Photo {
  name: string
  url: string
  burst_id?: string
  burst_index?: number
  burst_size?: number
  taken_at?: string
}

/** Display URL for a photo — serves a cached resized JPEG, not the original. */
export function imageUrl(url: string, size: ImageSize = 'view'): string {
  const [path, query = ''] = url.split('?')
  const params = new URLSearchParams(query)
  if (size === 'thumb') params.set('size', 'thumb')
  else params.delete('size')
  const q = params.toString()
  return q ? `${path}?${q}` : path
}

export interface Stats {
  total: number
  remaining: number
  root: number
  liked: number
  disliked: number
  review: number
}

export interface SessionStats {
  reviewed: number
  liked: number
  disliked: number
  review: number
  avg_decision_seconds: number
}

export interface StatsResponse {
  filesystem: Stats
  session: SessionStats
}

export interface PhotosResponse {
  photos: Photo[]
  stats: Stats
}

export interface BurstGroup {
  id: string
  size: number
  photos: Photo[]
}

export interface LibraryResponse {
  bucket: Bucket
  photos: Photo[]
  bursts: BurstGroup[]
  singles: Photo[]
  stats?: Stats
}

export interface ActionResponse {
  success: boolean
  photo: string
  action: PhotoAction
  stats: Stats
}

export interface MoveResponse {
  success: boolean
  filename: string
  from: Bucket
  to: Bucket
  stats: Stats
}

export interface BulkMoveResponse {
  success: boolean
  movedCount: number
  failedCount: number
  results: { filename: string; success: boolean; error?: string }[]
  stats: Stats
}

export interface BurstKeepResponse {
  success: boolean
  kept: string
  moved: string[]
  stats: Stats
}

export interface UndoPhoto {
  name: string
  url: string
  bucket: Bucket
}

export interface UndoResponse {
  success: boolean
  photo?: string
  photos?: UndoPhoto[]
  count?: number
  reason?: string
  stats?: Stats
}

export interface ExifMetadata {
  camera?: string
  lens?: string
  focal_length?: string
  aperture?: string
  shutter?: string
  iso?: string
  taken_at?: string
  has_gps?: boolean
}

export interface SessionState {
  mode: string
  lastPhoto: string
  lastBucket: string
}

export interface ApiError {
  success: false
  error: { code: string; message: string }
}

async function parseJSON<T>(res: Response): Promise<T> {
  const data = await res.json()
  if (!res.ok) {
    const err = data as ApiError
    throw new Error(err.error?.message ?? `Request failed (${res.status})`)
  }
  return data as T
}

function withDecision(ms?: number): HeadersInit {
  if (!ms || ms <= 0) return {}
  return { 'X-Decision-Ms': String(Math.round(ms)) }
}

export async function fetchPhotos(): Promise<PhotosResponse> {
  const res = await fetch('/api/photos')
  return parseJSON<PhotosResponse>(res)
}

export async function fetchStats(): Promise<StatsResponse> {
  const res = await fetch('/api/stats')
  return parseJSON<StatsResponse>(res)
}

export async function fetchLibrary(bucket: Bucket): Promise<LibraryResponse> {
  const res = await fetch(`/api/library/${bucket}`)
  return parseJSON<LibraryResponse>(res)
}

export async function detectBursts(bucket: Bucket): Promise<LibraryResponse> {
  const res = await fetch(`/api/library/${bucket}/detect-bursts`, { method: 'POST' })
  return parseJSON<LibraryResponse>(res)
}

export async function classifyPhoto(
  filename: string,
  action: PhotoAction,
  decisionMs?: number,
): Promise<ActionResponse> {
  const res = await fetch(`/api/photos/${encodeURIComponent(filename)}/action`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...withDecision(decisionMs) },
    body: JSON.stringify({ action }),
  })
  return parseJSON<ActionResponse>(res)
}

export async function movePhoto(
  filename: string,
  from: Bucket,
  to: Bucket,
  decisionMs?: number,
): Promise<MoveResponse> {
  const res = await fetch('/api/photos/move', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...withDecision(decisionMs) },
    body: JSON.stringify({ filename, from, to }),
  })
  return parseJSON<MoveResponse>(res)
}

export async function movePhotosBulk(
  photos: { filename: string; from: Bucket }[],
  to: Bucket,
): Promise<BulkMoveResponse> {
  const res = await fetch('/api/photos/move-bulk', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ photos, to }),
  })
  return parseJSON<BulkMoveResponse>(res)
}

export async function burstKeep(
  filename: string,
  from: Bucket = 'root',
  keepAction: PhotoAction = 'liked',
  restAction: PhotoAction = 'disliked',
): Promise<BurstKeepResponse> {
  const res = await fetch(`/api/photos/${encodeURIComponent(filename)}/burst-keep`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ keep_action: keepAction, rest_action: restAction, from }),
  })
  return parseJSON<BurstKeepResponse>(res)
}

export interface ExportLikedResponse {
  success: boolean
  destination: string
  batch_size: number
  max_bytes: number
  exported: number
  failed: number
  batches: number
  batch_dirs: string[]
  errors?: string[]
}

export interface ResetUnclassifiedResponse {
  success: boolean
  movedCount: number
  failedCount: number
  stats: Stats
}

export async function resetToUnclassified(): Promise<ResetUnclassifiedResponse> {
  const res = await fetch('/api/photos/reset-unclassified', { method: 'POST' })
  return parseJSON<ResetUnclassifiedResponse>(res)
}

export async function exportLiked(
  destination: string,
  batchSize = 30,
  maxBytes = 15 * 1024 * 1024,
): Promise<ExportLikedResponse> {
  const res = await fetch('/api/export/liked', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      destination,
      batch_size: batchSize,
      max_bytes: maxBytes,
    }),
  })
  return parseJSON<ExportLikedResponse>(res)
}

export async function undoLast(): Promise<UndoResponse> {
  const res = await fetch('/api/undo', { method: 'POST' })
  return parseJSON<UndoResponse>(res)
}

export async function fetchExif(filename: string, from?: Bucket): Promise<ExifMetadata> {
  const q = from && from !== 'root' ? `?from=${encodeURIComponent(from)}` : ''
  const res = await fetch(`/api/photos/${encodeURIComponent(filename)}/exif${q}`)
  return parseJSON<ExifMetadata>(res)
}

export async function fetchSession(): Promise<SessionState> {
  const res = await fetch('/api/session')
  return parseJSON<SessionState>(res)
}

export async function saveSession(state: SessionState): Promise<SessionState> {
  const res = await fetch('/api/session', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(state),
  })
  return parseJSON<SessionState>(res)
}
