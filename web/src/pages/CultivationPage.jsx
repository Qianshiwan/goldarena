import CultivationPanel from '../components/cultivation/CultivationPanel'

export default function CultivationPage() {
  return (
    <div className="min-h-screen bg-dark-bg text-white p-6">
      <div className="mb-6">
        <h1 className="text-2xl font-bold gold-gradient">交易境界</h1>
        <p className="text-gray-400 mt-1 text-sm">
          交易即是修行 · 灵气积累 · 境界突破 · 仙尊无为
        </p>
      </div>
      <div className="max-w-3xl mx-auto">
        <CultivationPanel />
      </div>
    </div>
  )
}
