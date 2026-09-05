package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goldarena/goldarena/internal/common"
	"github.com/goldarena/goldarena/pkg/errs"
)

// 选拔赛档位（报名时由管理员按档位往金龟子钱包充值对应参赛资金）
const (
	JinguiziTierSmall  = "small"  // 小账户 100万
	JinguiziTierMedium = "medium" // 中账户 500万
	JinguiziTierLarge  = "large"  // 大账户 1000万
)

var jinguiziTierCapital = map[string]float64{
	JinguiziTierSmall:  1000000,
	JinguiziTierMedium: 5000000,
	JinguiziTierLarge:  10000000,
}

// jinguiziTierFee is the real-money 管理费 charged for 缴费报名 (per tier).
var jinguiziTierFee = map[string]float64{
	JinguiziTierSmall:  200,
	JinguiziTierMedium: 1000,
	JinguiziTierLarge:  2000,
}

var jinguiziTierLabel = map[string]string{
	JinguiziTierSmall:  "小账户(100万)",
	JinguiziTierMedium: "中账户(500万)",
	JinguiziTierLarge:  "大账户(1000万)",
}

// jinguiziStageTargets are the cumulative-return gates a participant must clear at
// each milestone month. Falling short at a milestone eliminates them; clearing it
// marks the stage reached. Mirrors the 选拔赛海报 rules.
var jinguiziStageTargets = []struct {
	Months    int
	ReturnPct float64
}{
	{1, 0.01}, // 1月 ≥ 1%
	{3, 0.10}, // 3月 ≥ 10%
	{6, 0.29}, // 6月 ≥ 29% (赛期终点)
}

// jinguiziRewardCoeff defines the 选拔赛达标奖励 formula per tier:
//   reward = (Base + ReturnPct × Coeff) × Fee,   triggered when ReturnPct >= Trigger
// Trigger / Base / Coeff are all set so that hitting the trigger exactly gives a
// reward equal to 1× the management fee (so the user's "minimum profitable exit"
// is also the lowest reward tier). Higher returnPct then scales linearly per
// tier's Coeff. 触发线 ≥ 20% applies to every tier.
var jinguiziRewardCoeff = map[string]struct {
	Base, Coeff, Trigger float64
}{
	JinguiziTierSmall:  {Base: 1, Coeff: 1, Trigger: 0.20}, // (1 + 1×r) × 200
	JinguiziTierMedium: {Base: 1, Coeff: 2, Trigger: 0.20}, // (1 + 2×r) × 1000
	JinguiziTierLarge:  {Base: 2, Coeff: 3, Trigger: 0.20}, // (2 + 3×r) × 2000
}

// jinguiziFeeRefundPct is the fraction of the 管理费 refunded back to the
// participant's 游戏币 wallet on a successful settle (达标 or 未达 trigger 都退).
const jinguiziFeeRefundPct = 0.06

// jinguiziRewardResult bundles everything the settle handler needs in one call:
// the cash portion to refund to the 游戏币 wallet, the bonus portion (if any)
// to credit into the 金龟子 wallet, and whether the reward trigger fired.
type jinguiziRewardResult struct {
	FeeRefund float64 // 6% of 管理费 → 游戏币钱包
	Reward    float64 // (Base + r*Coeff)*Fee → 金龟子钱包 (0 if 未达触发线)
	Triggered bool    // true if ReturnPct >= Trigger (reward fired)
	Reason    string  // human-readable summary, e.g. "达标奖励: 公式(1+25%×2)*1000=1500元"
}

// calculateJinguiziReward applies the 选拔赛 settlement formula to a tier at a
// given cumulative returnPct. refund always equals fee × jinguiziFeeRefundPct (6%).
// reward is only non-zero when the per-tier trigger (≥20%) is cleared.
func calculateJinguiziReward(tier string, returnPct float64) jinguiziRewardResult {
	fee := jinguiziTierFee[tier]
	res := jinguiziRewardResult{
		FeeRefund: fee * jinguiziFeeRefundPct,
		Reason:    fmt.Sprintf("退管理费%.0f%%=¥%.0f", jinguiziFeeRefundPct*100, fee*jinguiziFeeRefundPct),
	}
	c, ok := jinguiziRewardCoeff[tier]
	if !ok {
		return res
	}
	if returnPct < c.Trigger {
		res.Reason = fmt.Sprintf("退管理费%.0f%%=¥%.0f;盈利率%.1f%%未达触发线%.0f%%,无奖励",
			jinguiziFeeRefundPct*100, res.FeeRefund, returnPct*100, c.Trigger*100)
		return res
	}
	res.Reward = (c.Base + returnPct*c.Coeff) * fee
	res.Triggered = true
	res.Reason = fmt.Sprintf("退管理费%.0f%%=¥%.0f;达标奖励: (%.0f+%.1f%%×%d)×¥%.0f = ¥%.0f",
		jinguiziFeeRefundPct*100, res.FeeRefund,
		c.Base, returnPct*100, int(c.Coeff), fee, res.Reward)
	return res
}

