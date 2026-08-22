package main

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goldarena/goldarena/internal/common"
	"github.com/goldarena/goldarena/pkg/db"
	"github.com/goldarena/goldarena/pkg/errs"
	"github.com/goldarena/goldarena/pkg/redis"
)

// CultivationService handles cultivation level (修仙等级) operations
type CultivationService struct {
	pg  *db.Postgres
	rdb *redis.Redis
	mem *common.MemoryStore
}

func NewCultivationService(pg *db.Postgres, rdb *redis.Redis, mem *common.MemoryStore) *CultivationService {
	return &CultivationService{pg: pg, rdb: rdb, mem: mem}
}

func (s *CultivationService) isMemoryMode() bool {
	return s.pg == nil || s.pg.Pool == nil
}

// GetCultivationProgress returns the user's current cultivation level and progress
func (s *CultivationService) GetCultivationProgress(c *gin.Context) {
	userID := c.GetInt64("user_id")

	// Calculate trade stats (live)
	stats := s.calculateTradeStats(userID)

	// 灵气值实时由交易业绩计算, 境界由灵气值推导
	exp := common.CalcSpiritEnergy(stats)

	// Get full progress
	progress := common.GetCultivationProgress(exp, stats)

	common.Success(c, progress)
}

// GetAllLevels returns all cultivation level definitions
func (s *CultivationService) GetAllLevels(c *gin.Context) {
	levels := common.AllCultivationLevels()
	common.Success(c, levels)
}

// GetCultivationRank returns the cultivation ranking of all users
func (s *CultivationService) GetCultivationRank(c *gin.Context) {
	// Always try memory mode first (works in dev mode and as DB fallback)
	if s.isMemoryMode() {
		users := s.getAllUsersCultivation()
		common.Success(c, users)
		return
	}

	// DB mode: query top users by cultivation level + spirit energy
	rows, err := s.pg.Pool.Query(context.Background(),
		`SELECT id, username, nickname, avatar, cultivation_level, spirit_energy
		 FROM users
		 WHERE status = 1
		 ORDER BY cultivation_level DESC, spirit_energy DESC
		 LIMIT 100`)
	if err != nil {
		// Fallback to memory mode on DB error
		users := s.getAllUsersCultivation()
		common.Success(c, users)
		return
	}
	defer rows.Close()

	type RankUser struct {
		Rank            int    `json:"rank"`
		UserID          int64  `json:"user_id"`
		Username        string `json:"username"`
		Nickname        string `json:"nickname"`
		Avatar          string `json:"avatar"`
		CultivationLevel int   `json:"cultivation_level"`
		SpiritEnergy    int64  `json:"spirit_energy"`
		LevelName       string `json:"level_name"`
		LevelTitle      string `json:"level_title"`
		LevelColor      string `json:"level_color"`
		Realm           string `json:"realm"`
	}

	var result []RankUser
	rank := 0
	for rows.Next() {
		rank++
		var u RankUser
		var level int
		var energy int64
		err := rows.Scan(&u.UserID, &u.Username, &u.Nickname, &u.Avatar, &level, &energy)
		if err != nil {
			continue
		}
		u.Rank = rank
		u.CultivationLevel = level
		u.SpiritEnergy = energy
		info := common.GetCultivationInfo(common.CultivationLevel(level))
		u.LevelName = info.Name
		u.LevelTitle = info.Title
		u.LevelColor = info.Color
		u.Realm = string(info.Realm)
		result = append(result, u)
	}

	common.Success(c, result)
}

// Breakthrough attempts to advance to the next cultivation level
func (s *CultivationService) Breakthrough(c *gin.Context) {
	userID := c.GetInt64("user_id")

	// 灵气值实时计算, 境界由灵气值推导
	stats := s.calculateTradeStats(userID)
	exp := common.CalcSpiritEnergy(stats)
	expLevel := common.GetCultivationByExp(exp).Level

	// Check if breakthrough is possible
	canBreak, reqs := common.CheckBreakthrough(expLevel, exp, stats)

	if !canBreak {
		common.Error(c, errs.InvalidParam, "breakthrough conditions not met")
		return
	}
	_ = reqs // reqs available for logging/debugging

	// Perform breakthrough
	newLevel := int(expLevel) + 1
	if newLevel > 10 {
		common.Error(c, errs.InvalidParam, "already at max level")
		return
	}

	s.updateUserCultivation(userID, newLevel, exp)

	info := common.GetCultivationInfo(common.CultivationLevel(newLevel))
	common.Success(c, gin.H{
		"success":     true,
		"new_level":   newLevel,
		"level_name":  info.Name,
		"title":       info.Title,
		"realm":       info.Realm,
		"message":     fmt.Sprintf("突破成功！晋升至%s · %s", info.Name, info.Title),
		"features":    info.Features,
	})
}

