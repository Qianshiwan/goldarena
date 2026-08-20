package common

import (
	"fmt"
	"math"
)

// ========== Cultivation (修仙等级) System ==========
//
// 十级修仙交易等级体系，从练气境到仙尊境。
// 灵气值 (Spirit Energy) 是核心进度指标，由交易业绩综合计算。
// 境界突破需同时满足: 灵气值 + 交易笔数 + 胜率 + 收益率。
//
// 文案对应「黄金交易十境」: 低境看涨跌，中境懂盈亏，高境悟大道，最高境顺势无为。

// CultivationLevel 修仙境界等级
type CultivationLevel int

const (
	LevelQiRefining       CultivationLevel = 1  // 练气境
	LevelFoundation       CultivationLevel = 2  // 筑基境
	LevelGoldenCore       CultivationLevel = 3  // 金丹境
	LevelNascentSoul      CultivationLevel = 4  // 元婴境
	LevelSpiritTransform  CultivationLevel = 5  // 化神境
	LevelVoidRefinement   CultivationLevel = 6  // 炼虚境
	LevelBodyIntegration  CultivationLevel = 7  // 合体境
	LevelGreatVehicle     CultivationLevel = 8  // 大乘境
	LevelTribulation      CultivationLevel = 9  // 渡劫境
	LevelAscension        CultivationLevel = 10 // 仙尊境
)

// Realm 四大境域
type Realm string

const (
	RealmMortal Realm = "凡境" // Level 1
	RealmEarth  Realm = "地境" // Level 2
	RealmHuman  Realm = "人境" // Level 3
	RealmHeaven Realm = "天境" // Level 4-5
	RealmImmortal Realm = "仙境" // Level 6-8
	RealmSacred Realm = "圣境" // Level 9-10
)

// CultivationInfo 修仙境界信息
type CultivationInfo struct {
	Level        CultivationLevel `json:"level"`
	Name         string           `json:"name"`          // 中文名: 练气境
	NameEn       string           `json:"name_en"`       // 英文名: Qi Refining
	Title        string           `json:"title"`         // 称号/交易身份: 新手小白
	Realm        Realm            `json:"realm"`         // 境域: 凡境
	Color        string           `json:"color"`         // 主题色: #888780
	ColorLight   string           `json:"color_light"`   // 浅色背景: #F1EFE8
	Icon         string           `json:"icon"`          // 图标符号
	MinExp       int64            `json:"min_exp"`       // 最低灵气值
	MaxExp       int64            `json:"max_exp"`       // 最高灵气值
	MinTrades    int              `json:"min_trades"`    // 最低交易笔数
	MinWinRate   float64          `json:"min_win_rate"`  // 最低胜率 (%)
	MinReturn    float64          `json:"min_return"`    // 最低收益率 (%)
	MaxLeverage  int              `json:"max_leverage"`  // 解锁最大杠杆
	FeeDiscount  float64          `json:"fee_discount"`  // 手续费折扣 (1.0=原价, 0.7=7折)
	Features     []string         `json:"features"`      // 解锁特权列表
	Description  string           `json:"description"`   // 境界描述 (修仙隐喻 + 交易解读)
	Traits       []string         `json:"traits"`        // 境界特点 (交易成熟度特征)
}

