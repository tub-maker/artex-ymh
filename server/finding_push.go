package server

import (
	"context"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/evidence"
)

// pushFindingRecorder 包一层 evidence.Store，在「严重 / 高危」发现写入成功后推送。
//
// 为什么要包一层而不是改 evidence 包：evidence 是纯存储层，不该知道通知机制；
// 推送属于宿主（server）职责。这样存储层保持可测试，通知策略也能单独调整。
type pushFindingRecorder struct {
	s     *Server
	inner *evidence.Store
}

func (r pushFindingRecorder) Record(
	ctx context.Context,
	in db.RecordFindingInput,
	refs []db.TrafficRef,
) (*db.RecordedFinding, error) {
	rec, err := r.inner.Record(ctx, in, refs)
	if err != nil || rec == nil {
		return rec, err
	}

	// 合并进既有发现（同一漏洞重复上报）时不重复通知，否则同一条漏洞会刷屏。
	if rec.Merged {
		return rec, nil
	}

	sev := strings.ToLower(strings.TrimSpace(in.Severity))
	eventKey := ""
	title := ""
	switch sev {
	case "critical":
		eventKey = "finding.critical"
		title = "发现严重漏洞"
	case "high":
		eventKey = "finding.high"
		title = "发现高危漏洞"
	case "medium":
		eventKey = "finding.medium"
		title = "发现中危漏洞"
	case "low":
		eventKey = "finding.low"
		title = "发现低危漏洞"
	default:
		// 未知等级不推送，避免脏数据变成噪音
		return rec, nil
	}

	body := strings.TrimSpace(in.Name)
	if body == "" {
		body = in.VulnClass
	}
	if body == "" {
		body = "任务 #" + itoa(in.TaskID)
	}

	go r.s.notifyEvent(eventKey, title, body, "/m/findings")
	return rec, nil
}

// findingRecorder 返回带推送能力的记录器，供 Agent 注入。
func (s *Server) findingRecorder() pushFindingRecorder {
	return pushFindingRecorder{s: s, inner: s.evidenceStore()}
}

// itoa 小工具，避免为一个整数引入 strconv 到调用处。
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
