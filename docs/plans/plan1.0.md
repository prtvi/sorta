# Local Sorta — Product & Engineering Specification

Build a local-first photo segregation application for quickly reviewing and classifying photos from a camera.

The application should feel like a lightweight desktop utility, but the UI should run in a browser. Photos must NEVER be uploaded to any external service. All files remain on the user's local filesystem.

The primary interaction should be extremely fast: open a directory containing photos, view one photo at a time, and classify each photo using keyboard shortcuts or buttons.

---

# 1. Product Goal

The application solves this workflow:

1. User has a directory containing photos from their camera.
2. User starts the application against that directory.
3. Application scans the directory for supported image files.
4. It creates three subdirectories:

   * `liked/`
   * `disliked/`
   * `review/`
5. It displays one photo at a time.
6. User classifies the photo as:

   * Like
   * Dislike
   * Review
7. The application physically moves the file into the corresponding directory.
8. The next photo appears immediately.
9. User can undo the most recent classification.
10. User can see progress and classification counts.

The experience should be optimized for processing hundreds or thousands of photos quickly.

Think of the interaction model as:

> Tinder for photos.

The user should spend almost all of their time looking at the photo and pressing a key.

---

# 2. Important Product Principles

Prioritize these in order:

1. Local filesystem safety
2. Fast photo browsing
3. Minimal UI
4. Keyboard-first interaction
5. Reliable file operations
6. Undo
7. Good visual presentation
8. Extensibility

Do NOT overengineer the initial version.

There should be:

* No database
* No authentication
* No cloud storage
* No external API
* No user accounts
* No analytics
* No unnecessary backend state

The filesystem is the source of truth.

---

# 3. Recommended Technology Stack

## Backend

Use:

* Go
* Go standard library wherever possible
* `net/http`
* `os`
* `filepath`
* `io`
* `encoding/json`
* `mime`

Avoid unnecessary frameworks unless there is a compelling reason.

The backend is responsible for:

* filesystem access
* directory scanning
* image serving
* moving files
* undo
* application configuration
* serving the frontend production build

---

# Frontend

Use:

* React
* TypeScript
* Vite
* CSS/Tailwind if useful

Do NOT use Next.js.

There is no need for SSR, routing infrastructure, server components, etc.

The frontend is essentially a single-page application.

---

# 4. Application Startup

The application should eventually be usable like:

```bash
sorta /path/to/photos
```

Example:

```bash
sorta ~/Pictures/everest
```

The supplied directory is the working directory.

The application should:

1. Validate that the directory exists.
2. Validate that it is a directory.
3. Create:

```text
liked/
disliked/
review/
```

if they do not exist.
4. Scan the working directory.
5. Find supported image files.
6. Start the HTTP server.
7. Serve the React application.
8. Ideally open the browser automatically.

For development, it is acceptable to run frontend and backend separately.

For production, the React build should ideally be embedded into the Go binary using Go's `embed` functionality.

---

# 5. Directory Structure

Given:

```text
~/Pictures/everest/
```

containing:

```text
DSC00001.JPG
DSC00002.JPG
DSC00003.JPG
DSC00004.JPG
```

the application should create:

```text
everest/
├── DSC00001.JPG
├── DSC00002.JPG
├── DSC00003.JPG
├── DSC00004.JPG
│
├── liked/
├── disliked/
└── review/
```

Only photos in the root working directory should initially be considered for classification.

Do not recursively scan `liked`, `disliked`, or `review`.

This prevents already-classified photos from appearing again.

---

# 6. Supported Image Formats

Initial version:

```text
.jpg
.jpeg
.png
.webp
```

Matching should be case-insensitive:

```text
.JPG
.JPEG
.PNG
```

should all work.

Future support:

```text
.ARW
.CR2
.CR3
.NEF
.RAF
.DNG
```

RAW files should NOT be implemented in V1 unless straightforward.

RAW support will likely require preview/thumbnail generation.

---

# 7. Backend Architecture

Use a simple layered structure.

Suggested structure:

```text
sorta/
│
├── cmd/
│   └── sorta/
│       └── main.go
│
├── internal/
│   ├── scanner/
│   │   └── scanner.go
│   │
│   ├── filesystem/
│   │   └── filesystem.go
│   │
│   ├── server/
│   │   ├── server.go
│   │   ├── handlers.go
│   │   └── middleware.go
│   │
│   └── models/
│       └── models.go
│
├── web/
│   ├── src/
│   ├── package.json
│   └── vite.config.ts
│
├── go.mod
└── README.md
```

