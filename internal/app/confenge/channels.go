package confenge

// Channel kinds for evidence-grounded multichannel generation.
const (
	ChannelEmailInitial         = "EMAIL_INITIAL"
	ChannelEmailFollowup        = "EMAIL_FOLLOWUP"
	ChannelWhatsAppInitial      = "WHATSAPP_INITIAL"
	ChannelWhatsAppContinuation = "WHATSAPP_CONTINUATION"
	ChannelReplyDraft           = "REPLY_DRAFT"
)

func IsWhatsAppChannel(ch string) bool {
	return ch == ChannelWhatsAppInitial || ch == ChannelWhatsAppContinuation
}

func IsEmailChannel(ch string) bool {
	return ch == ChannelEmailInitial || ch == ChannelEmailFollowup || ch == ChannelReplyDraft || ch == ""
}

func PersistChannel(kind string) string {
	if IsWhatsAppChannel(kind) {
		return "WHATSAPP"
	}
	return "EMAIL"
}