// getUserCultivation returns (level, spiritEnergy) for a user
func (s *CultivationService) getUserCultivation(userID int64) (int, int64) {
	if s.isMemoryMode() {
		user := s.mem.GetUserByID(userID)
		if user == nil {
			return 1, 0
		}
		if user.CultivationLevel < 1 {
			return 1, 0
		}
		return user.CultivationLevel, user.SpiritEnergy
	}

	var level int
	var energy int64
	err := s.pg.Pool.QueryRow(context.Background(),
		`SELECT cultivation_level, spirit_energy FROM users WHERE id=$1`, userID).Scan(&level, &energy)
	if err != nil {
		return 1, 0
	}
	if level < 1 {
		level = 1
	}
	return level, energy
}

// updateUserCultivation updates user's cultivation level
func (s *CultivationService) updateUserCultivation(userID int64, level int, energy int64) {
	if s.isMemoryMode() {
		s.mem.UpdateUserCultivation(userID, level, energy)
		return
	}

	s.pg.Pool.Exec(context.Background(),
		`UPDATE users SET cultivation_level=$1, spirit_energy=$2, updated_at=NOW() WHERE id=$3`,
		level, energy, userID)
}

// calculateTradeStats computes trade statistics for cultivation progress
func (s *CultivationService) calculateTradeStats(userID int64) common.TradeStats {
	if s.isMemoryMode() {
		return s.calcStatsFromMemory(userID)
	}
	return s.calcStatsFromDB(userID)
}

func (s *CultivationService) calcStatsFromMemory(userID int64) common.TradeStats {
	closedPositions := s.mem.GetClosedPositions(userID)

	stats := common.TradeStats{
		TotalTrades: len(closedPositions),
	}

	if len(closedPositions) == 0 {
		return stats
	}

	totalPnL := 0.0
	winning := 0
	maxStreak := 0
	currentStreak := 0
	totalVolume := 0.0
	totalHoldMinutes := 0.0

	for _, p := range closedPositions {
		totalPnL += p.FloatingPnL
		totalVolume += p.Volume

		if p.ClosedAt != nil && !p.ClosedAt.IsZero() {
			holdMin := p.ClosedAt.Sub(p.CreatedAt).Minutes()
			if holdMin > 0 {
				totalHoldMinutes += holdMin
			}
		}

		if p.FloatingPnL > 0 {
			winning++
			currentStreak++
			if currentStreak > maxStreak {
				maxStreak = currentStreak
			}
		} else {
			currentStreak = 0
		}
	}

	stats.WinningTrades = winning
	stats.LosingTrades = len(closedPositions) - winning
	if len(closedPositions) > 0 {
		stats.WinRate = float64(winning) / float64(len(closedPositions)) * 100
	}
	stats.TotalPnL = totalPnL
	stats.MaxWinStreak = maxStreak
	stats.TotalVolume = totalVolume
	stats.AvgHoldMinutes = totalHoldMinutes / float64(len(closedPositions))

	// Calculate return rate based on initial capital (10000 GC default)
	initialCapital := 10000.0
	wallet := s.mem.GetWallet(userID)
	if wallet != nil && wallet.TotalRecharged > 0 {
		initialCapital = wallet.TotalRecharged
	}
	if initialCapital > 0 {
		stats.ReturnRate = (totalPnL / initialCapital) * 100
	}

	return stats
}

