package main

import (
	"encoding/json"
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
	{1, 0.10}, // 1月 ≥ 10%
	{3, 0.50}, // 3月 ≥ 50%
	{6, 1.00}, // 6月 ≥ 100% (赛期终点)
}

// jinguiziRewardCoeff defines the 选拔赛达标奖励 formula per tier. The bonus is a
// FIXED amount granted once the participant clears the 触发线 (returnPct >= Trigger,
// i.e. 盈利 ≥ 100%). 触发后奖金 = (Base + jinguiziRewardConstPct × Coeff) × Fee;
// 其中的 20% 是固定系数(见 jinguiziRewardConstPct), 不随实际盈利率变动.
// Base / Coeff / Fee 见下表. 触发线 盈利 ≥ 100% 对所有档位一致.
var jinguiziRewardCoeff = map[string]struct {
	Base, Coeff, Trigger float64
}{
	JinguiziTierSmall:  {Base: 1, Coeff: 1, Trigger: 1.00}, // (1 + 20%×1) × 200  = 240
	JinguiziTierMedium: {Base: 1, Coeff: 2, Trigger: 1.00}, // (1 + 20%×2) × 1000 = 1400
	JinguiziTierLarge:  {Base: 2, Coeff: 3, Trigger: 1.00}, // (2 + 20%×3) × 2000 = 5200
}

// jinguiziRewardConstPct is the FIXED percent used in the 达标奖励 bonus once the
// 触发线 is cleared: bonus = (Base + jinguiziRewardConstPct × Coeff) × Fee. It is
// NOT the participant's actual returnPct — the bonus is a fixed amount per tier.
const jinguiziRewardConstPct = 0.20

// 【报名费=赛事管理费】规则 (2026-09-09 起, 2026-09-09 追加中档): 6月结算时按盈利分三档:
//  1) 盈利 ≥ 100% (达标最终考核): 全额退还报名费(=赛事管理费) + 固定达标奖励,
//     退费 ¥200/¥1000/¥2000, 奖励 (Base + 20%×Coeff)×Fee = 240/1400/5200。
//  2) 10% < 盈利 < 100% (盈利大于10%但未达最终考核标准): 仅**全额退还管理费**, 无奖励。
//  3) 盈利 ≤ 10% (未达标): 报名费不退还, 亦无奖励。
// 旧的「盈利≥6% 退 6% 管理费」两道门槛已废除。
// (历史: jinguiziFeeRefundPct=0.06 / jinguiziFeeRefundTrigger=0.06 已删除)

// jinguiziFeeRefundTrigger is the 6月结算中档门槛: 盈利 > 10% 即退还管理费(无奖励),
// 盈利 ≤ 10% 不退费。注意它与达标线(100%)无关, 仅决定管理费是否退还。
const jinguiziFeeRefundTrigger = 0.10

// jinguiziRewardResult bundles everything the settle handler needs in one call:
// the cash portions (fee refund + bonus) to be manually granted by the admin,
// and which gate fired.
type jinguiziRewardResult struct {
	FeeRefund float64 // 报名费/管理费退款 → 游戏币钱包手动流水 (达标或中档>10% 时=全额 fee, 否则 0)
	Reward    float64 // (Base + 20%*Coeff)*Fee 固定奖金 → 现金人工发放(发消息通知用户) (仅达标≥100% 时>0)
	Triggered bool    // true if ReturnPct >= 100% 达标线 (退费 + 奖励同时触发)
	Reason    string  // human-readable summary
}

