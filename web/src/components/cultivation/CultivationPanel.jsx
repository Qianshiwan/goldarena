import { useState, useEffect, useCallback } from 'react'
import { cultivationAPI } from '../../services/api'

export default function CultivationPanel() {
  const [progress, setProgress] = useState(null)
  const [levels, setLevels] = useState([])
  const [rank, setRank] = useState([])
  const [tab, setTab] = useState('progress')
  const [loading, setLoading] = useState(false)
  const [breakthroughMsg, setBreakthroughMsg] = useState(null)

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const [progRes, lvlRes] = await Promise.all([
        cultivationAPI.getProgress(),
        cultivationAPI.getAllLevels(),
      ])
      setProgress(progRes.data?.data)
      setLevels(lvlRes.data?.data || [])
    } catch (e) {
      console.error('Failed to load cultivation data:', e)
    } finally {
      setLoading(false)
    }
  }, [])

  const loadRank = useCallback(async () => {
    try {
      const res = await cultivationAPI.getRank()
      setRank(res.data?.data || [])
    } catch (e) {
      console.error('Failed to load rank:', e)
    }
  }, [])

  useEffect(() => {
    loadData()
  }, [loadData])

  useEffect(() => {
    if (tab === 'rank') loadRank()
  }, [tab, loadRank])

  const handleBreakthrough = async () => {
    try {
      const res = await cultivationAPI.breakthrough()
      const data = res.data?.data
      if (data?.success) {
        setBreakthroughMsg(data)
        loadData()
      }
    } catch (e) {
      setBreakthroughMsg({
        success: false,
        message: e.response?.data?.message || '突破条件未满足',
      })
    }
  }

  const handleRefresh = async () => {
    try {
      await cultivationAPI.refresh()
      loadData()
    } catch (e) {
      console.error('Refresh failed:', e)
    }
  }

  if (loading && !progress) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-gray-500 text-sm">加载交易境界中...</div>
      </div>
    )
  }

  if (!progress) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-gray-500 text-sm">暂无交易境界数据，完成交易后即可积累灵气</div>
      </div>
    )
  }

  const cur = progress.current_level
  const next = progress.next_level
  const stats = progress.stats

  return (
    <div className="space-y-4">
      {breakthroughMsg && (
        <div
          className={`rounded-lg p-4 border ${
            breakthroughMsg.success
              ? 'bg-yellow-900/20 border-yellow-600/50 text-yellow-300'
              : 'bg-red-900/20 border-red-600/50 text-red-300'
          }`}
        >
          <p className="text-sm font-medium">{breakthroughMsg.message}</p>
          {breakthroughMsg.features && (
            <p className="text-xs mt-1 opacity-80">
              解锁特权: {breakthroughMsg.features.join(' · ')}
            </p>
          )}
          <button
            className="mt-2 text-xs underline opacity-70"
            onClick={() => setBreakthroughMsg(null)}
          >
            关闭
          </button>
        </div>
      )}

      <div className="flex gap-2 border-b border-gray-700 pb-2">
        {['progress', 'levels', 'rank'].map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-3 py-1 text-sm rounded transition-colors ${
              tab === t
                ? 'bg-gray-700 text-white'
                : 'text-gray-400 hover:text-gray-200'
            }`}
          >
            {t === 'progress' ? '我的境界' : t === 'levels' ? '境界图鉴' : '交易榜'}
          </button>
        ))}
        <button
          onClick={handleRefresh}
          className="ml-auto px-3 py-1 text-xs text-gray-500 hover:text-gray-300"
        >
          刷新灵气
        </button>
      </div>

      {tab === 'progress' && (
        <div className="space-y-4">
          <div
            className="rounded-xl p-5 border"
            style={{
              background: cur.color_light + '20',
              borderColor: cur.color + '60',
            }}
          >
            <div className="flex items-center gap-4">
              <div
                className="w-16 h-16 rounded-full flex items-center justify-center text-2xl font-bold text-white"
                style={{ background: cur.color }}
              >
                {cur.icon}
              </div>
              <div className="flex-1">
                <div className="flex items-center gap-2">
                  <span
                    className="text-lg font-bold"
                    style={{ color: cur.color }}
                  >
                    {cur.name}
                  </span>
                  <span className="text-xs text-gray-500">{cur.name_en}</span>
                </div>
                <p className="text-sm text-gray-400 mt-0.5">
                  {cur.title} · {cur.realm}
                </p>
                <p className="text-xs text-gray-500 mt-1">{cur.description}</p>
              </div>
              <div className="text-right">
                <div className="text-xs text-gray-500">境界</div>
                <div
                  className="text-2xl font-bold"
                  style={{ color: cur.color }}
                >
                  {cur.level}
                </div>
              </div>
            </div>

            <div className="mt-4">
              <div className="flex justify-between text-xs mb-1">
                <span className="text-gray-400">灵气值</span>
                <span className="text-gray-300 font-mono">
                  {progress.spirit_energy.toLocaleString()} /{' '}
                  {progress.level_max_exp >= 999999
                    ? '∞'
                    : progress.level_max_exp.toLocaleString()}
                </span>
              </div>
              <div className="h-2 bg-gray-700/50 rounded-full overflow-hidden">
                <div
                  className="h-full rounded-full transition-all duration-500"
                  style={{
                    width: `${Math.min(progress.progress_pct, 100)}%`,
                    background: cur.color,
                  }}
                />
              </div>
              <div className="text-right text-xs text-gray-500 mt-0.5">
                {progress.progress_pct.toFixed(1)}%
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <StatCard label="交易笔数" value={stats.total_trades} />
            <StatCard
              label="胜率"
              value={`${stats.win_rate.toFixed(1)}%`}
            />
            <StatCard
              label="总盈亏"
              value={stats.total_pnl.toFixed(2)}
              positive={stats.total_pnl >= 0}
            />
            <StatCard
              label="收益率"
              value={`${stats.return_rate.toFixed(1)}%`}
              positive={stats.return_rate >= 0}
            />
          </div>

          {next && (
            <div className="rounded-lg p-4 bg-gray-800/50 border border-gray-700">
              <div className="flex items-center justify-between mb-3">
                <div className="flex items-center gap-2">
                  <span
                    className="w-8 h-8 rounded-full flex items-center justify-center text-sm text-white"
                    style={{ background: next.color }}
                  >
                    {next.icon}
                  </span>
                  <div>
                    <span
                      className="text-sm font-bold"
                      style={{ color: next.color }}
                    >
                      {next.name}
                    </span>
                    <span className="text-xs text-gray-500 ml-2">
                      {next.title}
                    </span>
                  </div>
                </div>
                {progress.can_breakthrough && (
                  <button
                    onClick={handleBreakthrough}
                    className="px-4 py-1.5 text-sm font-medium rounded-lg text-white"
                    style={{ background: next.color }}
                  >
                    突破
                  </button>
                )}
              </div>

              <div className="space-y-2">
                {progress.requirements?.map((req, i) => (
                  <div
                    key={i}
                    className="flex items-center justify-between text-sm"
                  >
                    <span className="text-gray-400">{req.name}</span>
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-gray-300">
                        {req.current}
                      </span>
                      <span className="text-gray-600">/</span>
                      <span className="font-mono text-gray-500">
                        {req.required}
                      </span>
                      <span
                        className={
                          req.met ? 'text-green-400' : 'text-red-400'
                        }
                      >
                        {req.met ? '✓' : '✗'}
                      </span>
                    </div>
                  </div>
                ))}
              </div>

              <div className="mt-3 pt-3 border-t border-gray-700">
                <p className="text-xs text-gray-500 mb-1">突破后解锁:</p>
                <div className="flex flex-wrap gap-1.5">
                  {next.features.map((f, i) => (
                    <span
                      key={i}
                      className="px-2 py-0.5 text-xs rounded"
                      style={{
                        background: next.color + '20',
                        color: next.color,
                      }}
                    >
                      {f}
                    </span>
                  ))}
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {tab === 'levels' && (
        <div className="space-y-2">
          {levels.map((lvl) => {
            const isCurrent = lvl.level === cur.level
            const isAchieved = lvl.level <= cur.level
            return (
              <div
                key={lvl.level}
                className={`rounded-lg p-3 border transition-all ${
                  isCurrent ? 'ring-1' : ''
                }`}
                style={{
                  background: isAchieved
                    ? lvl.color_light + '15'
                    : 'rgba(31, 41, 55, 0.3)',
                  borderColor: isCurrent
                    ? lvl.color
                    : isAchieved
                    ? lvl.color + '40'
                    : 'rgba(75, 85, 99, 0.3)',
                }}
              >
                <div className="flex items-center gap-3">
                  <div
                    className={`w-10 h-10 rounded-full flex items-center justify-center text-lg font-bold ${
                      isAchieved ? 'text-white' : 'text-gray-600'
                    }`}
                    style={{
                      background: isAchieved ? lvl.color : '#374151',
                      opacity: isAchieved ? 1 : 0.5,
                    }}
                  >
                    {lvl.icon}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span
                        className="text-sm font-bold"
                        style={{
                          color: isAchieved ? lvl.color : '#6b7280',
                        }}
                      >
                        {lvl.name}
                      </span>
                      <span className="text-xs text-gray-600">
                        {lvl.name_en}
                      </span>
                      {isCurrent && (
                        <span
                          className="text-xs px-1.5 py-0.5 rounded"
                          style={{
                            background: lvl.color + '30',
                            color: lvl.color,
                          }}
                        >
                          当前
                        </span>
                      )}
                    </div>
                    <p className="text-xs text-gray-500 truncate">
                      {lvl.title} · {lvl.realm}
                    </p>
                  </div>
                  <div className="text-right">
                    <div className="text-xs text-gray-600">灵气</div>
                    <div className="text-xs font-mono text-gray-400">
                      {lvl.min_exp >= 999999
                        ? '500K+'
                        : `${(lvl.min_exp / 1000).toFixed(0)}K`}
                    </div>
                  </div>
                </div>
                <p className="text-xs text-gray-500 mt-2 pl-13">
                  {lvl.description}
                </p>
                {lvl.traits && lvl.traits.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-2">
                    {lvl.traits.map((t, i) => (
                      <span
                        key={'trait-' + i}
                        className="px-1.5 py-0.5 text-xs rounded"
                        style={{
                          background: isAchieved ? lvl.color + '20' : 'rgba(55,65,81,0.4)',
                          color: isAchieved ? lvl.color : '#9ca3af',
                        }}
                      >
                        {t}
                      </span>
                    ))}
                  </div>
                )}
                <div className="flex flex-wrap gap-1 mt-2">
                  {lvl.features.map((f, i) => (
                    <span
                      key={i}
                      className={`px-1.5 py-0.5 text-xs rounded ${
                        isAchieved
                          ? 'bg-gray-700/50 text-gray-400'
                          : 'bg-gray-800/30 text-gray-600'
                      }`}
                    >
                      {f}
                    </span>
                  ))}
                </div>
              </div>
            )
          })}
        </div>
      )}

      {tab === 'rank' && (
        <div className="space-y-1">
          {rank.length === 0 ? (
            <div className="text-center py-8 text-gray-500 text-sm">
              暂无排名数据
            </div>
          ) : (
            rank.slice(0, 50).map((u, i) => (
              <div
                key={u.user_id || i}
                className="flex items-center gap-3 p-2 rounded hover:bg-gray-800/50"
              >
                <div
                  className={`w-7 text-center text-sm font-bold ${
                    i === 0
                      ? 'text-yellow-400'
                      : i === 1
                      ? 'text-gray-300'
                      : i === 2
                      ? 'text-orange-400'
                      : 'text-gray-500'
                  }`}
                >
                  {i + 1}
                </div>
                <div
                  className="w-8 h-8 rounded-full flex items-center justify-center text-xs text-white"
                  style={{ background: u.level_color || '#888' }}
                >
                  {(u.nickname || u.username || '?')[0]}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="text-sm text-gray-200 truncate">
                    {u.nickname || u.username}
                  </div>
                  <div className="text-xs text-gray-500">
                    <span style={{ color: u.level_color }}>
                      {u.level_name}
                    </span>
                    {' · '}
                    {u.level_title}
                  </div>
                </div>
                <div className="text-right">
                  <div className="text-xs text-gray-400 font-mono">
                    {(u.spirit_energy || 0).toLocaleString()}
                  </div>
                  <div className="text-xs text-gray-600">灵气</div>
                </div>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  )
}

function StatCard({ label, value, positive }) {
  return (
    <div className="rounded-lg p-3 bg-gray-800/50">
      <div className="text-xs text-gray-500">{label}</div>
      <div
        className={`text-lg font-bold mt-0.5 font-mono ${
          positive === undefined
            ? 'text-gray-200'
            : positive
            ? 'text-green-400'
            : 'text-red-400'
        }`}
      >
        {value}
      </div>
    </div>
  )
}