Keep this structure flexible. Do not create abstractions that don't provide value.

---

# 8. Core Backend Models

Define something conceptually similar to:

```go
type Photo struct {
    Name string `json:"name"`
    URL  string `json:"url"`
}
```

For actions:

```go
type PhotoAction string

const (
    ActionLiked    PhotoAction = "liked"
    ActionDisliked PhotoAction = "disliked"
    ActionReview   PhotoAction = "review"
)
```

Request:

```go
type ActionRequest struct {
    Action PhotoAction `json:"action"`
}
```

Undo information:

```go
type ActionRecord struct {
    Filename     string
    OriginalPath string
    Destination string
    Action       PhotoAction
}
```

The undo history can initially live in memory.

No database is necessary.

---

# 9. Backend APIs

Keep the API small.

## GET /api/photos

Return all currently unclassified photos.

Example:

```json
{
  "photos": [
    {
      "name": "DSC00001.JPG",
      "url": "/photos/DSC00001.JPG"
    },
    {
      "name": "DSC00002.JPG",
      "url": "/photos/DSC00002.JPG"
    }
  ]
}
```

The order should be deterministic.

Prefer alphabetical filename order initially.

Future possibility: sort by EXIF capture timestamp.

---

# GET /api/photos/:filename

Serve the actual image file.

Do not convert it to base64.

The browser should receive the actual image bytes.

Example:

```text
GET /photos/DSC00001.JPG
```

The backend should safely resolve the filename against the configured working directory.

IMPORTANT:

Do not allow path traversal.

Requests such as:

```text
/photos/../../some-file
```

must never allow access outside the configured working directory.

---

# POST /api/photos/:filename/action

Request:

```json
{
  "action": "liked"
}
```

Possible actions:

```text
liked
disliked
review
```

Backend should:

1. Validate the filename.
2. Verify that the source file exists.
3. Validate the action.
4. Ensure destination directory exists.
5. Move the file using filesystem rename.
6. Record the action for undo.
7. Return success and optionally the next photo.

Example response:

```json
{
  "success": true,
  "photo": "DSC00001.JPG",
  "action": "liked"
}
```

---

# POST /api/undo

Undo the most recent action.

If:

```text
DSC00001.JPG
```

was moved to:

```text
liked/DSC00001.JPG
```

undo should move it back to:

```text
DSC00001.JPG
```

If there is no action to undo:

```json
{
  "success": false,
  "reason": "nothing_to_undo"
}
```

Initially maintain an in-memory stack:

```go
[]ActionRecord
```

Every classification pushes a record.

Undo pops the last record.

---

# 10. File Movement Safety

File movement is the most important backend operation.

Use:

```go
os.Rename()
```

where possible.

Before moving:

```text
source exists?
destination directory exists?
destination filename already exists?
```

Handle conflicts safely.

For example, if:

```text
liked/DSC00001.JPG
```

already exists, DO NOT silently overwrite it.

Possible strategy:

```text
DSC00001.JPG
DSC00001 (1).JPG
DSC00001 (2).JPG
```

However, investigate whether a safer behavior is preferable.

The key requirement is:

> Never lose or overwrite a user's photo accidentally.

---

# 11. Frontend Layout

The application should have a minimal dark UI.

Conceptually:

```text
┌────────────────────────────────────────────────────┐
│                                                    │
│  Sorta                         324 / 1284  │
│                                                    │
│                                                    │
│                                                    │
│                  ┌───────────────┐                 │
│                  │               │                 │
│                  │               │                 │
│                  │     PHOTO     │                 │
│                  │               │                 │
│                  │               │                 │
│                  └───────────────┘                 │
│                                                    │
│              DSC01234.JPG                          │
│                                                    │
│     [ Dislike ]    [ Review ]    [ Like ]         │
│                                                    │
│             [ Undo ]                               │
│                                                    │
│  Disliked: 213   Review: 48   Liked: 124           │
│                                                    │
└────────────────────────────────────────────────────┘
```

The image should dominate the UI.

Avoid unnecessary panels.

---

# 12. Photo Viewer

The photo should:

* fit inside the available viewport
* preserve aspect ratio
* never be stretched
* never overflow the viewport
* have a dark background
* preferably use `object-fit: contain`

The user should be able to see the entire image.

Example:

```css
img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
}
```

Do not crop photos in the primary viewer.

---

# 13. Keyboard Controls

Keyboard interaction is the most important feature.

Recommended mappings:

```text
L → Like
D → Dislike
R → Review
```