// JinguiziService manages the isolated 金龟子模拟币 wallet. It is intentionally
// decoupled from the main game-coin wallet: no shared structs, tables, or
// recharge paths. Admins recharge participants directly (manual grant); there is
// no real-money payment flow for this currency.
type JinguiziService struct {
	mem       *common.MemoryStore
	marketSvc *MarketService
}

func NewJinguiziService(mem *common.MemoryStore, marketSvc *MarketService) *JinguiziService {
	return &JinguiziService{mem: mem, marketSvc: marketSvc}
}

// resolveTargetUser resolves a target user from either user_id or username.
// Returns (userID, nil) when found, or an error response already written.
func (s *JinguiziService) resolveTargetUser(c *gin.Context, userID int64, username string) (int64, bool) {
	if userID != 0 {
		if u := s.mem.GetUserByID(userID); u != nil {
			return userID, true
		}
		// userID 查不到时回退：可能前端把数字用户名(如"555")当成了 user_id
		if username != "" {
			if u := s.mem.GetUserByUsername(username); u != nil {
				return u.ID, true
			}
		}
		common.Error(c, errs.UserNotFound, "用户不存在")
		return 0, false
	}
	if username != "" {
		u := s.mem.GetUserByUsername(username)
		if u == nil {
			common.Error(c, errs.UserNotFound, "用户不存在")
			return 0, false
		}
		return u.ID, true
	}
	common.Error(c, errs.InvalidParam, "user_id 或 username 必须提供一个")
	return 0, false
}

// ========== Admin endpoints (require role=admin) ==========

type AdminRechargeJinguiziReq struct {
	UserID   int64   `json:"user_id"`
	Username string  `json:"username"`
	Amount   float64 `json:"amount" binding:"required"` // must be > 0
	Remark   string  `json:"remark"`
}

// AdminRecharge grants 金龟子模拟币 to a participant. This is the ONLY way coins
// enter the system — there is no payment path.
func (s *JinguiziService) AdminRecharge(c *gin.Context) {
	operatorID := c.GetInt64("user_id")
	var req AdminRechargeJinguiziReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	uid, ok := s.resolveTargetUser(c, req.UserID, req.Username)
	if !ok {
		return
	}
	if req.Amount <= 0 {
		common.Error(c, errs.InvalidParam, "amount 必须为正数")
		return
	}

	w := s.mem.EnsureJinguiziWallet(uid)
	balanceBefore := w.Balance
	newBalance := balanceBefore + req.Amount
	s.mem.UpdateJinguiziBalance(uid, newBalance, w.Frozen)
	s.mem.AddJinguiziRecharged(uid, req.Amount)

	remark := req.Remark
	if remark == "" {
		remark = "管理员充值金龟子模拟币"
	}
	s.mem.SaveJinguiziTransaction(uid, &common.JinguiziTransaction{
		UserID: uid, OperatorID: operatorID, Type: "admin_recharge", Amount: req.Amount,
		BalanceBefore: balanceBefore, BalanceAfter: newBalance, Remark: remark, CreatedAt: time.Now(),
	})

	common.Success(c, gin.H{
		"user_id":        uid,
		"amount":         req.Amount,
		"balance_before": balanceBefore,
		"balance_after":  newBalance,
	})
}

type AdminAdjustJinguiziReq struct {
	UserID   int64   `json:"user_id"`
	Username string  `json:"username"`
	Amount   float64 `json:"amount"` // signed delta (may be negative)
	Remark   string  `json:"remark"`
}

