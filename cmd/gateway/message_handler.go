package main

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goldarena/goldarena/internal/common"
	"github.com/goldarena/goldarena/pkg/errs"
)

// MessageService handles 1:1 conversations between a user and the platform.
// Messages are simple HTTP-based; either side can send. Admin replies are sent
// as "platform".
type MessageService struct {
	mem     *common.MemoryStore
	userSvc *UserService
}

func NewMessageService(mem *common.MemoryStore, userSvc *UserService) *MessageService {
	return &MessageService{mem: mem, userSvc: userSvc}
}

type sendMessageReq struct {
	Content string `json:"content" binding:"required"`
}

// ListMyMessages returns the current user's full conversation with the
// platform, and marks any unread "platform" messages as read.
func (s *MessageService) ListMyMessages(c *gin.Context) {
	userID := c.GetInt64("user_id")
	s.mem.MarkMessagesRead(userID, "user")
	common.Success(c, s.mem.GetMessages(userID))
}

// MyUnreadCount returns how many platform messages the current user has not
// read yet (for navbar badges). Does NOT mark anything read.
func (s *MessageService) MyUnreadCount(c *gin.Context) {
	userID := c.GetInt64("user_id")
	count := 0
	for _, msg := range s.mem.GetMessages(userID) {
		if msg.Sender == "platform" && !msg.Read {
			count++
		}
	}
	common.Success(c, gin.H{"unread": count})
}

// SendMessage lets the current user leave a message for the platform.
func (s *MessageService) SendMessage(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req sendMessageReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	if len(req.Content) > 2000 {
		common.Error(c, errs.InvalidParam, "内容不能超过 2000 字")
		return
	}
	msg := &common.Message{
		UserID:  userID,
		Sender:  "user",
		Content: req.Content,
		Read:    false,
		CreatedAt: time.Now(),
	}
	s.mem.SaveMessage(msg)
	common.Success(c, msg)
}

// ListConversations returns, for admins, all users who have ever sent a
// message to the platform, with their latest message and unread counts.
func (s *MessageService) ListConversations(c *gin.Context) {
	unread := s.mem.GetUnreadMessageCounts()
	uids := s.mem.GetMessageConversationUserIDs()
	items := make([]gin.H, 0, len(uids))
	for _, uid := range uids {
		user := s.mem.GetUserByID(uid)
		name := ""
		if user != nil {
			name = user.Username
		}
		msgs := s.mem.GetMessages(uid)
		var lastContent string
		var lastAt time.Time
		if len(msgs) > 0 {
			last := msgs[len(msgs)-1]
			lastContent = last.Content
			lastAt = last.CreatedAt
		}
		items = append(items, gin.H{
			"user_id":      uid,
			"username":     name,
			"last_content": lastContent,
			"last_at":      lastAt,
			"unread":       unread[uid],
		})
	}
	common.Success(c, items)
}

// ListUserMessages returns the full thread for a specific user. Admin only.
func (s *MessageService) ListUserMessages(c *gin.Context) {
	uid := parseIDParam(c, "user_id")
	if uid == 0 {
		common.Error(c, errs.InvalidParam, "user_id required")
		return
	}
	if s.mem.GetUserByID(uid) == nil {
		common.Error(c, errs.NotFound, "用户不存在")
		return
	}
	s.mem.MarkMessagesRead(uid, "platform")
	common.Success(c, s.mem.GetMessages(uid))
}

// ReplyAsPlatform lets an admin send a message to a user.
func (s *MessageService) ReplyAsPlatform(c *gin.Context) {
	uid := parseIDParam(c, "user_id")
	if uid == 0 {
		common.Error(c, errs.InvalidParam, "user_id required")
		return
	}
	if s.mem.GetUserByID(uid) == nil {
		common.Error(c, errs.NotFound, "用户不存在")
		return
	}
	var req sendMessageReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	if len(req.Content) > 2000 {
		common.Error(c, errs.InvalidParam, "内容不能超过 2000 字")
		return
	}
	msg := &common.Message{
		UserID:    uid,
		Sender:    "platform",
		Content:   req.Content,
		Read:      false,
		CreatedAt: time.Now(),
	}
	s.mem.SaveMessage(msg)
	common.Success(c, msg)
}

func parseIDParam(c *gin.Context, key string) int64 {
	var id int64
	if _, err := fmt.Sscanf(c.Param(key), "%d", &id); err != nil {
		return 0
	}
	return id
}
