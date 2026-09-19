# Sorta V1.5 — Library, Review & Advanced Culling

## IMPORTANT CONTEXT

Sorta V1 has ALREADY been implemented.

Do NOT rebuild the project.

Do NOT replace the existing architecture.

Do NOT initialize a new project.

Do NOT rewrite working V1 functionality unless required for V1.5.

First inspect the existing codebase and understand:

* existing Go backend structure
* existing React/Vite frontend structure
* existing API endpoints
* existing photo scanning logic
* existing file movement implementation
* existing undo implementation
* existing keyboard shortcuts
* existing state management
* existing styling/component structure
* existing tests

Then implement V1.5 incrementally on top of the existing implementation.

The existing V1 behavior must continue to work exactly as before.

---

# V1.5 OBJECTIVE

Turn Sorta from a simple one-at-a-time photo culling tool into a small local photo management application.

V1 already supports:

```text
Root directory
     ↓
One photo at a time
     ↓
Like / Dislike / Review
     ↓
Physical file movement
```

V1.5 adds:

```text
                 ┌─────────────┐
                 │    CULL     │
                 └──────┬──────┘
                        │
              ┌─────────┼─────────┐
              ▼         ▼         ▼
           Liked      Review    Disliked
              │         │         │
              └─────────┼─────────┘
                        ▼
                    LIBRARY
                        │
              ┌─────────┼──────────┐
              ▼         ▼          ▼
            Move      Compare    Review
              │
              ▼
             Root
              │
              ▼
             CULL
```

The filesystem remains the source of truth.

Do NOT introduce SQLite in V1.5.

---

# SELECTED V1.5 FEATURES

Implement ONLY these features:

1. Library view
2. Move photos between buckets
3. Multi-select
4. Drag/drop between buckets
5. Dedicated Review mode
6. Fullscreen
7. Compare mode
8. Keyboard-first Library
9. Quick reject
10. Resume where the user left off
11. Session statistics

Do not implement:

* EXIF
* RAW
* AI
* duplicate detection
* burst detection
* ratings
* tags
* collections
* image editing
* export
* cloud sync

---

# 1. FIRST TASK — INSPECT EXISTING V1

Before modifying anything:

1. Inspect the repository.
2. Identify the backend entrypoint.
3. Identify the frontend entrypoint.
4. Identify the existing photo model.
5. Identify existing filesystem helpers.
6. Identify existing scanner.
7. Identify existing file move operation.
8. Identify existing undo implementation.
9. Identify existing keyboard shortcut implementation.
10. Identify how frontend state is currently managed.
11. Identify how images are currently served.
12. Identify existing tests.

Do not assume the architecture described in previous plans exactly matches the current code.

Use the existing implementation as the source of truth.

Before writing code, provide a short summary:

```text
Existing architecture:
Backend:
Frontend:
Current APIs:
Current filesystem behavior:
Current undo behavior:
Current keyboard behavior:
Relevant components:
```

Then implement the features below.

---

# 2. DOMAIN MODEL

The existing application probably already has concepts corresponding to the following.

Preserve the existing types if they already exist.

Otherwise define a bucket/status abstraction.

```typescript
type Bucket =
    | "root"
    | "liked"
    | "review"
    | "disliked";
```

Backend equivalent:

```go
type Bucket string

const (
    BucketRoot     Bucket = "root"
    BucketLiked    Bucket = "liked"
    BucketReview   Bucket = "review"
    BucketDisliked Bucket = "disliked"
)
```

The important conceptual model is:

```text
root      = unclassified
liked     = liked photos
review    = photos requiring another look
disliked  = rejected photos
```

These are physically represented by directories.

---

# 3. FEATURE 1 — LIBRARY VIEW

Add a new top-level application mode.

Existing:

```text
Cull
```

Add:

```text
Library
```

Navigation:

```text
┌─────────────────────────────────────┐
│ Sorta       Cull       Library      │
└─────────────────────────────────────┘
```

Do not introduce a routing library unless the existing project already uses one.

A simple application state is sufficient:

```typescript
type AppMode = "cull" | "library";
```

---

## Library UI

Library should look like a desktop photo browser.

