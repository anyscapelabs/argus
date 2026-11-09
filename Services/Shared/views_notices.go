package Shared

type NoticesResult struct {
	Notices []PermissionNoticeView `json:"notices"`
	Error   *ActionError           `json:"error,omitempty"`
}
