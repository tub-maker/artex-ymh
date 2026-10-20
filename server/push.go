package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/Autumn-27/artex/db"
)

// Web Push 通知（移动端）。
//
// 流程：
//   1. 前端 GET /api/push/vapid 拿公钥
//   2. 浏览器 pushManager.subscribe() 生成订阅
//   3. 前端 POST /api/push/subscribe 上报 endpoint + 两个加密密钥
//   4. 后端在任务终态 / 高危发现时群发，遇到 404/410 自动清理失效订阅
//
// VAPID 密钥首次使用时生成并写进 settings 表，重启不丢。

const (
	pushVAPIDPrivSetting = "push_vapid_private_v1"
	pushVAPIDPubSetting  = "push_vapid_public_v1"
)

// pushNotifier 是给 Manager 用的包级回调：任务终态时触发推送。
// 由 Server 构造时注入；nil 表示推送未启用（数据库不可用）。
var pushNotifier func(title, body, url string)

// pushKeys 返回 VAPID 密钥对，首次调用时生成并持久化。
func (s *Server) pushKeys() (priv, pub string, err error) {
	d := s.m.pg
	if d == nil {
		return "", "", errors.New("数据库未连接")
	}
	priv, okPriv, err := d.GetSetting(pushVAPIDPrivSetting)
	if err != nil {
		return "", "", err
	}
	pub, okPub, err := d.GetSetting(pushVAPIDPubSetting)
	if err != nil {
		return "", "", err
	}
	if okPriv && okPub && priv != "" && pub != "" {
		return priv, pub, nil
	}

	newPriv, newPub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", err
	}
	if err := d.SetSetting(pushVAPIDPrivSetting, newPriv); err != nil {
		return "", "", err
	}
	if err := d.SetSetting(pushVAPIDPubSetting, newPub); err != nil {
		return "", "", err
	}
	log.Printf("[push] 已生成 VAPID 密钥对")
	return newPriv, newPub, nil
}

// GET /api/push/vapid — 前端订阅前需要公钥。
func (s *Server) pushVAPID(w http.ResponseWriter, r *http.Request) {
	_, pub, err := s.pushKeys()
	if err != nil {
		writeErr(w, 500, "推送密钥不可用: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"public_key": pub})
}

// POST /api/push/subscribe — 上报一条浏览器订阅。
func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	d := s.pg(w)
	if d == nil {
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	if req.Endpoint == "" || req.Keys.P256dh == "" || req.Keys.Auth == "" {
		writeErr(w, 400, "endpoint / keys 不能为空")
		return
	}
	sub := db.PushSubscription{
		Endpoint: req.Endpoint,
		P256dh:   req.Keys.P256dh,
		Auth:     req.Keys.Auth,
		UA:       truncateStr(r.UserAgent(), 200),
	}
	if err := d.SavePushSubscription(sub); err != nil {
		writeErr(w, 500, "保存订阅失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/push/unsubscribe — 用户主动关闭通知时调用。
func (s *Server) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	d := s.pg(w)
	if d == nil {
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := decode(r, &req); err != nil || strings.TrimSpace(req.Endpoint) == "" {
		writeErr(w, 400, "endpoint 不能为空")
		return
	}
	if err := d.DeletePushSubscription(req.Endpoint); err != nil {
		writeErr(w, 500, "删除订阅失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/push/test — 发一条测试通知，用于验证链路。
func (s *Server) pushTest(w http.ResponseWriter, r *http.Request) {
	d := s.pg(w)
	if d == nil {
		return
	}
	n, err := d.CountPushSubscriptions()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if n == 0 {
		writeErr(w, 400, "还没有订阅，请先在设置里开启通知")
		return
	}
	s.NotifyPush("ARTEX 测试通知", "看到这条说明推送链路是通的", "/m/")
	writeJSON(w, 200, map[string]any{"ok": true, "sent_to": n})
}

// GET /api/push/status — 前端用来显示"已开启/未开启"。
func (s *Server) pushStatus(w http.ResponseWriter, r *http.Request) {
	d := s.pg(w)
	if d == nil {
		return
	}
	n, err := d.CountPushSubscriptions()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"count": n})
}

// NotifyPush 向所有订阅群发一条通知。失败静默，绝不影响主流程。
func (s *Server) NotifyPush(title, body, url string) {
	if s == nil || s.m == nil || s.m.pg == nil {
		return
	}
	priv, pub, err := s.pushKeys()
	if err != nil {
		return
	}
	subs, err := s.m.pg.ListPushSubscriptions()
	if err != nil || len(subs) == 0 {
		return
	}

	payload, err := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
		"url":   url,
	})
	if err != nil {
		return
	}

	for _, sub := range subs {
		go func(sub db.PushSubscription) {
			resp, err := webpush.SendNotification(payload, &webpush.Subscription{
				Endpoint: sub.Endpoint,
				Keys:     webpush.Keys{Auth: sub.Auth, P256dh: sub.P256dh},
			}, &webpush.Options{
				Subscriber:      "mailto:admin@artex.local",
				VAPIDPublicKey:  pub,
				VAPIDPrivateKey: priv,
				TTL:             86400,
				Urgency:         webpush.UrgencyNormal,
			})
			if err != nil {
				return
			}
			defer resp.Body.Close()
			switch {
			case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
				// 订阅已失效（用户卸载/清缓存），清掉
				_ = s.m.pg.DeletePushSubscription(sub.Endpoint)
			case resp.StatusCode >= 200 && resp.StatusCode < 300:
				_ = s.m.pg.TouchPushSubscription(sub.Endpoint)
			}
		}(sub)
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// 保证 time 被使用（TTL 计算留作扩展）
var _ = time.Now