Also support arrow keys:

```text
→ → Like
← → Dislike
↓ → Review
```

Undo:

```text
Backspace → Undo
Ctrl/Cmd + Z → Undo
```

Avoid mapping actions to keys that conflict with browser functionality.

Keyboard shortcuts should work whenever the user is not typing into a text input.

---

# 14. Button Interaction

Buttons should exist for discoverability.

Three primary buttons:

```text
DISLIKE
REVIEW
LIKE
```

They should be visually distinct.

But keyboard shortcuts should be the fastest path.

The user should be able to process hundreds of images without touching the mouse.

---

# 15. Progress

Show:

```text
324 / 1284
```

meaning:

```text
324 photos processed
1284 total photos initially
```

Also display:

```text
Liked      124
Disliked   213
Review      48
Remaining  899
```

Counts should reflect actual filesystem state where practical.

The frontend should not assume that its state is the sole source of truth.

---

# 16. Photo Navigation State

Frontend state might look like:

```typescript
interface Photo {
    name: string;
    url: string;
}

interface Stats {
    total: number;
    remaining: number;
    liked: number;
    disliked: number;
    review: number;
}
```

Main state:

```typescript
const [photos, setPhotos] = useState<Photo[]>([]);
const [currentIndex, setCurrentIndex] = useState(0);
const [stats, setStats] = useState<Stats>();
```

When a photo is classified:

1. Call backend.
2. On success, remove it from the current frontend queue.
3. Display the next photo.
4. Update counters.
5. Add an undo record locally if useful.

The backend remains authoritative about whether the move actually succeeded.

---

# 17. Important UX Behavior After Classification

Suppose:

```text
photos:

A
B  ← current
C
D
```

User likes B.

Backend moves:

```text
B → liked/B
```

Frontend immediately becomes:

```text
A
C  ← current
D
```

Do NOT reload the entire page.

Do NOT request the entire photo list again after every action.

The transition should feel instantaneous.

---

# 18. Tinder-like Animation

Implement this after the basic workflow works.

When the user clicks Like:

```text
photo
   \
    \
     →→→
```

Animate the photo leaving the screen toward the right.

Dislike:

```text
      photo
     /
    /
←←←
```

Review:

```text
photo
  ↓
  ↓
```

Then display the next image.

The animation should be short, around:

```text
200–300ms
```

Do not let animation delay file operations unnecessarily.

---

# 19. Drag/Swipe Interaction

Future enhancement:

Allow the user to physically drag the image.

Conceptually:

```text
drag right → Like
drag left  → Dislike
drag down  → Review
```

While dragging, show a visual indicator.

For example:

```text
drag right

          LIKE
           ✓
       ┌───────┐
       │ PHOTO │────>
       └───────┘
```

The drag threshold should be configurable.

This should be implemented only after keyboard/button classification is stable.

---

# 20. Undo

Undo is critical.

Support:

```text
Backspace
Ctrl/Cmd + Z
Undo button
```

For V1, support at least one-step undo.

Prefer implementing a stack so multiple undo operations are possible.

Example:

```text
Action 1 → Like A
Action 2 → Dislike B
Action 3 → Review C

Undo → restores C
Undo → restores B
Undo → restores A
```

Potential future behavior:

```text
Redo
```

but do not implement initially unless trivial.

---

# 21. Handling Application Restart

Important question:

The user may classify:

```text
500 photos
```

then close the application.

When they reopen:

```bash
sorta ~/Pictures/trip
```

the already-classified files are in:

```text
liked/
disliked/
review/
```

Therefore they naturally won't appear in the root scan.

No database is required.

This is intentional.

The filesystem itself persists the classification state.

---

# 22. Refresh / External Changes

Eventually the user may modify files manually while the app is running.

For V1:

* detect missing current files gracefully
* do not crash
* if a file disappears, skip it
* optionally provide a Refresh button

Future:

Use filesystem watchers to detect changes.

Potential Go package:

```text
fsnotify
```

but don't introduce it unless necessary.

---

# 23. Image Loading Performance

Do not load every full-resolution image into memory simultaneously.

Initially:

```text
load current image
preload next image
```

This will make navigation feel much faster.

Potential frontend behavior:

```typescript
const nextImage = new Image();
nextImage.src = nextPhoto.url;
```

Only preload one or two upcoming images.

For a large photo collection, avoid rendering thousands of `<img>` elements.

---

# 24. Browser Caching

Use sensible cache headers for images.

Since files are immutable while being reviewed, browser caching can be useful.