```text
┌────────────────────────────────────────────────────┐
│ Sorta        Cull     Library                      │
├───────────────┬────────────────────────────────────┤
│               │                                    │
│ Liked    243  │     ┌────┐ ┌────┐ ┌────┐ ┌────┐  │
│               │     │    │ │    │ │    │ │    │  │
│ Review    38  │     │ 📷 │ │ 📷 │ │ 📷 │ │ 📷 │  │
│               │     │    │ │    │ │    │ │    │  │
│ Disliked 412  │     └────┘ └────┘ └────┘ └────┘  │
│               │                                    │
│               │     ┌────┐ ┌────┐ ┌────┐ ┌────┐  │
│               │     │ 📷 │ │ 📷 │ │ 📷 │ │ 📷 │  │
│               │     └────┘ └────┘ └────┘ └────┘  │
└───────────────┴────────────────────────────────────┘
```

Sidebar:

```text
Liked
Review
Disliked
```

Each should display the current count.

Clicking a bucket loads its photos.

---

# 4. LIBRARY BACKEND API

Inspect the existing APIs first.

If equivalent functionality already exists, extend it instead of creating duplicate endpoints.

Otherwise add:

```http
GET /api/library/:bucket
```

Allowed buckets:

```text
liked
review
disliked
```

Do not expose arbitrary filesystem paths.

Response:

```json
{
  "bucket": "liked",
  "photos": [
    {
      "name": "DSC00001.JPG",
      "url": "/photos/liked/DSC00001.JPG"
    },
    {
      "name": "DSC00002.JPG",
      "url": "/photos/liked/DSC00002.JPG"
    }
  ]
}
```

Only scan files directly inside the bucket directory.

Do not recursively scan.

Reuse the existing supported-image detection logic.

Do not create a second scanner implementation.

---

# 5. LIBRARY THUMBNAILS

The Library should display photos as a grid.

Use lazy loading.

Do not load every full-resolution image eagerly.

At minimum:

```html
<img loading="lazy" ... />
```

If the existing application already has thumbnail generation, use it.

If not, use the existing image endpoint initially.

Do NOT introduce an image processing pipeline just for V1.5.

Thumbnail generation can be a later optimization.

---

# 6. FEATURE 2 — MOVE PHOTOS BETWEEN BUCKETS

The Library must allow a photo to be moved between:

```text
root
liked
review
disliked
```

Examples:

```text
liked/DSC001.JPG
        ↓
review/DSC001.JPG
```

```text
review/DSC001.JPG
        ↓
root/DSC001.JPG
```

```text
disliked/DSC001.JPG
        ↓
liked/DSC001.JPG
```

All moves must physically move the file.

Use the existing safe file movement implementation if one exists.

---

# 7. MOVE API

Inspect the existing movement endpoint.

If it can safely support arbitrary bucket-to-bucket moves, extend it.

Otherwise add a generic operation such as:

```http
POST /api/photos/move
```

Request:

```json
{
  "filename": "DSC00123.JPG",
  "from": "liked",
  "to": "review"
}
```

The backend must:

1. Validate source bucket.
2. Validate destination bucket.
3. Validate filename.
4. Resolve source path.
5. Resolve destination path.
6. Verify source exists.
7. Verify destination directory exists.
8. Check destination conflict.
9. Move the file.
10. Return the result.

---

# 8. MOVE BACK TO ROOT

A particularly important action is:

```text
Move to Unclassified
```

Example:

```text
liked/DSC00123.JPG
        ↓
DSC00123.JPG
```

The photo should then become eligible for Cull mode again.

Do not create another special implementation for this.

Treat root as a valid bucket.

---

# 9. FILE SAFETY

This application manipulates the user's real photos.

Never silently overwrite a destination file.

If:

```text
liked/DSC001.JPG
```

already exists:

```json
{
  "success": false,
  "error": {
    "code": "DESTINATION_EXISTS",
    "message": "A photo with this name already exists in the destination."
  }
}
```

Do not delete the source.

Do not overwrite the destination.

Do not automatically rename files unless the existing V1 implementation already has an established conflict strategy.

Preserve the existing V1 behavior if one exists.

---

