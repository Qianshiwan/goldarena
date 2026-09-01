package common

import (
	"time"
)

// ========== User Model ==========
type User struct {
	ID              int64     `json:"id"`
	Username        string    `json:"username"`
	Nickname        string    `json:"nickname"`
	PasswordHash    string    `json:"-"`
	Email           string    `json:"email,omitempty"`
	Phone           string    `json:"phone,omitempty"`
	Avatar          string    `json:"avatar,omitempty"`
	IsVerified      bool      `json:"is_verified"`
	Role            string    `json:"role"` // user, vip_silver, vip_gold, vip_diamond, admin
	Status          int       `json:"status"` // 1=active, 0=disabled
	CultivationLevel int      `json:"cultivation_level"` // 修仙境界 1-10
	SpiritEnergy    int64     `json:"spirit_energy"`     // 灵气值
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ========== Wallet Model ==========
type Wallet struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	Balance     float64   `json:"balance"`      // Available game coins
	Frozen      float64   `json:"frozen"`       // Frozen as margin
	TotalRecharged float64 `json:"total_recharged"`
	Version     int64     `json:"-"`            // Optimistic lock
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type WalletTransaction struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	Type          string    `json:"type"` // recharge, withdraw_frozen, margin_freeze, margin_release, pnl_credit, pnl_debit, contest_reward, bonus
	Amount        float64   `json:"amount"`
	BalanceBefore float64   `json:"balance_before"`
	BalanceAfter  float64   `json:"balance_after"`
	ReferenceID   string    `json:"reference_id,omitempty"`
	Remark        string    `json:"remark,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// ========== Market/Quote Model ==========
type Quote struct {
	Symbol        string    `json:"symbol"`         // "GC"
	ContractMonth string    `json:"contract_month"` // "202612"
	Bid           float64   `json:"bid"`
	Ask           float64   `json:"ask"`
	Price         float64   `json:"price"`          // Last price
	Open          float64   `json:"open"`
	High          float64   `json:"high"`
	Low           float64   `json:"low"`
	PreviousSettle float64  `json:"previous_settle"`
	Volume        int64     `json:"volume"`
	OpenInterest  int64     `json:"open_interest"`
	Change        float64   `json:"change"`
	ChangePercent float64   `json:"change_percent"`
	Timestamp     int64     `json:"timestamp"`      // Unix millisecond
}

type KLine struct {
	Symbol        string    `json:"symbol"`
	ContractMonth string    `json:"contract_month"`
	Period        string    `json:"period"` // 1m, 5m, 15m, 1h, 4h, 1d
	Open          float64   `json:"open"`
	High          float64   `json:"high"`
	Low           float64   `json:"low"`
	Close         float64   `json:"close"`
	Volume        float64   `json:"volume"`
	Timestamp     int64     `json:"timestamp"`
	CreatedAt     time.Time `json:"created_at"`
}

// ========== Trading Models ==========
type Order struct {
	ID            int64     `json:"id"`
	OrderNo       string    `json:"order_no"`
	UserID        int64     `json:"user_id"`
	ContestID     *int64    `json:"contest_id,omitempty"`
	Symbol        string    `json:"symbol"`         // "GC"
	ContractMonth string    `json:"contract_month"` // "202612"
	Direction     int       `json:"direction"`      // 1=long, 2=short
	OrderType     int       `json:"order_type"`     // 1=market, 2=limit, 3=stop, 4=take_profit
	Volume        float64   `json:"volume"`         // Lots
	Leverage      int       `json:"leverage"`
	Price         *float64  `json:"price,omitempty"` // Limit price
	StopLoss      *float64  `json:"stop_loss,omitempty"`
	TakeProfit    *float64  `json:"take_profit,omitempty"`
	Status        int       `json:"status"`          // 1=pending, 2=filled, 3=cancelled, 4=rejected
	ExecutedPrice *float64  `json:"executed_price,omitempty"`
	Margin        float64   `json:"margin"`          // Frozen margin
	SpreadCost    float64   `json:"spread_cost"`     // Spread cost
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Position struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	ContestID     *int64    `json:"contest_id,omitempty"`
	OrderNo       string    `json:"order_no"`
	Symbol        string    `json:"symbol"`
	ContractMonth string    `json:"contract_month"`
	Direction     int       `json:"direction"` // 1=long, 2=short
	Volume        float64   `json:"volume"`
	Leverage      int       `json:"leverage"`
	OpenPrice     float64   `json:"open_price"`
	CurrentPrice  float64   `json:"current_price"`
	StopLoss      *float64  `json:"stop_loss,omitempty"`
	TakeProfit    *float64  `json:"take_profit,omitempty"`
	Margin        float64   `json:"margin"`
	FloatingPnL   float64   `json:"floating_pnl"`
	SpreadCost    float64   `json:"spread_cost"`
	Status        int       `json:"status"` // 1=open, 2=closed
	ClosedAt      *time.Time `json:"closed_at,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Account summary
type Account struct {
	Balance     float64 `json:"balance"`
	Equity      float64 `json:"equity"`
	Margin      float64 `json:"margin"`
	FreeMargin  float64 `json:"free_margin"`
	MarginLevel float64 `json:"margin_level"` // percentage
	FloatingPnL float64 `json:"floating_pnl"`
	TotalPnL    float64 `json:"total_pnl"`
}

// ========== Contest Models ==========
type Contest struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	EntryFee    float64    `json:"entry_fee"`    // Game coin
	InitialCapital float64 `json:"initial_capital"`
	StartTime   time.Time  `json:"start_time"`
	EndTime     time.Time  `json:"end_time"`
	RegDeadline time.Time  `json:"reg_deadline"`
	Status      int        `json:"status"` // 1=pending, 2=registration, 3=active, 4=ended, 5=settled
	MaxParticipants int    `json:"max_participants"`
	TotalPrize  float64    `json:"total_prize"`  // RMB
	PrizeDistribution string `json:"prize_distribution"` // JSON
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type ContestRank struct {
	Rank       int     `json:"rank"`
	UserID     int64   `json:"user_id"`
	Nickname   string  `json:"nickname"`
	Avatar     string  `json:"avatar"`
	InitialCapital float64 `json:"initial_capital"`
	CurrentEquity   float64 `json:"current_equity"`
	ReturnRate float64 `json:"return_rate"` // percentage
	TotalPnL   float64 `json:"total_pnl"`
	WinRate    float64 `json:"win_rate"`
	TradeCount int     `json:"trade_count"`
}

// ========== Jinguizi (金龟子) Simulated Coin Wallet ==========
// A wallet SEPARATE from the main game-coin Wallet. It holds "金龟子模拟币", the
// dedicated contest currency (选拔赛参赛资金). It is deliberately isolated:
//   - stored in its own tables / maps (ga_jinguizi_wallets, ga_jinguizi_txns)
//   - recharged ONLY by admins (no real-money payment path involved)
//   - never mixed with the main wallet used for normal trading margin
//
// This keeps contest funds auditable and independent from general game coins.
type JinguiziWallet struct {
	UserID         int64     `json:"user_id"`
	ID             int64     `json:"id"`
	Balance        float64   `json:"balance"`         // available 金龟子模拟币
	Frozen         float64   `json:"frozen"`          // frozen as contest margin (reserved for future contest wiring)
	TotalRecharged float64   `json:"total_recharged"` // cumulative admin recharge
	Version        int64     `json:"-"`               // optimistic lock
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// JinguiziEnrollment records a participant's 选拔赛 (contest) entry funded by
// 金龟子模拟币. It is the contest-lifecycle half of the isolated 金龟子 system:
// the wallet (above) holds the coins; this record tracks which tier the
// participant entered, their dedicated contest capital, and the settlement
// outcome. One active enrollment per user (user_id is the primary key).
type JinguiziEnrollment struct {
	UserID         int64      `json:"user_id"`
	Tier           string     `json:"tier"`            // small / medium / large
	InitialCapital float64    `json:"initial_capital"` // 金龟子模拟币 granted at enrollment
	Status         string     `json:"status"`          // active / settled / eliminated
	ContestID      int64      `json:"contest_id"`      // optional link to a Contest
	EnrolledAt     time.Time  `json:"enrolled_at"`
	SettledAt      *time.Time `json:"settled_at,omitempty"`
	Remark         string     `json:"remark,omitempty"`
	// ---- 实时判定字段（自动淘汰 / 阶段达标） ----
	PeakEquity  float64 `json:"peak_equity"`  // 历史最高动态权益
	StageReached int     `json:"stage_reached"` // 已通过的最高阶段(月)：0/1/3/6/9
}

// JinguiziTransaction records every change to a 金龟子 wallet.
//   - admin_recharge : admin grants coins to a participant
//   - admin_deduct   : admin removes coins (penalty / entry fee / correction)
//   - contest_entry  : coins granted as contest enrollment capital
//   - contest_reward : awarded for contest performance (settlement)
//   - settlement     : end-of-contest settlement (e.g. eliminated → capital reclaimed)
type JinguiziTransaction struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	OperatorID    int64     `json:"operator_id"` // admin who acted; 0 = system
	Type          string    `json:"type"`
	Amount        float64   `json:"amount"` // signed
	BalanceBefore float64   `json:"balance_before"`
	BalanceAfter  float64   `json:"balance_after"`
	Remark        string    `json:"remark,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}
