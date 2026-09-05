package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/goldarena/goldarena/internal/common"
	"github.com/goldarena/goldarena/pkg/db"
	"github.com/goldarena/goldarena/pkg/errs"
	"github.com/goldarena/goldarena/pkg/redis"
	"github.com/spf13/viper"
)

type TradeService struct {
	pg        *db.Postgres
	rdb       *redis.Redis
	mem       *common.MemoryStore
	marketSvc *MarketService
}

func NewTradeService(pg *db.Postgres, rdb *redis.Redis, mem *common.MemoryStore, marketSvc *MarketService) *TradeService {
	return &TradeService{pg: pg, rdb: rdb, mem: mem, marketSvc: marketSvc}
}

// getContractSize returns the configured London gold spot contract multiplier
// (troy ounces per standard lot). Falls back to 100 oz if unconfigured.
func getContractSize() float64 {
	cs := viper.GetFloat64("trading.contract_size")
	if cs <= 0 {
		return 100.0
	}
	return cs
}

func (s *TradeService) isMemoryMode() bool {
	return s.pg == nil || s.pg.Pool == nil
}

// tradeWallet abstracts the wallet a trade's margin is drawn from. When the user has
// an ACTIVE 金龟子 contest enrollment, margin is routed to the isolated 金龟子 wallet;
// otherwise it uses the normal game-coin wallet. This isolates contest funds from the
// main wallet/recharge logic without duplicating it.
type tradeWallet struct {
	contest bool
	userID  int64
	w       *common.Wallet
	jw      *common.JinguiziWallet
}

// loadTradeWallet picks the wallet for userID. When contestID is provided and matches
// the user's active enrollment, the contest wallet is forced (covers pending-order fills
// whose margin was frozen from the contest wallet at placement time).
func (s *TradeService) loadTradeWallet(userID int64, contestID *int64) (*tradeWallet, *common.JinguiziEnrollment) {
	enr := s.mem.GetActiveEnrollment(userID)
	if enr != nil && (contestID == nil || *contestID == 0 || *contestID == enr.UserID) {
		return &tradeWallet{contest: true, userID: userID, jw: s.mem.EnsureJinguiziWallet(userID)}, enr
	}
	return &tradeWallet{contest: false, userID: userID, w: s.mem.GetWallet(userID)}, nil
}

func (tw *tradeWallet) balance() float64 {
	if tw.contest {
		return tw.jw.Balance
	}
	if tw.w == nil {
		return 0
	}
	return tw.w.Balance
}

func (tw *tradeWallet) frozen() float64 {
	if tw.contest {
		return tw.jw.Frozen
	}
	if tw.w == nil {
		return 0
	}
	return tw.w.Frozen
}

func (tw *tradeWallet) set(bal, froz float64) {
	if tw.contest {
		tw.jw.Balance = bal
		tw.jw.Frozen = froz
		return
	}
	if tw.w != nil {
		tw.w.Balance = bal
		tw.w.Frozen = froz
	}
}

func (tw *tradeWallet) save(s *TradeService) {
	if tw.contest {
		s.mem.UpdateJinguiziBalance(tw.userID, tw.jw.Balance, tw.jw.Frozen)
		return
	}
	if tw.w != nil {
		s.mem.UpdateWalletBalance(tw.userID, tw.w.Balance, tw.w.Frozen)
	}
}

// txn records the margin operation in the appropriate ledger (金龟子 or main).
// For contest wallets the type is namespaced (contest_*) so the isolated ledger is clear.
func (tw *tradeWallet) txn(s *TradeService, tType string, amount, before, after float64, ref, remark string) {
	now := time.Now()
	if tw.contest {
		switch tType {
		case "margin_freeze":
			tType = "contest_margin_freeze"
		case "margin_release":
			tType = "contest_margin_release"
		case "pnl_credit":
			tType = "contest_pnl_credit"
		case "pnl_debit":
			tType = "contest_pnl_debit"
		}
		s.mem.SaveJinguiziTransaction(tw.userID, &common.JinguiziTransaction{
			UserID: tw.userID, OperatorID: 0, Type: tType, Amount: amount,
			BalanceBefore: before, BalanceAfter: after, Remark: remark, CreatedAt: now,
		})
		return
	}
	s.mem.SaveWalletTransaction(tw.userID, &common.WalletTransaction{
		ID: time.Now().UnixNano(), UserID: tw.userID, Type: tType, Amount: amount,
		BalanceBefore: before, BalanceAfter: after, ReferenceID: ref, Remark: remark, CreatedAt: now,
	})
}

// ========== Order Handlers ==========

type PlaceOrderReq struct {
	Symbol        string  `json:"symbol" binding:"required"`
	ContractMonth string  `json:"contract_month"`
	Direction     int     `json:"direction" binding:"required"` // 1=long, 2=short
	OrderType     int     `json:"order_type" binding:"required"` // 1=market, 2=limit, 3=stop
	Volume        float64 `json:"volume" binding:"required"`
	Leverage      int     `json:"leverage"`
	Price         *float64 `json:"price"`
	StopLoss      *float64 `json:"stop_loss"`
	TakeProfit    *float64 `json:"take_profit"`
	ContestID     *int64   `json:"contest_id"`
}