However, don't allow stale content after a file is moved.

Since the moved photo leaves the queue, this should naturally not be a major issue.

---

# 25. Fullscreen

Add a fullscreen button.

The user should be able to enter:

```text
fullscreen
```

and see only:

```text
PHOTO
```

with minimal controls.

Keyboard controls should continue working.

Potential shortcut:

```text
F → fullscreen
```

---

# 26. Zoom

Future enhancement:

Allow:

```text
+ → zoom in
- → zoom out
0 → reset
```

Mouse wheel / trackpad zoom can also be supported.

This is useful for deciding whether a photo is actually sharp.

For example:

```text
100%
200%
400%
```

Do not implement zoom before the basic sorter is complete.

---

# 27. EXIF Metadata — Future Scope

A very useful future feature.

Display:

```text
Sony ILCE-6400
16mm
f/8
1/320s
ISO 100
```

Extract EXIF on the backend.

Potential information:

* Camera model
* Lens
* Focal length
* Aperture
* Shutter speed
* ISO
* Date/time
* GPS if present

Do not expose GPS information by default unless explicitly requested.

---

# 28. EXIF-Based Sorting — Future

Once EXIF parsing exists, support filters such as:

```text
ISO > 3200
```

or:

```text
f/2.8
```

or:

```text
16mm–35mm
```

or:

```text
photos from 2026-09-01
```

Possible future UI:

```text
Filters

Camera: Sony A6400
Lens: Any
ISO: Any
Date: Any
Focal length: Any
```

---

# 29. RAW Support — Future

The user may have:

```text
.ARW
```

files from a Sony camera.

Browser support for RAW files is not something to rely on.

Future architecture:

```text
ARW
 ↓
Go backend
 ↓
RAW decoder
 ↓
JPEG preview
 ↓
Browser
```

Generate previews on demand.

Potentially cache them in:

```text
.sorta/
    thumbnails/
```

Do not modify the original RAW files.

---

# 30. Thumbnail Generation — Future

For extremely large images, generate thumbnails.

Instead of:

```text
Browser
 ↓
24MP JPEG
```

use:

```text
Browser
 ↓
thumbnail
```

while optionally loading the full-resolution image when zooming.

Possible cache:

```text
.sorta/
└── cache/
    ├── DSC00001.jpg
    ├── DSC00002.jpg
    └── ...
```

The cache should be safely disposable.

Never mix generated cache files with user photos.

---

# 31. Duplicate Detection — Future

Potential feature:

Detect duplicate or near-duplicate photos.

Example:

```text
DSC00123.JPG
DSC00124.JPG
DSC00125.JPG
```

all nearly identical.

Show them together:

```text
┌────────┐ ┌────────┐ ┌────────┐
│        │ │        │ │        │
│ PHOTO  │ │ PHOTO  │ │ PHOTO  │
│        │ │        │ │        │
└────────┘ └────────┘ └────────┘
```

The user can select the best one.

This would require image hashing, such as perceptual hashes.

Do not implement in V1.

---

# 32. Burst Mode — Future

Camera burst photography is common.

Potential feature:

Detect consecutive photos with similar timestamps and/or visual similarity.

Example:

```text
Burst — 7 photos

[1] [2] [3] [4] [5] [6] [7]

Select best
```

This could make the application much more powerful for photography workflows.

---

# 33. AI Assistance — Future

Potential future feature:

Automatically identify:

* blurry photos
* closed eyes
* poor exposure
* duplicates
* out-of-focus shots
* composition
* faces
* landscapes
* portraits

But keep this optional.

The fundamental application should work completely without AI.

Potential architecture:

```text
Local photo
    ↓
Local model
    ↓
Quality score
    ↓
User makes final decision
```

Do not send private photos to an external AI API unless the user explicitly chooses to.

---

# 34. AI Ranking — Future

Instead of automatically deciding:

```text
GOOD / BAD
```

AI could provide:

```text
Sharpness:       92
Exposure:        87
Composition:     78
Face quality:    95

Overall:         88
```

The user still makes the final decision.

Potential UI:

```text
AI Quality
████████████████░░ 88
```

This should never prevent manual classification.

---

# 35. Sessions / Resume — Future

Potentially support explicit sessions.

Example:

```text
Trip: Valley of Flowers
Progress: 632 / 1,284
```

However, this should not require a database initially.

The current filesystem structure already gives us resume capability.

---

# 36. Export / Collection Management — Future

Potential future functionality:

```text
Create collection
```

For example:

