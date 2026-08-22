package main

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// parsePagination reads page (1-based) and page_size from query, clamps to sane bounds.
// Default: page=1, page_size=20, max page_size=100.
func parsePagination(c *gin.Context) (page, pageSize int) {
	page = 1
	pageSize = 20
	if v := c.Query("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := c.Query("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if pageSize < 1 {
		pageSize = 1
	}
	return page, pageSize
}
