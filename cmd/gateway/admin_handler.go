package main

import (
	"context"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goldarena/goldarena/internal/common"
	"github.com/goldarena/goldarena/pkg/db"
	"github.com/goldarena/goldarena/pkg/errs"
	"github.com/goldarena/goldarena/pkg/jwt"
	"golang.org/x/crypto/bcrypt"
)

// AdminService powers the platform management console. Every handler runs
// behind AdminMiddleware, which rejects non-admin callers. The active
// deployment runs in memory mode (SQLite write-through), so memory paths are
// the primary implementation; Postgres branches mirror them for production.
type AdminService struct {
	pg        *db.Postgres
	mem       *common.MemoryStore
	jwtMgr    *jwt.Manager
	marketSvc *MarketService
	tradeSvc  *TradeService
}

func NewAdminService(pg *db.Postgres, mem *common.MemoryStore, jwtMgr *jwt.Manager, marketSvc *MarketService, tradeSvc *TradeService) *AdminService {
	return &AdminService{pg: pg, mem: mem, jwtMgr: jwtMgr, marketSvc: marketSvc, tradeSvc: tradeSvc}
}

func (s *AdminService) isMemoryMode() bool { return s.pg == nil || s.pg.Pool == nil }

// seedAdminIfNeeded creates a default admin account on first boot if none exists.
// Credentials come from config (admin.seed_username / admin.seed_password); safe
// defaults are applied when config is absent. The account is persisted via the
// same SQLite write-through as normal users, so it survives restarts.
func seedAdminIfNeeded(mem *common.MemoryStore, username, password string) {
	if username == "" {
		username = "admin"
	}
	if password == "" {
		password = "Admin@8888"
	}
	for _, u := range mem.GetAllUsers() {
		if u.Role == "admin" {
			return // already seeded
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("[admin] seed failed: %v", err)
		return
	}
	now := time.Now()
	user := &common.User{
		ID:               mem.NextUserID(),
		Username:         username,
		Nickname:         "平台管理员",
		PasswordHash:     string(hash),
		Email:            username + "@goldarena.local",
		IsVerified:       true,
		Role:             "admin",
		Status:           1,
		CultivationLevel: 10,
		SpiritEnergy:     0,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	mem.SaveUser(user)
	mem.SaveWallet(&common.Wallet{
		ID: user.ID, UserID: user.ID, Balance: 0, Frozen: 0,
		TotalRecharged: 0, Version: 1, CreatedAt: now, UpdatedAt: now,
	})
	log.Printf("[admin] seeded default admin account: %s (change the password in config.yaml)", username)
}

// ========== Middleware ==========

// AdminMiddleware authenticates the JWT and requires role == "admin".
func (s *AdminService) AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := ""
		if authHeader := c.GetHeader("Authorization"); authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenStr = parts[1]
			}
		}
		if tokenStr == "" {
			tokenStr = c.Query("token")
		}
		if tokenStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": errs.Unauthorized, "message": "missing token"})
			c.Abort()
			return
		}
		claims, err := s.jwtMgr.ParseToken(tokenStr)
		if err != nil || claims.TokenType != "access" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": errs.InvalidToken, "message": "invalid token"})
			c.Abort()
			return
		}
		var role string
		if s.isMemoryMode() {
			u := s.mem.GetUserByID(claims.UserID)
			if u == nil {
				c.JSON(http.StatusForbidden, gin.H{"code": errs.Forbidden, "message": "forbidden"})
				c.Abort()
				return
			}
			role = u.Role
		} else {
			if err := s.pg.Pool.QueryRow(context.Background(),
				`SELECT role FROM users WHERE id=$1`, claims.UserID).Scan(&role); err != nil {
				c.JSON(http.StatusForbidden, gin.H{"code": errs.Forbidden, "message": "forbidden"})
				c.Abort()
				return
			}
		}
		if role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"code": errs.Forbidden, "message": "需要管理员权限"})
			c.Abort()
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}

// ========== Dashboard ==========

func (s *AdminService) Dashboard(c *gin.Context) {
	if s.isMemoryMode() {
		s.dashboardMem(c)
		return
	}
	s.dashboardPG(c)
}

