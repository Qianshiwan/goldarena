/**
 * WebSocket 实时行情服务
 * 连接后端 /api/v1/ws，支持频道订阅（quote / kline）与自动重连。
 *
 * 用法：
 *   const off = wsClient.subscribe(
 *     { channel: 'kline', symbol: 'XAU', period: '1m' },
 *     (msg) => { console.log(msg.type, msg.data) }
 *   )
 *   off() // 取消订阅
 */

const WS_URL = () => {
  const token = localStorage.getItem('access_token') || ''
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${window.location.host}/api/v1/ws?token=${encodeURIComponent(token)}`
}

class WSClient {
  constructor() {
    this.ws = null
    this.connected = false
    this.reconnectTimer = null
    this.heartbeatTimer = null
    this.handlers = new Map() // key: channel -> Set<fn>
    this.reconnectDelay = 2000
    this.shouldReconnect = true
  }

  connect() {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return
    }
    try {
      this.ws = new WebSocket(WS_URL())
    } catch (e) {
      this.scheduleReconnect()
      return
    }

    this.ws.onopen = () => {
      this.connected = true
      this.reconnectDelay = 2000
      // 重连后重新订阅所有频道
      this.resubscribeAll()
      // 心跳保活
      this.startHeartbeat()
    }

    this.ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)
        this.dispatch(msg)
      } catch (e) {
        // ignore malformed messages
      }
    }

    this.ws.onclose = () => {
      this.connected = false
      this.stopHeartbeat()
      this.scheduleReconnect()
    }

    this.ws.onerror = () => {
      try { this.ws.close() } catch (e) { /* noop */ }
    }
  }

  scheduleReconnect() {
    if (!this.shouldReconnect || this.reconnectTimer) return
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, this.reconnectDelay)
    // 指数退避，上限 30s
    this.reconnectDelay = Math.min(this.reconnectDelay * 1.5, 30000)
  }

  startHeartbeat() {
    this.stopHeartbeat()
    this.heartbeatTimer = setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(JSON.stringify({ action: 'ping' }))
      }
    }, 20000)
  }

  stopHeartbeat() {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer)
      this.heartbeatTimer = null
    }
  }

  send(obj) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(obj))
    }
  }

  /**
   * 订阅频道
   * @param {{channel: string, symbol?: string, period?: string}} spec
   * @param {(msg: object) => void} handler
   * @returns {() => void} 取消订阅函数
   */
  subscribe(spec, handler) {
    const key = this.channelKey(spec)
    if (!this.handlers.has(key)) {
      this.handlers.set(key, new Set())
    }
    this.handlers.get(key).add(handler)
    // 订阅协议：{"action":"subscribe","channel":"kline","symbol":"XAU","period":"1m"}
    this.send({
      action: 'subscribe',
      channel: spec.channel,
      symbol: spec.symbol || '',
      period: spec.period || '',
    })
    if (this.connected) {
      // 确保订阅消息已发出（连接刚建立时 send 可能未就绪）
    }
    // 若未连接则尝试连接
    if (!this.ws || this.ws.readyState === WebSocket.CLOSED) {
      this.connect()
    }
    return () => this.unsubscribe(spec, handler)
  }

  unsubscribe(spec, handler) {
    const key = this.channelKey(spec)
    const set = this.handlers.get(key)
    if (set) {
      set.delete(handler)
      if (set.size === 0) {
        this.handlers.delete(key)
        this.send({
          action: 'unsubscribe',
          channel: spec.channel,
          symbol: spec.symbol || '',
          period: spec.period || '',
        })
      }
    }
  }

  resubscribeAll() {
    this.handlers.forEach((_, key) => {
      // key 格式: channel:symbol:period
      const [channel, symbol, period] = key.split(':')
      this.send({
        action: 'subscribe',
        channel,
        symbol: symbol || '',
        period: period || '',
      })
    })
  }

  channelKey(spec) {
    return `${spec.channel}:${spec.symbol || ''}:${spec.period || ''}`
  }

  dispatch(msg) {
    const { type, data } = msg
    if (!type) return
    // 尝试匹配频道：type 可能是 "kline" / "quote" / "kline_complete"
    if (type === 'kline' || type === 'kline_complete') {
      const period = data?.period || ''
      const symbol = data?.symbol || ''
      const keys = [
        `${type}:${symbol}:${period}`,
        `kline:${symbol}:${period}`,
      ]
      for (const key of keys) {
        this.emit(key, msg)
      }
      return
    }
    if (type === 'quote') {
      const symbol = data?.symbol || ''
      this.emit(`quote:${symbol}:`, msg)
      this.emit(`quote:${symbol}`, msg)
      return
    }
    // 通用分发
    const key = `${type}:::`
    this.emit(key, msg)
  }

  emit(key, msg) {
    const set = this.handlers.get(key)
    if (set) {
      set.forEach((fn) => {
        try { fn(msg) } catch (e) { /* handler error */ }
      })
    }
  }
}

// 单例
export const wsClient = new WSClient()

export default wsClient