func (s *TradeService) PlaceOrder(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req PlaceOrderReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}

	// Set defaults
	if req.Leverage <= 0 {
		req.Leverage = 10
	}
	if req.Leverage > 1000 {
		common.Error(c, errs.InvalidLeverage, "max leverage is 1000")
		return
	}
	if req.Volume < 0.01 || req.Volume > 100 {
		common.Error(c, errs.InvalidVolume, "volume must be 0.01-100")
		return
	}
	if req.Direction != 1 && req.Direction != 2 {
		common.Error(c, errs.InvalidParam, "direction must be 1 (long) or 2 (short)")
		return
	}

	// Get current quote from Redis
	quote, err := s.getQuote(req.Symbol, req.ContractMonth)
	if err != nil {
		common.Error(c, errs.MarketClosed, "market data unavailable")
		return
	}

	// Validate order type
	if req.OrderType < 1 || req.OrderType > 3 {
		common.Error(c, errs.InvalidParam, "order_type must be 1(market), 2(limit), or 3(stop)")
		return
	}

	// Pending orders (limit/stop) require a trigger price
	isPending := req.OrderType == 2 || req.OrderType == 3
	if isPending && req.Price == nil {
		common.Error(c, errs.InvalidParam, "trigger price required for limit/stop orders")
		return
	}
	// SL/TP validation: stop_loss must be < open price for long, > for short; take_profit must be > open for long, < for short
	if isPending && req.StopLoss != nil && req.TakeProfit != nil {
		if *req.StopLoss >= *req.TakeProfit {
			common.Error(c, errs.InvalidParam, "stop loss must be below take profit")
			return
		}
	}

	// Determine execution price (for market orders only; pending orders use their trigger price later)
	execPrice := quote.Price
	if req.OrderType == 1 { // Market — execute immediately at current spread
		if req.Direction == 1 {
			execPrice = quote.Ask
		} else {
			execPrice = quote.Bid
		}
	} else if isPending {
		// Pending order: margin is estimated from trigger price, but we use quote for safety check
		execPrice = *req.Price
	}

	// Calculate margin: (price * contractSize * volume) / leverage
	contractSize := getContractSize()
	margin := (execPrice * contractSize * req.Volume) / float64(req.Leverage)

	// Calculate spread cost (only for market orders; pending orders pay spread on fill)
	spreadCost := 0.0
	if req.OrderType == 1 {
		spread := quote.Ask - quote.Bid
		spreadCost = spread * contractSize * req.Volume
	}

	// ---- PENDING ORDER: save as status=1, freeze margin, no position yet ----
	if isPending {
		if s.isMemoryMode() {
			s.placePendingOrderMem(c, userID, &req, quote, execPrice, margin)
			return
		}
		// PG path for pending orders
		s.placePendingOrderPG(c, userID, &req, execPrice, margin)
		return
	}

	// ---- MARKET ORDER: immediate execution (existing logic) ----

	// Memory mode fallback
	if s.isMemoryMode() {
		s.placeOrderMem(c, userID, &req, quote, execPrice, margin, spreadCost)
		return
	}

	// Check wallet balance
	tx, err := s.pg.Pool.Begin(context.Background())
	if err != nil {
		common.Error(c, errs.Internal, "tx begin failed")
		return
	}
	defer tx.Rollback(context.Background())

	var walletID int64
	var balance float64
	var version int64
	err = tx.QueryRow(context.Background(),
		`SELECT id, balance, version FROM wallets WHERE user_id=$1 FOR UPDATE`, userID).Scan(&walletID, &balance, &version)
	if err != nil {
		common.Error(c, errs.Internal, "wallet not found")
		return
	}

	totalCost := margin + spreadCost
	if balance < totalCost {
		common.Error(c, errs.InsufficientMargin,
			fmt.Sprintf("need %.2f, have %.2f", totalCost, balance))
		return
	}

	// Deduct balance, freeze margin
	newBalance := balance - totalCost
	_, err = tx.Exec(context.Background(),
		`UPDATE wallets SET balance=$1, frozen=frozen+$2, version=version+1, updated_at=NOW()
		 WHERE id=$3 AND version=$4`, newBalance, margin, walletID, version)
	if err != nil {
		common.Error(c, errs.Internal, "wallet update failed")
		return
	}

	// Create order
	orderNo := fmt.Sprintf("ORD%s%04d", time.Now().Format("20060102150405"), time.Now().Nanosecond()%10000)
	var orderID int64
	err = tx.QueryRow(context.Background(),
		`INSERT INTO orders (order_no, user_id, contest_id, symbol, contract_month,
		 direction, order_type, volume, leverage, price, stop_loss, take_profit,
		 status, executed_price, margin, spread_cost)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,2,$13,$14,$15) RETURNING id`,
		orderNo, userID, req.ContestID, req.Symbol, req.ContractMonth,
		req.Direction, req.OrderType, req.Volume, req.Leverage,
		req.Price, req.StopLoss, req.TakeProfit,
		execPrice, margin, spreadCost).Scan(&orderID)
	if err != nil {
		common.Error(c, errs.Internal, "order creation failed: "+err.Error())
		return
	}

	// Create position
	var positionID int64
	err = tx.QueryRow(context.Background(),
		`INSERT INTO positions (user_id, contest_id, order_no, symbol, contract_month,
		 direction, volume, leverage, open_price, current_price, stop_loss, take_profit,
		 margin, spread_cost, status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,1) RETURNING id`,
		userID, req.ContestID, orderNo, req.Symbol, req.ContractMonth,
		req.Direction, req.Volume, req.Leverage,
		execPrice, execPrice, req.StopLoss, req.TakeProfit,
		margin, spreadCost).Scan(&positionID)
	if err != nil {
		common.Error(c, errs.Internal, "position creation failed: "+err.Error())
		return
	}

	// Record wallet transactions
	_, _ = tx.Exec(context.Background(),
		`INSERT INTO wallet_transactions (user_id, type, amount, balance_before, balance_after, reference_id, remark)
		 VALUES ($1, 'margin_freeze', $2, $3, $4, $5, '开仓保证金')`,
		userID, margin, balance, balance-margin, orderNo)

	if err := tx.Commit(context.Background()); err != nil {
		common.Error(c, errs.Internal, "commit failed: "+err.Error())
		return
	}

	common.Success(c, gin.H{
		"order_id":       orderID,
		"order_no":       orderNo,
		"position_id":    positionID,
		"executed_price": execPrice,
		"margin":         margin,
		"spread_cost":    math.Round(spreadCost*100) / 100,
	})
}

// ========== Position Handlers ==========

