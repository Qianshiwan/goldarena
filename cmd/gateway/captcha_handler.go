package main

import (
	"github.com/gin-gonic/gin"
	"github.com/goldarena/goldarena/internal/common"
	"github.com/goldarena/goldarena/pkg/errs"
)

type trackPoint struct {
	X int   `json:"x"`
	T int64 `json:"t"`
}

// GetCaptcha returns a fresh slider captcha (key + images + piece Y).
func (s *UserService) GetCaptcha(c *gin.Context) {
	key, bg, thumb, thumbY, err := s.kit.Cap.Generate()
	if err != nil {
		common.Error(c, errs.Internal, "生成验证码失败")
		return
	}
	common.Success(c, gin.H{"key": key, "image": bg, "thumb": thumb, "thumb_y": thumbY})
}

// VerifyCaptcha validates the drag result and, on success, returns a single-use ticket.
func (s *UserService) VerifyCaptcha(c *gin.Context) {
	var req struct {
		Key   string       `json:"key" binding:"required"`
		X     int          `json:"x"`
		Track []trackPoint `json:"track"`
	}
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	if !validTrack(req.Track, req.X) {
		common.Error(c, errs.InvalidParam, "验证行为异常，请重试")
		return
	}
	ticket, ok, reason := s.kit.Cap.Verify(req.Key, req.X)
	if !ok {
		common.Error(c, errs.InvalidParam, reason)
		return
	}
	common.Success(c, gin.H{"ticket": ticket})
}

// validTrack is a light anti-bot check on the drag trajectory: it must take a
// human-like amount of time, contain enough samples, be temporally ordered, and
// end exactly where the client claims it did.
func validTrack(track []trackPoint, finalX int) bool {
	if len(track) < 5 {
		return false
	}
	first, last := track[0], track[len(track)-1]
	dur := last.T - first.T
	if dur < 200 || dur > 30000 {
		return false
	}
	if last.X != finalX {
		return false
	}
	for i := 1; i < len(track); i++ {
		if track[i].T < track[i-1].T {
			return false
		}
	}
	return true
}
