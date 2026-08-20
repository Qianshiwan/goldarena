import { useState, useRef, useEffect, useCallback } from 'react'
import { authAPI } from '../services/api'

const W = 320
const H = 160
const PIECE_R = 18
const MAX_D = W - 2 * PIECE_R

// SliderCaptcha shows a draggable puzzle. On success it calls
// onVerified(ticket) with a single-use ticket that the parent must forward to
// /auth/send-code.
export default function SliderCaptcha({ onVerified }) {
  const [data, setData] = useState(null) // { key, image, thumb, thumb_y }
  const [offset, setOffset] = useState(0) // drag distance (0..MAX_D)
  const [verified, setVerified] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const draggingRef = useRef(false)
  const offsetRef = useRef(0)
  const startXRef = useRef(0)
  const trackRef = useRef([])
  const dataRef = useRef(null)
  const onVerifiedRef = useRef(onVerified)
  onVerifiedRef.current = onVerified

  const fetchCaptcha = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const { data: res } = await authAPI.getCaptcha()
      const d = res.data
      dataRef.current = d
      setData(d)
      offsetRef.current = 0
      setOffset(0)
      setVerified(false)
    } catch (e) {
      setError('加载验证码失败，请刷新')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchCaptcha()
  }, [fetchCaptcha])

  const verifyAsync = useCallback(
    async (cur, x) => {
      try {
        const { data: res } = await authAPI.verifyCaptcha({ key: cur.key, x, track: trackRef.current })
        setVerified(true)
        setError('')
        onVerifiedRef.current && onVerifiedRef.current(res.data.ticket)
      } catch (e) {
        setError(e.response?.data?.message || '验证失败，请重试')
        offsetRef.current = 0
        setOffset(0)
        fetchCaptcha()
      }
    },
    [fetchCaptcha],
  )

  const onPointerDown = (e) => {
    if (verified || !data || loading) return
    try {
      e.currentTarget.setPointerCapture(e.pointerId)
    } catch {}
    draggingRef.current = true
    startXRef.current = e.clientX
    trackRef.current = [{ x: PIECE_R, t: Date.now() }]
  }

  const onPointerMove = (e) => {
    if (!draggingRef.current) return
    let d = e.clientX - startXRef.current
    if (d < 0) d = 0
    if (d > MAX_D) d = MAX_D
    offsetRef.current = d
    setOffset(d)
    trackRef.current.push({ x: PIECE_R + d, t: Date.now() })
  }

  const onPointerUp = (e) => {
    if (!draggingRef.current) return
    draggingRef.current = false
    try {
      e.currentTarget.releasePointerCapture(e.pointerId)
    } catch {}
    const cur = dataRef.current
    if (!cur) return
    const x = PIECE_R + offsetRef.current // global centre-x of the piece
    // final track point must use the same coordinate as x (else the server's
    // anti-bot check rejects the attempt)
    trackRef.current.push({ x: x, t: Date.now() })
    verifyAsync(cur, x)
  }

  return (
    <div className="select-none">
      <div
        className="relative overflow-hidden rounded-lg border border-gray-700 bg-gray-900"
        style={{ width: W, height: H }}
      >
        {data && (
          <>
            <img src={data.image} alt="captcha" className="absolute inset-0 h-full w-full" draggable={false} />
            <img
              src={data.thumb}
              alt="piece"
              className="absolute left-0 top-0 h-full w-full"
              style={{ transform: `translateX(${offset}px)`, willChange: 'transform' }}
              draggable={false}
            />
          </>
        )}
        {loading && (
          <div className="absolute inset-0 flex items-center justify-center bg-gray-900/70 text-gray-300 text-sm">
            加载中...
          </div>
        )}
      </div>

      <div
        className="relative mt-2 h-10 cursor-grab rounded bg-gray-800 active:cursor-grabbing"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
      >
        <div
          className="absolute top-0 left-0 h-full rounded bg-gold/30"
          style={{ width: offset + PIECE_R }}
        />
        <div
          className="absolute top-0 flex h-full w-10 items-center justify-center rounded btn-gold"
          style={{ left: offset }}
        >
          {'»'}
        </div>
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-gray-400">
          {verified ? '✓ 验证通过' : error || '拖动滑块拼合图片'}
        </div>
      </div>

      {error && !verified && (
        <div className="mt-1 flex justify-end">
          <button
            type="button"
            onClick={fetchCaptcha}
            className="text-xs text-gold hover:underline"
          >
            换一张
          </button>
        </div>
      )}
    </div>
  )
}