# 10. FEATURE 3 — MULTI-SELECT

Library mode must support selecting multiple photos.

Each tile should have a selection affordance.

Example:

```text
┌──────────┐
│       ☑  │
│          │
│   PHOTO  │
│          │
└──────────┘
```

Selected tiles should have a clear visual state.

Example:

```text
3 photos selected
```

Then show a selection toolbar:

```text
┌───────────────────────────────────────────────┐
│ 3 selected                                    │
│                                               │
│ [ Move to Root ] [ Review ] [ Like ] [ Reject ] │
└───────────────────────────────────────────────┘
```

Do not make the toolbar unnecessarily large.

---

# 11. SELECTION SEMANTICS

Implement:

### Click thumbnail

Open viewer.

### Click selection checkbox

Toggle selection.

### Shift-click

If straightforward with the existing grid implementation, support range selection.

### Ctrl/Cmd-click

Toggle individual selection.

If implementing all of these introduces complexity, prioritize:

1. checkbox selection
2. select all
3. clear selection

Then add range selection.

---

# 12. SELECT ALL

Add:

```text
Select All
```

and:

```text
Clear Selection
```

When all photos are selected:

```text
243 selected
```

Do not create individual HTTP requests for each photo.

Use a bulk endpoint.

---

# 13. BULK MOVE API

Add/reuse:

```http
POST /api/photos/move-bulk
```

Request:

```json
{
  "photos": [
    {
      "filename": "DSC001.JPG",
      "from": "liked"
    },
    {
      "filename": "DSC002.JPG",
      "from": "liked"
    }
  ],
  "to": "review"
}
```

Return per-file results.

Example:

```json
{
  "success": false,
  "movedCount": 2,
  "failedCount": 1,
  "results": [
    {
      "filename": "DSC001.JPG",
      "success": true
    },
    {
      "filename": "DSC002.JPG",
      "success": false,
      "error": "DESTINATION_EXISTS"
    }
  ]
}
```

The frontend must remove/update only successfully moved photos.

---

# 14. BULK MOVE CONFIRMATION

Individual actions:

```text
L
D
R
```

must remain instant.

No confirmation.

Bulk actions should ask for confirmation when moving multiple files.

Example:

```text
Move 87 photos to Review?

This will physically move the files.

[Cancel] [Move 87 Photos]
```

For bulk operations, the user must explicitly confirm.

---

# 15. FEATURE 4 — DRAG & DROP

Allow photos to be dragged onto sidebar buckets.

Example:

```text
                PHOTO
                  │
                  │ drag
                  ▼
             ┌─────────┐
             │ Review  │
             │   38    │
             └─────────┘
```

Dropping the photo onto:

```text
Liked
Review
Disliked
```

moves it there.

The drag/drop operation must call the same backend move operation.

Do NOT create separate filesystem logic for drag/drop.

---

# 16. DRAG MULTIPLE PHOTOS

If multiple photos are selected:

```text
5 photos selected
```

and the user drags one of them onto Review:

```text
all 5 selected photos
        ↓
      Review
```

This should trigger the same bulk move flow.

Require confirmation for multi-photo drag/drop.

Single-photo drag/drop does not need confirmation.

---

# 17. FEATURE 5 — DEDICATED REVIEW MODE

Review deserves a dedicated mode because these are undecided photos.

From Library:

```text
Review (38)

[ Review Photos ]
```

Clicking this opens a one-photo-at-a-time review experience.

Example:

```text
┌───────────────────────────────────────┐
│ Review — 17 / 38                     │
│                                       │
│                                       │
│                PHOTO                  │
│                                       │
│                                       │
│                                       │
│        [ Reject ]    [ Keep ]         │
│                                       │
│               Undo                    │
└───────────────────────────────────────┘
```

---

# 18. REVIEW MODE ACTIONS

Since the photo is already in Review, don't show "Review" as an action.

Primary actions:

```text
Keep
Reject
```

Map them to:

```text
Keep
    review → liked

Reject
    review → disliked
```

Optional:

```text
Unclassify
    review → root
```

but keep it secondary.

---

# 19. REVIEW KEYBOARD SHORTCUTS

Use:

```text
L → Keep
D → Reject

→ → Keep
← → Reject

Backspace → Undo
Cmd/Ctrl + Z → Undo
```

Do not use `R` as an action in Review mode.

The photo is already in Review.

---

# 20. REVIEW NAVIGATION

After a successful move:

```text
review/DSC001.JPG
        ↓
liked/DSC001.JPG
        ↓
next review photo
```

Do not reload the entire review list.

Remove the current photo from the in-memory queue and display the next one.

---

# 21. FEATURE 6 — FULLSCREEN

Add fullscreen support to the existing PhotoViewer.

Use the browser Fullscreen API.

Add:

```text
⛶
```

button.

Keyboard shortcut:

```text
F
```

Expected behavior:

```text
F
 ↓
fullscreen
```

and:

```text
Escape
 ↓
exit fullscreen
```

The photo should continue using:

```css
object-fit: contain;
```

Do not crop the image.

---

# 22. FULLSCREEN UI

Normal mode:

```text
Photo
controls
filename
progress
```

Fullscreen:

```text
                    PHOTO
```

Controls should appear subtly on hover or when moving the mouse.

Keyboard shortcuts must continue working in fullscreen.

---

# 23. FEATURE 7 — COMPARE MODE

Add a manual comparison feature.

The user selects 2–4 photos in Library.

Then:

```text
[ Compare ]
```

becomes available.

Clicking it opens:

```text
┌────────────┐ ┌────────────┐ ┌────────────┐
│            │ │            │ │            │
│     A      │ │     B      │ │     C      │
│            │ │            │ │            │
└────────────┘ └────────────┘ └────────────┘
```

The initial version does NOT need automatic similarity detection.

The user explicitly chooses the photos to compare.

---

# 24. COMPARE MODE

Requirements:

* display 2–4 selected photos
* preserve aspect ratio
* allow exiting compare mode
* allow opening a photo larger
* show filenames
* keep images aligned as much as practical

Example:

```text
Compare

[ DSC001.JPG ] [ DSC002.JPG ] [ DSC003.JPG ]

                [ Exit Compare ]
```

---

# 25. COMPARE ACTIONS

Initially keep actions simple.

Allow the user to:

```text
Open / zoom photo
```

and then return to Library.

Optionally provide:

```text
Move selected photo to Liked
Move selected photo to Review
Move selected photo to Disliked
Move selected photo to Root
```

Do not introduce an automatic:

```text
"Choose best"
```

workflow yet.

The user should explicitly decide what happens.

---

# 26. FEATURE 8 — KEYBOARD-FIRST LIBRARY

The Library should be usable without constantly reaching for the mouse.

Maintain a concept of:

```text
focused photo
```

and:

```text
selected photos
```

These are different.

Example:

```text
┌──────┐ ┌──────┐ ┌──────┐
│      │ │  ◉   │ │      │
│  A   │ │  B   │ │  C   │
└──────┘ └──────┘ └──────┘
          focused
```

Arrow keys move focus.

Space toggles selection.

---

# 27. LIBRARY KEYBOARD SHORTCUTS

Implement:

```text
Arrow keys
    Move focus

Space
    Select / deselect focused photo

Enter
    Open focused photo

L
    Move focused/selected photo(s) to Liked

D
    Move focused/selected photo(s) to Disliked

R
    Move focused/selected photo(s) to Review

U
    Move focused/selected photo(s) to Root

X
    Quick reject

Backspace
    Undo

Cmd/Ctrl + Z
    Undo

F
    Fullscreen
```

If no photos are selected:

```text
L
```

acts on the focused photo.

If photos are selected:

```text
L
```

acts on all selected photos.

This makes keyboard behavior predictable.

---

# 28. KEYBOARD SAFETY

Do not trigger shortcuts when:

* typing in an input
* typing in a search field
* interacting with a text area

Create a reusable keyboard shortcut hook if the existing code doesn't already have one.

Do not duplicate keyboard event handling across components.

---

# 29. FEATURE 9 — QUICK REJECT

Add:

```text
X
```

as a quick rejection shortcut.

In Cull mode:

```text
X
 ↓
root → disliked
```