// cultivationLevels 全部十个修仙境界定义 (黄金交易十境)
var cultivationLevels = []CultivationInfo{
	{
		Level: LevelQiRefining, Name: "练气境", NameEn: "Qi Refining",
		Title: "新手小白", Realm: RealmMortal,
		Color: "#888780", ColorLight: "#F1EFE8", Icon: "○",
		MinExp: 0, MaxExp: 50,
		MinTrades: 0, MinWinRate: 0, MinReturn: 0,
		MaxLeverage: 10, FeeDiscount: 1.0,
		Features:    []string{"基础交易", "市价单"},
		Description: "修仙：吸纳天地灵气，刚入门，不知法门，灵气散乱。交易：刚接触黄金，凭感觉买卖，听消息、看短视频喊单。不懂 K 线、止损，买卖全靠直觉，频繁操作，赚一点就跑，亏了死扛。",
		Traits:      []string{"无交易体系", "盈亏全靠运气", "频繁操作 / 亏了死扛"},
	},
	{
		Level: LevelFoundation, Name: "筑基境", NameEn: "Foundation Establishment",
		Title: "技术摸索者", Realm: RealmEarth,
		Color: "#639922", ColorLight: "#EAF3DE", Icon: "◐",
		MinExp: 50, MaxExp: 500,
		MinTrades: 10, MinWinRate: 0, MinReturn: 0,
		MaxLeverage: 20, FeeDiscount: 1.0,
		Features:    []string{"限价单", "基础K线图"},
		Description: "修仙：灵气凝聚，打下基础，学会基础功法。交易：开始学习技术指标（MACD、均线、支撑压力），热衷寻找精准买卖点。沉迷复盘、研究各种战法，试图找到“稳赚指标”。",
		Traits:      []string{"学技术指标", "沉迷复盘找稳赚指标", "易过度交易 / 迷信技术"},
	},
	{
		Level: LevelGoldenCore, Name: "金丹境", NameEn: "Golden Core",
		Title: "战术能手", Realm: RealmHuman,
		Color: "#BA7517", ColorLight: "#FAEEDA", Icon: "◑",
		MinExp: 500, MaxExp: 2000,
		MinTrades: 50, MinWinRate: 40, MinReturn: 0,
		MaxLeverage: 30, FeeDiscount: 1.0,
		Features:    []string{"止损止盈", "5m/15m K线", "持仓分析"},
		Description: "修仙：灵气凝结金丹，力量自成一体，拥有本命手段。交易：形成固定交易信号模型，知道什么时候不进场，学会设置止损。能够阶段性盈利，可以抓住波段行情。",
		Traits:      []string{"有固定信号模型", "会设止损", "缺大局观 / 难守利润"},
	},
	{
		Level: LevelNascentSoul, Name: "元婴境", NameEn: "Nascent Soul",
		Title: "风险掌控者", Realm: RealmHeaven,
		Color: "#534AB7", ColorLight: "#EEEDFE", Icon: "◔",
		MinExp: 2000, MaxExp: 5000,
		MinTrades: 100, MinWinRate: 45, MinReturn: 10,
		MaxLeverage: 50, FeeDiscount: 0.95,
		Features:    []string{"50x杠杆", "1h/4h K线", "交易统计"},
		Description: "修仙：金丹化婴，元神长存，明白进退之道，懂得规避凶险。交易：彻底吃透风控，仓位管理刻进本能。不再追求每一笔都赚钱，接受亏损。能够区分机会与陷阱，不会强行开仓。",
		Traits:      []string{"风控刻进本能", "接受亏损 / 区分机会陷阱", "盈利有上限"},
	},
	{
		Level: LevelSpiritTransform, Name: "化神境", NameEn: "Spirit Transformation",
		Title: "趋势行者", Realm: RealmHeaven,
		Color: "#185FA5", ColorLight: "#E6F1FB", Icon: "◕",
		MinExp: 5000, MaxExp: 10000,
		MinTrades: 200, MinWinRate: 50, MinReturn: 25,
		MaxLeverage: 75, FeeDiscount: 0.95,
		Features:    []string{"75x杠杆", "日K线", "深度行情"},
		Description: "修仙：元神出窍，洞悉山川大势，不拘泥小范围争斗。交易：放弃短线频繁博弈，专注大周期趋势交易。看懂宏观驱动（美元、利率、地缘、通胀），结合技术共振做单。",
		Traits:      []string{"专注大周期趋势", "看懂宏观驱动", "顺势 / 中长期稳定收益"},
	},
	{
		Level: LevelVoidRefinement, Name: "炼虚境", NameEn: "Void Refinement",
		Title: "多维度悟道者", Realm: RealmImmortal,
		Color: "#0F6E56", ColorLight: "#E1F5EE", Icon: "●",
		MinExp: 10000, MaxExp: 20000,
		MinTrades: 500, MinWinRate: 55, MinReturn: 50,
		MaxLeverage: 75, FeeDiscount: 0.9,
		Features:    []string{"高级技术指标", "MACD/KDJ", "多周期分析"},
		Description: "修仙：看破虚实，明白涨跌幻象，不以一时得失动心。交易：打通基本面、资金流向、市场情绪、技术形态。理解行情所有走势都是资金博弈结果，不再预判顶底，跟随力量运行。",
		Traits:      []string{"多维度打通", "不预判顶底 / 跟随力量", "心态蜕变"},
	},
	{
		Level: LevelBodyIntegration, Name: "合体境", NameEn: "Body Integration",
		Title: "知行合一者", Realm: RealmImmortal,
		Color: "#993C1D", ColorLight: "#FAECE7", Icon: "◉",
		MinExp: 20000, MaxExp: 50000,
		MinTrades: 1000, MinWinRate: 60, MinReturn: 100,
		MaxLeverage: 100, FeeDiscount: 0.9,
		Features:    []string{"100x杠杆", "自定义指标", "策略回测"},
		Description: "修仙：肉身与元神合一，念头与行动没有隔阂。交易：规则内化于心，看到符合标准机会果断进场，信号消失立刻离场。不存在“知道却做不到”，杜绝情绪化扛单、报复性交易。",
		Traits:      []string{"规则内化 / 执行零偏差", "杜绝情绪化扛单", "资金曲线平滑"},
	},
	{
		Level: LevelGreatVehicle, Name: "大乘境", NameEn: "Great Vehicle",
		Title: "市场观道者", Realm: RealmImmortal,
		Color: "#A32D2D", ColorLight: "#FCEBEB", Icon: "◎",
		MinExp: 50000, MaxExp: 100000,
		MinTrades: 2000, MinWinRate: 65, MinReturn: 200,
		MaxLeverage: 100, FeeDiscount: 0.9,
		Features:    []string{"专属客服", "API接口", "批量下单"},
		Description: "修仙：通晓天地规则，知晓盛极而衰、物极必反，看透周期轮回。交易：读懂金融周期，明白黄金长期底层逻辑。懂得取舍，主动放弃低质量机会，只参与盈亏比极高的行情。可以预判重大拐点，提前布局。",
		Traits:      []string{"读懂金融周期", "只做高盈亏比行情", "捕捉跨月大行情"},
	},
	{
		Level: LevelTribulation, Name: "渡劫境", NameEn: "Tribulation Crossing",
		Title: "守心圣人", Realm: RealmSacred,
		Color: "#993556", ColorLight: "#FBEAF0", Icon: "◈",
		MinExp: 100000, MaxExp: 500000,
		MinTrades: 5000, MinWinRate: 70, MinReturn: 500,
		MaxLeverage: 100, FeeDiscount: 0.7,
		Features:    []string{"手续费7折", "专属策略", "高级风控", "VIP通道"},
		Description: "修仙：面临天道考验，最大敌人是自身心魔，一念之差万劫不复。交易：技术、认知早已圆满，最大考验是人性心魔。巨大浮盈、连续亏损、市场巨大诱惑都难以动摇。懂得节制，永不膨胀，敬畏黑天鹅。",
		Traits:      []string{"战胜心魔 / 敬畏黑天鹅", "极强资金耐力", "风险在自身傲慢"},
	},
	{
		Level: LevelAscension, Name: "仙尊境", NameEn: "Immortal Venerable",
		Title: "无为交易者", Realm: RealmSacred,
		Color: "#412402", ColorLight: "#FAEEDA", Icon: "✦",
		MinExp: 500000, MaxExp: math.MaxInt64,
		MinTrades: 10000, MinWinRate: 75, MinReturn: 1000,
		MaxLeverage: 100, FeeDiscount: 0.5,
		Features:    []string{"手续费5折", "至尊徽章", "定制界面", "宗师认证", "传承开宗"},
		Description: "修仙：渡过天劫，顺应天道，无争无执，大道自然。交易：达到交易最高境界——无为。不强求盈利，不试图征服市场，只等待市场给出完美机会。行情适合自己体系就交易，没有机会长久空仓；看淡盈亏，交易只是遵循规律的自然行为。不与人论涨跌，不被外界杂音干扰，与市场共振共生。",
		Traits:      []string{"无为 / 不强行盈利", "长久空仓等完美机会", "与市场共振共生"},
	},
}

