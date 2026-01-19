package handler

import (
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/group"
	"github.com/warmbly/warmbly/internal/app/role"
	"github.com/warmbly/warmbly/internal/app/sequence"
	"github.com/warmbly/warmbly/internal/app/socket"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/tz"
	"github.com/warmbly/warmbly/internal/app/unibox"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/tasks"
)

type Handler struct {
	AuthService     auth.AuthService
	TokenService    token.TokenService
	UserService     user.UserService
	EmailService    email.EmailService
	CampaignService campaign.CampaignService
	ContactService  contact.ContactService
	SequenceService sequence.SequenceService
	RoleService     role.RoleService
	UniboxService   unibox.UniboxService

	FolderService   group.GroupService
	TagService      group.GroupService
	CategoryService group.GroupService

	TzService     tz.TzService
	SocketService socket.SocketService
	TasksService  tasks.TasksService
}
