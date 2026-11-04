package album

import "github.com/ivan223/web1-personal-site/server/internal/logging"

// Имена событий уезжают в базу: суффиксы править нельзя, только добавлять.
var reg = logging.NewRegistrar("album")

var (
	EventPhotoUploaded     = reg.Register("photo.uploaded", "загрузка кадра")
	EventPhotoDeleted      = reg.Register("photo.deleted", "удаление кадра")
	EventPhotoDeleteDenied = reg.Register("photo.delete_denied", "отказ в удалении чужого кадра")
)