// TradeStats 交易统计 (用于计算灵气值和境界)
type TradeStats struct {
	TotalTrades    int     `json:"total_trades"`     // 总交易笔数
	WinningTrades  int     `json:"winning_trades"`   // 盈利笔数
	LosingTrades   int     `json:"losing_trades"`    // 亏损笔数
	WinRate        float64 `json:"win_rate"`         // 胜率 (%)
	TotalPnL       float64 `json:"total_pnl"`        // 总盈亏
	ReturnRate     float64 `json:"return_rate"`      // 收益率 (%)
	MaxWinStreak   int     `json:"max_win_streak"`   // 最大连胜
	MaxLossStreak  int     `json:"max_loss_streak"`  // 最大连亏
	AvgHoldMinutes float64 `json:"avg_hold_minutes"` // 平均持仓时间(分钟)
	TotalVolume    float64 `json:"total_volume"`     // 总交易量(手)
}

// CultivationProgress 修仙进度 (返回给前端)
type CultivationProgress struct {
	CurrentLevel    CultivationInfo  `json:"current_level"`
	NextLevel       *CultivationInfo `json:"next_level,omitempty"`
	SpiritEnergy    int64            `json:"spirit_energy"`   // 当前灵气值
	LevelMinExp     int64            `json:"level_min_exp"`   // 当前境界最低灵气
	LevelMaxExp     int64            `json:"level_max_exp"`   // 当前境界最高灵气
	ProgressPct     float64          `json:"progress_pct"`    // 当前境界进度 (%)
	Stats           TradeStats       `json:"stats"`
	CanBreakthrough bool             `json:"can_breakthrough"` // 是否满足突破条件
	Requirements    []Requirement    `json:"requirements"`     // 突破条件清单
}

