import { useCallback, useEffect, useRef, useState } from 'react'

const ESC_HOLD_MS = 550

type KeyboardAPI = {
  lock: (keys?: string[]) => Promise<void>
  unlock: () => void
}

function getKeyboard(): KeyboardAPI | undefined {
  return (navigator as Navigator & { keyboard?: KeyboardAPI }).keyboard
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  const tag = target.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || target.isContentEditable
}

/** Fullscreen with hold-Esc to exit so a tap can close modals first. */
export function useFullscreen() {
  const [fullscreen, setFullscreen] = useState(false)
  const [escHolding, setEscHolding] = useState(false)
  const cssModeRef = useRef(false)
  const escTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const clearEscTimer = useCallback(() => {
    if (escTimerRef.current !== null) {
      clearTimeout(escTimerRef.current)
      escTimerRef.current = null
    }
    setEscHolding(false)
  }, [])

  const exitFullscreen = useCallback(async () => {
    clearEscTimer()
    try {
      getKeyboard()?.unlock()
    } catch {
      /* ignore */
    }
    cssModeRef.current = false
    document.body.classList.remove('sorta-css-fullscreen')
    if (document.fullscreenElement) {
      try {
        await document.exitFullscreen()
      } catch {
        /* ignore */
      }
    }
    setFullscreen(false)
  }, [clearEscTimer])

  const enterFullscreen = useCallback(async () => {
    const keyboard = getKeyboard()

    if (document.documentElement.requestFullscreen) {
      try {
        await document.documentElement.requestFullscreen()
        if (keyboard?.lock) {
          try {
            await keyboard.lock(['Escape'])
            cssModeRef.current = false
            document.body.classList.remove('sorta-css-fullscreen')
            setFullscreen(true)
            return
          } catch {
            try {
              await document.exitFullscreen()
            } catch {
              /* ignore */
            }
          }
        } else {
          try {
            await document.exitFullscreen()
          } catch {
            /* ignore */
          }
        }
      } catch {
        /* fall through to CSS mode */
      }
    }

    // CSS fullscreen when native Esc cannot be locked (Safari / older browsers).
    cssModeRef.current = true
    document.body.classList.add('sorta-css-fullscreen')
    setFullscreen(true)
  }, [])

  const toggleFullscreen = useCallback(() => {
    if (fullscreen || document.fullscreenElement || cssModeRef.current) {
      void exitFullscreen()
    } else {
      void enterFullscreen()
    }
  }, [fullscreen, enterFullscreen, exitFullscreen])

  useEffect(() => {
    const onFs = () => {
      if (document.fullscreenElement) {
        setFullscreen(true)
        return
      }
      if (cssModeRef.current) return
      try {
        getKeyboard()?.unlock()
      } catch {
        /* ignore */
      }
      setFullscreen(false)
      clearEscTimer()
    }
    document.addEventListener('fullscreenchange', onFs)
    return () => document.removeEventListener('fullscreenchange', onFs)
  }, [clearEscTimer])

  useEffect(() => {
    if (!fullscreen) {
      clearEscTimer()
      return
    }

    const onDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      if (isTypingTarget(e.target)) return
      e.preventDefault()
      if (e.repeat) return
      clearEscTimer()
      setEscHolding(true)
      escTimerRef.current = setTimeout(() => {
        escTimerRef.current = null
        setEscHolding(false)
        void exitFullscreen()
      }, ESC_HOLD_MS)
    }

    const onUp = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      clearEscTimer()
    }

    window.addEventListener('keydown', onDown, true)
    window.addEventListener('keyup', onUp, true)
    window.addEventListener('blur', clearEscTimer)
    return () => {
      window.removeEventListener('keydown', onDown, true)
      window.removeEventListener('keyup', onUp, true)
      window.removeEventListener('blur', clearEscTimer)
      clearEscTimer()
    }
  }, [fullscreen, exitFullscreen, clearEscTimer])

  useEffect(() => {
    return () => {
      document.body.classList.remove('sorta-css-fullscreen')
      try {
        getKeyboard()?.unlock()
      } catch {
        /* ignore */
      }
    }
  }, [])

  return { fullscreen, escHolding, toggleFullscreen, exitFullscreen }
}
