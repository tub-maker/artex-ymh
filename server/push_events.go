package server

import (
	"net/http"
	"sync"
	"time"
)

// 推送事件总线。
//
// 之前的问题：App 里 15 个事件只是标签，后端没有任何按事件触发的代码，
// 用户勾了也收不到。这里补上「事件 → 偏好判断 → 推送」这条链。

// pushEventDefaults 是各事件的默认开关。
//
// 必须与 App 的 PushEvent 枚举保持一致（lib/core/push/push_events.dart），
// 否则用户看到的勾选状态和实际行为会对不上。
var pushEventDefaults = map[string]bool{
	"task.done":          true,
	"task.failed":        true,
	"task.timeout":       true,
	"task.paused":        false,
	"task.started":       false,
	"finding.critical":   true,
	"finding.high":       true,
	"finding.medium":     false,
	"finding.low":        false,
	"task.scope_request": true,
	"agent.blocked":      true,
	"asset.new":          false,
	"tunnel.down":        true,
	"tunnel.up":          false,
	"credential.new":     false,
}

// 偏好缓存：事件触发频率不高，但避免每次推送都查一次库。
var (
	pushPrefMu     sync.RWMutex
	pushPrefCache  map[string]bool
	pushPrefCached time.Time
)

const pushPrefTTL = 30 * time.Second

// pushEventEnabled 判断某个事件是否开启。
//
// 优先级：数据库里的显式设置 > 代码默认值。
// 存储不可用时**按默认值放行** —— 推送是附加能力，不该因为读不到偏好就静默失效。
func (s *Server) pushEventEnabled(eventKey string) bool {
	def, known := pushEventDefaults[eventKey]
	if !known {
		// 未登记的事件一律不发，避免拼错 key 时静默变成「全都推」
		return false
	}
	if s == nil || s.m == nil || s.m.pg == nil {
		return def
	}

	pushPrefMu.RLock()
	if pushPrefCache != nil && time.Since(pushPrefCached) < pushPrefTTL {
		if v, ok := pushPrefCache[eventKey]; ok {
			pushPrefMu.RUnlock()
			return v
		}
		pushPrefMu.RUnlock()
		return def
	}
	pushPrefMu.RUnlock()

	stored, err := s.m.pg.GetPushPreferences()
	if err != nil {
		return def
	}
	pushPrefMu.Lock()
	pushPrefCache = stored
	pushPrefCached = time.Now()
	pushPrefMu.Unlock()

	if v, ok := stored[eventKey]; ok {
		return v
	}
	return def
}

// notifyEvent 是事件推送的统一入口：先查偏好，再真正发。
func (s *Server) notifyEvent(eventKey, title, body, url string) {
	if s == nil {
		return
	}
	if !s.pushEventEnabled(eventKey) {
		return
	}
	s.NotifyPush(title, body, url)
}

// invalidatePushPrefCache 在偏好被修改后清缓存，让新设置立即生效。
func invalidatePushPrefCache() {
	pushPrefMu.Lock()
	pushPrefCache = nil
	pushPrefCached = time.Time{}
	pushPrefMu.Unlock()
}

// GET /api/push/preferences — 返回生效中的偏好（默认值 + 用户设置合并后）。
func (s *Server) pushPreferencesGet(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}

	effective := make(map[string]bool, len(pushEventDefaults))
	for k, v := range pushEventDefaults {
		effective[k] = v
	}
	stored, err := pg.GetPushPreferences()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	for k, v := range stored {
		if _, ok := pushEventDefaults[k]; ok {
			effective[k] = v
		}
	}

	writeJSON(w, 200, map[string]any{
		"preferences": effective,
		"defaults":    pushEventDefaults,
	})
}

// PUT /api/push/preferences — 保存用户勾选。
//
// 只接受已登记的事件键：拼错的键直接丢弃，
// 免得数据库里堆一堆永远不会生效的行。
func (s *Server) pushPreferencesPut(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}

	var req struct {
		Preferences map[string]bool `json:"preferences"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}

	filtered := make(map[string]bool, len(req.Preferences))
	ignored := 0
	for k, v := range req.Preferences {
		if _, ok := pushEventDefaults[k]; ok {
			filtered[k] = v
		} else {
			ignored++
		}
	}
	if len(filtered) == 0 {
		writeErr(w, 400, "没有可识别的事件键")
		return
	}

	if err := pg.SetPushPreferences(filtered); err != nil {
		writeErr(w, 500, "保存失败: "+err.Error())
		return
	}
	invalidatePushPrefCache()

	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"updated": len(filtered),
		"ignored": ignored,
	})
}