func (s *AdminService) dashboardMem(c *gin.Context) {
	users := s.mem.GetAllUsers()
	totalUsers := len(users)
	today := time.Now().Format("2006-01-02")
	todayNewUsers := 0
	totalBalance := 0.0
	for _, u := range users {
		if u.CreatedAt.Format("2006-01-02") == today {
			todayNewUsers++
		}
		if w := s.mem.GetWallet(u.ID); w != nil {
			totalBalance += w.Balance
		}
	}

	positions := s.mem.GetAllPositions()
	openPositions := len(positions)
	totalMargin := 0.0
	totalFloating := 0.0
	for i := range positions {
		totalMargin += positions[i].Margin
		if q, err := s.tradeSvc.getQuote(positions[i].Symbol, positions[i].ContractMonth); err == nil {
			totalFloating += s.tradeSvc.calculatePnL(&positions[i], q.Price)
		} else {
			totalFloating += positions[i].FloatingPnL
		}
	}

	orders := s.mem.GetAllPaymentOrders()
	pendingPayments := 0
	totalRechargeRMB := 0.0
	totalGameCoins := 0.0
	todayRechargeRMB := 0.0
	for _, o := range orders {
		if o.Status == common.PaymentPending {
			pendingPayments++
		}
		if o.Status == common.PaymentPaid {
			totalRechargeRMB += o.AmountRMB
			totalGameCoins += o.GameCoins
			if o.PaidAt != nil && o.PaidAt.Format("2006-01-02") == today {
				todayRechargeRMB += o.AmountRMB
			}
		}
	}

	price := 0.0
	if q := s.marketSvc.fetchQuote("XAU", "SPOT"); q != nil {
		price = q.Price
	}

	common.Success(c, gin.H{
		"total_users":        totalUsers,
		"today_new_users":    todayNewUsers,
		"total_balance":      math.Round(totalBalance*100) / 100,
		"open_positions":     openPositions,
		"total_margin":       math.Round(totalMargin*100) / 100,
		"total_floating_pnl": math.Round(totalFloating*100) / 100,
		"pending_payments":   pendingPayments,
		"total_recharge_rmb": math.Round(totalRechargeRMB*100) / 100,
		"total_game_coins":   totalGameCoins,
		"today_recharge_rmb": math.Round(todayRechargeRMB*100) / 100,
		"current_price":      price,
	})
}