func (s *TradeService) GetPositions(c *gin.Context) {
	userID := c.GetInt64("user_id")
	contestID := c.Query("contest_id")

	// Memory mode fallback
	if s.isMemoryMode() {
		s.getPositionsMem(c, userID, contestID)
		return
	}

	// Update all positions with current price first
	s.updatePositionsWithQuote(userID)

	query := `SELECT id, user_id, contest_id, order_no, symbol, contract_month,
		direction, volume, leverage, open_price, current_price,
		COALESCE(stop_loss,0), COALESCE(take_profit,0), margin, floating_pnl, spread_cost,
		status, created_at
		FROM positions WHERE user_id=$1 AND status=1`
	args := []interface{}{userID}
	switch {
	case contestID == "" || contestID == "null":
		query += " AND contest_id IS NULL"
	case contestID == "all":
		// no filter
	default:
		if cid, err := strconv.ParseInt(contestID, 10, 64); err == nil {
			query += " AND contest_id=$2"
			args = append(args, cid)
		}
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.pg.Pool.Query(context.Background(), query, args...)
	if err != nil {
		common.Error(c, errs.Internal, "query failed")
		return
	}
	defer rows.Close()

	var positions []common.Position
	for rows.Next() {
		var p common.Position
		if err := rows.Scan(&p.ID, &p.UserID, &p.ContestID, &p.OrderNo, &p.Symbol, &p.ContractMonth,
			&p.Direction, &p.Volume, &p.Leverage, &p.OpenPrice, &p.CurrentPrice,
			&p.StopLoss, &p.TakeProfit, &p.Margin, &p.FloatingPnL, &p.SpreadCost,
			&p.Status, &p.CreatedAt); err != nil {
			continue
		}
		positions = append(positions, p)
	}

	common.Success(c, positions)
}

func (s *TradeService) ClosePosition(c *gin.Context) {
	userID := c.GetInt64("user_id")

	// We expect positionID in request body
	var req struct {
		PositionID int64 `json:"position_id" binding:"required"`
	}
	if err := common.BindJSON(c, &req); err != nil {
		return
	}

	// Memory mode fallback
	if s.isMemoryMode() {
		s.closePositionMem(c, userID, req.PositionID)
		return
	}

	tx, err := s.pg.Pool.Begin(context.Background())
	if err != nil {
		common.Error(c, errs.Internal, "tx begin failed")
		return
	}
	defer tx.Rollback(context.Background())

	// Get position
	var p common.Position
	err = tx.QueryRow(context.Background(),
		`SELECT id, user_id, symbol, contract_month, direction, volume, margin, floating_pnl, spread_cost
		 FROM positions WHERE id=$1 AND user_id=$2 AND status=1 FOR UPDATE`,
		req.PositionID, userID).Scan(&p.ID, &p.UserID, &p.Symbol, &p.ContractMonth,
		&p.Direction, &p.Volume, &p.Margin, &p.FloatingPnL, &p.SpreadCost)
	if err != nil {
		common.Error(c, errs.PositionNotFound, "position not found")
		return
	}

	// Get current quote
	quote, err := s.getQuote(p.Symbol, p.ContractMonth)
	if err == nil {
		p.CurrentPrice = quote.Price
		p.FloatingPnL = s.calculatePnL(&p, p.CurrentPrice)
	}

	// Update position
	now := time.Now()
	_, err = tx.Exec(context.Background(),
		`UPDATE positions SET status=2, floating_pnl=$1, current_price=$2, closed_at=$3, updated_at=NOW()
		 WHERE id=$4`, p.FloatingPnL, p.CurrentPrice, now, p.ID)
	if err != nil {
		common.Error(c, errs.Internal, "close position failed")
		return
	}

	// Get wallet
	var walletID int64
	var balance float64
	var frozen float64
	var version int64
	err = tx.QueryRow(context.Background(),
		`SELECT id, balance, frozen, version FROM wallets WHERE user_id=$1 FOR UPDATE`, userID).Scan(
		&walletID, &balance, &frozen, &version)
	if err != nil {
		common.Error(c, errs.Internal, "wallet not found")
		return
	}

	// Release margin + credit PnL
	totalReturn := p.Margin + p.FloatingPnL
	newBalance := balance + totalReturn
	newFrozen := frozen - p.Margin
	_, err = tx.Exec(context.Background(),
		`UPDATE wallets SET balance=$1, frozen=$2, version=version+1, updated_at=NOW()
		 WHERE id=$3 AND version=$4`, newBalance, newFrozen, walletID, version)
	if err != nil {
		common.Error(c, errs.Internal, "wallet update failed")
		return
	}

	// Record PnL transaction
	_, _ = tx.Exec(context.Background(),
		`INSERT INTO wallet_transactions (user_id, type, amount, balance_before, balance_after, reference_id, remark)
		 VALUES ($1, 'margin_release', $2, $3, $4, $5, '平仓释放保证金')`,
		userID, p.Margin, balance, balance+p.Margin, p.OrderNo)

	if p.FloatingPnL != 0 {
		txnType := "pnl_credit"
		if p.FloatingPnL < 0 {
			txnType = "pnl_debit"
		}
		_, _ = tx.Exec(context.Background(),
			`INSERT INTO wallet_transactions (user_id, type, amount, balance_before, balance_after, reference_id, remark)
			 VALUES ($1, $2, $3, $4, $5, $6, '平仓盈亏')`,
			userID, txnType, p.FloatingPnL, balance+p.Margin, newBalance, p.OrderNo)
	}

	if err := tx.Commit(context.Background()); err != nil {
		common.Error(c, errs.Internal, "commit failed")
		return
	}

	common.Success(c, gin.H{
		"position_id":    p.ID,
		"close_price":    p.CurrentPrice,
		"realized_pnl":   math.Round(p.FloatingPnL*100) / 100,
		"margin_returned": p.Margin,
	})
}

// ========== Trade PnL ==========

type TradePnLReq struct {
	ContestID *int64 `json:"contest_id"`
}

func (s *TradeService) GetTradePnL(c *gin.Context) {
	userID := c.GetInt64("user_id")
	// contest_id 过滤：null/空=游戏币, "all"=全部, "<id>"=指定 contest
	contestParam := c.Query("contest_id")
	contestOnly := contestParam != "" && contestParam != "all"

	// Memory mode fallback
	if s.isMemoryMode() {
		closed := s.mem.GetClosedPositions(userID)
		// 按平仓时间从近到远排序
		sort.Slice(closed, func(i, j int) bool {
			if closed[i].ClosedAt == nil { return false }
			if closed[j].ClosedAt == nil { return true }
			return closed[i].ClosedAt.After(*closed[j].ClosedAt)
		})
		totalPnL := 0.0
		trades := []gin.H{}
		for _, p := range closed {
			// 过滤 contest
			if contestOnly {
				if cid, err := strconv.ParseInt(contestParam, 10, 64); err == nil {
					if p.ContestID == nil || *p.ContestID != cid {
						continue
					}
				}
			} else if contestParam == "" {
				// 显式 null 表示游戏币
				if p.ContestID != nil {
					continue
				}
			}
			totalPnL += p.FloatingPnL
			var closedAt *time.Time
			if p.ClosedAt != nil && !p.ClosedAt.IsZero() {
				closedAt = p.ClosedAt
			}
			trades = append(trades, gin.H{
				"id":           p.ID,
				"symbol":       p.Symbol,
				"direction":    p.Direction,
				"volume":       p.Volume,
				"leverage":     p.Leverage,
				"open_price":   p.OpenPrice,
				"close_price":  p.CurrentPrice,
				"pnl":          p.FloatingPnL,
				"spread_cost":  p.SpreadCost,
				"margin":       p.Margin,
				"contest_id":   p.ContestID,
				"created_at":   p.CreatedAt,
				"closed_at":    closedAt,
			})
		}
		common.Success(c, gin.H{
			"total_pnl": math.Round(totalPnL*100) / 100,
			"trades":    trades,
		})
		return
	}

	query := `SELECT p.id, p.symbol, p.direction, p.volume, p.leverage,
		        p.open_price, p.current_price, p.floating_pnl, p.spread_cost,
		        p.margin, p.contest_id, p.created_at, p.closed_at
		 FROM positions p WHERE p.user_id=$1 AND p.status=2`
	args := []any{userID}
	if contestOnly {
		if cid, err := strconv.ParseInt(contestParam, 10, 64); err == nil {
			query += " AND p.contest_id=$2"
			args = append(args, cid)
		}
	} else if contestParam == "" {
		query += " AND p.contest_id IS NULL"
	}
	query += " ORDER BY p.closed_at DESC LIMIT 100"
	rows, err := s.pg.Pool.Query(context.Background(), query, args...)
	if err != nil {
		common.Error(c, errs.Internal, "query failed")
		return
	}
	defer rows.Close()

	totalPnL := 0.0
	var trades []gin.H
	for rows.Next() {
		var id int64
		var symbol string
		var direction int
		var volume, leverage, openPrice, closePrice, pnl, spreadCost, margin float64
		var contestID *int64
		var createdAt time.Time
		var closedAt *time.Time
		if err := rows.Scan(&id, &symbol, &direction, &volume, &leverage,
			&openPrice, &closePrice, &pnl, &spreadCost, &margin,
			&contestID, &createdAt, &closedAt); err != nil {
			continue
		}
		totalPnL += pnl
		trades = append(trades, gin.H{
			"id":          id,
			"symbol":      symbol,
			"direction":   direction,
			"volume":      volume,
			"leverage":    leverage,
			"open_price":  openPrice,
			"close_price": closePrice,
			"pnl":         pnl,
			"spread_cost": spreadCost,
			"margin":      margin,
			"contest_id":  contestID,
			"created_at":  createdAt,
			"closed_at":   closedAt,
		})
	}

	common.Success(c, gin.H{
		"total_pnl": math.Round(totalPnL*100) / 100,
		"trades":    trades,
	})
}

// GetClosedPositionsPage returns a paginated list of closed positions for the user.
// Kept separate from GetTradePnL so the trade calendar (which needs ALL closed trades)
// is unaffected. Response: { list, total, page, page_size, total_pnl }
func (s *TradeService) GetClosedPositionsPage(c *gin.Context) {
	userID := c.GetInt64("user_id")
	page, pageSize := parsePagination(c)

	if s.isMemoryMode() {
		closed := s.mem.GetClosedPositions(userID)
		// 按平仓时间从近到远排序
		sort.Slice(closed, func(i, j int) bool {
			if closed[i].ClosedAt == nil {
				return false
			}
			if closed[j].ClosedAt == nil {
				return true
			}
			return closed[i].ClosedAt.After(*closed[j].ClosedAt)
		})
		total := len(closed)
		totalPnL := 0.0
		for _, p := range closed {
			totalPnL += p.FloatingPnL
		}
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		var trades []gin.H
		for _, p := range closed[start:end] {
			var closedAt *time.Time
			if p.ClosedAt != nil && !p.ClosedAt.IsZero() {
				closedAt = p.ClosedAt
			}
			trades = append(trades, gin.H{
				"id":          p.ID,
				"symbol":      p.Symbol,
				"direction":   p.Direction,
				"volume":      p.Volume,
				"leverage":    p.Leverage,
				"open_price":  p.OpenPrice,
				"close_price": p.CurrentPrice,
				"pnl":         p.FloatingPnL,
				"spread_cost": p.SpreadCost,
				"margin":      p.Margin,
				"created_at":  p.CreatedAt,
				"closed_at":   closedAt,
			})
		}
		if trades == nil {
			trades = []gin.H{}
		}
		common.Success(c, gin.H{
			"list":      trades,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
			"total_pnl": math.Round(totalPnL*100) / 100,
		})
		return
	}

	offset := (page - 1) * pageSize
	var total int64
	_ = s.pg.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM positions WHERE user_id=$1 AND status=2`, userID).Scan(&total)

	rows, err := s.pg.Pool.Query(context.Background(),
		`SELECT p.id, p.symbol, p.direction, p.volume, p.leverage,
		        p.open_price, p.current_price, p.floating_pnl, p.spread_cost,
		        p.margin, p.created_at, p.closed_at
		 FROM positions p WHERE p.user_id=$1 AND p.status=2
		 ORDER BY p.closed_at DESC LIMIT $2 OFFSET $3`, userID, pageSize, offset)
	if err != nil {
		common.Error(c, errs.Internal, "query failed")
		return
	}
	defer rows.Close()

	totalPnL := 0.0
	var trades []gin.H
	for rows.Next() {
		var id int64
		var symbol string
		var direction int
		var volume, leverage, openPrice, closePrice, pnl, spreadCost, margin float64
		var contestID *int64
		var createdAt time.Time
		var closedAt *time.Time
		if err := rows.Scan(&id, &symbol, &direction, &volume, &leverage,
			&openPrice, &closePrice, &pnl, &spreadCost, &margin,
			&contestID, &createdAt, &closedAt); err != nil {
			continue
		}
		totalPnL += pnl
		trades = append(trades, gin.H{
			"id":          id,
			"symbol":      symbol,
			"direction":   direction,
			"volume":      volume,
			"leverage":    leverage,
			"open_price":  openPrice,
			"close_price": closePrice,
			"pnl":         pnl,
			"spread_cost": spreadCost,
			"margin":      margin,
			"contest_id":  contestID,
			"created_at":  createdAt,
			"closed_at":   closedAt,
		})
	}
	if trades == nil {
		trades = []gin.H{}
	}

	common.Success(c, gin.H{
		"list":      trades,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"total_pnl": math.Round(totalPnL*100) / 100,
	})
}

// ========== Helpers ==========

func (s *TradeService) getQuote(symbol, contractMonth string) (*common.Quote, error) {
	// Try Redis first (market service stores full quote JSON here)
	if s.rdb != nil {
		key := fmt.Sprintf("market:quote:%s:%s", symbol, contractMonth)
		data, err := s.rdb.CacheGet(context.Background(), key)
		if err == nil && data != "" {
			var quote common.Quote
			if json.Unmarshal([]byte(data), &quote) == nil && quote.Price > 0 {
				return &quote, nil
			}
		}
	}

	// Fallback 1: Get cached quote directly from MarketService (works without Redis)
	if s.marketSvc != nil {
		if q := s.marketSvc.GetCachedQuote(symbol); q != nil && q.Price > 0 {
			return q, nil
		}
	}

	// Fallback 2: NO invented price. Returning a fake ~4139 quote would
	// misprice trades by ~5% during an outage. Refuse instead — callers
	// surface this as "market data unavailable" and reject the order.
	return nil, fmt.Errorf("no real-time quote available for %s; cannot price trade", symbol)
}

func (s *TradeService) calculatePnL(p *common.Position, currentPrice float64) float64 {
	contractSize := getContractSize()
	var diff float64
	if p.Direction == 1 {
		diff = currentPrice - p.OpenPrice
	} else {
		diff = p.OpenPrice - currentPrice
	}
	return diff * contractSize * p.Volume
}

func (s *TradeService) updatePositionsWithQuote(userID int64) {
	// Update all open positions with current market price
	rows, err := s.pg.Pool.Query(context.Background(),
		`SELECT id, symbol, contract_month, direction, volume, open_price, leverage
		 FROM positions WHERE user_id=$1 AND status=1`, userID)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p common.Position
		rows.Scan(&p.ID, &p.Symbol, &p.ContractMonth, &p.Direction, &p.Volume, &p.OpenPrice, &p.Leverage)

		quote, qErr := s.getQuote(p.Symbol, p.ContractMonth)
		if qErr != nil {
			continue
		}
		pnl := s.calculatePnL(&p, quote.Price)
		s.pg.Pool.Exec(context.Background(),
			`UPDATE positions SET current_price=$1, floating_pnl=$2, updated_at=NOW()
			 WHERE id=$3`, quote.Price, pnl, p.ID)
	}
}

// generateOrderNo creates unique order number
func generateOrderNo() string {
	return fmt.Sprintf("ORD%s%04d", time.Now().Format("20060102150405"), time.Now().Nanosecond()%10000)
}

// keep uuid import happy
var _ = uuid.NewString

// ========== Memory Mode Implementations ==========

func (s *TradeService) placeOrderMem(c *gin.Context, userID int64, req *PlaceOrderReq, quote *common.Quote, execPrice, margin, spreadCost float64) {
	// Route margin to the 金龟子 contest wallet when the user has an active enrollment.
	tw, enr := s.loadTradeWallet(userID, req.ContestID)
	if !tw.contest && tw.w == nil {
		common.Error(c, errs.Internal, "wallet not found")
		return
	}
	if enr != nil && req.ContestID == nil {
		id := enr.UserID
		req.ContestID = &id
	}

	totalCost := margin + spreadCost
	if tw.balance() < totalCost {
		common.Error(c, errs.InsufficientMargin,
			fmt.Sprintf("need %.2f, have %.2f", totalCost, tw.balance()))
		return
	}

	// Deduct balance, freeze margin
	newBalance := tw.balance() - totalCost
	newFrozen := tw.frozen() + margin
	tw.set(newBalance, newFrozen)
	tw.save(s)

	// Create order
	orderNo := fmt.Sprintf("ORD%s%04d", time.Now().Format("20060102150405"), time.Now().Nanosecond()%10000)
	orderID := s.mem.NextOrderID()
	now := time.Now()
	s.mem.SaveOrder(&common.Order{
		ID:            orderID,
		OrderNo:       orderNo,
		UserID:        userID,
		ContestID:     req.ContestID,
		Symbol:        req.Symbol,
		ContractMonth: req.ContractMonth,
		Direction:     req.Direction,
		OrderType:     req.OrderType,
		Volume:        req.Volume,
		Leverage:      req.Leverage,
		Price:         req.Price,
		StopLoss:      req.StopLoss,
		TakeProfit:    req.TakeProfit,
		Status:        2, // filled
		ExecutedPrice: &execPrice,
		Margin:        margin,
		SpreadCost:    spreadCost,
		CreatedAt:     now,
		UpdatedAt:     now,
	})

	// Create position
	positionID := s.mem.NextPositionID()
	contractMonth := req.ContractMonth
	if contractMonth == "" {
		contractMonth = "SPOT"
	}
	pos := &common.Position{
		ID:            positionID,
		UserID:        userID,
		ContestID:     req.ContestID,
		OrderNo:       orderNo,
		Symbol:        req.Symbol,
		ContractMonth: contractMonth,
		Direction:     req.Direction,
		Volume:        req.Volume,
		Leverage:      req.Leverage,
		OpenPrice:     execPrice,
		CurrentPrice:  execPrice,
		StopLoss:      req.StopLoss,
		TakeProfit:    req.TakeProfit,
		Margin:        margin,
		FloatingPnL:   0,
		SpreadCost:    spreadCost,
		Status:        1, // open
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	s.mem.SavePosition(pos)

	// Record wallet / contest transaction
	balBefore := newBalance + totalCost
	balAfter := newBalance + spreadCost
	tw.txn(s, "margin_freeze", margin, balBefore, balAfter, orderNo, "开仓保证金")

	common.Success(c, gin.H{
		"order_id":       orderID,
		"order_no":       orderNo,
		"position_id":    positionID,
		"executed_price": execPrice,
		"margin":         margin,
		"spread_cost":    math.Round(spreadCost*100) / 100,
	})
}

func (s *TradeService) getPositionsMem(c *gin.Context, userID int64, contestID string) {
	positions := s.mem.GetPositions(userID, nil)

	// Update floating PnL with current quote
	for i := range positions {
		p := &positions[i]
		quote, err := s.getQuote(p.Symbol, p.ContractMonth)
		if err == nil {
			pnl := s.calculatePnL(p, quote.Price)
			p.CurrentPrice = quote.Price
			p.FloatingPnL = pnl
			cp := *p
			s.mem.UpdatePosition(&cp)
		}
	}

	// Filter by contest_id: "" or "null" → gamecoin (contest_id IS NULL),
	// "<id>" → matching contest, "all" → all
	filtered := make([]common.Position, 0, len(positions))
	for _, p := range positions {
		if contestID == "" || contestID == "null" {
			if p.ContestID != nil {
				continue
			}
		} else if contestID != "all" {
			if cid, err := strconv.ParseInt(contestID, 10, 64); err == nil {
				if p.ContestID == nil || *p.ContestID != cid {
					continue
				}
			}
		}
		filtered = append(filtered, p)
	}

	common.Success(c, filtered)
}

func (s *TradeService) closePositionMem(c *gin.Context, userID int64, positionID int64) {
	pos := s.mem.GetPositionByID(positionID)
	if pos == nil || pos.UserID != userID || pos.Status != 1 {
		common.Error(c, errs.PositionNotFound, "position not found")
		return
	}

	// Get current quote
	quote, err := s.getQuote(pos.Symbol, pos.ContractMonth)
	if err == nil {
		pos.CurrentPrice = quote.Price
		pos.FloatingPnL = s.calculatePnL(pos, quote.Price)
	}

	// Close position
	now := time.Now()
	pos.Status = 2
	pos.ClosedAt = &now
	s.mem.UpdatePosition(pos)

	// Update wallet (金龟子 contest wallet when enrolled, else main wallet)
	tw, _ := s.loadTradeWallet(userID, pos.ContestID)
	if tw.contest || tw.w != nil {
		balBefore := tw.balance()
		totalReturn := pos.Margin + pos.FloatingPnL
		newBalance := balBefore + totalReturn
		newFrozen := tw.frozen() - pos.Margin
		if newFrozen < 0 {
			newFrozen = 0
		}
		tw.set(newBalance, newFrozen)
		tw.save(s)

		// Record transactions
		tw.txn(s, "margin_release", pos.Margin, balBefore, balBefore+pos.Margin, pos.OrderNo, "平仓释放保证金")

		if pos.FloatingPnL != 0 {
			txnType := "pnl_credit"
			if pos.FloatingPnL < 0 {
				txnType = "pnl_debit"
			}
			tw.txn(s, txnType, pos.FloatingPnL, balBefore+pos.Margin, newBalance, pos.OrderNo, "平仓盈亏")
		}
	}

	common.Success(c, gin.H{
		"position_id":     pos.ID,
		"close_price":     pos.CurrentPrice,
		"realized_pnl":    math.Round(pos.FloatingPnL*100) / 100,
		"margin_returned": pos.Margin,
	})
}

// ========== Pending Order (挂单) Handlers ==========

// placePendingOrderMem saves a limit/stop order as pending (status=1) in memory mode.
// Margin is frozen but no position is created until the order is filled by the matching engine.
func (s *TradeService) placePendingOrderMem(c *gin.Context, userID int64, req *PlaceOrderReq, quote *common.Quote, triggerPrice, margin float64) {
	// Route margin to the 金龟子 contest wallet when the user has an active enrollment.
	tw, enr := s.loadTradeWallet(userID, req.ContestID)
	if !tw.contest && tw.w == nil {
		common.Error(c, errs.Internal, "wallet not found")
		return
	}
	if enr != nil && req.ContestID == nil {
		id := enr.UserID
		req.ContestID = &id
	}
	if tw.balance() < margin {
		common.Error(c, errs.InsufficientMargin,
			fmt.Sprintf("need %.2f for margin freeze, have %.2f", margin, tw.balance()))
		return
	}

	// Freeze margin only (no spread cost until filled)
	newBalance := tw.balance() - margin
	newFrozen := tw.frozen() + margin
	tw.set(newBalance, newFrozen)
	tw.save(s)

	orderNo := generateOrderNo()
	orderID := s.mem.NextOrderID()
	now := time.Now()
	s.mem.SaveOrder(&common.Order{
		ID:            orderID,
		OrderNo:       orderNo,
		UserID:        userID,
		ContestID:     req.ContestID,
		Symbol:        req.Symbol,
		ContractMonth: req.ContractMonth,
		Direction:     req.Direction,
		OrderType:     req.OrderType,
		Volume:        req.Volume,
		Leverage:      req.Leverage,
		Price:         req.Price, // trigger price
		StopLoss:      req.StopLoss,
		TakeProfit:    req.TakeProfit,
		Status:        1, // PENDING
		Margin:        margin,
		CreatedAt:     now,
		UpdatedAt:     now,
	})

	balBefore := newBalance + margin
	tw.txn(s, "margin_freeze", margin, balBefore, newBalance, orderNo, "挂单冻结保证金")

	common.Success(c, gin.H{
		"order_id":   orderID,
		"order_no":   orderNo,
		"status":     "pending",
		"trigger_price": triggerPrice,
		"margin":      margin,
	})
}

// placePendingOrderPG saves a limit/stop order as pending in PostgreSQL mode.
func (s *TradeService) placePendingOrderPG(c *gin.Context, userID int64, req *PlaceOrderReq, triggerPrice, margin float64) {
	tx, err := s.pg.Pool.Begin(context.Background())
	if err != nil {
		common.Error(c, errs.Internal, "tx begin failed")
		return
	}
	defer tx.Rollback(context.Background())

	var walletID int64
	var balance float64
	var version int64
	err = tx.QueryRow(context.Background(),
		`SELECT id, balance, version FROM wallets WHERE user_id=$1 FOR UPDATE`, userID).Scan(&walletID, &balance, &version)
	if err != nil {
		common.Error(c, errs.Internal, "wallet not found")
		return
	}
	if balance < margin {
		common.Error(c, errs.InsufficientMargin,
			fmt.Sprintf("need %.2f, have %.2f", margin, balance))
		return
	}

	newBalance := balance - margin
	_, err = tx.Exec(context.Background(),
		`UPDATE wallets SET balance=$1, frozen=frozen+$2, version=version+1, updated_at=NOW()
		 WHERE id=$3 AND version=$4`, newBalance, margin, walletID, version)
	if err != nil {
		common.Error(c, errs.Internal, "wallet update failed")
		return
	}

	orderNo := generateOrderNo()
	var orderID int64
	err = tx.QueryRow(context.Background(),
		`INSERT INTO orders (order_no, user_id, contest_id, symbol, contract_month,
		 direction, order_type, volume, leverage, price, stop_loss, take_profit,
		 status, margin, spread_cost)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,$13,0) RETURNING id`,
		orderNo, userID, req.ContestID, req.Symbol, req.ContractMonth,
		req.Direction, req.OrderType, req.Volume, req.Leverage,
		req.Price, req.StopLoss, req.TakeProfit, margin).Scan(&orderID)
	if err != nil {
		common.Error(c, errs.Internal, "pending order creation failed: "+err.Error())
		return
	}

	_, _ = tx.Exec(context.Background(),
		`INSERT INTO wallet_transactions (user_id, type, amount, balance_before, balance_after, reference_id, remark)
		 VALUES ($1, 'margin_freeze', $2, $3, $4, $5, '挂单冻结保证金')`,
		userID, margin, balance, newBalance, orderNo)

	if err := tx.Commit(context.Background()); err != nil {
		common.Error(c, errs.Internal, "commit failed: "+err.Error())
		return
	}

	common.Success(c, gin.H{
		"order_id":       orderID,
		"order_no":       orderNo,
		"status":         "pending",
		"trigger_price":  triggerPrice,
		"margin":         margin,
	})
}

// CancelOrder cancels a pending order and returns frozen margin to the wallet.
func (s *TradeService) CancelOrder(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req struct {
		OrderID int64 `json:"order_id" binding:"required"`
	}
	if err := common.BindJSON(c, &req); err != nil {
		return
	}

	if s.isMemoryMode() {
		s.cancelOrderMem(c, userID, req.OrderID)
		return
	}

	// PG path
	tx, err := s.pg.Pool.Begin(context.Background())
	if err != nil {
		common.Error(c, errs.Internal, "tx begin failed")
		return
	}
	defer tx.Rollback(context.Background())

	var orderStatus int
	var orderMargin float64
	var orderNo string
	err = tx.QueryRow(context.Background(),
		`SELECT status, margin, order_no FROM orders WHERE id=$1 AND user_id=$2 FOR UPDATE`,
		req.OrderID, userID).Scan(&orderStatus, &orderMargin, &orderNo)
	if err != nil {
		common.Error(c, errs.InvalidParam, "order not found or not yours")
		return
	}
	if orderStatus != 1 {
		common.Error(c, errs.InvalidParam, "only pending orders can be cancelled")
		return
	}

	// Update order status to cancelled
	_, err = tx.Exec(context.Background(),
		`UPDATE orders SET status=3, updated_at=NOW() WHERE id=$1`, req.OrderID)
	if err != nil {
		common.Error(c, errs.Internal, "cancel update failed")
		return
	}

	// Return frozen margin
	var walletID int64
	var balance float64
	var frozen float64
	var version int64
	err = tx.QueryRow(context.Background(),
		`SELECT id, balance, frozen, version FROM wallets WHERE user_id=$1 FOR UPDATE`, userID).Scan(
		&walletID, &balance, &frozen, &version)
	if err != nil {
		common.Error(c, errs.Internal, "wallet not found")
		return
	}

	newBalance := balance + orderMargin
	newFrozen := frozen - orderMargin
	if newFrozen < 0 {
		newFrozen = 0
	}
	_, err = tx.Exec(context.Background(),
		`UPDATE wallets SET balance=$1, frozen=$2, version=version+1, updated_at=NOW()
		 WHERE id=$3 AND version=$4`, newBalance, newFrozen, walletID, version)
	if err != nil {
		common.Error(c, errs.Internal, "wallet refund failed")
		return
	}

	_, _ = tx.Exec(context.Background(),
		`INSERT INTO wallet_transactions (user_id, type, amount, balance_before, balance_after, reference_id, remark)
		 VALUES ($1, 'margin_release', $2, $3, $4, $5, '撤单退还保证金')`,
		userID, orderMargin, balance, newBalance, orderNo)

	if err := tx.Commit(context.Background()); err != nil {
		common.Error(c, errs.Internal, "commit failed")
		return
	}

	common.Success(c, gin.H{"order_id": req.OrderID, "status": "cancelled", "refunded_margin": orderMargin})
}

func (s *TradeService) cancelOrderMem(c *gin.Context, userID int64, orderID int64) {
	orders := s.mem.GetAllOrders()
	var target *common.Order
	for i := range orders {
		if orders[i].ID == orderID && orders[i].UserID == userID {
			target = &orders[i]
			break
		}
	}
	if target == nil || target.Status != 1 {
		common.Error(c, errs.InvalidParam, "pending order not found")
		return
	}

	// Cancel the order
	now := time.Now()
	target.Status = 3 // CANCELLED
	target.UpdatedAt = now
	s.mem.UpdateOrder(target)

	// Return frozen margin (金龟子 contest wallet when enrolled, else main wallet)
	tw, _ := s.loadTradeWallet(userID, target.ContestID)
	if tw.contest || tw.w != nil {
		balBefore := tw.balance()
		newBalance := balBefore + target.Margin
		newFrozen := tw.frozen() - target.Margin
		if newFrozen < 0 {
			newFrozen = 0
		}
		tw.set(newBalance, newFrozen)
		tw.save(s)

		tw.txn(s, "margin_release", target.Margin, balBefore, newBalance, target.OrderNo, "撤单退还保证金")
	}

	common.Success(c, gin.H{"order_id": orderID, "status": "cancelled", "refunded_margin": target.Margin})
}

// GetPendingOrders returns the current user's pending (unfilled) orders.
// When contest_id is provided, only that contest's orders are returned (used by
// the 选拔赛 trading page). Otherwise we still skip contest orders when the user
// has any gamecoin pending orders so the 交易大厅 view stays clean.
func (s *TradeService) GetPendingOrders(c *gin.Context) {
	userID := c.GetInt64("user_id")
	contestIDStr := c.Query("contest_id")

	if s.isMemoryMode() {
		orders := s.mem.GetAllOrders()
		var pending []common.Order
		for _, o := range orders {
			if o.UserID != userID || o.Status != 1 {
				continue
			}
			if contestIDStr != "" {
				// contest mode: only return orders belonging to that contest
				if cid, err := strconv.ParseInt(contestIDStr, 10, 64); err == nil {
					if o.ContestID == nil || *o.ContestID != cid {
						continue
					}
				}
			} else {
				// gamecoin mode: only return non-contest orders
				if o.ContestID != nil {
					continue
				}
			}
			pending = append(pending, o)
		}
		if pending == nil {
			pending = []common.Order{}
		}
		common.Success(c, pending)
		return
	}

	query := `SELECT id, order_no, symbol, contract_month, direction, order_type, volume, leverage,
		        price, stop_loss, take_profit, margin, status, contest_id, created_at
		 FROM orders WHERE user_id=$1 AND status=1`
	args := []any{userID}
	if contestIDStr != "" {
		if cid, err := strconv.ParseInt(contestIDStr, 10, 64); err == nil {
			query += " AND contest_id=$2"
			args = append(args, cid)
		}
	} else {
		query += " AND contest_id IS NULL"
	}
	query += " ORDER BY created_at ASC"
	rows, err := s.pg.Pool.Query(context.Background(), query, args...)
	if err != nil {
		common.Error(c, errs.Internal, "query failed")
		return
	}
	defer rows.Close()

	var orders []common.Order
	for rows.Next() {
		var o common.Order
		if err := rows.Scan(&o.ID, &o.OrderNo, &o.Symbol, &o.ContractMonth,
			&o.Direction, &o.OrderType, &o.Volume, &o.Leverage,
			&o.Price, &o.StopLoss, &o.TakeProfit, &o.Margin, &o.Status,
			&o.ContestID, &o.CreatedAt); err != nil {
			continue
		}
		orders = append(orders, o)
	}
	if orders == nil {
		orders = []common.Order{}
	}
	common.Success(c, orders)
}

// ========== Order Matching Engine (挂单撮合引擎) ==========
// Called from MarketService on every tick to check if any pending orders should be filled.

// CheckPendingOrders scans all pending orders and fills those whose trigger conditions are met.
// Matching rules:
//   Limit Long  (type=2, dir=1): fill when current price <= trigger price (buy at/below target)
//   Limit Short (type=2, dir=2): fill when current price >= trigger price (sell at/above target)
//   Stop Long   (type=3, dir=1): fill when current price >= trigger price (stop-buy on breakout)
//   Stop Short  (type=3, dir=2): fill when current price <= trigger price (stop-sell on breakdown)
func (s *TradeService) CheckPendingOrders(quote *common.Quote) []gin.H {
	if quote == nil || quote.Price <= 0 {
		return nil
	}
	price := quote.Price

	var filled []gin.H

	// Memory mode
	if s.isMemoryMode() {
		filled = s.matchPendingOrdersMem(quote, price)
	} else {
		filled = s.matchPendingOrdersPG(quote, price)
	}

	if len(filled) > 0 && s.marketSvc != nil && s.marketSvc.hub != nil {
		// Notify affected users via WebSocket
		for _, f := range filled {
			userID := f["user_id"].(int64)
			msg, _ := json.Marshal(gin.H{"type": "trade", "event": "order_filled", "data": f})
			s.marketSvc.hub.BroadcastToChannel(fmt.Sprintf("trade:%d", userID), msg)
		}
	}

	return filled
}

func (s *TradeService) matchPendingOrdersMem(quote *common.Quote, currentPrice float64) []gin.H {
	orders := s.mem.GetAllOrders()
	var filled []gin.H

	for _, ord := range orders {
		if ord.Status != 1 {
			continue
		}
		if ord.Price == nil {
			continue
		}
		triggerPrice := *ord.Price

		shouldFill := false
		switch ord.OrderType {
		case 2: // Limit order
			if ord.Direction == 1 && currentPrice <= triggerPrice { // Limit long: buy at or below
				shouldFill = true
			} else if ord.Direction == 2 && currentPrice >= triggerPrice { // Limit short: sell at or above
				shouldFill = true
			}
		case 3: // Stop order
			if ord.Direction == 1 && currentPrice >= triggerPrice { // Stop long: buy on break above
				shouldFill = true
			} else if ord.Direction == 2 && currentPrice <= triggerPrice { // Stop short: sell on break below
				shouldFill = true
			}
		}

		if !shouldFill {
			continue
		}

		// Fill the order — use actual market price for execution
		execPrice := currentPrice
		if ord.Direction == 1 && quote.Ask > 0 {
			execPrice = quote.Ask
		} else if ord.Direction == 2 && quote.Bid > 0 {
			execPrice = quote.Bid
		}

		// Calculate spread cost at fill time
		contractSize := getContractSize()
		spreadCost := 0.0
		if quote.Ask > 0 && quote.Bid > 0 {
			spreadCost = (quote.Ask - quote.Bid) * contractSize * ord.Volume
		}

		positionID := s.mem.NextPositionID()
		now := time.Now()

		// Create position
		pos := &common.Position{
			ID:            positionID,
			UserID:        ord.UserID,
			ContestID:     ord.ContestID,
			OrderNo:       ord.OrderNo,
			Symbol:        ord.Symbol,
			ContractMonth: ord.ContractMonth,
			Direction:     ord.Direction,
			Volume:        ord.Volume,
			Leverage:      ord.Leverage,
			OpenPrice:     execPrice,
			CurrentPrice:  execPrice,
			StopLoss:      ord.StopLoss,
			TakeProfit:    ord.TakeProfit,
			Margin:        ord.Margin,
			FloatingPnL:   0,
			SpreadCost:    spreadCost,
			Status:        1, // OPEN
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		s.mem.SavePosition(pos)

		// Update order to filled
		ord.Status = 2 // FILLED
		ord.ExecutedPrice = &execPrice
		ord.SpreadCost = spreadCost
		ord.UpdatedAt = now
		s.mem.UpdateOrder(&ord)

		// Deduct spread cost from wallet (margin was already frozen)
		if spreadCost > 0 {
			tw, _ := s.loadTradeWallet(ord.UserID, ord.ContestID)
			if tw.contest || tw.w != nil {
				tw.set(tw.balance()-spreadCost, tw.frozen())
				tw.save(s)
			}
		}

		filled = append(filled, gin.H{
			"order_id":       ord.ID,
			"order_no":       ord.OrderNo,
			"user_id":        ord.UserID,
			"position_id":    positionID,
			"executed_price": execPrice,
			"direction":      ord.Direction,
			"symbol":         ord.Symbol,
			"volume":         ord.Volume,
		})

		log.Printf("[MATCH] Order %s (%s %s %.2f) FILLED @ %.2f, position #%d",
			ord.OrderNo, directionName(ord.Direction), orderTypeName(ord.OrderType),
			triggerPrice, execPrice, positionID)
	}

	return filled
}

func (s *TradeService) matchPendingOrdersPG(quote *common.Quote, currentPrice float64) []gin.H {
	// Get all pending orders
	rows, err := s.pg.Pool.Query(context.Background(),
		`SELECT id, order_no, user_id, symbol, contract_month, direction, order_type, volume, leverage,
		        price, stop_loss, take_profit, margin
		 FROM orders WHERE status=1`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	type pendingOrd struct {
		ID            int64
		OrderNo       string
		UserID        int64
		Symbol        string
		ContractMonth string
		Direction     int
		OrderType     int
		Volume        float64
		Leverage      int
		Price         float64
		StopLoss      *float64
		TakeProfit    *float64
		Margin        float64
		ContestID     *int64
	}

	var pending []pendingOrd
	for rows.Next() {
		var p pendingOrd
		if err := rows.Scan(&p.ID, &p.OrderNo, &p.UserID, &p.Symbol, &p.ContractMonth,
			&p.Direction, &p.OrderType, &p.Volume, &p.Leverage,
			&p.Price, &p.StopLoss, &p.TakeProfit, &p.Margin, &p.ContestID); err != nil {
			continue
		}
		pending = append(pending, p)
	}

	var filled []gin.H
	contractSize := getContractSize()

	for _, ord := range pending {
		shouldFill := false
		switch ord.OrderType {
		case 2:
			if ord.Direction == 1 && currentPrice <= ord.Price {
				shouldFill = true
			} else if ord.Direction == 2 && currentPrice >= ord.Price {
				shouldFill = true
			}
		case 3:
			if ord.Direction == 1 && currentPrice >= ord.Price {
				shouldFill = true
			} else if ord.Direction == 2 && currentPrice <= ord.Price {
				shouldFill = true
			}
		}
		if !shouldFill {
			continue
		}

		execPrice := currentPrice
		if ord.Direction == 1 && quote.Ask > 0 {
			execPrice = quote.Ask
		} else if ord.Direction == 2 && quote.Bid > 0 {
			execPrice = quote.Bid
		}

		spreadCost := 0.0
		if quote.Ask > 0 && quote.Bid > 0 {
			spreadCost = (quote.Ask - quote.Bid) * contractSize * ord.Volume
		}

		// TX: mark order filled + create position + deduct spread
		tx, txErr := s.pg.Pool.Begin(context.Background())
		if txErr != nil {
			continue
		}

		var posID int64
		err := tx.QueryRow(context.Background(),
			`UPDATE orders SET status=2, executed_price=$1, spread_cost=$2, updated_at=NOW() WHERE id=$3 AND status=1
			 RETURNING id`, execPrice, spreadCost, ord.ID).Scan(&posID)
		if err != nil {
			tx.Rollback(context.Background())
			continue
		}

		txErr = tx.QueryRow(context.Background(),
			`INSERT INTO positions (user_id, contest_id, order_no, symbol, contract_month,
			 direction, volume, leverage, open_price, current_price, stop_loss, take_profit,
			 margin, spread_cost, status)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,1) RETURNING id`,
			ord.UserID, ord.ContestID, ord.OrderNo, ord.Symbol, ord.ContractMonth,
			ord.Direction, ord.Volume, ord.Leverage,
			execPrice, execPrice, ord.StopLoss, ord.TakeProfit,
			ord.Margin, spreadCost).Scan(&posID)
		if txErr != nil {
			tx.Rollback(context.Background())
			continue
		}

		if spreadCost > 0 {
			tx.Exec(context.Background(),
				`UPDATE wallets SET balance=balance-$1 WHERE user_id=$2`, spreadCost, ord.UserID)
		}

		tx.Commit(context.Background())

		filled = append(filled, gin.H{
			"order_id":       ord.ID,
			"order_no":       ord.OrderNo,
			"user_id":        ord.UserID,
			"position_id":    posID,
			"executed_price": execPrice,
			"direction":      ord.Direction,
			"symbol":         ord.Symbol,
			"volume":         ord.Volume,
		})

		log.Printf("[MATCH] Order %s (%s %s %.2f) FILLED @ %.2f, position #%d",
			ord.OrderNo, directionName(ord.Direction), orderTypeName(ord.OrderType),
			ord.Price, execPrice, posID)
	}

	return filled
}

// ========== Stop-Loss / Take-Profit Trigger (止盈止损触发引擎) ==========

// CheckStopTriggers scans all open positions and auto-closes those that hit SL or TP.
func (s *TradeService) CheckStopTriggers(quote *common.Quote) []gin.H {
	if quote == nil || quote.Price <= 0 {
		return nil
	}
	price := quote.Price

	var triggered []gin.H

	if s.isMemoryMode() {
		triggered = s.checkSLTPMem(quote, price)
	} else {
		triggered = s.checkSLTPPG(quote, price)
	}

	if len(triggered) > 0 && s.marketSvc != nil && s.marketSvc.hub != nil {
		for _, t := range triggered {
			userID := t["user_id"].(int64)
			msg, _ := json.Marshal(gin.H{"type": "trade", "event": "sltp_triggered", "data": t})
			s.marketSvc.hub.BroadcastToChannel(fmt.Sprintf("trade:%d", userID), msg)
		}
	}

	return triggered
}

func (s *TradeService) checkSLTPMem(quote *common.Quote, currentPrice float64) []gin.H {
	allPos := s.mem.GetAllPositions()
	var triggered []gin.H

	for _, p := range allPos {
		reason := ""
		if p.StopLoss != nil && p.Direction == 1 && currentPrice <= *p.StopLoss {
			reason = "stop_loss"
		} else if p.StopLoss != nil && p.Direction == 2 && currentPrice >= *p.StopLoss {
			reason = "stop_loss"
		} else if p.TakeProfit != nil && p.Direction == 1 && currentPrice >= *p.TakeProfit {
			reason = "take_profit"
		} else if p.TakeProfit != nil && p.Direction == 2 && currentPrice <= *p.TakeProfit {
			reason = "take_profit"
		}

		if reason == "" {
			continue
		}

		// Close position at trigger price
		pnl := s.calculatePnL(&p, currentPrice)
		now := time.Now()
		p.Status = 2
		p.CurrentPrice = currentPrice
		p.FloatingPnL = pnl
		p.ClosedAt = &now
		p.UpdatedAt = now
		s.mem.UpdatePosition(&p)

		// Release margin + credit/debit PnL (金龟子 contest wallet when enrolled, else main)
		tw, _ := s.loadTradeWallet(p.UserID, p.ContestID)
		if tw.contest || tw.w != nil {
			balBefore := tw.balance()
			totalReturn := p.Margin + pnl
			newBalance := balBefore + totalReturn
			newFrozen := tw.frozen() - p.Margin
			if newFrozen < 0 {
				newFrozen = 0
			}
			tw.set(newBalance, newFrozen)
			tw.save(s)

			tw.txn(s, "margin_release", p.Margin, balBefore, balBefore+p.Margin, p.OrderNo, reason+"_释放保证金")
			if pnl != 0 {
				txnType := "pnl_credit"
				if pnl < 0 {
					txnType = "pnl_debit"
				}
				tw.txn(s, txnType, pnl, balBefore+p.Margin, newBalance, p.OrderNo, reason+"_自动平仓盈亏")
			}
		}

		triggered = append(triggered, gin.H{
			"position_id":   p.ID,
			"user_id":       p.UserID,
			"order_no":      p.OrderNo,
			"symbol":        p.Symbol,
			"direction":     p.Direction,
			"close_price":   currentPrice,
			"realized_pnl":  math.Round(pnl*100) / 100,
			"reason":        reason,
		})

		log.Printf("[SLTP] Position #%d %s %s triggered @ %.2f, pnl=%.2f",
			p.ID, directionName(p.Direction), reason, currentPrice, math.Round(pnl*100)/100)
	}

	return triggered
}

func (s *TradeService) checkSLTPPG(quote *common.Quote, currentPrice float64) []gin.H {
	rows, err := s.pg.Pool.Query(context.Background(),
		`SELECT id, user_id, order_no, symbol, direction, volume, leverage, open_price,
		        margin, stop_loss, take_profit
		 FROM positions WHERE status=1`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	type openPos struct {
		ID          int64
		UserID      int64
		OrderNo     string
		Symbol      string
		Direction   int
		Volume      float64
		Leverage    int
		OpenPrice   float64
		Margin      float64
		StopLoss    *float64
		TakeProfit  *float64
	}

	var positions []openPos
	for rows.Next() {
		var p openPos
		if err := rows.Scan(&p.ID, &p.UserID, &p.OrderNo, &p.Symbol, &p.Direction,
			&p.Volume, &p.Leverage, &p.OpenPrice, &p.Margin, &p.StopLoss, &p.TakeProfit); err != nil {
			continue
		}
		positions = append(positions, p)
	}

	var triggered []gin.H

	for _, p := range positions {
		reason := ""
		if p.StopLoss != nil && p.Direction == 1 && currentPrice <= *p.StopLoss {
			reason = "stop_loss"
		} else if p.StopLoss != nil && p.Direction == 2 && currentPrice >= *p.StopLoss {
			reason = "stop_loss"
		} else if p.TakeProfit != nil && p.Direction == 1 && currentPrice >= *p.TakeProfit {
			reason = "take_profit"
		} else if p.TakeProfit != nil && p.Direction == 2 && currentPrice <= *p.TakeProfit {
			reason = "take_profit"
		}
		if reason == "" {
			continue
		}

		pnl := s.calculatePnL(&common.Position{
			Direction:  p.Direction,
			Volume:     p.Volume,
			OpenPrice:  p.OpenPrice,
		}, currentPrice)

		// TX: close position + return margin + PnL
		tx, txErr := s.pg.Pool.Begin(context.Background())
		if txErr != nil {
			continue
		}

		now := time.Now()
		_, txErr = tx.Exec(context.Background(),
			`UPDATE positions SET status=2, current_price=$1, floating_pnl=$2, closed_at=$3, updated_at=NOW()
			 WHERE id=$4 AND status=1`, currentPrice, pnl, now, p.ID)
		if txErr != nil {
			tx.Rollback(context.Background())
			continue
		}

		totalReturn := p.Margin + pnl
		tx.Exec(context.Background(),
			`UPDATE wallets SET balance=balance+$1, frozen=frozen-GREATEST(frozen-$2,0), version=version+1, updated_at=NOW()
			 WHERE user_id=$3`, totalReturn, p.Margin, p.UserID)

		tx.Exec(context.Background(),
			`INSERT INTO wallet_transactions (user_id, type, amount, balance_before, balance_after, reference_id, remark)
			 VALUES ($1, 'margin_release', $2, 0, 0, $3, $4)`,
			p.UserID, p.Margin, p.OrderNo, reason+"_释放保证金")

		if pnl != 0 {
			txnType := "pnl_credit"
			if pnl < 0 {
				txnType = "pnl_debit"
			}
			tx.Exec(context.Background(),
				`INSERT INTO wallet_transactions (user_id, type, amount, balance_before, balance_after, reference_id, remark)
				 VALUES ($1, $2, $3, 0, 0, $4, $5)`,
				p.UserID, txnType, pnl, p.OrderNo, reason+"_自动平仓盈亏")
		}

		tx.Commit(context.Background())

		triggered = append(triggered, gin.H{
			"position_id":   p.ID,
			"user_id":       p.UserID,
			"order_no":      p.OrderNo,
			"symbol":        p.Symbol,
			"direction":     p.Direction,
			"close_price":   currentPrice,
			"realized_pnl":  math.Round(pnl*100) / 100,
			"reason":        reason,
		})

		log.Printf("[SLTP] Position #%d %s %s triggered @ %.2f, pnl=%.2f",
			p.ID, directionName(p.Direction), reason, currentPrice, math.Round(pnl*100)/100)
	}

	return triggered
}

// UpdateOrderSLTP allows modifying stop-loss/take-profit on an open position.
func (s *TradeService) UpdateOrderSLTP(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req struct {
		PositionID int64    `json:"position_id" binding:"required"`
		StopLoss   *float64 `json:"stop_loss"`
		TakeProfit *float64 `json:"take_profit"`
	}
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	if req.StopLoss == nil && req.TakeProfit == nil {
		common.Error(c, errs.InvalidParam, "at least one of stop_loss or take_profit required")
		return
	}
	if req.StopLoss != nil && req.TakeProfit != nil && *req.StopLoss >= *req.TakeProfit {
		common.Error(c, errs.InvalidParam, "stop loss must be below take profit")
		return
	}

	if s.isMemoryMode() {
		pos := s.mem.GetPositionByID(req.PositionID)
		if pos == nil || pos.UserID != userID || pos.Status != 1 {
			common.Error(c, errs.PositionNotFound, "position not found")
			return
		}
		if req.StopLoss != nil {
			pos.StopLoss = req.StopLoss
		}
		if req.TakeProfit != nil {
			pos.TakeProfit = req.TakeProfit
		}
		pos.UpdatedAt = time.Now()
		s.mem.UpdatePosition(pos)
		common.Success(c, gin.H{"position_id": req.PositionID, "stop_loss": pos.StopLoss, "take_profit": pos.TakeProfit})
		return
	}

	result, err := s.pg.Pool.Exec(context.Background(),
		`UPDATE positions SET
			stop_loss = COALESCE($1, stop_loss),
			take_profit = COALESCE($2, take_profit),
			updated_at = NOW()
		 WHERE id=$3 AND user_id=$4 AND status=1`,
		req.StopLoss, req.TakeProfit, req.PositionID, userID)
	if err != nil {
		common.Error(c, errs.Internal, "update failed")
		return
	}
	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		common.Error(c, errs.PositionNotFound, "position not found")
		return
	}
	common.Success(c, gin.H{"position_id": req.PositionID, "stop_loss": req.StopLoss, "take_profit": req.TakeProfit})
}

// ========== Helpers ==========

func directionName(d int) string {
	if d == 1 { return "做多" }
	return "做空"
}

func orderTypeName(t int) string {
	switch t {
	case 1: return "市价"
	case 2: return "限价"
	case 3: return "止损价"
	default: return fmt.Sprintf("类型%d", t)
	}
}