// AdminAdjust applies a signed delta to a participant's 金龟子 balance (e.g.
// penalty deduction, entry-fee, or correction). Negative values never push the
// balance below zero.
func (s *JinguiziService) AdminAdjust(c *gin.Context) {
	operatorID := c.GetInt64("user_id")
	var req AdminAdjustJinguiziReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	uid, ok := s.resolveTargetUser(c, req.UserID, req.Username)
	if !ok {
		return
	}
	if req.Amount == 0 {
		common.Error(c, errs.InvalidParam, "amount 不能为 0")
		return
	}

	w := s.mem.EnsureJinguiziWallet(uid)
	balanceBefore := w.Balance
	newBalance := balanceBefore + req.Amount
	if newBalance < 0 {
		newBalance = 0
	}
	delta := newBalance - balanceBefore
	s.mem.UpdateJinguiziBalance(uid, newBalance, w.Frozen)

	txnType := "admin_recharge"
	if delta < 0 {
		txnType = "admin_deduct"
	}
	remark := req.Remark
	if remark == "" {
		if delta > 0 {
			remark = "管理员增加金龟子模拟币"
		} else {
			remark = "管理员扣减金龟子模拟币"
		}
	}
	s.mem.SaveJinguiziTransaction(uid, &common.JinguiziTransaction{
		UserID: uid, OperatorID: operatorID, Type: txnType, Amount: delta,
		BalanceBefore: balanceBefore, BalanceAfter: newBalance, Remark: remark, CreatedAt: time.Now(),
	})

	common.Success(c, gin.H{
		"user_id":        uid,
		"delta":          delta,
		"balance_before": balanceBefore,
		"balance_after":  newBalance,
	})
}

// AdminList returns every 金龟子 wallet (with joined user info) for the admin console.
func (s *JinguiziService) AdminList(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("keyword"))
	all := s.mem.GetAllJinguiziWallets()
	rows := make([]gin.H, 0)
	for _, w := range all {
		u := s.mem.GetUserByID(w.UserID)
		uname, nick := "", ""
		if u != nil {
			uname, nick = u.Username, u.Nickname
		}
		if keyword != "" && !strings.Contains(uname, keyword) && !strings.Contains(nick, keyword) {
			continue
		}
		rows = append(rows, gin.H{
			"user_id":         w.UserID,
			"username":        uname,
			"nickname":        nick,
			"balance":         w.Balance,
			"frozen":          w.Frozen,
			"total_recharged": w.TotalRecharged,
		})
		// Attach enrollment summary when the user is in the 选拔赛.
		if enr := s.mem.GetJinguiziEnrollment(w.UserID); enr != nil {
			rows[len(rows)-1]["enrollment_status"] = enr.Status
			rows[len(rows)-1]["tier"] = enr.Tier
			rows[len(rows)-1]["initial_capital"] = enr.InitialCapital
			rows[len(rows)-1]["stage_reached"] = enr.StageReached
			rows[len(rows)-1]["peak_equity"] = enr.PeakEquity
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i]["user_id"].(int64) > rows[j]["user_id"].(int64)
	})
	common.Success(c, gin.H{"total": len(rows), "list": rows})
}

// ========== User endpoints (require login) ==========

// GetWallet returns the caller's own 金龟子 wallet.
func (s *JinguiziService) GetWallet(c *gin.Context) {
	userID := c.GetInt64("user_id")
	w := s.mem.EnsureJinguiziWallet(userID)
	common.Success(c, w)
}

