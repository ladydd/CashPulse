import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * Scratch-to-reveal balance (lottery-style).
 * Cover is a frosted layer; finger/mouse erases it. After idle, auto-reseals.
 */
export default function BalanceScratch({ value, known = false, emptyText = '暂无余额' }) {
  const wrapRef = useRef(null)
  const canvasRef = useRef(null)
  const drawingRef = useRef(false)
  const lastRef = useRef(null)
  const hideTimerRef = useRef(null)
  const resealTimerRef = useRef(null)
  const [hint, setHint] = useState(true)
  const [resealing, setResealing] = useState(false)

  const paintCover = useCallback(() => {
    const canvas = canvasRef.current
    const wrap = wrapRef.current
    if (!canvas || !wrap) return
    const dpr = Math.min(window.devicePixelRatio || 1, 2)
    const rect = wrap.getBoundingClientRect()
    const w = Math.max(1, Math.floor(rect.width))
    const h = Math.max(1, Math.floor(rect.height))
    canvas.width = Math.floor(w * dpr)
    canvas.height = Math.floor(h * dpr)
    canvas.style.width = `${w}px`
    canvas.style.height = `${h}px`
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    ctx.globalCompositeOperation = 'source-over'

    // Soft metallic / frosted cover matching the hero card
    const g = ctx.createLinearGradient(0, 0, w, h)
    g.addColorStop(0, '#2f4f41')
    g.addColorStop(0.45, '#5f8672')
    g.addColorStop(1, '#243f34')
    ctx.fillStyle = g
    ctx.fillRect(0, 0, w, h)

    // Diagonal sheen stripes
    ctx.save()
    ctx.globalAlpha = 0.12
    ctx.strokeStyle = '#fff'
    ctx.lineWidth = 10
    for (let i = -h; i < w + h; i += 22) {
      ctx.beginPath()
      ctx.moveTo(i, 0)
      ctx.lineTo(i + h, h)
      ctx.stroke()
    }
    ctx.restore()

    // Speckles
    ctx.fillStyle = 'rgba(255,255,255,0.08)'
    for (let i = 0; i < 90; i++) {
      const x = Math.random() * w
      const y = Math.random() * h
      const r = Math.random() * 1.8 + 0.4
      ctx.beginPath()
      ctx.arc(x, y, r, 0, Math.PI * 2)
      ctx.fill()
    }

    // Hint text on the foil
    ctx.fillStyle = 'rgba(255,255,255,0.72)'
    ctx.font = '600 13px "SF Pro Text", "PingFang SC", system-ui, sans-serif'
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    ctx.fillText('用手指划开看余额', w / 2, h / 2)
  }, [])

  const clearTimers = () => {
    if (hideTimerRef.current) {
      clearTimeout(hideTimerRef.current)
      hideTimerRef.current = null
    }
    if (resealTimerRef.current) {
      clearTimeout(resealTimerRef.current)
      resealTimerRef.current = null
    }
  }

  const scheduleReseal = useCallback(() => {
    if (hideTimerRef.current) clearTimeout(hideTimerRef.current)
    // Short peek: reseal soon after user stops scratching.
    hideTimerRef.current = setTimeout(() => {
      setResealing(true)
      resealTimerRef.current = setTimeout(() => {
        paintCover()
        setHint(true)
        setResealing(false)
      }, 320)
    }, 1800)
  }, [paintCover])

  useEffect(() => {
    if (!known) return undefined
    // New balance value → reseal
    clearTimers()
    setResealing(false)
    setHint(true)
    // next frame so layout width is ready
    const id = requestAnimationFrame(() => paintCover())
    return () => {
      cancelAnimationFrame(id)
      clearTimers()
    }
  }, [known, value, paintCover])

  const scratchAt = (clientX, clientY) => {
    const canvas = canvasRef.current
    if (!canvas) return
    const rect = canvas.getBoundingClientRect()
    const x = clientX - rect.left
    const y = clientY - rect.top
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.globalCompositeOperation = 'destination-out'
    ctx.lineJoin = 'round'
    ctx.lineCap = 'round'
    ctx.lineWidth = 32
    ctx.strokeStyle = 'rgba(0,0,0,1)'
    ctx.fillStyle = 'rgba(0,0,0,1)'

    const prev = lastRef.current
    ctx.beginPath()
    if (prev) {
      ctx.moveTo(prev.x, prev.y)
      ctx.lineTo(x, y)
      ctx.stroke()
    }
    ctx.beginPath()
    ctx.arc(x, y, 16, 0, Math.PI * 2)
    ctx.fill()
    lastRef.current = { x, y }
    if (hint) setHint(false)
    scheduleReseal()
  }

  const onPointerDown = (e) => {
    if (!known || resealing) return
    e.preventDefault()
    drawingRef.current = true
    lastRef.current = null
    try {
      e.currentTarget.setPointerCapture?.(e.pointerId)
    } catch {
      /* ignore */
    }
    scratchAt(e.clientX, e.clientY)
  }

  const onPointerMove = (e) => {
    if (!drawingRef.current) return
    e.preventDefault()
    scratchAt(e.clientX, e.clientY)
  }

  const onPointerUp = (e) => {
    drawingRef.current = false
    lastRef.current = null
    try {
      e.currentTarget.releasePointerCapture?.(e.pointerId)
    } catch {
      /* ignore */
    }
  }

  if (!known) {
    return <div className="balance-value bare">{emptyText}</div>
  }

  return (
    <div
      className={`balance-scratch ${hint ? 'is-covered' : 'is-open'} ${resealing ? 'is-resealing' : ''}`}
      ref={wrapRef}
      role="img"
      aria-label={hint ? '余额已遮盖，划开查看' : `余额 ${value}`}
    >
      <div className="balance-value scratch-reveal" aria-hidden={hint}>
        {value}
      </div>
      <canvas
        ref={canvasRef}
        className="balance-scratch-canvas"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onPointerLeave={onPointerUp}
      />
      {resealing ? <div className="balance-reseal-veil" aria-hidden="true" /> : null}
    </div>
  )
}