```text
Best Photos
Instagram
Print
Portfolio
```

Instead of physically moving files, this could use:

* symlinks
* hard links
* metadata
* a local database

But this is intentionally outside V1.

---

# 37. Safety Requirements

This application operates directly on user's photos.

Treat file operations as destructive.

Never:

* delete photos automatically
* overwrite existing files silently
* recursively modify arbitrary directories
* follow unsafe paths
* move files outside the configured working directory

The backend should always constrain filesystem operations to the root directory supplied at startup.

---

# 38. Error Handling

If a move fails:

```text
Could not move photo.
```

The current photo should remain visible.

Do NOT advance to the next photo unless the backend confirms that the action succeeded.

Possible errors:

* permission denied
* destination exists
* source disappeared
* filesystem error
* invalid filename

Display a small non-blocking error message.

Do not destroy the browsing flow with modal dialogs for ordinary errors.

---

# 39. API Error Format

Use a consistent format:

```json
{
  "success": false,
  "error": {
    "code": "FILE_MOVE_FAILED",
    "message": "Could not move photo"
  }
}
```

Possible codes:

```text
INVALID_ACTION
PHOTO_NOT_FOUND
FILE_MOVE_FAILED
DESTINATION_EXISTS
INVALID_FILENAME
NOTHING_TO_UNDO
```

---

# 40. CORS / Local Development

Production:

```text
Go
 ├── API
 └── React static files
```

Same origin.

Development:

```text
Vite → localhost:5173
Go   → localhost:8080
```

Configure Vite proxy so frontend can call:

```text
/api/*
```

without manually dealing with CORS.

Do not add broad CORS permissions in production.

---

# 41. Security

Although this is a localhost application, do not assume that security doesn't matter.

Important:

* sanitize filenames
* prevent path traversal
* bind to localhost by default
* do not expose the server publicly
* don't accept arbitrary filesystem paths from HTTP requests
* don't allow API calls to specify arbitrary destination directories

The working directory should be determined when starting the application.

Example:

```bash
sorta ~/Pictures/trip
```

The browser should not be able to change the root directory.

---

# 42. Testing

Backend tests should cover:

## Scanner

* finds JPG
* finds JPEG
* finds PNG
* case insensitive extensions
* ignores directories
* ignores `liked`
* ignores `disliked`
* ignores `review`
* deterministic ordering

## File movement

* moves liked photo
* moves disliked photo
* moves review photo
* handles missing file
* handles destination conflict
* doesn't escape root directory

## Undo

```text
move → undo
```

should restore the original location.

Test multiple actions:

```text
A → liked
B → disliked
C → review

undo → C
undo → B
undo → A
```

## API

Test:

```text
GET /api/photos
GET /photos/:filename
POST /api/photos/:filename/action
POST /api/undo
```

---

# 43. Frontend Testing

At minimum test:

* initial photo loading
* next photo after Like
* next photo after Dislike
* next photo after Review
* keyboard shortcuts
* undo
* loading state
* error state
* empty state

---

# 44. Empty State

If there are no remaining photos:

```text
🎉 You're done!

All photos have been sorted.

Liked: 382
Disliked: 641
Review: 103
```

Buttons:

```text
[ Open liked folder ]
[ Open review folder ]
```

Opening filesystem folders may require a future native integration, so for V1 these buttons can be omitted.

---

# 45. Initial UI States

Handle:

## Loading

```text
Loading photos...
```

## Error

```text
Could not load photos.

[ Retry ]
```

## No photos

```text
No photos found in this directory.
```

## Finished

```text
All photos sorted!
```

---

# 46. Visual Design

Use a dark interface.

Reason:

The user is evaluating photographs, so the UI should stay visually neutral.

Suggested design:

* almost-black background
* minimal borders
* large image
* subtle controls
* large buttons
* no distracting gradients
* no unnecessary cards
* no excessive animations

The photo should always be the visual focus.

---

# 47. Responsive Design

Primary target:

```text
Desktop
```

because the user will process photos on their computer.

But make it reasonably usable on:

* laptop
* large monitor
* tablet

Mobile is not a primary requirement.

---

# 48. Application State Model

Do NOT duplicate too much state between backend and frontend.

Backend owns:

```text
filesystem
```

Frontend owns:

```text
current browsing queue
current index
UI state
```

After classification:

```text
Frontend → API
             ↓
        filesystem move
             ↓
          success
             ↓
Frontend removes photo
```

This is the preferred flow.

---

# 49. Initial Implementation Phases

Implement in this exact order.