func (s *AdminService) dashboardPG(c *gin.Context) {
	var totalUsers, todayNewUsers, openPositions, pendingPayments int
	var totalBalance, totalMargin, totalFloating, totalRechargeRMB, todayRechargeRMB float64
	s.pg.Pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&totalUsers)
	s.pg.Pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE DATE(created_at)=CURRENT_DATE`).Scan(&todayNewUsers)
	s.pg.Pool.QueryRow(context.Background(), `SELECT COALESCE(SUM(balance),0) FROM wallets`).Scan(&totalBalance)
	s.pg.Pool.QueryRow(context.Background(), `SELECT count(*) FROM positions WHERE status=1`).Scan(&openPositions)
	s.pg.Pool.QueryRow(context.Background(), `SELECT COALESCE(SUM(margin),0), COALESCE(SUM(floating_pnl),0) FROM positions WHERE status=1`).Scan(&totalMargin, &totalFloating)
	s.pg.Pool.QueryRow(context.Background(), `SELECT count(*) FROM ga_payment_orders WHERE status='pending'`).Scan(&pendingPayments)
	s.pg.Pool.QueryRow(context.Background(), `SELECT COALESCE(SUM(amount_rmb),0) FROM ga_payment_orders WHERE status='paid'`).Scan(&totalRechargeRMB)
	s.pg.Pool.QueryRow(context.Background(), `SELECT COALESCE(SUM(amount_rmb),0) FROM ga_payment_orders WHERE status='paid' AND DATE(paid_at)=CURRENT_DATE`).Scan(&todayRechargeRMB)

	price := 0.0
	if q := s.marketSvc.fetchQuote("XAU", "SPOT"); q != nil {
		price = q.Price
	}
	common.Success(c, gin.H{
		"total_users":        totalUsers,
		"today_new_users":    todayNewUsers,
		"total_balance":      math.Round(totalBalance*100) / 100,
		"open_positions":     openPositions,
		"total_margin":       math.Round(totalMargin*100) / 100,
		"total_floating_pnl": math.Round(totalFloating*100) / 100,
		"pending_payments":   pendingPayments,
		"total_recharge_rmb": math.Round(totalRechargeRMB*100) / 100,
		"total_game_coins":   0,
		"today_recharge_rmb": math.Round(todayRechargeRMB*100) / 100,
		"current_price":      price,
	})
}

// ========== Users ==========

func (s *AdminService) ListUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if size < 1 || size > 100 {
		size = 20
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	if s.isMemoryMode() {
		s.listUsersMem(c, page, size, keyword)
		return
	}
	s.listUsersPG(c, page, size, keyword)
}

func (s *AdminService) listUsersMem(c *gin.Context, page, size int, keyword string) {
	all := s.mem.GetAllUsers()
	matched := make([]*common.User, 0, len(all))
	for _, u := range all {
		if keyword == "" ||
			strings.Contains(u.Username, keyword) ||
			strings.Contains(u.Email, keyword) ||
			strings.Contains(u.Nickname, keyword) {
			matched = append(matched, u)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID > matched[j].ID })
	total := len(matched)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	rows := make([]gin.H, 0)
	for _, u := range matched[start:end] {
		balance := 0.0
		frozen := 0.0
		if w := s.mem.GetWallet(u.ID); w != nil {
			balance = w.Balance
			frozen = w.Frozen
		}
		rows = append(rows, gin.H{
			"id":         u.ID,
			"username":   u.Username,
			"nickname":   u.Nickname,
			"email":      u.Email,
			"role":       u.Role,
			"status":     u.Status,
			"balance":    balance,
			"frozen":     frozen,
			"created_at": u.CreatedAt,
		})
	}
	common.Success(c, gin.H{"total": total, "page": page, "size": size, "list": rows})
}

func (s *AdminService) listUsersPG(c *gin.Context, page, size int, keyword string) {
	like := "%" + keyword + "%"
	var total int
	s.pg.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM users WHERE ($1='' OR username ILIKE $1 OR email ILIKE $1 OR nickname ILIKE $1)`, like).Scan(&total)
	rows, err := s.pg.Pool.Query(context.Background(),
		`SELECT u.id,u.username,u.nickname,u.email,u.role,u.status,
		        COALESCE(w.balance,0),COALESCE(w.frozen,0),u.created_at
		 FROM users u LEFT JOIN wallets w ON w.user_id=u.id
		 WHERE ($1='' OR u.username ILIKE $1 OR u.email ILIKE $1 OR u.nickname ILIKE $1)
		 ORDER BY u.id DESC LIMIT $2 OFFSET $3`, like, size, (page-1)*size)
	if err != nil {
		common.Error(c, errs.Internal, "query failed")
		return
	}
	defer rows.Close()
	list := make([]gin.H, 0)
	for rows.Next() {
		var id int64
		var username, nickname, email, role string
		var status int
		var balance, frozen float64
		var createdAt time.Time
		rows.Scan(&id, &username, &nickname, &email, &role, &status, &balance, &frozen, &createdAt)
		list = append(list, gin.H{
			"id": id, "username": username, "nickname": nickname, "email": email,
			"role": role, "status": status, "balance": balance, "frozen": frozen, "created_at": createdAt,
		})
	}
	common.Success(c, gin.H{"total": total, "page": page, "size": size, "list": list})
}