// Requirement 突破条件项
type Requirement struct {
	Name     string `json:"name"`     // 条件名称
	Current  string `json:"current"`  // 当前值
	Required string `json:"required"` // 需要达到的值
	Met      bool   `json:"met"`      // 是否达标
}

// GetCultivationInfo 根据等级获取境界信息
func GetCultivationInfo(level CultivationLevel) CultivationInfo {
	if level < 1 {
		level = 1
	}
	if level > 10 {
		level = 10
	}
	return cultivationLevels[level-1]
}

// GetCultivationByExp 根据灵气值获取境界 (不考虑其他条件，仅按灵气值)
func GetCultivationByExp(exp int64) CultivationInfo {
	for i := len(cultivationLevels) - 1; i >= 0; i-- {
		if exp >= cultivationLevels[i].MinExp {
			return cultivationLevels[i]
		}
	}
	return cultivationLevels[0]
}

// CalcSpiritEnergy 根据交易统计计算灵气值
// 公式: 交易笔数×1 + 盈利笔数×2 + 收益率×10 + 最大连胜×5
// 注: 收益率为负时不倒扣（取 max(0, 收益率)），鼓励积极交易
func CalcSpiritEnergy(stats TradeStats) int64 {
	baseExp := int64(stats.TotalTrades) * 1
	winExp := int64(stats.WinningTrades) * 2
	// 收益率正向贡献：盈利加分，亏损不扣
	returnVal := stats.ReturnRate
	if returnVal < 0 {
		returnVal = 0
	}
	returnExp := int64(returnVal * 10)
	streakBonus := int64(stats.MaxWinStreak) * 5

	total := baseExp + winExp + returnExp + streakBonus
	if total < 0 {
		total = 0
	}
	return total
}

