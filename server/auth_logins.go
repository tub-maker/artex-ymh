package server

import (
	"log"
	"net/http"
	"strconv"
	"time"
)

// 登录记录：让「谁在什么时候、从哪个 IP 登录过」可查。
//
// 动机：异地登录或陌生 IP 爆破失败，事后必须能看出来 ——
// 否则"被别人登了也不知道"。

// recordAuthLogin 记录一次登录尝试。
//
// 故意不返回错误给调用方：审计是附加能力，
// 记不下来也不能把用户挡在门外。失败只写日志。
func (s *Server) recordAuthLogin(r *http.Request, ok bool, reason string) {
	if s == nil || s.m == nil || s.m.pg == nil {
		return
	}
	ip := clientIP(r)
	ua := r.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	if err := s.m.pg.RecordAuthLogin(ip, ua, ok, reason); err != nil {
		// 不阻断登录，只在服务端日志留一行
		log.Printf("[auth] 登录记录写入失败: %v", err)
	}
}

// GET /api/auth/logins?limit=50 — 最近登录记录。
//
// 该路径在 /api/auth/* 下，requireAuth 不覆盖，所以这里自己校验 token。
func (s *Server) authLoginHistory(w http.ResponseWriter, r *http.Request) {
	if !s.validAccessToken(extractToken(r)) {
		writeErr(w, 401, "未授权")
		return
	}
	pg := s.pg(w)
	if pg == nil {
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	items, err := pg.ListAuthLogins(limit)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"id":     it.ID,
			"at":     it.At.UTC().Format(time.RFC3339),
			"ip":     it.IP,
			"ua":     it.UA,
			"ok":     it.OK,
			"reason": it.Reason,
		})
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