## Phase 1 — Backend foundation

Implement:

* CLI argument parsing
* root directory validation
* create `liked`
* create `disliked`
* create `review`
* image scanner
* basic HTTP server

Do not build UI yet.

---

## Phase 2 — API

Implement:

```text
GET /api/photos
GET /photos/:filename
POST /api/photos/:filename/action
POST /api/undo
```

Add backend tests.

---

## Phase 3 — React UI

Implement:

* photo viewer
* loading state
* empty state
* buttons
* progress
* counters

Do not implement fancy animation yet.

---

## Phase 4 — Keyboard controls

Implement:

```text
L
D
R

←
→
↓

Backspace
Ctrl/Cmd + Z
```

Make sure the entire application can be operated without a mouse.

---

## Phase 5 — Undo

Implement:

* action stack
* undo button
* keyboard shortcuts
* safe file restoration

---

## Phase 6 — UX polish

Add:

* transitions
* Tinder-like movement
* fullscreen
* better progress indicator
* better error messages
* loading indicators

---

## Phase 7 — Performance

Add:

* preload next photo
* browser caching
* efficient image serving
* avoid unnecessary React renders

Measure before introducing more complexity.

---

# 50. V1 Definition of Done

V1 is complete when the following works:

```bash
sorta ~/Pictures/my-trip
```

Application opens in browser.

It shows:

```text
DSC00001.JPG
```

User presses:

```text
L
```

and:

```text
DSC00001.JPG
```

is physically moved to:

```text
liked/DSC00001.JPG
```

and the next photo appears immediately.

Similarly:

```text
D → disliked/
R → review/
```

Undo restores the previous photo.

The application survives:

* hundreds of photos
* thousands of photos
* restarting the application
* already partially sorted directories

No photo should ever be lost because of an application error.

---

# 51. Important Engineering Constraint

Do not implement future features prematurely.

The first version should NOT include:

* database
* authentication
* cloud
* AI
* RAW decoding
* duplicate detection
* EXIF filtering
* image editing
* facial recognition
* complex state management
* Electron
* Tauri

Build the smallest useful application first.

The architecture should make those features possible later, but they should not complicate V1.

---

# 52. Potential V2 Roadmap

After V1 is stable:

### V2 — Better photo review

* swipe gestures
* zoom
* fullscreen
* EXIF metadata
* multiple undo
* configurable keyboard shortcuts
* thumbnail/preloading optimization

### V3 — Photographer workflow

* RAW support
* RAW previews
* EXIF filtering
* duplicate detection
* burst detection
* side-by-side comparison

### V4 — Intelligent assistance

* blur detection
* exposure analysis
* duplicate/near-duplicate grouping
* face detection
* AI quality scoring
* smart grouping

### V5 — Photo management

* collections
* tags
* ratings
* search
* saved sessions
* virtual albums
* export workflows

---

# 53. Potential Long-Term Architecture

If the application grows substantially, the architecture could evolve into:

```text
                    ┌──────────────────┐
                    │   React UI       │
                    │                  │
                    │ Viewer           │
                    │ Filters          │
                    │ Collections      │
                    │ Metadata         │
                    └────────┬─────────┘
                             │
                             ▼
                    ┌──────────────────┐
                    │   Go Backend     │
                    │                  │
                    │ Photo Service    │
                    │ File Service     │
                    │ Metadata Service │
                    │ AI Service       │
                    └────────┬─────────┘
                             │
             ┌───────────────┼────────────────┐
             ▼               ▼                ▼
        Filesystem        Cache          Local DB
                                          SQLite
```

But do NOT implement this architecture now.

The V1 architecture should remain:

```text
React
  ↓
Go
  ↓
Filesystem
```

---

# 54. Cursor Instructions

When implementing this project:

1. First inspect the repository.
2. Determine whether a project already exists.
3. If empty, initialize the project using the architecture above.
4. Implement one phase at a time.
5. After each phase, run tests/builds.
6. Do not make large unrelated changes.
7. Keep dependencies minimal.
8. Prefer Go standard library.
9. Keep frontend components small and understandable.
10. Add comments only where they explain non-obvious decisions.
11. Do not implement future-scope functionality unless explicitly requested.
12. Do not change the core filesystem semantics without discussing the implications.

Before implementing major architectural changes, explain the tradeoff.

The most important invariant is:

> The user's original photo files are never accidentally deleted or overwritten.

Start with Phase 1 and Phase 2, then implement the React frontend.

After each phase, verify that the application can actually be run locally.