// calculateJinguiziReward applies the 选拔赛 6月结算 formula to a tier at a
// given cumulative returnPct. Three outcomes:
//  1) returnPct >= Trigger(100%) 达标: 全额退报名费 + 固定奖励 (Base + 20% × Coeff) × Fee。
//  2) jinguiziFeeRefundTrigger(10%) < returnPct < 100%: 仅全额退管理费(=报名费), 无奖励。
//  3) returnPct <= 10%: 两者皆为 0 (报名费不退还, 无奖励)。
// 退费与奖励均为现金人工发放。
func calculateJinguiziReward(tier string, returnPct float64) jinguiziRewardResult {
	fee := jinguiziTierFee[tier]
	c, ok := jinguiziRewardCoeff[tier]
	trigger := 1.00
	if ok {
		trigger = c.Trigger
	}
	switch {
	case returnPct >= trigger: // 达标最终考核
		res := jinguiziRewardResult{Triggered: true}
		res.FeeRefund = fee
		res.Reward = (c.Base + jinguiziRewardConstPct*c.Coeff) * fee
		res.Reason = fmt.Sprintf("达标(盈利≥%.0f%%): 全额退还报名费/管理费¥%.0f; 达标奖励: (%.0f+%.0f%%×%d)×¥%.0f = ¥%.0f",
			trigger*100, res.FeeRefund, c.Base, jinguiziRewardConstPct*100, int(c.Coeff), fee, res.Reward)
		return res
	case returnPct > jinguiziFeeRefundTrigger: // 中档: 盈利>10% 未达最终标准, 退管理费无奖励
		return jinguiziRewardResult{
			FeeRefund: fee,
			Reason:    fmt.Sprintf("盈利%.1f%%>%.0f%%但未达最终考核标准(%.0f%%): 仅全额退还管理费¥%.0f, 无奖励", returnPct*100, jinguiziFeeRefundTrigger*100, trigger*100, fee),
		}
	default: // 未达标: 盈利≤10%, 不退费无奖励
		return jinguiziRewardResult{
			Reason: fmt.Sprintf("盈利%.1f%%≤%.0f%%: 未达标, 报名费不退还, 无奖励", returnPct*100, jinguiziFeeRefundTrigger*100),
		}
	}
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
			rows[len(rows)-1]["eliminated_reason"] = enr.EliminatedReason
			rows[len(rows)-1]["eliminated_snapshot"] = enr.EliminatedSnapshot
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
//     then computes the per-tier result via calculateJinguiziReward (达标线 ≥ 100%).
//     ⚠️ 【只入金不出金, 奖励由人工发放】结算时**不**自动入游戏币/金龟子钱包。
//     【报名费=赛事管理费】达标时**全额退还报名费**, 不达标不退还:
//     仅写一条 manual 流水(type=contest_fee_refund_manual)记录待人工发放的全额报名费退款,
//     而**达标奖励不写金龟子钱包流水**(奖励为现金, 不是金龟子币), 改为向用户发送达标通知消息.
//     Marks settled either way.
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

	// 1) 达标 → 全额退还报名费(=赛事管理费), 仅记 manual 流水不入游戏币钱包
	//    (平台只入金不出金, 由管理员线下发放; 不达标则 FeeRefund=0 无此流水)
	if res.FeeRefund > 0 {
		gw := s.mem.GetWallet(uid)
		if gw == nil {
			gw = &common.Wallet{UserID: uid, Balance: 0, Frozen: 0}
		}
		gbCurrent := gw.Balance
	// 平台政策: 退报名费不再自动入游戏币钱包, 仅记一条 manual_* 流水.
	// BalanceBefore == BalanceAfter == 当前余额, Amount>0 表示「待发放金额」.
	// 管理员可在 /admin/jinguizi 按用户名查流水, 自行通知用户发放.
	refundRemark := "选拔赛结算·全额退还报名费/管理费"
	if !res.Triggered {
		refundRemark = "选拔赛结算·盈利>10%未达最终标准·退还管理费"
	}
	s.mem.SaveWalletTransaction(uid, &common.WalletTransaction{
		UserID:        uid,
		Type:          "contest_fee_refund_manual",
		Amount:        res.FeeRefund,
		BalanceBefore: gbCurrent,
		BalanceAfter:  gbCurrent,
		Remark:        fmt.Sprintf("[待人工发放] %s(%s)", refundRemark, jinguiziTierLabel[enr.Tier]),
		CreatedAt:     now,
	})
		out["gamecoin_balance_before"] = gbCurrent
		out["gamecoin_balance_after"] = gbCurrent
		out["fee_refund"] = res.FeeRefund
		out["manual_pending_gamecoin"] = res.FeeRefund
	}

	// 2) 现金发放(退费/奖励) → 不写金龟子钱包流水, 改为向用户发送站内信通知。
	//    中档(仅退管理费)与达标(退费+奖励)均发信; 响应字段供管理员结算面板参考.
	jwCurrent := 0.0
	jw := s.mem.EnsureJinguiziWallet(uid)
	jwCurrent = jw.Balance
	if res.FeeRefund > 0 || res.Reward > 0 {
		msgParts := []string{}
		if res.FeeRefund > 0 {
			msgParts = append(msgParts, fmt.Sprintf("报名费全额退还 ¥%.0f 元", res.FeeRefund))
		}
		if res.Reward > 0 {
			msgParts = append(msgParts, fmt.Sprintf("达标奖励 ¥%.0f 元", res.Reward))
		}
		var msgContent string
		if res.Triggered {
			msgContent = fmt.Sprintf("🎉 恭喜您通过金龟子选拔赛达标(盈利率%.1f%%)！%s，均为现金，由管理员人工发放，请留意线下联系。",
				returnPct*100, strings.Join(msgParts, "、"))
		} else {
			msgContent = fmt.Sprintf("金龟子选拔赛结算(盈利率%.1f%%)：%s，均为现金，由管理员人工发放，请留意线下联系。",
				returnPct*100, strings.Join(msgParts, "、"))
		}
		s.mem.SaveMessage(&common.Message{
			UserID:    uid,
			Sender:    "platform",
			Content:   msgContent,
			Read:      false,
			CreatedAt: now,
		})
		if res.Reward > 0 {
			out["manual_pending_jinguizi"] = res.Reward
		}
	}
	enr.Status = "settled"
	enr.SettledAt = &now
	s.mem.SaveJinguiziEnrollment(enr)

	out["return_pct"] = returnPct
	out["reward"] = res.Reward
	out["triggered"] = res.Triggered
	out["reward_reason"] = res.Reason
	out["jinguizi_balance_before"] = jwCurrent
	out["jinguizi_balance_after"] = jwCurrent
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
		s.eliminateEnrollment(enr, fmt.Sprintf("本金回撤%.1f%%≥5%%(本金%.0f→权益%.0f)", principalDD*100, enr.InitialCapital, equity), equity)
		return
	}

	// 2) Peak dynamic-equity drawdown >= 6% -> eliminate.
	if enr.PeakEquity > 0 {
		peakDD := (enr.PeakEquity - equity) / enr.PeakEquity
		if peakDD >= 0.06 {
			s.eliminateEnrollment(enr, fmt.Sprintf("历史最高动态权益回撤%.1f%%≥6%%(峰值%.0f→权益%.0f)", peakDD*100, enr.PeakEquity, equity), equity)
			return
		}
	}

	// 3) Stage-profit gates (1月10% / 3月50% / 6月100%).
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
			s.eliminateEnrollment(enr, fmt.Sprintf("%d月阶段盈利未达标(需≥%.0f%%,实际%.1f%%)", st.Months, st.ReturnPct*100, ret*100), equity)
			return
		}
		enr.StageReached = st.Months
	}

	s.mem.SaveJinguiziEnrollment(enr)
}

