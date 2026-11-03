/** Shared across Cull / Library so directory warmup runs once per page session. */
let directoryPreviewsReady = false

export function areDirectoryPreviewsReady(): boolean {
  return directoryPreviewsReady
}

export function markDirectoryPreviewsReady(): void {
  directoryPreviewsReady = true
}

export function resetDirectoryPreviewsReady(): void {
  directoryPreviewsReady = false
}