func (s *CultivationService) calcStatsFromDB(userID int64) common.TradeStats {
	stats := common.TradeStats{}

	rows, err := s.pg.Pool.Query(context.Background(),
		`SELECT floating_pnl, volume, created_at, COALESCE(closed_at, created_at)
		 FROM positions
		 WHERE user_id=$1 AND status=2
		 ORDER BY created_at ASC`, userID)
	if err != nil {
		return stats
	}
	defer rows.Close()

	type closedPos struct {
		pnl      float64
		volume   float64
		created  time.Time
		closed   time.Time
	}

	var positions []closedPos
	for rows.Next() {
		var p closedPos
		if err := rows.Scan(&p.pnl, &p.volume, &p.created, &p.closed); err != nil {
			continue
		}
		positions = append(positions, p)
	}

	stats.TotalTrades = len(positions)
	if len(positions) == 0 {
		return stats
	}

	totalPnL := 0.0
	winning := 0
	maxStreak := 0
	currentStreak := 0
	totalVolume := 0.0
	totalHoldMinutes := 0.0

	for _, p := range positions {
		totalPnL += p.pnl
		totalVolume += p.volume
		holdMin := p.closed.Sub(p.created).Minutes()
		if holdMin > 0 {
			totalHoldMinutes += holdMin
		}

		if p.pnl > 0 {
			winning++
			currentStreak++
			if currentStreak > maxStreak {
				maxStreak = currentStreak
			}
		} else {
			currentStreak = 0
		}
	}

	stats.WinningTrades = winning
	stats.LosingTrades = len(positions) - winning
	stats.WinRate = float64(winning) / float64(len(positions)) * 100
	stats.TotalPnL = totalPnL
	stats.MaxWinStreak = maxStreak
	stats.TotalVolume = totalVolume
	stats.AvgHoldMinutes = totalHoldMinutes / float64(len(positions))

	// Get initial capital from total recharged
	var totalRecharged float64
	s.pg.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(total_recharged, 10000) FROM wallets WHERE user_id=$1`, userID).Scan(&totalRecharged)
	if totalRecharged <= 0 {
		totalRecharged = 10000
	}
	stats.ReturnRate = (totalPnL / totalRecharged) * 100

	return stats
}

// RefreshSpiritEnergy recalculates and updates spirit energy based on current stats
func (s *CultivationService) RefreshSpiritEnergy(c *gin.Context) {
	userID := c.GetInt64("user_id")

	stats := s.calculateTradeStats(userID)
	newExp := common.CalcSpiritEnergy(stats)

	// 境界由灵气值推导, 持久化以保证排行榜与展示一致
	expLevel := common.GetCultivationByExp(newExp).Level
	s.updateUserCultivation(userID, int(expLevel), newExp)

	common.Success(c, gin.H{
		"spirit_energy": newExp,
		"level":         int(expLevel),
		"stats":         stats,
	})
}

// getAllUsersCultivation returns all users with their cultivation info (memory mode)
func (s *CultivationService) getAllUsersCultivation() []gin.H {
	allUsers := s.mem.GetAllUsers()

	type userRank struct {
		rank            int
		userID          int64
		username        string
		nickname        string
		avatar          string
		cultivationLevel int
		spiritEnergy    int64
	}

	var users []userRank
	for _, u := range allUsers {
		users = append(users, userRank{
			userID:           u.ID,
			username:         u.Username,
			nickname:         u.Nickname,
			avatar:           u.Avatar,
			cultivationLevel: u.CultivationLevel,
			spiritEnergy:     u.SpiritEnergy,
		})
	}

	// Sort by level desc, then energy desc
	for i := 0; i < len(users); i++ {
		for j := i + 1; j < len(users); j++ {
			if users[j].cultivationLevel > users[i].cultivationLevel ||
				(users[j].cultivationLevel == users[i].cultivationLevel && users[j].spiritEnergy > users[i].spiritEnergy) {
				users[i], users[j] = users[j], users[i]
			}
		}
	}

	result := make([]gin.H, 0, len(users))
	for i, u := range users {
		info := common.GetCultivationInfo(common.CultivationLevel(u.cultivationLevel))
		if u.cultivationLevel < 1 {
			info = common.GetCultivationInfo(1)
		}
		result = append(result, gin.H{
			"rank":             i + 1,
			"user_id":          u.userID,
			"username":         u.username,
			"nickname":         u.nickname,
			"avatar":           u.avatar,
			"cultivation_level": u.cultivationLevel,
			"spirit_energy":    u.spiritEnergy,
			"level_name":       info.Name,
			"level_title":      info.Title,
			"level_color":      info.Color,
			"realm":            info.Realm,
		})
	}

	return result
}

// helper: avoid unused import warning
var _ = math.MaxInt64
var _ = time.Now
var _ = context.Background
