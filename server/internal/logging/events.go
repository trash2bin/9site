package logging

// Action is one durable user action copied to the action journal.
type Action struct {
	Login  string
	Event  string
	Target string
	Result string
	IP     string
}

// Stable action event names.
const (
	EventLoginSucceeded       = "auth.login.succeeded"
	EventLoginFailed          = "auth.login.failed"
	EventRegistered           = "auth.registered"
	EventLoggedOut            = "auth.logged_out"
	EventProfileUpdated       = "auth.profile.updated"
	EventPasswordChanged      = "auth.password.changed"
	EventPasswordResetAsked   = "auth.password.reset_requested"
	EventPasswordResetApplied = "auth.password.reset_completed"
	EventAvatarUpdated        = "auth.avatar.updated"
	EventRoleChanged          = "auth.role.changed"
	EventAdminBootstrapped    = "system.admin.bootstrapped"
	EventPhotoUploaded        = "album.photo.uploaded"
	EventPhotoDeleted         = "album.photo.deleted"
	EventPhotoDeleteDenied    = "album.photo.delete_denied"
	EventLeadSubmitted        = "lead.submitted"
	EventLeadSubmitFailed     = "lead.submit_failed"
)

// Stable action results.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
)

// EventNames returns the action event names available to the admin journal.
func EventNames() []string {
	return []string{
		EventAdminBootstrapped,
		EventLoginSucceeded,
		EventLoginFailed,
		EventRegistered,
		EventLoggedOut,
		EventProfileUpdated,
		EventPasswordChanged,
		EventPasswordResetAsked,
		EventPasswordResetApplied,
		EventAvatarUpdated,
		EventRoleChanged,
		EventPhotoUploaded,
		EventPhotoDeleted,
		EventPhotoDeleteDenied,
		EventLeadSubmitted,
		EventLeadSubmitFailed,
	}
}