func (s *AdminService) GetUser(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if s.isMemoryMode() {
		u := s.mem.GetUserByID(id)
		if u == nil {
			common.Error(c, errs.UserNotFound, "user not found")
			return
		}
		balance, frozen := 0.0, 0.0
		if w := s.mem.GetWallet(id); w != nil {
			balance, frozen = w.Balance, w.Frozen
		}
		positions := make([]common.Position, 0)
		for _, p := range s.mem.GetPositions(id, nil) {
			if p.Status == 1 {
				positions = append(positions, p)
			}
		}
		orders := make([]common.Order, 0)
		for _, o := range s.mem.GetAllOrders() {
			if o.UserID == id {
				orders = append(orders, o)
				if len(orders) >= 20 {
					break
				}
			}
		}
		common.Success(c, gin.H{
			"id": u.ID, "username": u.Username, "nickname": u.Nickname, "email": u.Email,
			"role": u.Role, "status": u.Status, "balance": balance, "frozen": frozen,
			"positions": positions, "orders": orders,
		})
		return
	}
	// pg
	var u common.User
	if err := s.pg.Pool.QueryRow(context.Background(),
		`SELECT id,username,nickname,email,role,status FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Username, &u.Nickname, &u.Email, &u.Role, &u.Status); err != nil {
		common.Error(c, errs.UserNotFound, "user not found")
		return
	}
	var balance, frozen float64
	s.pg.Pool.QueryRow(context.Background(), `SELECT COALESCE(balance,0),COALESCE(frozen,0) FROM wallets WHERE user_id=$1`, id).
		Scan(&balance, &frozen)
	rows, _ := s.pg.Pool.Query(context.Background(),
		`SELECT id,user_id,symbol,contract_month,direction,volume,leverage,open_price,current_price,margin,floating_pnl,status,created_at
		 FROM positions WHERE user_id=$1 ORDER BY id DESC LIMIT 20`, id)
	positions := make([]common.Position, 0)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var p common.Position
			rows.Scan(&p.ID, &p.UserID, &p.Symbol, &p.ContractMonth, &p.Direction, &p.Volume,
				&p.Leverage, &p.OpenPrice, &p.CurrentPrice, &p.Margin, &p.FloatingPnL, &p.Status, &p.CreatedAt)
			positions = append(positions, p)
		}
	}
	common.Success(c, gin.H{
		"id": u.ID, "username": u.Username, "nickname": u.Nickname, "email": u.Email,
		"role": u.Role, "status": u.Status, "balance": balance, "frozen": frozen, "positions": positions,
	})
}

type AdminBalanceReq struct {
	Amount float64 `json:"amount"` // signed delta in game coins
	Remark string  `json:"remark"`
}

func (s *AdminService) AdjustBalance(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req AdminBalanceReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	if req.Amount == 0 {
		common.Error(c, errs.InvalidParam, "amount 不能为 0")
		return
	}
	if s.isMemoryMode() {
		w := s.mem.GetWallet(id)
		if w == nil {
			common.Error(c, errs.Internal, "wallet not found")
			return
		}
		balanceBefore := w.Balance
		newBalance := balanceBefore + req.Amount
		if newBalance < 0 {
			newBalance = 0
		}
		s.mem.UpdateWalletBalance(id, newBalance, w.Frozen)
		remark := req.Remark
		if remark == "" {
			if req.Amount > 0 {
				remark = "管理员发放游戏币"
			} else {
				remark = "管理员扣减游戏币"
			}
		}
		s.mem.SaveWalletTransaction(id, &common.WalletTransaction{
			ID: time.Now().UnixNano(), UserID: id, Type: "admin_adjust", Amount: newBalance - balanceBefore,
			BalanceBefore: balanceBefore, BalanceAfter: newBalance, Remark: remark, CreatedAt: time.Now(),
		})
		common.Success(c, gin.H{"user_id": id, "balance_before": balanceBefore, "balance_after": newBalance})
		return
	}
	// pg
	var walletID int64
	var balance, frozen, version float64
	if err := s.pg.Pool.QueryRow(context.Background(),
		`SELECT id,balance,frozen,version FROM wallets WHERE user_id=$1 FOR UPDATE`, id).
		Scan(&walletID, &balance, &frozen, &version); err != nil {
		common.Error(c, errs.Internal, "wallet not found")
		return
	}
	newBalance := balance + req.Amount
	if newBalance < 0 {
		newBalance = 0
	}
	delta := newBalance - balance
	if _, err := s.pg.Pool.Exec(context.Background(),
		`UPDATE wallets SET balance=$1,version=version+1,updated_at=NOW() WHERE id=$2 AND version=$3`,
		newBalance, walletID, int64(version)); err != nil {
		common.Error(c, errs.Internal, "update failed")
		return
	}
	remark := req.Remark
	if remark == "" {
		if req.Amount > 0 {
			remark = "管理员发放游戏币"
		} else {
			remark = "管理员扣减游戏币"
		}
	}
	s.pg.Pool.Exec(context.Background(),
		`INSERT INTO wallet_transactions (user_id,type,amount,balance_before,balance_after,remark)
		 VALUES ($1,'admin_adjust',$2,$3,$4,$5)`, id, delta, balance, newBalance, remark)
	common.Success(c, gin.H{"user_id": id, "balance_before": balance, "balance_after": newBalance})
}

type AdminStatusReq struct {
	Status int `json:"status"` // 1=enable, 0=freeze
}

func (s *AdminService) SetUserStatus(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req AdminStatusReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	if req.Status != 0 && req.Status != 1 {
		common.Error(c, errs.InvalidParam, "status 必须为 0 或 1")
		return
	}
	if s.isMemoryMode() {
		s.mem.UpdateUserStatus(id, req.Status)
		common.Success(c, gin.H{"user_id": id, "status": req.Status})
		return
	}
	if _, err := s.pg.Pool.Exec(context.Background(),
		`UPDATE users SET status=$1,updated_at=NOW() WHERE id=$2`, req.Status, id); err != nil {
		common.Error(c, errs.Internal, "update failed")
		return
	}
	common.Success(c, gin.H{"user_id": id, "status": req.Status})
}

// ========== Positions (cross-user) ==========

func (s *AdminService) ListPositions(c *gin.Context) {
	if s.isMemoryMode() {
		nick := map[int64]string{}
		for _, u := range s.mem.GetAllUsers() {
			nick[u.ID] = u.Nickname
		}
		list := make([]gin.H, 0)
		for _, p := range s.mem.GetAllPositions() {
			list = append(list, gin.H{
				"id": p.ID, "user_id": p.UserID, "nickname": nick[p.UserID],
				"symbol": p.Symbol, "contract_month": p.ContractMonth, "direction": p.Direction,
				"volume": p.Volume, "leverage": p.Leverage, "open_price": p.OpenPrice,
				"current_price": p.CurrentPrice, "margin": p.Margin, "floating_pnl": p.FloatingPnL,
				"created_at": p.CreatedAt,
			})
		}
		common.Success(c, gin.H{"total": len(list), "list": list})
		return
	}
	rows, err := s.pg.Pool.Query(context.Background(),
		`SELECT p.id,p.user_id,u.nickname,p.symbol,p.contract_month,p.direction,p.volume,p.leverage,
		        p.open_price,p.current_price,p.margin,p.floating_pnl,p.created_at
		 FROM positions p LEFT JOIN users u ON u.id=p.user_id WHERE p.status=1 ORDER BY p.id DESC`)
	if err != nil {
		common.Error(c, errs.Internal, "query failed")
		return
	}
	defer rows.Close()
	list := make([]gin.H, 0)
	for rows.Next() {
		var p common.Position
		var nickname string
		rows.Scan(&p.ID, &p.UserID, &nickname, &p.Symbol, &p.ContractMonth, &p.Direction, &p.Volume,
			&p.Leverage, &p.OpenPrice, &p.CurrentPrice, &p.Margin, &p.FloatingPnL, &p.CreatedAt)
		list = append(list, gin.H{
			"id": p.ID, "user_id": p.UserID, "nickname": nickname, "symbol": p.Symbol,
			"contract_month": p.ContractMonth, "direction": p.Direction, "volume": p.Volume,
			"leverage": p.Leverage, "open_price": p.OpenPrice, "current_price": p.CurrentPrice,
			"margin": p.Margin, "floating_pnl": p.FloatingPnL, "created_at": p.CreatedAt,
		})
	}
	common.Success(c, gin.H{"total": len(list), "list": list})
}

// ForceClosePosition lets an admin liquidate any open position regardless of owner.
func (s *AdminService) ForceClosePosition(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if s.isMemoryMode() {
		s.adminForceCloseMem(c, id)
		return
	}
	s.adminForceClosePG(c, id)
}

func (s *AdminService) adminForceCloseMem(c *gin.Context, id int64) {
	pos := s.mem.GetPositionByID(id)
	if pos == nil || pos.Status != 1 {
		common.Error(c, errs.PositionNotFound, "position not found")
		return
	}
	if q, err := s.tradeSvc.getQuote(pos.Symbol, pos.ContractMonth); err == nil {
		pos.CurrentPrice = q.Price
		pos.FloatingPnL = s.tradeSvc.calculatePnL(pos, q.Price)
	}
	now := time.Now()
	pos.Status = 2
	pos.ClosedAt = &now
	s.mem.UpdatePosition(pos)

	if wallet := s.mem.GetWallet(pos.UserID); wallet != nil {
		totalReturn := pos.Margin + pos.FloatingPnL
		newBalance := wallet.Balance + totalReturn
		newFrozen := wallet.Frozen - pos.Margin
		if newFrozen < 0 {
			newFrozen = 0
		}
		s.mem.UpdateWalletBalance(pos.UserID, newBalance, newFrozen)
		s.mem.SaveWalletTransaction(pos.UserID, &common.WalletTransaction{
			ID: time.Now().UnixNano(), UserID: pos.UserID, Type: "margin_release", Amount: pos.Margin,
			BalanceBefore: wallet.Balance, BalanceAfter: wallet.Balance + pos.Margin,
			ReferenceID: pos.OrderNo, Remark: "管理员强制平仓-释放保证金", CreatedAt: now,
		})
		if pos.FloatingPnL != 0 {
			txnType := "pnl_credit"
			if pos.FloatingPnL < 0 {
				txnType = "pnl_debit"
			}
			s.mem.SaveWalletTransaction(pos.UserID, &common.WalletTransaction{
				ID: time.Now().UnixNano() + 1, UserID: pos.UserID, Type: txnType, Amount: pos.FloatingPnL,
				BalanceBefore: wallet.Balance + pos.Margin, BalanceAfter: newBalance,
				ReferenceID: pos.OrderNo, Remark: "管理员强制平仓-盈亏", CreatedAt: now,
			})
		}
	}
	common.Success(c, gin.H{
		"position_id": pos.ID, "close_price": pos.CurrentPrice,
		"realized_pnl": math.Round(pos.FloatingPnL*100) / 100, "margin_returned": pos.Margin,
	})
}

func (s *AdminService) adminForceClosePG(c *gin.Context, id int64) {
	var p common.Position
	if err := s.pg.Pool.QueryRow(context.Background(),
		`SELECT id,user_id,symbol,contract_month,direction,volume,margin,floating_pnl,spread_cost
		 FROM positions WHERE id=$1 AND status=1 FOR UPDATE`, id).
		Scan(&p.ID, &p.UserID, &p.Symbol, &p.ContractMonth, &p.Direction, &p.Volume, &p.Margin, &p.FloatingPnL, &p.SpreadCost); err != nil {
		common.Error(c, errs.PositionNotFound, "position not found")
		return
	}
	if q, err := s.tradeSvc.getQuote(p.Symbol, p.ContractMonth); err == nil {
		p.CurrentPrice = q.Price
		p.FloatingPnL = s.tradeSvc.calculatePnL(&p, q.Price)
	}
	now := time.Now()
	tx, err := s.pg.Pool.Begin(context.Background())
	if err != nil {
		common.Error(c, errs.Internal, "tx begin failed")
		return
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(context.Background(),
		`UPDATE positions SET status=2,floating_pnl=$1,current_price=$2,closed_at=$3,updated_at=NOW() WHERE id=$4`,
		p.FloatingPnL, p.CurrentPrice, now, p.ID); err != nil {
		common.Error(c, errs.Internal, "close failed")
		return
	}
	var walletID int64
	var balance, frozen, version float64
	if err := tx.QueryRow(context.Background(),
		`SELECT id,balance,frozen,version FROM wallets WHERE user_id=$1 FOR UPDATE`, p.UserID).
		Scan(&walletID, &balance, &frozen, &version); err != nil {
		common.Error(c, errs.Internal, "wallet not found")
		return
	}
	totalReturn := p.Margin + p.FloatingPnL
	newBalance := balance + totalReturn
	newFrozen := frozen - p.Margin
	if newFrozen < 0 {
		newFrozen = 0
	}
	if _, err := tx.Exec(context.Background(),
		`UPDATE wallets SET balance=$1,frozen=$2,version=version+1,updated_at=NOW() WHERE id=$3 AND version=$4`,
		newBalance, newFrozen, walletID, int64(version)); err != nil {
		common.Error(c, errs.Internal, "wallet update failed")
		return
	}
	tx.Exec(context.Background(),
		`INSERT INTO wallet_transactions (user_id,type,amount,balance_before,balance_after,reference_id,remark)
		 VALUES ($1,'margin_release',$2,$3,$4,$5,'管理员强制平仓-释放保证金')`,
		p.UserID, p.Margin, balance, balance+p.Margin, p.OrderNo)
	if p.FloatingPnL != 0 {
		txnType := "pnl_credit"
		if p.FloatingPnL < 0 {
			txnType = "pnl_debit"
		}
		tx.Exec(context.Background(),
			`INSERT INTO wallet_transactions (user_id,type,amount,balance_before,balance_after,reference_id,remark)
			 VALUES ($1,$2,$3,$4,$5,$6,'管理员强制平仓-盈亏')`,
			p.UserID, txnType, p.FloatingPnL, balance+p.Margin, newBalance, p.OrderNo)
	}
	if err := tx.Commit(context.Background()); err != nil {
		common.Error(c, errs.Internal, "commit failed")
		return
	}
	common.Success(c, gin.H{
		"position_id": p.ID, "close_price": p.CurrentPrice,
		"realized_pnl": math.Round(p.FloatingPnL*100) / 100, "margin_returned": p.Margin,
	})
}

// ========== Orders (cross-user) ==========

func (s *AdminService) ListOrders(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if size < 1 || size > 100 {
		size = 20
	}
	if s.isMemoryMode() {
		all := s.mem.GetAllOrders()
		total := len(all)
		start := (page - 1) * size
		if start > total {
			start = total
		}
		end := start + size
		if end > total {
			end = total
		}
		list := make([]gin.H, 0)
		for _, o := range all[start:end] {
			list = append(list, gin.H{
				"id": o.ID, "user_id": o.UserID, "order_no": o.OrderNo, "symbol": o.Symbol,
				"contract_month": o.ContractMonth, "direction": o.Direction, "order_type": o.OrderType,
				"volume": o.Volume, "leverage": o.Leverage, "status": o.Status,
				"executed_price": o.ExecutedPrice, "created_at": o.CreatedAt,
			})
		}
		common.Success(c, gin.H{"total": total, "page": page, "size": size, "list": list})
		return
	}
	rows, err := s.pg.Pool.Query(context.Background(),
		`SELECT id,user_id,order_no,symbol,contract_month,direction,order_type,volume,leverage,status,executed_price,created_at
		 FROM orders ORDER BY id DESC LIMIT $1 OFFSET $2`, size, (page-1)*size)
	if err != nil {
		common.Error(c, errs.Internal, "query failed")
		return
	}
	defer rows.Close()
	list := make([]gin.H, 0)
	for rows.Next() {
		var o common.Order
		var executed *float64
		rows.Scan(&o.ID, &o.UserID, &o.OrderNo, &o.Symbol, &o.ContractMonth, &o.Direction,
			&o.OrderType, &o.Volume, &o.Leverage, &o.Status, &executed, &o.CreatedAt)
		o.ExecutedPrice = executed
		list = append(list, gin.H{
			"id": o.ID, "user_id": o.UserID, "order_no": o.OrderNo, "symbol": o.Symbol,
			"contract_month": o.ContractMonth, "direction": o.Direction, "order_type": o.OrderType,
			"volume": o.Volume, "leverage": o.Leverage, "status": o.Status,
			"executed_price": o.ExecutedPrice, "created_at": o.CreatedAt,
		})
	}
	common.Success(c, gin.H{"total": 0, "page": page, "size": size, "list": list})
}

// ========== Payments ==========

func (s *AdminService) ListPayments(c *gin.Context) {
	status := c.Query("status")
	if s.isMemoryMode() {
		all := s.mem.GetAllPaymentOrders()
		list := make([]gin.H, 0)
		for _, o := range all {
			if status != "" && o.Status != status {
				continue
			}
			list = append(list, gin.H{
				"id": o.ID, "user_id": o.UserID, "out_trade_no": o.OutTradeNo, "channel": o.Channel,
				"amount_rmb": o.AmountRMB, "game_coins": o.GameCoins, "status": o.Status,
				"provider": o.Provider, "created_at": o.CreatedAt, "paid_at": o.PaidAt,
			})
		}
		common.Success(c, gin.H{"total": len(list), "list": list})
		return
	}
	rows, err := s.pg.Pool.Query(context.Background(),
		`SELECT id,user_id,out_trade_no,channel,amount_rmb,game_coins,status,provider,created_at,paid_at
		 FROM ga_payment_orders WHERE ($1='' OR status=$1) ORDER BY created_at DESC`, status)
	if err != nil {
		common.Error(c, errs.Internal, "query failed")
		return
	}
	defer rows.Close()
	list := make([]gin.H, 0)
	for rows.Next() {
		var o common.PaymentOrder
		rows.Scan(&o.ID, &o.UserID, &o.OutTradeNo, &o.Channel, &o.AmountRMB, &o.GameCoins,
			&o.Status, &o.Provider, &o.CreatedAt, &o.PaidAt)
		list = append(list, gin.H{
			"id": o.ID, "user_id": o.UserID, "out_trade_no": o.OutTradeNo, "channel": o.Channel,
			"amount_rmb": o.AmountRMB, "game_coins": o.GameCoins, "status": o.Status,
			"provider": o.Provider, "created_at": o.CreatedAt, "paid_at": o.PaidAt,
		})
	}
	common.Success(c, gin.H{"total": len(list), "list": list})
}

// CreditPayment manually marks a pending payment order as paid and credits the
// game coins to the user's wallet (operator override / top-up recovery).
func (s *AdminService) CreditPayment(c *gin.Context) {
	no := c.Param("no")
	if s.isMemoryMode() {
		o := s.mem.GetPaymentOrderByOutTradeNo(no)
		if o == nil {
			common.Error(c, errs.Internal, "order not found")
			return
		}
		if o.Status != common.PaymentPending {
			common.Error(c, errs.InvalidParam, "订单已处理")
			return
		}
		now := time.Now()
		s.mem.UpdatePaymentOrderStatus(no, common.PaymentPaid, &now)
		if w := s.mem.GetWallet(o.UserID); w != nil {
			newBalance := w.Balance + o.GameCoins
			s.mem.UpdateWalletBalance(o.UserID, newBalance, w.Frozen)
			s.mem.SaveWalletTransaction(o.UserID, &common.WalletTransaction{
				ID: time.Now().UnixNano(), UserID: o.UserID, Type: "recharge", Amount: o.GameCoins,
				BalanceBefore: w.Balance, BalanceAfter: newBalance, ReferenceID: o.OutTradeNo,
				Remark: "管理员手动补单入账", CreatedAt: now,
			})
		}
		common.Success(c, gin.H{"out_trade_no": no, "game_coins": o.GameCoins, "status": common.PaymentPaid})
		return
	}
	var o common.PaymentOrder
	if err := s.pg.Pool.QueryRow(context.Background(),
		`SELECT id,user_id,out_trade_no,amount_rmb,game_coins,status FROM ga_payment_orders WHERE out_trade_no=$1`, no).
		Scan(&o.ID, &o.UserID, &o.OutTradeNo, &o.AmountRMB, &o.GameCoins, &o.Status); err != nil {
		common.Error(c, errs.Internal, "order not found")
		return
	}
	if o.Status != common.PaymentPending {
		common.Error(c, errs.InvalidParam, "订单已处理")
		return
	}
	now := time.Now()
	if _, err := s.pg.Pool.Exec(context.Background(),
		`UPDATE ga_payment_orders SET status='paid',paid_at=$1 WHERE out_trade_no=$2`, now, no); err != nil {
		common.Error(c, errs.Internal, "update failed")
		return
	}
	var walletID int64
	var balance, frozen, version float64
	if err := s.pg.Pool.QueryRow(context.Background(),
		`SELECT id,balance,frozen,version FROM wallets WHERE user_id=$1 FOR UPDATE`, o.UserID).
		Scan(&walletID, &balance, &frozen, &version); err != nil {
		common.Error(c, errs.Internal, "wallet not found")
		return
	}
	newBalance := balance + o.GameCoins
	if _, err := s.pg.Pool.Exec(context.Background(),
		`UPDATE wallets SET balance=$1,total_recharged=total_recharged+$2,version=version+1,updated_at=NOW() WHERE id=$3 AND version=$4`,
		newBalance, o.GameCoins, walletID, int64(version)); err != nil {
		common.Error(c, errs.Internal, "wallet update failed")
		return
	}
	s.pg.Pool.Exec(context.Background(),
		`INSERT INTO wallet_transactions (user_id,type,amount,balance_before,balance_after,reference_id,remark)
		 VALUES ($1,'recharge',$2,$3,$4,$5,'管理员手动补单入账')`,
		o.UserID, o.GameCoins, balance, newBalance, o.OutTradeNo)
	common.Success(c, gin.H{"out_trade_no": no, "game_coins": o.GameCoins, "status": common.PaymentPaid})
}