// CheckBreakthrough 检查是否满足突破到下一境界的条件
func CheckBreakthrough(currentLevel CultivationLevel, exp int64, stats TradeStats) (bool, []Requirement) {
	if currentLevel >= 10 {
		return true, []Requirement{{Name: "已达最高境界", Current: "仙尊境", Required: "—", Met: true}}
	}

	nextInfo := cultivationLevels[currentLevel] // index = level, 即下一级
	reqs := []Requirement{
		{
			Name:     "灵气值",
			Current:  fmt.Sprintf("%d", exp),
			Required: fmt.Sprintf("%d", nextInfo.MinExp),
			Met:      exp >= nextInfo.MinExp,
		},
		{
			Name:     "交易笔数",
			Current:  fmt.Sprintf("%d", stats.TotalTrades),
			Required: fmt.Sprintf("%d", nextInfo.MinTrades),
			Met:      stats.TotalTrades >= nextInfo.MinTrades,
		},
	}

	if nextInfo.MinWinRate > 0 {
		reqs = append(reqs, Requirement{
			Name:     "胜率",
			Current:  fmt.Sprintf("%.1f%%", stats.WinRate),
			Required: fmt.Sprintf("%.0f%%", nextInfo.MinWinRate),
			Met:      stats.WinRate >= nextInfo.MinWinRate,
		})
	}

	if nextInfo.MinReturn > 0 {
		reqs = append(reqs, Requirement{
			Name:     "收益率",
			Current:  fmt.Sprintf("%.1f%%", stats.ReturnRate),
			Required: fmt.Sprintf("%.0f%%", nextInfo.MinReturn),
			Met:      stats.ReturnRate >= nextInfo.MinReturn,
		})
	}

	allMet := true
	for _, r := range reqs {
		if !r.Met {
			allMet = false
			break
		}
	}
	return allMet, reqs
}

// GetCultivationProgress 获取完整修仙进度
// 境界完全由灵气值 (exp) 推导, 不依赖后端存储的 cultivation_level,
// 以保证交易境界页与交易大厅页显示的境界始终一致。
func GetCultivationProgress(exp int64, stats TradeStats) CultivationProgress {
	// 境界等级由灵气值决定
	expLevel := GetCultivationByExp(exp).Level
	currentInfo := GetCultivationInfo(expLevel)

	progress := CultivationProgress{
		CurrentLevel: currentInfo,
		SpiritEnergy: exp,
		LevelMinExp:  currentInfo.MinExp,
		LevelMaxExp:  currentInfo.MaxExp,
		Stats:        stats,
	}

	// 计算当前境界进度百分比
	if currentInfo.MaxExp == math.MaxInt64 {
		progress.ProgressPct = 100.0
	} else {
		levelRange := float64(currentInfo.MaxExp - currentInfo.MinExp)
		if levelRange > 0 {
			progress.ProgressPct = float64(exp-currentInfo.MinExp) / levelRange * 100
			if progress.ProgressPct > 100 {
				progress.ProgressPct = 100
			}
			if progress.ProgressPct < 0 {
				progress.ProgressPct = 0
			}
		}
	}

	// 下一境界信息
	if expLevel < 10 {
		nextInfo := cultivationLevels[expLevel] // index = level = next level - 1
		progress.NextLevel = &nextInfo
		canBreak, reqs := CheckBreakthrough(expLevel, exp, stats)
		progress.CanBreakthrough = canBreak
		progress.Requirements = reqs
	} else {
		progress.CanBreakthrough = false
		progress.Requirements = []Requirement{
			{Name: "已达最高境界", Current: "仙尊境", Required: "—", Met: true},
		}
	}

	return progress
}

// AllCultivationLevels 返回全部境界定义 (给前端展示用)
func AllCultivationLevels() []CultivationInfo {
	return cultivationLevels
}
