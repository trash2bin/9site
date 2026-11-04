package leads

import "github.com/ivan223/web1-personal-site/server/internal/logging"

// Домен lead закреплён в базе раньше каталога среза, чужой namespace.
var reg = logging.NewForeignRegistrar("lead")

var (
	EventLeadSubmitted    = reg.Register("submitted", "заявка с формы")
	EventLeadSubmitFailed = reg.Register("submit_failed", "заявка отклонена")
)
