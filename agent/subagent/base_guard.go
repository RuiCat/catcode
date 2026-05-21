package subagent

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"catcode/core/utils"
	"catcode/data/embed"
)

const guardCacheMaxSize = 100

// guardLRUEntry LRU 缓存链表节点
type guardLRUEntry struct {
	key   string
	value guardReviewResult
}

// guardLRUCache 基于双向链表的 LRU 缓存，用于 guard 审查结果
type guardLRUCache struct {
	mu      sync.Mutex
	maxSize int
	keys    *list.List
	entries map[string]*list.Element
}

// newGuardLRUCache 创建 LRU 缓存
func newGuardLRUCache(maxSize int) *guardLRUCache {
	return &guardLRUCache{
		maxSize: maxSize,
		keys:    list.New(),
		entries: make(map[string]*list.Element),
	}
}

// get 获取缓存值，命中时将其移到链表头部（最近使用）
func (c *guardLRUCache) get(key string) (guardReviewResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.entries[key]; ok {
		c.keys.MoveToFront(elem)
		return elem.Value.(*guardLRUEntry).value, true
	}
	return guardReviewResult{}, false
}

// set 设置缓存值，若超过 maxSize 则淘汰最旧条目
func (c *guardLRUCache) set(key string, value guardReviewResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.entries[key]; ok {
		elem.Value.(*guardLRUEntry).value = value
		c.keys.MoveToFront(elem)
		return
	}
	entry := &guardLRUEntry{key: key, value: value}
	elem := c.keys.PushFront(entry)
	c.entries[key] = elem
	if c.keys.Len() > c.maxSize {
		oldest := c.keys.Back()
		if oldest != nil {
			c.keys.Remove(oldest)
			delete(c.entries, oldest.Value.(*guardLRUEntry).key)
		}
	}
}

// guardReviewResult guard 审查结果
type guardReviewResult struct {
	approved bool
	reason   string
}

// reviewWithGuard 通过 guard 子智能体审查 bash 命令
func (sa *BaseAgent) reviewWithGuard(ctx context.Context, command string) guardReviewResult {
	hash := sha256.Sum256([]byte(strings.TrimSpace(command)))
	cacheKey := hex.EncodeToString(hash[:16])

	if cached, ok := sa.guardCache.get(cacheKey); ok {
		return cached
	}

	reviewCmd := command
	if len(reviewCmd) > 1000 {
		reviewCmd = reviewCmd[:1000]
	}

	var taskDesc string
	if tmpl, err := embed.GetPrompt("guard_review"); err == nil {
		taskDesc = fmt.Sprintf(tmpl, reviewCmd)
	} else {
		taskDesc = fmt.Sprintf("审查命令安全性: %s", reviewCmd)
	}

	contextSummary := fmt.Sprintf("[任务上下文] 当前子智能体: %s, 当前任务: %s", sa.agentType, sa.task)

	guardCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	ch, err := sa.guardReviewer.Execute(guardCtx, "guard", taskDesc, contextSummary)
	if err != nil {
		result := guardReviewResult{approved: false, reason: "guard 子智能体不可用，拒绝执行。请通过 /guard-review 命令手动审查"}
		sa.guardCache.set(cacheKey, result)
		return result
	}

	var resultText strings.Builder
	for text := range ch {
		resultText.WriteString(text)
	}

	rawOutput := resultText.String()

	var result guardReviewResult
	lowerOutput := strings.ToLower(rawOutput)
	if strings.Contains(lowerOutput, `"level":"critical"`) ||
		strings.Contains(lowerOutput, `"level":"high"`) ||
		strings.Contains(lowerOutput, `"approved":false`) ||
		strings.Contains(lowerOutput, `"safe":false`) {
		result = guardReviewResult{approved: false, reason: "guard 判定为高风险: " + utils.TruncateStr(rawOutput, 200)}
	} else {
		result = guardReviewResult{approved: true, reason: "guard 审查通过"}
	}

	sa.guardCache.set(cacheKey, result)

	if inst, err := sa.guardReviewer.GetOrCreate("guard"); err == nil {
		inst.GetSession().Clear()
	}

	return result
}