// GetTransactions returns the caller's own 金龟子 transaction history (newest first).
func (s *JinguiziService) GetTransactions(c *gin.Context) {
	userID := c.GetInt64("user_id")
	page, pageSize := parsePagination(c)

	all := s.mem.GetJinguiziTransactions(userID)
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	total := len(all)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	list := all[start:end]
	if list == nil {
		list = []common.JinguiziTransaction{}
	}
	common.Success(c, gin.H{
		"list":      list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ========== 选拔赛报名 / 结算（contest lifecycle, 金龟子子系统） ==========

type AdminEnrollJinguiziReq struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	Tier      string `json:"tier"` // small / medium / large
	ContestID int64  `json:"contest_id"`
}

// AdminEnroll enrolls a participant into the 金龟子选拔赛 at a chosen tier. It
// grants the tier's dedicated contest capital into the isolated 金龟子 wallet
// (contest_entry) and records an active enrollment. Re-enrolling a user who is
// already active is rejected; a settled/eliminated user may re-enroll.
func (s *JinguiziService) AdminEnroll(c *gin.Context) {
	operatorID := c.GetInt64("user_id")
	var req AdminEnrollJinguiziReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	capital, ok := jinguiziTierCapital[req.Tier]
	if !ok {
		common.Error(c, errs.InvalidParam, "tier 必须是 small / medium / large 之一")
		return
	}
	uid, ok := s.resolveTargetUser(c, req.UserID, req.Username)
	if !ok {
		return
	}

	// Reject duplicate active enrollment (allow re-enroll after settle/eliminate).
	if ex := s.mem.GetJinguiziEnrollment(uid); ex != nil && ex.Status == "active" {
		common.Error(c, errs.InvalidParam, "该用户已报名且参赛中，不能重复报名")
		return
	}

	label := jinguiziTierLabel[req.Tier]
	capital, balanceBefore, newBalance, err := s.enrollCore(operatorID, uid, req.Tier, req.ContestID, "选拔赛报名·"+label)
	if err != nil {
		common.Error(c, errs.Internal, err.Error())
		return
	}

	common.Success(c, gin.H{
		"user_id":          uid,
		"tier":             req.Tier,
		"tier_label":       label,
		"initial_capital":  capital,
		"balance_before":   balanceBefore,
		"balance_after":    newBalance,
		"enrollment_status": "active",
	})
}

// enrollCore grants the tier's contest capital into the isolated 金龟子 wallet,
// records the contest_entry transaction and the active enrollment. Shared by
// admin manual enrollment and paid (缴费) enrollment.
func (s *JinguiziService) enrollCore(operatorID int64, uid int64, tier string, contestID int64, remark string) (capital, balanceBefore, balanceAfter float64, err error) {
	capital, ok := jinguiziTierCapital[tier]
	if !ok {
		return 0, 0, 0, fmt.Errorf("tier 必须是 small / medium / large 之一")
	}
	w := s.mem.EnsureJinguiziWallet(uid)
	balanceBefore = w.Balance
	balanceAfter = balanceBefore + capital
	s.mem.UpdateJinguiziBalance(uid, balanceAfter, w.Frozen)
	s.mem.AddJinguiziRecharged(uid, capital)

	now := time.Now()
	s.mem.SaveJinguiziTransaction(uid, &common.JinguiziTransaction{
		UserID: uid, OperatorID: operatorID, Type: "contest_entry", Amount: capital,
		BalanceBefore: balanceBefore, BalanceAfter: balanceAfter,
		Remark: remark, CreatedAt: now,
	})

	enr := &common.JinguiziEnrollment{
		UserID:         uid,
		Tier:           tier,
		InitialCapital: capital,
		Status:         "active",
		ContestID:      contestID,
		EnrolledAt:     now,
		Remark:         jinguiziTierLabel[tier],
	}
	s.mem.SaveJinguiziEnrollment(enr)
	return capital, balanceBefore, balanceAfter, nil
}

// CheckCanEnroll verifies the user exists and has no active enrollment. Used by
// the 缴费报名 flow before opening a payment order.
func (s *JinguiziService) CheckCanEnroll(userID int64) error {
	if s.mem.GetUserByID(userID) == nil {
		return fmt.Errorf("用户不存在")
	}
	if ex := s.mem.GetJinguiziEnrollment(userID); ex != nil && ex.Status == "active" {
		return fmt.Errorf("该用户已报名且参赛中，不能重复报名")
	}
	return nil
}

// EnrollAfterPayment auto-enrolls a user whose 报名费 payment just succeeded.
// Called from PaymentService.creditIfPending for contest_<tier> orders. If the
// user became active meanwhile (e.g. admin enrolled manually), it succeeds as a
// no-op so the order can still be closed as paid.
func (s *JinguiziService) EnrollAfterPayment(userID int64, tier, outTradeNo string, fee float64) error {
	if _, ok := jinguiziTierCapital[tier]; !ok {
		return fmt.Errorf("无效档位: %s", tier)
	}
	if ex := s.mem.GetJinguiziEnrollment(userID); ex != nil && ex.Status == "active" {
		log.Printf("[JINGUIZI] enroll-after-payment: user=%d already active, skip (order=%s)", userID, outTradeNo)
		return nil
	}
	label := jinguiziTierLabel[tier]
	capital, before, after, err := s.enrollCore(0, userID, tier, 0,
		fmt.Sprintf("缴费报名·%s(报名费¥%.0f,订单%s)", label, fee, outTradeNo))
	if err != nil {
		return err
	}
	log.Printf("[JINGUIZI] user=%d paid-enrolled tier=%s fee=%.0f capital=%.0f (order=%s, balance %.0f -> %.0f)",
		userID, tier, fee, capital, outTradeNo, before, after)
	return nil
}

type AdminSettleJinguiziReq struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Action   string `json:"action"` // settle (达标结算) / eliminate (淘汰)
	Note     string `json:"note"`
}

// AdminSettle settles an active enrollment.
//   - eliminate: reclaims the dedicated contest capital (floor 0) and marks eliminated.
//   - settle:    computes the current cumulative returnPct = (equity-initial)/initial,
//     refunds 6% of the 管理费 to the user's 游戏币 wallet, then — if returnPct
//     ≥ 20% (per tier) — credits the formula reward
//     (Base + ReturnPct × Coeff) × Fee into the 金龟子 wallet.
//     Below the trigger, only the 6% refund is granted. Marks settled either way.
func (s *JinguiziService) AdminSettle(c *gin.Context) {
	operatorID := c.GetInt64("user_id")
	var req AdminSettleJinguiziReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	if req.Action != "settle" && req.Action != "eliminate" {
		common.Error(c, errs.InvalidParam, "action 必须是 settle 或 eliminate")
		return
	}
	uid, ok := s.resolveTargetUser(c, req.UserID, req.Username)
	if !ok {
		return
	}
	enr := s.mem.GetJinguiziEnrollment(uid)
	if enr == nil || enr.Status != "active" {
		common.Error(c, errs.InvalidParam, "该用户没有进行中的参赛记录")
		return
	}
	now := time.Now()
	out := gin.H{
		"user_id":          uid,
		"action":           req.Action,
		"tier":             enr.Tier,
		"enrollment_status": "",
	}

	if req.Action == "eliminate" {
		// 淘汰: 收回参赛资金(原有逻辑,无奖励无退费)
		jw := s.mem.EnsureJinguiziWallet(uid)
		balanceBefore := jw.Balance
		reclaim := enr.InitialCapital
		newBalance := balanceBefore - reclaim
		if newBalance < 0 {
			newBalance = 0
		}
		delta := newBalance - balanceBefore
		s.mem.UpdateJinguiziBalance(uid, newBalance, jw.Frozen)
		s.mem.SaveJinguiziTransaction(uid, &common.JinguiziTransaction{
			UserID: uid, OperatorID: operatorID, Type: "settlement", Amount: delta,
			BalanceBefore: balanceBefore, BalanceAfter: newBalance,
			Remark: "选拔赛淘汰·收回参赛资金", CreatedAt: now,
		})
		enr.Status = "eliminated"
		enr.SettledAt = &now
		s.mem.SaveJinguiziEnrollment(enr)
		out["balance_before"] = balanceBefore
		out["balance_after"] = newBalance
		out["delta"] = delta
		out["enrollment_status"] = "eliminated"
		common.Success(c, out)
		return
	}

	// === settle: 公式结算 ===
	_, _, _, equity := s.computeJinguiziEquity(uid)
	returnPct := 0.0
	if enr.InitialCapital > 0 {
		returnPct = equity/enr.InitialCapital - 1
	}
	res := calculateJinguiziReward(enr.Tier, returnPct)

	// 1) 退 6% 管理费 → 游戏币钱包
	if res.FeeRefund > 0 {
		gw := s.mem.GetWallet(uid)
		if gw == nil {
			// 游戏币钱包不存在则用 EnsureJinguiziWallet 同款的零值初始化,
			// 这里直接用 GetWallet 拿到的 nil 走默认 0; 实际产品上每个用户
			// 都有游戏币钱包(注册时建), 这里只是兜底.
			gw = &common.Wallet{UserID: uid, Balance: 0, Frozen: 0}
		}
		gbBefore := gw.Balance
		gbAfter := gbBefore + res.FeeRefund
		s.mem.UpdateWalletBalance(uid, gbAfter, gw.Frozen)
		s.mem.SaveWalletTransaction(uid, &common.WalletTransaction{
			UserID:        uid,
			Type:          "contest_fee_refund",
			Amount:        res.FeeRefund,
			BalanceBefore: gbBefore,
			BalanceAfter:  gbAfter,
			Remark: fmt.Sprintf("选拔赛结算·退还%.0f%%管理费(%s)", jinguiziFeeRefundPct*100, jinguiziTierLabel[enr.Tier]),
			CreatedAt:     now,
		})
		out["gamecoin_balance_before"] = gbBefore
		out["gamecoin_balance_after"] = gbAfter
		out["fee_refund"] = res.FeeRefund
	}

	// 2) 发放奖励 → 金龟子钱包(仅当触发)
	jwBefore := 0.0
	jwAfter := 0.0
	if res.Reward > 0 {
		jw := s.mem.EnsureJinguiziWallet(uid)
		jwBefore = jw.Balance
		jwAfter = jwBefore + res.Reward
		s.mem.UpdateJinguiziBalance(uid, jwAfter, jw.Frozen)
		s.mem.AddJinguiziRecharged(uid, res.Reward)
		s.mem.SaveJinguiziTransaction(uid, &common.JinguiziTransaction{
			UserID:        uid,
			OperatorID:    operatorID,
			Type:          "contest_reward",
			Amount:        res.Reward,
			BalanceBefore: jwBefore,
			BalanceAfter:  jwAfter,
			Remark:        fmt.Sprintf("选拔赛达标奖励(%s,盈利率%.1f%%)", jinguiziTierLabel[enr.Tier], returnPct*100),
			CreatedAt:     now,
		})
	}
	enr.Status = "settled"
	enr.SettledAt = &now
	s.mem.SaveJinguiziEnrollment(enr)

	out["return_pct"] = returnPct
	out["reward"] = res.Reward
	out["triggered"] = res.Triggered
	out["reward_reason"] = res.Reason
	out["jinguizi_balance_before"] = jwBefore
	out["jinguizi_balance_after"] = jwAfter
	out["enrollment_status"] = "settled"
	common.Success(c, out)
}

// GetEnrollment returns the caller's own 选拔赛 enrollment plus a live snapshot of
// their contest equity / drawdown / stage progress.
func (s *JinguiziService) GetEnrollment(c *gin.Context) {
	userID := c.GetInt64("user_id")
	e := s.mem.GetJinguiziEnrollment(userID)
	if e == nil {
		common.Success(c, gin.H{"enrollment": nil})
		return
	}
	balance, frozen, unrealized, equity := s.computeJinguiziEquity(userID)
	principalDD := 0.0
	if e.InitialCapital > 0 {
		principalDD = (e.InitialCapital - equity) / e.InitialCapital
	}
	peakDD := 0.0
	if e.PeakEquity > 0 {
		peakDD = (e.PeakEquity - equity) / e.PeakEquity
	}
	ret := 0.0
	if e.InitialCapital > 0 {
		ret = equity/e.InitialCapital - 1
	}
	common.Success(c, gin.H{
		"enrollment": e,
		"equity": gin.H{
			"balance":             balance,
			"frozen":              frozen,
			"unrealized":          unrealized,
			"dynamic_equity":      equity,
			"peak_equity":         e.PeakEquity,
			"principal_drawdown":  principalDD,
			"peak_drawdown":       peakDD,
			"return_rate":         ret,
			"stage_reached":       e.StageReached,
		},
	})
}

// ========== 实时判定（自动淘汰 / 阶段达标） ==========

const jinguiziContractSize = 100.0

// jinguiziPositionPnL computes the floating PnL of a position at the given price.
func jinguiziPositionPnL(p common.Position, price float64) float64 {
	var diff float64
	if p.Direction == 1 {
		diff = price - p.OpenPrice
	} else {
		diff = p.OpenPrice - price
	}
	return diff * jinguiziContractSize * p.Volume
}

// computeJinguiziEquity returns the user's 金龟子 wallet balance, frozen margin,
// total unrealized PnL across open positions, and the resulting dynamic equity.
func (s *JinguiziService) computeJinguiziEquity(userID int64) (balance, frozen, unrealized, equity float64) {
	w := s.mem.EnsureJinguiziWallet(userID)
	balance = w.Balance
	frozen = w.Frozen
	positions := s.mem.GetPositions(userID, nil)
	for _, p := range positions {
		if p.Status != 1 {
			continue
		}
		var price float64
		if s.marketSvc != nil {
			if q := s.marketSvc.GetCachedQuote(p.Symbol); q != nil && q.Price > 0 {
				price = q.Price
			}
		}
		if price <= 0 {
			price = p.CurrentPrice // stale fallback
		}
		unrealized += jinguiziPositionPnL(p, price)
	}
	equity = balance + frozen + unrealized
	return
}

// StartJudgeLoop periodically evaluates every active enrollment for drawdown and
// stage-profit elimination. Runs as a background goroutine.
func (s *JinguiziService) StartJudgeLoop() {
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for range ticker.C {
			s.JudgeAll()
		}
	}()
}

