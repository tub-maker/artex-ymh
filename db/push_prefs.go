package db

// 推送事件偏好的存储。
//
// 只存「用户显式改过」的项；没记录的事件按代码里的默认值走，
// 这样新增事件时不需要给所有用户补行。

// GetPushPreferences 返回所有显式设置的偏好。
func (d *DB) GetPushPreferences() (map[string]bool, error) {
	rows, err := d.Query(`SELECT event_key, enabled FROM push_preferences`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var key string
		var enabled bool
		if err := rows.Scan(&key, &enabled); err != nil {
			return nil, err
		}
		out[key] = enabled
	}
	return out, rows.Err()
}

// SetPushPreference 写入单个事件的开关。
func (d *DB) SetPushPreference(eventKey string, enabled bool) error {
	_, err := d.Exec(`
INSERT INTO push_preferences(event_key, enabled)
VALUES ($1, $2)
ON CONFLICT (event_key) DO UPDATE
SET enabled = EXCLUDED.enabled, updated_at = now()`,
		eventKey, enabled)
	return err
}

// SetPushPreferences 批量写入（一个事务）。
func (d *DB) SetPushPreferences(prefs map[string]bool) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for key, enabled := range prefs {
		if _, err := tx.Exec(`
INSERT INTO push_preferences(event_key, enabled)
VALUES ($1, $2)
ON CONFLICT (event_key) DO UPDATE
SET enabled = EXCLUDED.enabled, updated_at = now()`, key, enabled); err != nil {
			return err
		}
	}
	return tx.Commit()
}
