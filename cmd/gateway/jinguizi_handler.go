package main

import (
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

var jinguiziTierLabel = map[string]string{
	JinguiziTierSmall:  "小账户(100万)",
	JinguiziTierMedium: "中账户(500万)",
	JinguiziTierLarge:  "大账户(1000万)",
}

// JinguiziService manages the isolated 金龟子模拟币 wallet. It is intentionally
// decoupled from the main game-coin wallet: no shared structs, tables, or
// recharge paths. Admins recharge participants directly (manual grant); there is
// no real-money payment flow for this currency.
type JinguiziService struct {
	mem *common.MemoryStore
}

func NewJinguiziService(mem *common.MemoryStore) *JinguiziService {
	return &JinguiziService{mem: mem}
}

// resolveTargetUser resolves a target user from either user_id or username.
// Returns (userID, nil) when found, or an error response already written.
func (s *JinguiziService) resolveTargetUser(c *gin.Context, userID int64, username string) (int64, bool) {
	if userID != 0 {
		if s.mem.GetUserByID(userID) == nil {
			common.Error(c, errs.UserNotFound, "用户不存在")
			return 0, false
		}
		return userID, true
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

	w := s.mem.EnsureJinguiziWallet(uid)
	balanceBefore := w.Balance
	newBalance := balanceBefore + capital
	s.mem.UpdateJinguiziBalance(uid, newBalance, w.Frozen)
	s.mem.AddJinguiziRecharged(uid, capital)

	label := jinguiziTierLabel[req.Tier]
	now := time.Now()
	s.mem.SaveJinguiziTransaction(uid, &common.JinguiziTransaction{
		UserID: uid, OperatorID: operatorID, Type: "contest_entry", Amount: capital,
		BalanceBefore: balanceBefore, BalanceAfter: newBalance,
		Remark: "选拔赛报名·" + label, CreatedAt: now,
	})

	enr := &common.JinguiziEnrollment{
		UserID:         uid,
		Tier:           req.Tier,
		InitialCapital: capital,
		Status:         "active",
		ContestID:      req.ContestID,
		EnrolledAt:     now,
		Remark:         label,
	}
	s.mem.SaveJinguiziEnrollment(enr)

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

type AdminSettleJinguiziReq struct {
	UserID   int64   `json:"user_id"`
	Username string  `json:"username"`
	Action   string  `json:"action"`  // settle (达标结算) / eliminate (淘汰)
	Reward   float64 `json:"reward"`  // settle 时可发放奖励金龟子币
	Note     string  `json:"note"`
}

// AdminSettle settles an active enrollment.
//   - eliminate: reclaims the dedicated contest capital (floor 0) and marks eliminated.
//   - settle:    marks settled; an optional reward is granted as contest_reward.
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
	w := s.mem.EnsureJinguiziWallet(uid)
	balanceBefore := w.Balance
	now := time.Now()
	var delta float64
	var newBalance float64

	if req.Action == "eliminate" {
		reclaim := enr.InitialCapital
		newBalance = balanceBefore - reclaim
		if newBalance < 0 {
			newBalance = 0
		}
		delta = newBalance - balanceBefore
		s.mem.UpdateJinguiziBalance(uid, newBalance, w.Frozen)
		s.mem.SaveJinguiziTransaction(uid, &common.JinguiziTransaction{
			UserID: uid, OperatorID: operatorID, Type: "settlement", Amount: delta,
			BalanceBefore: balanceBefore, BalanceAfter: newBalance,
			Remark: "选拔赛淘汰·收回参赛资金", CreatedAt: now,
		})
		enr.Status = "eliminated"
	} else { // settle
		newBalance = balanceBefore
		if req.Reward > 0 {
			newBalance = balanceBefore + req.Reward
			s.mem.UpdateJinguiziBalance(uid, newBalance, w.Frozen)
			s.mem.AddJinguiziRecharged(uid, req.Reward)
			s.mem.SaveJinguiziTransaction(uid, &common.JinguiziTransaction{
				UserID: uid, OperatorID: operatorID, Type: "contest_reward", Amount: req.Reward,
				BalanceBefore: balanceBefore, BalanceAfter: newBalance,
				Remark: "选拔赛达标奖励", CreatedAt: now,
			})
		}
		enr.Status = "settled"
	}

	sa := now
	enr.SettledAt = &sa
	s.mem.SaveJinguiziEnrollment(enr)

	common.Success(c, gin.H{
		"user_id":          uid,
		"action":           req.Action,
		"balance_before":   balanceBefore,
		"balance_after":    newBalance,
		"delta":            delta,
		"enrollment_status": enr.Status,
	})
}

// GetEnrollment returns the caller's own 选拔赛 enrollment (or null).
func (s *JinguiziService) GetEnrollment(c *gin.Context) {
	userID := c.GetInt64("user_id")
	e := s.mem.GetJinguiziEnrollment(userID)
	if e == nil {
		common.Success(c, gin.H{"enrollment": nil})
		return
	}
	common.Success(c, e)
}
