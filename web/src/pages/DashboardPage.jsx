import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import api, { cultivationAPI, jinguiziAPI } from '../services/api';
import useAuthStore from '../stores/authStore';

export default function DashboardPage() {
  const { user } = useAuthStore();
  const navigate = useNavigate();
  const [quote, setQuote] = useState(null);
  const [wallet, setWallet] = useState(null);
  const [cultivation, setCultivation] = useState(null);
  const [loading, setLoading] = useState(true);
  const [hasActiveEnrollment, setHasActiveEnrollment] = useState(false);

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [quoteRes, walletRes, cultRes, enrollRes] = await Promise.all([
          api.get('/market/quote?symbol=XAU'),
          api.get('/user/wallet'),
          cultivationAPI.getProgress().catch(() => null),
          jinguiziAPI.getEnrollment().catch(() => null),
        ]);
        setQuote(quoteRes.data.data);
        setWallet(walletRes.data.data);
        if (cultRes?.data?.data) setCultivation(cultRes.data.data);
        const enr = enrollRes?.data?.data?.enrollment;
        setHasActiveEnrollment(enr?.status === 'active');
      } catch (e) {
        console.error('Dashboard fetch error:', e);
      } finally {
        setLoading(false);
      }
    };
    fetchData();
  }, []);

  const isPositive = quote && quote.change >= 0;

  return (
    <div className="min-h-screen bg-dark-bg text-white p-6">
      {/* Header */}
      <div className="mb-8 flex items-start justify-between">
        <div>
          <h1 className="text-3xl font-bold gold-gradient">
            欢迎回来，{user?.nickname || '交易者'}
          </h1>
          <p className="text-gray-400 mt-2">
            金归子 GoldArena · 伦敦金归子现货模拟交易游戏
          </p>
        </div>
        {cultivation && cultivation.current_level && (
          <button
            onClick={() => navigate('/cultivation')}
            className="flex items-center gap-3 px-4 py-2.5 rounded-xl border hover:scale-105 transition-all duration-200"
            style={{
              background: cultivation.current_level.color_light + '20',
              borderColor: cultivation.current_level.color + '60',
            }}
          >
            <div
              className="w-10 h-10 rounded-full flex items-center justify-center text-lg font-bold text-white"
              style={{ background: cultivation.current_level.color }}
            >
              {cultivation.current_level.icon}
            </div>
            <div className="text-left">
              <div
                className="text-sm font-bold"
                style={{ color: cultivation.current_level.color }}
              >
                {cultivation.current_level.name}
              </div>
              <div className="text-xs text-gray-500">
                {cultivation.current_level.title} ·{' '}
                {cultivation.spirit_energy?.toLocaleString()} 灵气
              </div>
            </div>
          </button>
        )}
      </div>

      {loading ? (
        <div className="flex items-center justify-center h-64">
          <div className="animate-spin w-8 h-8 border-2 border-gold border-t-transparent rounded-full" />
        </div>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          {/* Market Overview Card */}
          <div className="col-span-2 bg-dark-card rounded-2xl border border-gray-800 p-6 hover:border-gold/30 transition-all duration-300">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-semibold text-gray-200">市场概览</h2>
              <span className="text-xs px-3 py-1 bg-gold/10 text-gold rounded-full border border-gold/20">
                LBM 实时
              </span>
            </div>
            {quote && (
              <div className="space-y-4">
                <div className="flex items-baseline gap-4">
                  <span className="text-3xl font-bold font-mono">{quote.price?.toFixed(2)}</span>
                  <span className="text-sm text-gray-400">美元/盎司</span>
                  <span className={`text-sm font-semibold ${isPositive ? 'text-green-400' : 'text-red-400'}`}>
                    {isPositive ? '+' : ''}{quote.change?.toFixed(2)} ({quote.change_percent?.toFixed(2)}%)
                  </span>
                </div>
                <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
                  <div>
                    <span className="text-gray-500">买价</span>
                    <p className="font-mono text-green-400">{quote.bid?.toFixed(2)}</p>
                  </div>
                  <div>
                    <span className="text-gray-500">卖价</span>
                    <p className="font-mono text-red-400">{quote.ask?.toFixed(2)}</p>
                  </div>
                  <div>
                    <span className="text-gray-500">日内高</span>
                    <p className="font-mono">{quote.high?.toFixed(2)}</p>
                  </div>
                  <div>
                    <span className="text-gray-500">日内低</span>
                    <p className="font-mono">{quote.low?.toFixed(2)}</p>
                  </div>
                  <div>
                    <span className="text-gray-500">开盘</span>
                    <p className="font-mono">{quote.open?.toFixed(2)}</p>
                  </div>
                  <div>
                    <span className="text-gray-500">前结算</span>
                    <p className="font-mono">{quote.previous_settle?.toFixed(2)}</p>
                  </div>
                  <div>
                    <span className="text-gray-500">成交量</span>
                    <p className="font-mono">{quote.volume?.toLocaleString()}</p>
                  </div>
                  <div>
                    <span className="text-gray-500">持仓量</span>
                    <p className="font-mono">{quote.open_interest?.toLocaleString()}</p>
                  </div>
                </div>
              </div>
            )}
            <div className="mt-4 flex gap-3">
              <button
                onClick={() => navigate('/trade')}
                className="px-6 py-2.5 bg-gold text-black font-semibold rounded-lg hover:bg-gold-light transition-all duration-200"
              >
                游戏币交易
              </button>
              <button
                onClick={() => navigate('/trade')}
                className="px-6 py-2.5 border border-gray-700 text-gray-300 rounded-lg hover:border-gray-500 transition-all duration-200"
              >
                查看行情图
              </button>
            </div>
          </div>

          {/* Wallet Card */}
          <div className="bg-dark-card rounded-2xl border border-gray-800 p-6 hover:border-gold/30 transition-all duration-300">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-semibold text-gray-200">我的钱包</h2>
              <span className="text-xs text-gray-500">游戏币</span>
            </div>
            {wallet && (
              <div className="space-y-4">
                <div>
                  <span className="text-2xl font-bold font-mono gold-gradient">
                    {wallet.balance?.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                  </span>
                  <p className="text-xs text-gray-500 mt-1">可用余额</p>
                </div>
                <div className="border-t border-gray-800 pt-4 space-y-2">
                  <div className="flex justify-between text-sm">
                    <span className="text-gray-400">冻结保证金</span>
                    <span className="font-mono">{wallet.frozen?.toLocaleString()}</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-gray-400">累计充值</span>
                    <span className="font-mono">{wallet.total_recharged?.toLocaleString()}</span>
                  </div>
                </div>
                <button
                  onClick={() => navigate('/wallet')}
                  className="w-full py-2.5 bg-gradient-to-r from-gold to-gold-light text-black font-semibold rounded-lg hover:opacity-90 transition-all duration-200"
                >
                  充值 ¥10 = 10,000游戏币
                </button>
              </div>
            )}
          </div>

          {/* Quick Trade Entry */}
          <div className="col-span-2 bg-dark-card rounded-2xl border border-gray-800 p-6 hover:border-gold/30 transition-all duration-300">
            <h2 className="text-lg font-semibold text-gray-200 mb-4">快捷入口</h2>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              <button
                onClick={() => navigate('/trade')}
                className="flex flex-col items-center gap-2 p-4 bg-dark-bg rounded-xl border border-gray-800 hover:border-gold/50 transition-all duration-200 group"
              >
                <span className="text-2xl">💰</span>
                <span className="text-sm group-hover:text-gold transition-colors">游戏币交易</span>
              </button>
              {hasActiveEnrollment && (
                <button
                  onClick={() => navigate('/contest-trade')}
                  className="flex flex-col items-center gap-2 p-4 bg-gradient-to-br from-gold/10 to-transparent rounded-xl border border-gold/40 hover:border-gold transition-all duration-200 group"
                >
                  <span className="text-2xl">🏆</span>
                  <span className="text-sm font-semibold text-gold group-hover:text-yellow-300 transition-colors">
                    选拔赛交易
                  </span>
                </button>
              )}
              <button
                onClick={() => navigate('/cultivation')}
                className="flex flex-col items-center gap-2 p-4 bg-dark-bg rounded-xl border border-gray-800 hover:border-gold/50 transition-all duration-200 group"
              >
                <span className="text-2xl">🔮</span>
                <span className="text-sm group-hover:text-gold transition-colors">交易境界</span>
              </button>
              <button
                onClick={() => navigate('/contest')}
                className="flex flex-col items-center gap-2 p-4 bg-dark-bg rounded-xl border border-gray-800 hover:border-gold/50 transition-all duration-200 group"
              >
                <span className="text-2xl">🏆</span>
                <span className="text-sm group-hover:text-gold transition-colors">赛事中心</span>
              </button>
              <button
                onClick={() => navigate('/pnl')}
                className="flex flex-col items-center gap-2 p-4 bg-dark-bg rounded-xl border border-gray-800 hover:border-gold/50 transition-all duration-200 group"
              >
                <span className="text-2xl">📈</span>
                <span className="text-sm group-hover:text-gold transition-colors">盈亏统计</span>
              </button>
            </div>
          </div>

          {/* Market News */}
          <div className="bg-dark-card rounded-2xl border border-gray-800 p-6 hover:border-gold/30 transition-all duration-300">
            <h2 className="text-lg font-semibold text-gray-200 mb-4">游戏规则</h2>
            <div className="space-y-3 text-sm">
              <div className="flex justify-between py-2 border-b border-gray-800">
                <span className="text-gray-400">游戏品种</span>
                <span>伦敦金现货 (XAU)</span>
              </div>
              <div className="flex justify-between py-2 border-b border-gray-800">
                <span className="text-gray-400">合约规格</span>
                <span>100金衡盎司/手</span>
              </div>
              <div className="flex justify-between py-2 border-b border-gray-800">
                <span className="text-gray-400">最小变动</span>
                <span>$0.10/盎司</span>
              </div>
              <div className="flex justify-between py-2 border-b border-gray-800">
                <span className="text-gray-400">杠杆</span>
                <span>1-1000倍</span>
              </div>
              <div className="flex justify-between py-2">
                <span className="text-gray-400">交易时间</span>
                <span>周一00:00-周五22:00 GMT</span>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