// jinguiziEliminationSnapshot captures the account state at the moment of elimination,
// so the reason + positions + PnL are preserved for post-mortem review.
type jinguiziEliminationSnapshot struct {
	InitialCapital float64                `json:"initial_capital"`
	EquityAtElim   float64                `json:"equity_at_elim"`
	Loss           float64                `json:"loss"` // initial_capital - equity (forfeited amount)
	PeakEquity     float64                `json:"peak_equity"`
	Reason         string                 `json:"reason"`
	Positions      []jinguiziPositionSnap `json:"positions"`
}

type jinguiziPositionSnap struct {
	Symbol       string  `json:"symbol"`
	Direction    int     `json:"direction"` // 1=long, 2=short
	Volume       float64 `json:"volume"`
	OpenPrice    float64 `json:"open_price"`
	CurrentPrice float64 `json:"current_price"`
	Margin       float64 `json:"margin"`
	FloatingPnl  float64 `json:"floating_pnl"`
}

// eliminateEnrollment reclaims all remaining 金龟子 contest funds and marks the
// enrollment eliminated. The contest account is forfeit on elimination. It also
// records a clear elimination reason and a snapshot of positions + PnL at that moment.
func (s *JinguiziService) eliminateEnrollment(enr *common.JinguiziEnrollment, reason string, equity float64) {
	jw := s.mem.EnsureJinguiziWallet(enr.UserID)
	before := jw.Balance + jw.Frozen
	now := time.Now()

	// Build position + PnL snapshot (contest-scoped holdings only).
	var snaps []jinguiziPositionSnap
	positions := s.mem.GetPositions(enr.UserID, nil)
	for _, p := range positions {
		if p.Status != 1 {
			continue
		}
		if enr.ContestID != 0 {
			if p.ContestID == nil || *p.ContestID != enr.ContestID {
				continue
			}
		}
		var price float64
		if s.marketSvc != nil {
			if q := s.marketSvc.GetCachedQuote(p.Symbol); q != nil && q.Price > 0 {
				price = q.Price
			}
		}
		if price <= 0 {
			price = p.CurrentPrice
		}
		snaps = append(snaps, jinguiziPositionSnap{
			Symbol:       p.Symbol,
			Direction:    p.Direction,
			Volume:       p.Volume,
			OpenPrice:    p.OpenPrice,
			CurrentPrice: price,
			Margin:       p.Margin,
			FloatingPnl:  jinguiziPositionPnL(p, price),
		})
	}
	snap := jinguiziEliminationSnapshot{
		InitialCapital: enr.InitialCapital,
		EquityAtElim:   equity,
		Loss:           enr.InitialCapital - equity,
		PeakEquity:     enr.PeakEquity,
		Reason:         reason,
		Positions:      snaps,
	}
	if b, err := json.Marshal(snap); err == nil {
		enr.EliminatedSnapshot = string(b)
	}
	enr.EliminatedReason = reason

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
	log.Printf("[JINGUIZI] user=%d eliminated: %s (equity before=%.2f, positions=%d)", enr.UserID, reason, before, len(snaps))
}

// AdminJudge forces an immediate evaluation pass (admin tool / test hook).
func (s *JinguiziService) AdminJudge(c *gin.Context) {
	s.JudgeAll()
	common.Success(c, gin.H{"message": "判定已执行", "active_remaining": len(s.mem.GetAllActiveEnrollments())})
}