It should behave exactly like Dislike.

No confirmation.

Do NOT delete the file.

Important:

```text
X = move to disliked
X ≠ permanent delete
```

---

# 30. QUICK REJECT IN LIBRARY

In Library:

```text
X
```

moves the focused photo to:

```text
disliked/
```

If multiple photos are selected:

```text
X
```

moves the selected photos to Disliked.

For multiple photos, use the bulk operation confirmation.

---

# 31. FEATURE 10 — RESUME WHERE YOU LEFT OFF

The filesystem already provides the basic resume behavior.

Classified files have moved out of root.

Therefore on restart:

```text
root
```

contains only unclassified photos.

Do not change this behavior.

---

# 32. PERSIST LAST PHOTO

Add lightweight session state.

Do NOT introduce SQLite.

Use a small file under:

```text
.sorta/
```

For example:

```text
.sorta/
└── session.json
```

Example:

```json
{
  "mode": "cull",
  "lastPhoto": "DSC00832.JPG",
  "lastBucket": "review"
}
```

This state is convenience metadata only.

The filesystem remains authoritative.

---

# 33. SESSION FILE SAFETY

The `.sorta` directory is application metadata.

Do not scan it as photos.

Do not expose it through the image-serving endpoint.

Do not put image bytes into session.json.

Do not store large amounts of data there.

---

# 34. RESUME LOGIC

When the application starts:

1. Scan root.
2. Load session state if available.
3. Check whether `lastPhoto` still exists.
4. If it exists, resume around that photo.
5. If it doesn't exist, gracefully select the first remaining photo.

Do not fail startup because the previous photo is gone.

For example:

```text
Previous:
DSC00832.JPG

But user manually deleted/moved it.

Startup:
→ find first valid remaining photo
```

---

# 35. LIBRARY RESUME

If the user was in Library:

```json
{
  "mode": "library",
  "lastBucket": "liked",
  "lastPhoto": "DSC00832.JPG"
}
```

restore:

```text
Library
  ↓
Liked
  ↓
DSC00832.JPG focused
```

If the photo no longer exists, open the bucket normally and focus the first available photo.

---

# 36. FEATURE 11 — SESSION STATISTICS

Add a lightweight session statistics system.

Track only the current application session.

Example:

```text
Session

Reviewed       382
Liked          124
Disliked       213
Review           45
```

The counts represent actions performed during the current application process.

Do NOT attempt to reconstruct historical sessions.

---

# 37. DECISION TIME

For Cull and Review modes, track:

```text
photo displayed timestamp
action timestamp
```

Calculate decision duration.

Example:

```text
Average decision time
1.7 seconds
```

This is optional UI information.

Do not make it a gamification system.

Do not send it anywhere.

---

# 38. SESSION STATISTICS UI

Add a lightweight stats area.

Potential header:

```text
Reviewed: 382
Liked: 124
Disliked: 213
Review: 45
```

Or a small Stats panel.

Do not allow statistics to dominate the photo viewer.

---

# 39. BACKEND STATS

If V1 already has a stats/count endpoint, extend it.

Otherwise add:

```http
GET /api/stats
```

Return filesystem-derived totals:

```json
{
  "root": 591,
  "liked": 243,
  "review": 38,
  "disliked": 412
}
```

These represent current filesystem state.

Do not confuse these with session statistics.

There are therefore two types of stats:

### Library stats

Current number of files in each bucket.

### Session stats

Actions performed during the current application session.

---

# 40. ACTION HISTORY

Reuse the existing V1 undo mechanism if possible.

Every new movement must participate in the same undo stack:

```text
root → liked
liked → review
review → root
```

Library actions and Cull actions should share the same underlying movement/undo mechanism.

Do NOT create two independent undo systems.

---

# 41. BULK UNDO

A bulk move should be one logical undo operation.

Example:

```text
A → Review
B → Review
C → Review
```

One:

```text
Cmd/Ctrl + Z
```

should restore:

```text
A → Liked
B → Liked
C → Liked
```

assuming all three succeeded.

If one restoration fails, report the individual failure.

---

# 42. PHOTO VIEWER CONTEXT

Extend the existing viewer to understand its context.