// JudgeAll evaluates all active enrollments once.
func (s *JinguiziService) JudgeAll() {
	for _, enr := range s.mem.GetAllActiveEnrollments() {
		s.evaluateEnrollment(enr)
	}
}

// evaluateEnrollment applies the 选拔赛 elimination rules to a single active enrollment.
func (s *JinguiziService) evaluateEnrollment(enr *common.JinguiziEnrollment) {
	now := time.Now()
	_, _, _, equity := s.computeJinguiziEquity(enr.UserID)

	// Track peak dynamic equity.
	if equity > enr.PeakEquity {
		enr.PeakEquity = equity
	}

	// 1) Principal drawdown >= 5% -> eliminate.
	principalDD := 0.0
	if enr.InitialCapital > 0 {
		principalDD = (enr.InitialCapital - equity) / enr.InitialCapital
	}
	if principalDD >= 0.05 {
		s.eliminateEnrollment(enr, fmt.Sprintf("本金回撤%.1f%%≥5%%", principalDD*100))
		return
	}

	// 2) Peak dynamic-equity drawdown >= 6% -> eliminate.
	if enr.PeakEquity > 0 {
		peakDD := (enr.PeakEquity - equity) / enr.PeakEquity
		if peakDD >= 0.06 {
			s.eliminateEnrollment(enr, fmt.Sprintf("历史最高动态权益回撤%.1f%%≥6%%", peakDD*100))
			return
		}
	}

	// 3) Stage-profit gates (1月1% / 3月10% / 6月29%).
	elapsed := now.Sub(enr.EnrolledAt)
	for _, st := range jinguiziStageTargets {
		if enr.StageReached >= st.Months {
			continue
		}
		if elapsed < time.Duration(st.Months)*30*24*time.Hour {
			continue
		}
		ret := 0.0
		if enr.InitialCapital > 0 {
			ret = equity/enr.InitialCapital - 1
		}
		if ret < st.ReturnPct {
			s.eliminateEnrollment(enr, fmt.Sprintf("%d月阶段盈利未达标(需≥%.0f%%,实际%.1f%%)", st.Months, st.ReturnPct*100, ret*100))
			return
		}
		enr.StageReached = st.Months
	}

	s.mem.SaveJinguiziEnrollment(enr)
}

