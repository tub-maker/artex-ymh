package db

import "time"

// 登录记录存储。
//
// 目的：让「谁在什么时候、从哪个 IP 登录过」可查 ——
// 异地登录、陌生 IP 爆破失败都会留下痕迹，而不是事后毫无线索。

// AuthLogin 是一条登录记录。
type AuthLogin struct {
	ID     int64
	At     time.Time
	IP     string
	UA     string
	OK     bool
	Reason string
}

// authLoginsKeep 保留的记录条数。登录频率很低，200 条足够回溯很久。
const authLoginsKeep = 200

// RecordAuthLogin 记录一次登录尝试（成功或失败）。
//
// 写入失败不影响登录流程 —— 审计是附加能力，不能因为记不下来就把用户挡在门外。
func (d *DB) RecordAuthLogin(ip, ua string, ok bool, reason string) error {
	if _, err := d.Exec(
		`INSERT INTO auth_logins(ip, ua, ok, reason) VALUES ($1, $2, $3, $4)`,
		ip, ua, ok, reason,
	); err != nil {
		return err
	}

	// 裁剪：只留最近 authLoginsKeep 条
	_, err := d.Exec(`
DELETE FROM auth_logins
WHERE id NOT IN (
    SELECT id FROM auth_logins ORDER BY id DESC LIMIT $1
)`, authLoginsKeep)
	return err
}

// ListAuthLogins 返回最近的登录记录（新的在前）。
func (d *DB) ListAuthLogins(limit int) ([]AuthLogin, error) {
	if limit <= 0 || limit > authLoginsKeep {
		limit = 50
	}
	rows, err := d.Query(`
SELECT id, at, ip, ua, ok, reason
FROM auth_logins
ORDER BY id DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuthLogin
	for rows.Next() {
		var a AuthLogin
		if err := rows.Scan(&a.ID, &a.At, &a.IP, &a.UA, &a.OK, &a.Reason); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