Conceptually:

```typescript
type ViewerContext =
    | "cull"
    | "library"
    | "review";
```

In Library:

```text
Previous / Next
```

should navigate through the currently selected bucket.

Example:

```text
Liked — 32 / 243
```

In Review:

```text
Review — 17 / 38
```

In Cull:

```text
Unclassified — 324 / 1284
```

---

# 43. IMPORTANT FRONTEND STATE RULE

Do not treat frontend state as the source of truth.

Flow:

```text
User action
    ↓
API request
    ↓
Filesystem operation
    ↓
success
    ↓
update frontend state
```

NOT:

```text
User action
    ↓
update frontend
    ↓
hope filesystem move succeeds
```

If a move fails:

```text
keep photo visible
show error
do not advance
```

---

# 44. LIBRARY STATE

Use whatever state architecture already exists.

If no abstraction exists, something similar to:

```typescript
interface LibraryState {
    bucket: Bucket;
    photos: Photo[];
    focusedIndex: number;
    selectedPhotos: Set<string>;
}
```

is sufficient.

Do not introduce Redux/Zustand/etc. solely for V1.5 unless the existing application already uses one.

---

# 45. PERFORMANCE

Target:

```text
1,000–5,000 photos
```

in a normal directory.

Library must:

* lazy-load images
* avoid eager loading every image
* avoid rendering unnecessary full-resolution images
* avoid re-fetching the entire library after every move

After moving a single photo:

```text
remove it from current UI
update count
```

Do not reload the entire application.

---

# 46. API REQUEST OPTIMIZATION

Single movement:

```text
1 API request
```

Bulk movement:

```text
1 bulk API request
```

Do NOT do:

```text
50 selected photos
→ 50 HTTP requests
```

for a bulk action.

---

# 47. ERROR HANDLING

All filesystem errors should be presented clearly.

Example:

```text
Couldn't move photo

A file with this name already exists in Review.
```

Use a non-blocking toast/banner where possible.

Do not use modal dialogs for ordinary single-photo errors.

For bulk operations, provide a summary:

```text
Moved 47 photos.
3 photos could not be moved.

[View errors]
```

---

# 48. EMPTY STATES

Library bucket empty:

```text
No photos here.

Photos you move to this bucket will appear here.
```

Review empty:

```text
No photos need review.

You're all caught up.
```

Cull empty:

```text
🎉 All photos have been classified.
```

---

# 49. IMPLEMENTATION ORDER

Implement V1.5 in these phases.

## Phase A — Codebase understanding

Inspect V1.

Do not modify functionality yet.

Identify reusable:

* scanner
* filesystem functions
* move functions
* undo
* viewer
* keyboard handling
* stats

---

## Phase B — Library

Implement:

```text
Library navigation
Bucket sidebar
Library API
Grid
Photo tiles
Viewer integration
Bucket counts
```

Verify.

---

## Phase C — Movement

Implement:

```text
Single photo movement
Move to root
Bucket-to-bucket movement
Destination conflict handling
```

Verify.

---

## Phase D — Multi-select

Implement:

```text
Selection
Select All
Clear Selection
Bulk Move API
Bulk Move UI
Confirmation
```

Verify.

---

## Phase E — Drag/drop

Implement:

```text
Single photo drag/drop
Multi-photo drag/drop
Bucket drop targets
```

Verify.

---

## Phase F — Review Mode

Implement:

```text
Dedicated review mode
Keep
Reject
Review keyboard shortcuts
Navigation
Undo
```

Verify.

---

## Phase G — Fullscreen + keyboard

Implement:

```text
Fullscreen
F
Library keyboard navigation
Space
Enter
L
D
R
U
X
Undo
```

Verify.

---

## Phase H — Compare

Implement:

```text
Select 2–4
Compare
Side-by-side view
Exit compare
```

Verify.

---

## Phase I — Resume + statistics

Implement:

```text
.sorta/session.json
Last photo
Last mode
Last bucket
Session statistics
Decision timing
```

Verify.

---

# 50. TESTING REQUIREMENTS

Do not rely solely on manual testing.

Extend existing tests.

## Backend

Test:

```text
GET library
GET stats

root → liked
root → review
root → disliked

liked → root
liked → review
liked → disliked

review → root
review → liked
review → disliked

disliked → root
disliked → liked
disliked → review
```

Also test:

```text
destination conflict
missing source
invalid bucket
invalid filename
path traversal
```

---

# 51. BULK TESTS

Test:

```text
all successful
partial failure
all failed
```

Verify the response accurately identifies each file.

---

# 52. UNDO TESTS

Test:

```text
single Cull action → undo
single Library move → undo
Review action → undo
bulk move → undo
multiple operations → multiple undo
```

Verify the actual filesystem state after each operation.

---

# 53. SESSION TESTS

Test:

```text
save last photo
restore last photo
missing last photo
invalid session file
missing session file
```

A corrupt/missing session file must not prevent Sorta from starting.

---

# 54. BACKWARD COMPATIBILITY

After V1.5:

The existing V1 workflow must still work:

```text
Start Sorta
↓
Photo appears
↓
L
↓
Photo physically moves to liked
↓
Next photo
```

Also:

```text
D
↓
disliked
```

```text
R
↓
review
```

```text
Undo
↓
photo restored
```

Do not regress this behavior.

---

# 55. UX PRINCIPLE

Sorta has two distinct workflows.

### Fast Culling

```text
One photo
     ↓
keyboard
     ↓
next photo
```

Optimized for speed.

### Library Management

```text
Grid
 ↓
select
 ↓
move
 ↓
compare
 ↓
review
```

Optimized for organization.

Do not let Library functionality clutter the Cull screen.

---

# 56. V1.5 FINAL EXPERIENCE

The complete workflow should feel like:

```text
             IMPORT PHOTOS
                   │
                   ▼
              ┌─────────┐
              │  CULL   │
              └────┬────┘
                   │
       ┌───────────┼────────────┐
       ▼           ▼            ▼
    LIKED        REVIEW      DISLIKED
       │           │
       │           ▼
       │       REVIEW MODE
       │           │
       │      ┌────┴────┐
       │      ▼         ▼
       │    LIKED    DISLIKED
       │
       └──────────┬────────────┘
                  ▼
              LIBRARY
                  │
        ┌─────────┼─────────┐
        ▼         ▼         ▼
      SELECT    COMPARE    MOVE
                            │
                            ▼
                           ROOT
                            │
                            ▼
                           CULL
```

---

# 57. V1.5 DEFINITION OF DONE

A user should be able to:

### Cull

```text
L / D / R / X
```

without touching the mouse.

### Browse

Open:

```text
Library → Liked
Library → Review
Library → Disliked
```

and see a thumbnail grid.

### Correct

Move:

```text
Liked → Review
Liked → Root
Review → Liked
Review → Disliked
Disliked → Root
```

and any other valid bucket transition.

### Bulk manage

Select:

```text
1 photo
10 photos
100 photos
```

and move them together.

### Drag

Drag photos onto bucket targets.

### Review

Open Review mode and rapidly decide:

```text
Keep / Reject
```

### Fullscreen

Press:

```text
F
```

and inspect the photo fullscreen.

### Compare

Select 2–4 photos and view them side-by-side.

### Keyboard

Navigate and classify Library photos without requiring a mouse.

### Quick reject

Press:

```text
X
```

to move the current photo to Disliked.

### Resume

Close and reopen Sorta and continue with the remaining photos.

### Statistics

See both:

```text
Current filesystem counts
```

and:

```text
Current session statistics
```

---

# 58. FINAL INSTRUCTION TO CURSOR

Do not blindly implement all features in one pass.

Work incrementally.

For each phase:

1. Inspect existing implementation.
2. Reuse existing abstractions.
3. Make the smallest necessary change.
4. Implement tests.
5. Run backend tests.
6. Run frontend tests/build.
7. Verify that existing V1 behavior still works.
8. Only then proceed to the next phase.

Do not introduce a database.

Do not rewrite working V1 code unnecessarily.

Do not introduce a new framework unless the current codebase genuinely requires it.

The highest priority remains:

> Never lose, overwrite, or silently delete a user's photograph.

The filesystem remains the source of truth.