// eliminateEnrollment reclaims all remaining 金龟子 contest funds and marks the
// enrollment eliminated. The contest account is forfeit on elimination.
func (s *JinguiziService) eliminateEnrollment(enr *common.JinguiziEnrollment, reason string) {
	jw := s.mem.EnsureJinguiziWallet(enr.UserID)
	before := jw.Balance + jw.Frozen
	now := time.Now()
	s.mem.UpdateJinguiziBalance(enr.UserID, 0, 0)
	s.mem.SaveJinguiziTransaction(enr.UserID, &common.JinguiziTransaction{
		UserID: enr.UserID, OperatorID: 0, Type: "settlement", Amount: -before,
		BalanceBefore: before, BalanceAfter: 0,
		Remark: "选拔赛自动淘汰·" + reason, CreatedAt: now,
	})
	enr.Status = "eliminated"
	sa := now
	enr.SettledAt = &sa
	s.mem.SaveJinguiziEnrollment(enr)
	log.Printf("[JINGUIZI] user=%d eliminated: %s (equity before=%.2f)", enr.UserID, reason, before)
}

// AdminJudge forces an immediate evaluation pass (admin tool / test hook).
func (s *JinguiziService) AdminJudge(c *gin.Context) {
	s.JudgeAll()
	common.Success(c, gin.H{"message": "判定已执行", "active_remaining": len(s.mem.GetAllActiveEnrollments())})
}
