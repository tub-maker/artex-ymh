package db

// Web Push 订阅存储。一条记录对应一台设备的一个浏览器订阅；
// endpoint 由浏览器下发且全局唯一，重复订阅走 upsert。

// PushSubscription 是一条推送目标。
type PushSubscription struct {
	Endpoint string
	P256dh   string
	Auth     string
	UA       string
}

// SavePushSubscription 新增或更新一条订阅（同一 endpoint 覆盖）。
func (d *DB) SavePushSubscription(sub PushSubscription) error {
	_, err := d.Exec(`
INSERT INTO push_subscriptions(endpoint, p256dh, auth, ua)
VALUES ($1, $2, $3, $4)
ON CONFLICT (endpoint) DO UPDATE
SET p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth, ua = EXCLUDED.ua`,
		sub.Endpoint, sub.P256dh, sub.Auth, sub.UA)
	return err
}

// ListPushSubscriptions 返回全部订阅，供群发使用。
func (d *DB) ListPushSubscriptions() ([]PushSubscription, error) {
	rows, err := d.Query(`SELECT endpoint, p256dh, auth, ua FROM push_subscriptions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PushSubscription
	for rows.Next() {
		var s PushSubscription
		if err := rows.Scan(&s.Endpoint, &s.P256dh, &s.Auth, &s.UA); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeletePushSubscription 删除失效订阅（推送返回 404/410 时调用）。
func (d *DB) DeletePushSubscription(endpoint string) error {
	_, err := d.Exec(`DELETE FROM push_subscriptions WHERE endpoint=$1`, endpoint)
	return err
}

// TouchPushSubscription 记录一次成功投递时间，便于排查"订阅还在但收不到"。
func (d *DB) TouchPushSubscription(endpoint string) error {
	_, err := d.Exec(`UPDATE push_subscriptions SET last_ok_at = now() WHERE endpoint=$1`, endpoint)
	return err
}

// CountPushSubscriptions 返回订阅数量（前端用来显示"已开启/未开启"）。
func (d *DB) CountPushSubscriptions() (int, error) {
	var n int
	err := d.QueryRow(`SELECT count(*) FROM push_subscriptions`).Scan(&n)
	return n, err
}
